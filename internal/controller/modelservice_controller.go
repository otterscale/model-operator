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
	"fmt"
	"slices"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	inferenceextv1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
	"github.com/otterscale/model-operator/internal/modelservice"
)

// ModelServiceReconciler reconciles a ModelService object.
// It ensures that the underlying Deployments, EPP stack, InferencePool,
// HTTPRoute, and monitoring resources match the desired state.
//
// The controller is intentionally kept thin: it orchestrates the reconciliation flow,
// while the actual resource construction and status derivation logic reside in
// internal/modelservice/.
type ModelServiceReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Version       string
	Recorder      events.EventRecorder
	DefaultImages modelservice.DefaultImages
	EPPConfig     modelservice.EPPConfig
	KitImage      string
}

// RBAC Permissions required by the controller:
// +kubebuilder:rbac:groups=model.otterscale.io,resources=modelservices,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=model.otterscale.io,resources=modelservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=model.otterscale.io,resources=modelservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts;services;configmaps;secrets,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=inference.networking.k8s.io,resources=inferencepools,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=networking.istio.io,resources=destinationrules,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=podmonitors;servicemonitors,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=inference.networking.x-k8s.io,resources=inferenceobjectives;inferencemodelrewrites,verbs=get;list;watch
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
// +kubebuilder:rbac:urls="/metrics",verbs=get

// Reconcile is the main loop for the ModelService controller.
// It implements level-triggered reconciliation: Fetch -> Finalizer -> Reconcile Resources -> Status Update.
//
<<<<<<< HEAD
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/reconcile
=======
// Namespace-scoped child resources are cleaned up automatically via OwnerReferences.
// Cluster-scoped resources (ClusterRole/ClusterRoleBinding) are cleaned up via a Finalizer.
>>>>>>> tmp-original-31-03-26-01-54
func (r *ModelServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(req.Name)
	ctx = log.IntoContext(ctx, logger)

	var ms modelv1alpha1.ModelService
	if err := r.Get(ctx, req.NamespacedName, &ms); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !ms.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, &ms)
	}

	if err := r.ensureFinalizer(ctx, &ms); err != nil {
		return ctrl.Result{}, err
	}

	// Apply operator-level image defaults to a copy so builders see resolved images
	// without mutating the original API object.
	msWithDefaults := ms.DeepCopy()
	modelservice.ApplyImageDefaults(msWithDefaults, r.DefaultImages)

	if err := r.reconcileResources(ctx, msWithDefaults); err != nil {
		return r.handleReconcileError(ctx, &ms, err)
	}

	if err := r.updateStatus(ctx, &ms); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// ensureFinalizer adds the cluster-RBAC finalizer if it is not already present.
func (r *ModelServiceReconciler) ensureFinalizer(ctx context.Context, ms *modelv1alpha1.ModelService) error {
	if ctrlutil.ContainsFinalizer(ms, modelservice.FinalizerClusterRBAC) {
		return nil
	}
	ctrlutil.AddFinalizer(ms, modelservice.FinalizerClusterRBAC)
	if err := r.Update(ctx, ms); err != nil {
		return fmt.Errorf("adding finalizer: %w", err)
	}
	log.FromContext(ctx).Info("Added finalizer", "finalizer", modelservice.FinalizerClusterRBAC)
	return nil
}

// handleDeletion cleans up cluster-scoped resources and removes the finalizer.
func (r *ModelServiceReconciler) handleDeletion(ctx context.Context, ms *modelv1alpha1.ModelService) (ctrl.Result, error) {
	if !ctrlutil.ContainsFinalizer(ms, modelservice.FinalizerClusterRBAC) {
		return ctrl.Result{}, nil
	}

	if err := modelservice.CleanupEPPClusterRBAC(ctx, r.Client, ms.Namespace, ms.Name); err != nil {
		return ctrl.Result{}, fmt.Errorf("cleaning up cluster RBAC: %w", err)
	}

	ctrlutil.RemoveFinalizer(ms, modelservice.FinalizerClusterRBAC)
	if err := r.Update(ctx, ms); err != nil {
		return ctrl.Result{}, fmt.Errorf("removing finalizer: %w", err)
	}
	log.FromContext(ctx).Info("Removed finalizer", "finalizer", modelservice.FinalizerClusterRBAC)
	return ctrl.Result{}, nil
}

// reconcileResources orchestrates the domain-level resource sync in order.
//
// Order matters: SA and ConfigMap must exist before the EPP Deployment references them,
// and the InferencePool/HTTPRoute reference the EPP Service.
func (r *ModelServiceReconciler) reconcileResources(ctx context.Context, ms *modelv1alpha1.ModelService) error {
	// 1. Serving Deployments
	if err := modelservice.EnsureDecodeDeployment(ctx, r.Client, r.Scheme, ms, r.Version, r.EPPConfig.Tracing, r.KitImage); err != nil {
		return err
	}
	if err := modelservice.EnsurePrefillDeployment(ctx, r.Client, r.Scheme, ms, r.Version, r.EPPConfig.Tracing, r.KitImage); err != nil {
		return err
	}

	// 2. EPP prerequisites
	if err := modelservice.EnsureEPPServiceAccount(ctx, r.Client, r.Scheme, ms, r.Version); err != nil {
		return err
	}
	if err := modelservice.EnsureEPPConfigMap(ctx, r.Client, r.Scheme, ms, r.Version); err != nil {
		return err
	}

	// 3. EPP Deployment (depends on SA + ConfigMap)
	configHash := modelservice.ConfigMapHash(ms)
	if err := modelservice.EnsureEPPDeployment(ctx, r.Client, r.Scheme, ms, r.EPPConfig, r.DefaultImages, r.Version, configHash); err != nil {
		return err
	}

	// 4. EPP Service
	if err := modelservice.EnsureEPPService(ctx, r.Client, r.Scheme, ms, r.Version); err != nil {
		return err
	}

	// 5. EPP metrics auth
	if err := modelservice.EnsureEPPSATokenSecret(ctx, r.Client, r.Scheme, ms, r.EPPConfig, r.Version); err != nil {
		return err
	}

	// 6. EPP RBAC (namespace-scoped)
	if err := modelservice.EnsureEPPRBAC(ctx, r.Client, r.Scheme, ms, r.Version); err != nil {
		return err
	}

	// 7. EPP ClusterRole/ClusterRoleBinding (cluster-scoped, managed via Finalizer)
	if err := modelservice.EnsureEPPClusterRBAC(ctx, r.Client, ms, r.EPPConfig, r.Version); err != nil {
		return err
	}

	// 8. InferencePool + HTTPRoute
	if err := modelservice.EnsureInferencePool(ctx, r.Client, r.Scheme, ms, r.Version); err != nil {
		return err
	}
	if err := modelservice.EnsureHTTPRoute(ctx, r.Client, r.Scheme, ms, r.Version, r.EPPConfig); err != nil {
		return err
	}

	// 9. Networking: Istio DestinationRule
	if err := modelservice.EnsureDestinationRule(ctx, r.Client, r.Scheme, ms, r.EPPConfig, r.Version); err != nil {
		return err
	}

	// 10. Monitoring
	if err := modelservice.EnsurePodMonitors(ctx, r.Client, r.Scheme, ms, r.Version); err != nil {
		return err
	}
	if err := modelservice.EnsureEPPServiceMonitor(ctx, r.Client, r.Scheme, ms, r.EPPConfig, r.Version); err != nil {
		return err
	}

	return nil
}

// handleReconcileError updates the Ready condition and records an event.
func (r *ModelServiceReconciler) handleReconcileError(ctx context.Context, ms *modelv1alpha1.ModelService, err error) (ctrl.Result, error) {
	r.setReadyConditionFalse(ctx, ms, "ReconcileError", err.Error())
	r.Recorder.Eventf(ms, nil, corev1.EventTypeWarning, "ReconcileError", "Reconcile", err.Error())
	return ctrl.Result{}, err
}

// setReadyConditionFalse patches the Ready condition to False.
func (r *ModelServiceReconciler) setReadyConditionFalse(ctx context.Context, ms *modelv1alpha1.ModelService, reason, message string) {
	logger := log.FromContext(ctx)

	patch := client.MergeFrom(ms.DeepCopy())
	meta.SetStatusCondition(&ms.Status.Conditions, metav1.Condition{
		Type:               modelservice.ConditionTypeReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: ms.Generation,
	})
	ms.Status.ObservedGeneration = ms.Generation

	if err := r.Status().Patch(ctx, ms, patch); err != nil {
		logger.Error(err, "Failed to patch Ready=False status condition", "reason", reason)
	}
}

// updateStatus calculates status from observed cluster state and patches the resource.
func (r *ModelServiceReconciler) updateStatus(ctx context.Context, ms *modelv1alpha1.ModelService) error {
	obs, err := modelservice.ObserveStatus(ctx, r.Client, ms)
	if err != nil {
		return err
	}

	newStatus := ms.Status.DeepCopy()
	newStatus.ObservedGeneration = ms.Generation
	newStatus.Phase = obs.Phase
	newStatus.DecodeReady = obs.DecodeReady
	newStatus.DecodeReplicas = obs.DecodeReplicas
	newStatus.PrefillReady = obs.PrefillReady
	newStatus.PrefillReplicas = obs.PrefillReplicas

	meta.SetStatusCondition(&newStatus.Conditions, metav1.Condition{
		Type:               modelservice.ConditionTypeReady,
		Status:             obs.Ready,
		Reason:             obs.Reason,
		Message:            obs.Message,
		ObservedGeneration: ms.Generation,
	})

	slices.SortFunc(newStatus.Conditions, func(a, b metav1.Condition) int {
		return cmp.Compare(a.Type, b.Type)
	})

	if equality.Semantic.DeepEqual(ms.Status, *newStatus) {
		return nil
	}

	patch := client.MergeFrom(ms.DeepCopy())
	ms.Status = *newStatus
	if err := r.Status().Patch(ctx, ms, patch); err != nil {
		return err
	}

	log.FromContext(ctx).Info("ModelService status updated",
		"phase", obs.Phase,
		"decodeReady", obs.DecodeReady,
		"prefillReady", obs.PrefillReady,
	)

	switch obs.Phase {
	case modelv1alpha1.ModelServiceReady:
		r.Recorder.Eventf(ms, nil, corev1.EventTypeNormal, "Ready", "Reconcile", "All serving replicas are ready")
	case modelv1alpha1.ModelServiceFailed:
		r.Recorder.Eventf(ms, nil, corev1.EventTypeWarning, "Failed", "Reconcile", obs.Message)
	}

	return nil
}

// SetupWithManager registers the controller with the Manager and defines watches.
//
// Watch configuration:
//   - ModelService: with GenerationChangedPredicate to skip status-only updates
//   - Owned resources: status/spec changes trigger re-reconciliation via OwnerReference mapping
func (r *ModelServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&modelv1alpha1.ModelService{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{}),
		).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&corev1.Secret{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&inferenceextv1.InferencePool{}).
		Owns(&gatewayv1.HTTPRoute{}).
		Named("modelservice").
		Complete(r)
}
