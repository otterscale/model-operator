/*
Copyright 2026 The OtterScale Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package artifact

import (
	"slices"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// ObservationResult encapsulates the observed state derived from a Job and its Pods.
type ObservationResult struct {
	Phase   modelv1alpha1.ArtifactPhase
	Ready   metav1.ConditionStatus
	Reason  string
	Message string
	Digest  string
}

// ObserveJobStatus derives the ModelArtifact phase, condition, and digest
// from the current state of a Job and its owned Pods.
//
// This is a pure function with no side effects — all observation logic is
// testable without a running cluster.
func ObserveJobStatus(job *batchv1.Job, pods []corev1.Pod) ObservationResult {
	if job == nil {
		return ObservationResult{
			Phase:   modelv1alpha1.PhasePending,
			Ready:   metav1.ConditionFalse,
			Reason:  "JobNotCreated",
			Message: "Waiting for job to be created",
		}
	}

	switch {
	case job.Status.Succeeded > 0:
		digest := extractTerminationMessage(pods)
		return ObservationResult{
			Phase:   modelv1alpha1.PhaseSucceeded,
			Ready:   metav1.ConditionTrue,
			Reason:  "Succeeded",
			Message: "Artifact pushed successfully",
			Digest:  digest,
		}

	case isJobFailed(job):
		msg := extractTerminationMessage(pods)
		if msg == "" {
			msg = failureReasonFromConditions(job)
		}
		return ObservationResult{
			Phase:   modelv1alpha1.PhaseFailed,
			Ready:   metav1.ConditionFalse,
			Reason:  "JobFailed",
			Message: truncateMessage(msg, 1024),
		}

	case job.Status.Active > 0:
		return ObservationResult{
			Phase:   modelv1alpha1.PhaseRunning,
			Ready:   metav1.ConditionUnknown,
			Reason:  "JobRunning",
			Message: "Import/pack/push pipeline is running",
		}

	default:
		return ObservationResult{
			Phase:   modelv1alpha1.PhasePending,
			Ready:   metav1.ConditionUnknown,
			Reason:  "JobPending",
			Message: "Job is pending scheduling",
		}
	}
}

// extractTerminationMessage reads the termination message from the first
// terminated container of the most recently finished Pod.
// Pods are sorted by FinishedAt (most recent first) since List order is unspecified.
func extractTerminationMessage(pods []corev1.Pod) string {
	type podWithTime struct {
		pod   *corev1.Pod
		finAt metav1.Time
	}
	var candidates []podWithTime
	for i := range pods {
		p := &pods[i]
		var latest metav1.Time
		hasMsg := false
		for _, cs := range p.Status.ContainerStatuses {
			if cs.State.Terminated != nil && cs.State.Terminated.Message != "" {
				hasMsg = true
				if latest.IsZero() || cs.State.Terminated.FinishedAt.After(latest.Time) {
					latest = cs.State.Terminated.FinishedAt
				}
			}
		}
		if hasMsg {
			candidates = append(candidates, podWithTime{pod: p, finAt: latest})
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	// Sort by FinishedAt descending (most recent first)
	slices.SortFunc(candidates, func(a, b podWithTime) int {
		switch {
		case a.finAt.After(b.finAt.Time):
			return -1
		case b.finAt.After(a.finAt.Time):
			return 1
		default:
			return 0
		}
	})
	for _, cs := range candidates[0].pod.Status.ContainerStatuses {
		if cs.State.Terminated != nil && cs.State.Terminated.Message != "" {
			return strings.TrimSpace(cs.State.Terminated.Message)
		}
	}
	return ""
}

func isJobFailed(job *batchv1.Job) bool {
	if job.Status.Failed > 0 {
		return true
	}
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func failureReasonFromConditions(job *batchv1.Job) string {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return c.Message
		}
	}
	return "Job failed with unknown reason"
}

func truncateMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen-3] + "..."
}
