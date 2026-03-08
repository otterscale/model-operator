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

package controller

import (
	"cmp"
	"context"
	"slices"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
	"github.com/otterscale/model-operator/internal/modelartifact"
)

// ModelArtifactReconciler reconciles an Artifact object.
// It ensures that the underlying PVC and Job resources match the desired state
// defined in the Artifact CR.
//
// The controller is intentionally kept thin: it orchestrates the reconciliation flow,
// while the actual resource construction and status derivation logic reside in
// internal/modelartifact/.
type ModelArtifactReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Version  string
	KitImage string
	Recorder events.EventRecorder
}

// RBAC Permissions required by the controller:
// +kubebuilder:rbac:groups=model.otterscale.io,resources=modelartifacts,verbs=get;list;watch
// +kubebuilder:rbac:groups=model.otterscale.io,resources=modelartifacts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch

// Reconcile is the main loop for the controller.
// It implements level-triggered reconciliation: Fetch -> Reconcile Resources -> Status Update.
//
// Deletion is handled entirely by Kubernetes garbage collection: all child resources
// are created with OwnerReferences pointing to the Artifact, so they are automatically
// cascade-deleted when the Artifact is removed. No finalizer is needed.
func (r *ModelArtifactReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(req.Name)
	ctx = log.IntoContext(ctx, logger)

	var ma modelv1alpha1.ModelArtifact
	if err := r.Get(ctx, req.NamespacedName, &ma); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if err := r.reconcileResources(ctx, &ma); err != nil {
		return r.handleReconcileError(ctx, &ma, err)
	}

	obs, err := r.updateStatus(ctx, &ma)
	if err != nil {
		return ctrl.Result{}, err
	}

	if obs.Phase == modelv1alpha1.PhaseSucceeded || obs.Phase == modelv1alpha1.PhaseFailed {
		if deleteErr := modelartifact.DeletePVC(ctx, r.Client, &ma); deleteErr != nil {
			logger.Error(deleteErr, "Failed to delete workspace PVC after completion")
		}
	}

	// Requeue to poll for digest when Job succeeded but digest not yet available from pod termination message
	if obs.Phase == modelv1alpha1.PhaseSucceeded && obs.Digest == "" {
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// reconcileResources orchestrates the domain-level resource sync in order.
func (r *ModelArtifactReconciler) reconcileResources(ctx context.Context, ma *modelv1alpha1.ModelArtifact) error {
	// Idempotent: skip if already in terminal state and no spec change
	if (ma.Status.Phase == modelv1alpha1.PhaseSucceeded || ma.Status.Phase == modelv1alpha1.PhaseFailed) &&
		ma.Status.ObservedGeneration == ma.Generation {
		return nil
	}

	selectorLabels := modelartifact.SelectorLabelsForArtifact(ma.Name)
	metadataLabels := modelartifact.LabelsForArtifact(ma.Name, r.Version)

	// If generation changed, clean up any stale Jobs from previous generation
	if ma.Status.ObservedGeneration != 0 && ma.Status.ObservedGeneration < ma.Generation {
		log.FromContext(ctx).Info("Spec changed, cleaning up stale resources",
			"oldGeneration", ma.Status.ObservedGeneration,
			"newGeneration", ma.Generation)
		if err := modelartifact.CleanupStaleJobs(ctx, r.Client, ma, selectorLabels); err != nil {
			return err
		}
		if err := modelartifact.DeletePVC(ctx, r.Client, ma); err != nil {
			return err
		}
		// End reconciliation here. The status will be updated to reflect the cleanup,
		// and the next reconciliation loop will create the new PVC and Job.
		return nil
	}

	if err := modelartifact.EnsurePVC(ctx, r.Client, r.Scheme, ma, metadataLabels); err != nil {
		return err
	}

	job, created, err := modelartifact.EnsureJob(ctx, r.Client, r.Scheme, ma, r.KitImage, selectorLabels, metadataLabels)
	if err != nil {
		return err
	}
	if created {
		r.Recorder.Eventf(ma, nil, corev1.EventTypeNormal, "JobCreated", "Reconcile",
			"Created pipeline job %s", job.Name)
	}
	return nil
}

// handleReconcileError categorizes errors and updates status accordingly.
// Transient errors are returned to the controller-runtime for exponential backoff retry.
func (r *ModelArtifactReconciler) handleReconcileError(ctx context.Context, ma *modelv1alpha1.ModelArtifact, err error) (ctrl.Result, error) {
	r.setReadyConditionFalse(ctx, ma, "ReconcileError", err.Error())
	r.Recorder.Eventf(ma, nil, corev1.EventTypeWarning, "ReconcileError", "Reconcile", err.Error())
	return ctrl.Result{}, err
}

// setReadyConditionFalse updates the Ready condition to False via status patch.
// Errors are logged rather than propagated to avoid masking the original reconcile error.
func (r *ModelArtifactReconciler) setReadyConditionFalse(ctx context.Context, ma *modelv1alpha1.ModelArtifact, reason, message string) {
	logger := log.FromContext(ctx)

	patch := client.MergeFrom(ma.DeepCopy())
	meta.SetStatusCondition(&ma.Status.Conditions, metav1.Condition{
		Type:               modelartifact.ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: ma.Generation,
	})
	ma.Status.ObservedGeneration = ma.Generation

	if err := r.Status().Patch(ctx, ma, patch); err != nil {
		logger.Error(err, "Failed to patch Ready=False status condition", "reason", reason)
	}
}

// listJobPods returns Pods owned by the given Job.
func (r *ModelArtifactReconciler) listJobPods(ctx context.Context, job *batchv1.Job) ([]corev1.Pod, error) {
	if job == nil {
		return nil, nil
	}

	var podList corev1.PodList
	if err := r.List(ctx, &podList,
		client.InNamespace(job.Namespace),
		client.MatchingLabels{"batch.kubernetes.io/job-name": job.Name},
	); err != nil {
		return nil, err
	}
	return podList.Items, nil
}

// updateStatus calculates the status based on the current observed state and patches the resource.
// It returns the ObservationResult for the caller to decide on post-status actions (e.g. PVC cleanup).
func (r *ModelArtifactReconciler) updateStatus(ctx context.Context, ma *modelv1alpha1.ModelArtifact) (modelartifact.ObservationResult, error) {
	selectorLabels := modelartifact.SelectorLabelsForArtifact(ma.Name)
	job, err := modelartifact.FindOwnedJob(ctx, r.Client, ma.Namespace, selectorLabels, ma)
	if err != nil {
		return modelartifact.ObservationResult{}, err
	}

	pods, err := r.listJobPods(ctx, job)
	if err != nil {
		return modelartifact.ObservationResult{}, err
	}

	obs := modelartifact.ObserveJobStatus(job, pods, ma.Status.Phase, ma.Status.Digest)

	newStatus := ma.Status.DeepCopy()
	newStatus.ObservedGeneration = ma.Generation
	newStatus.Phase = obs.Phase

	if obs.Digest != "" {
		newStatus.Digest = obs.Digest
		newStatus.Reference = modelartifact.OCIReference(ma)
	}

	if job != nil {
		newStatus.JobRef = &modelv1alpha1.ResourceReference{Name: job.Name, Namespace: job.Namespace}
		if job.Status.StartTime != nil {
			newStatus.StartTime = job.Status.StartTime
		}
		if job.Status.CompletionTime != nil {
			newStatus.CompletionTime = job.Status.CompletionTime
		}
	}

	meta.SetStatusCondition(&newStatus.Conditions, metav1.Condition{
		Type:               modelartifact.ConditionTypeReady,
		Status:             obs.Ready,
		Reason:             obs.Reason,
		Message:            obs.Message,
		ObservedGeneration: ma.Generation,
	})

	// Sort conditions by type for stable ordering
	slices.SortFunc(newStatus.Conditions, func(a, b metav1.Condition) int {
		return cmp.Compare(a.Type, b.Type)
	})

	// Only patch if status has changed to reduce API server load
	if equality.Semantic.DeepEqual(ma.Status, *newStatus) {
		return obs, nil
	}

	patch := client.MergeFrom(ma.DeepCopy())
	ma.Status = *newStatus
	if err := r.Status().Patch(ctx, ma, patch); err != nil {
		return obs, err
	}

	log.FromContext(ctx).Info("Artifact status updated", "phase", obs.Phase, "digest", obs.Digest)

	switch obs.Phase {
	case modelv1alpha1.PhaseSucceeded:
		r.Recorder.Eventf(ma, nil, corev1.EventTypeNormal, "Succeeded", "Reconcile",
			"Artifact pushed successfully")
	case modelv1alpha1.PhaseFailed:
		r.Recorder.Eventf(ma, nil, corev1.EventTypeWarning, "Failed", "Reconcile",
			"Artifact pipeline failed: %s", obs.Message)
	}

	return obs, nil
}

// SetupWithManager registers the controller with the Manager and defines watches.
//
// Watch configuration:
//   - Artifact: with GenerationChangedPredicate to skip status-only updates
//   - Owned Jobs: status changes automatically trigger re-reconciliation via OwnerReference mapping
func (r *ModelArtifactReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&modelv1alpha1.ModelArtifact{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Owns(&batchv1.Job{}).
		Named("modelartifact").
		Complete(r)
}
