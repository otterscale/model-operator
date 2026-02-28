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

package artifact_test

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
	"github.com/otterscale/model-operator/internal/artifact"
)

var _ = Describe("ObserveJobStatus", func() {
	Context("when Job is nil", func() {
		It("should return PhasePending with ConditionFalse", func() {
			result := artifact.ObserveJobStatus(nil, nil)
			Expect(result.Phase).To(Equal(modelv1alpha1.PhasePending))
			Expect(result.Ready).To(Equal(metav1.ConditionFalse))
			Expect(result.Reason).To(Equal("JobNotCreated"))
		})
	})

	Context("when Job is active", func() {
		It("should return PhaseRunning with ConditionUnknown", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{Active: 1},
			}
			result := artifact.ObserveJobStatus(job, nil)
			Expect(result.Phase).To(Equal(modelv1alpha1.PhaseRunning))
			Expect(result.Ready).To(Equal(metav1.ConditionUnknown))
			Expect(result.Reason).To(Equal("JobRunning"))
		})
	})

	Context("when Job succeeded", func() {
		It("should return PhaseSucceeded with digest from termination message", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{Succeeded: 1},
			}
			pods := []corev1.Pod{
				{
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										Message: "sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
									},
								},
							},
						},
					},
				},
			}

			result := artifact.ObserveJobStatus(job, pods)
			Expect(result.Phase).To(Equal(modelv1alpha1.PhaseSucceeded))
			Expect(result.Ready).To(Equal(metav1.ConditionTrue))
			Expect(result.Digest).To(Equal("sha256:abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"))
		})

		It("should return empty digest when no termination message", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{Succeeded: 1},
			}
			result := artifact.ObserveJobStatus(job, nil)
			Expect(result.Phase).To(Equal(modelv1alpha1.PhaseSucceeded))
			Expect(result.Digest).To(BeEmpty())
		})
	})

	Context("when Job failed", func() {
		It("should return PhaseFailed with termination message", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{
					Failed: 1,
					Conditions: []batchv1.JobCondition{
						{
							Type:    batchv1.JobFailed,
							Status:  corev1.ConditionTrue,
							Message: "BackoffLimitExceeded",
						},
					},
				},
			}
			pods := []corev1.Pod{
				{
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										Message: "kit import: repository not found",
									},
								},
							},
						},
					},
				},
			}

			result := artifact.ObserveJobStatus(job, pods)
			Expect(result.Phase).To(Equal(modelv1alpha1.PhaseFailed))
			Expect(result.Ready).To(Equal(metav1.ConditionFalse))
			Expect(result.Message).To(Equal("kit import: repository not found"))
		})

		It("should fall back to Job condition message when no termination message", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{
					Failed: 1,
					Conditions: []batchv1.JobCondition{
						{
							Type:    batchv1.JobFailed,
							Status:  corev1.ConditionTrue,
							Message: "BackoffLimitExceeded",
						},
					},
				},
			}

			result := artifact.ObserveJobStatus(job, nil)
			Expect(result.Phase).To(Equal(modelv1alpha1.PhaseFailed))
			Expect(result.Message).To(Equal("BackoffLimitExceeded"))
		})

		It("should detect failure from condition even when failed count is zero", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{
					Failed: 0,
					Conditions: []batchv1.JobCondition{
						{
							Type:    batchv1.JobFailed,
							Status:  corev1.ConditionTrue,
							Message: "DeadlineExceeded",
						},
					},
				},
			}

			result := artifact.ObserveJobStatus(job, nil)
			Expect(result.Phase).To(Equal(modelv1alpha1.PhaseFailed))
		})
	})

	Context("when Job has no activity", func() {
		It("should return PhasePending with ConditionUnknown", func() {
			job := &batchv1.Job{
				Status: batchv1.JobStatus{},
			}
			result := artifact.ObserveJobStatus(job, nil)
			Expect(result.Phase).To(Equal(modelv1alpha1.PhasePending))
			Expect(result.Ready).To(Equal(metav1.ConditionUnknown))
		})
	})

	Context("termination message extraction", func() {
		It("should find the message from the second pod when first has none", func() {
			pods := []corev1.Pod{
				{
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{}},
						},
					},
				},
				{
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										Message: "sha256:deadbeef",
									},
								},
							},
						},
					},
				},
			}

			job := &batchv1.Job{
				Status: batchv1.JobStatus{Succeeded: 1},
			}
			result := artifact.ObserveJobStatus(job, pods)
			Expect(result.Digest).To(Equal("sha256:deadbeef"))
		})
	})

	Context("message truncation", func() {
		It("should truncate messages exceeding the limit", func() {
			longMsg := strings.Repeat("a", 2000)
			job := &batchv1.Job{
				Status: batchv1.JobStatus{
					Failed: 1,
					Conditions: []batchv1.JobCondition{
						{
							Type:   batchv1.JobFailed,
							Status: corev1.ConditionTrue,
						},
					},
				},
			}
			pods := []corev1.Pod{
				{
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										Message: longMsg,
									},
								},
							},
						},
					},
				},
			}

			result := artifact.ObserveJobStatus(job, pods)
			Expect(result.Message).To(HaveLen(1024))
			Expect(result.Message).To(HaveSuffix("..."))
		})
	})
})
