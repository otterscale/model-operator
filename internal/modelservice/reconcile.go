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

package modelservice

import (
	"context"
	"fmt"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	istionetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	inferenceextv1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// EnsureDecodeDeployment creates or updates the decode Deployment.
func EnsureDecodeDeployment(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	version string,
) error {
	return ensureDeployment(ctx, c, scheme, ms, &ms.Spec.Decode, RoleDecode, DecodeName(ms.Name), ComponentDecode, version)
}

// EnsurePrefillDeployment creates or updates the prefill Deployment if configured.
func EnsurePrefillDeployment(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	version string,
) error {
	if ms.Spec.Prefill == nil {
		return cleanupDeployment(ctx, c, ms.Namespace, PrefillName(ms.Name))
	}
	return ensureDeployment(ctx, c, scheme, ms, ms.Spec.Prefill, RolePrefill, PrefillName(ms.Name), ComponentPrefill, version)
}

func ensureDeployment(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	role *modelv1alpha1.RoleSpec,
	roleName string,
	deployName string,
	component string,
	version string,
) error {
	selectorLabels := SelectorLabelsForRole(ms.Name, component)
	metadataLabels := LabelsForRole(ms.Name, component, version)
	podLabels := PodLabelsForRole(ms.Name, component, version, roleName)

	desired := BuildDeployment(ms, role, roleName, deployName, podLabels, metadataLabels, selectorLabels)
	if err := ctrlutil.SetControllerReference(ms, desired, scheme); err != nil {
		return fmt.Errorf("setting Deployment owner reference: %w", err)
	}

	var existing appsv1.Deployment
	err := c.Get(ctx, types.NamespacedName{Name: deployName, Namespace: ms.Namespace}, &existing)
	if apierrors.IsNotFound(err) {
		if err := c.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating Deployment %s: %w", deployName, err)
		}
		log.FromContext(ctx).Info("Created Deployment", "name", deployName, "role", roleName)
		return nil
	}
	if err != nil {
		return err
	}

	if !deploymentNeedsUpdate(&existing, desired) {
		return nil
	}

	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := c.Update(ctx, &existing); err != nil {
		return fmt.Errorf("updating Deployment %s: %w", deployName, err)
	}
	log.FromContext(ctx).Info("Updated Deployment", "name", deployName, "role", roleName)
	return nil
}

func deploymentNeedsUpdate(existing, desired *appsv1.Deployment) bool {
	return !equality.Semantic.DeepEqual(existing.Spec, desired.Spec) ||
		!equality.Semantic.DeepEqual(existing.Labels, desired.Labels)
}

func cleanupDeployment(ctx context.Context, c client.Client, namespace, name string) error {
	var dep appsv1.Deployment
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &dep)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	log.FromContext(ctx).Info("Cleaning up orphaned Deployment", "name", name)
	return client.IgnoreNotFound(c.Delete(ctx, &dep))
}

// EnsureInferencePool creates or updates the InferencePool if configured.
func EnsureInferencePool(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	version string,
) error {
	if ms.Spec.InferencePool == nil {
		return cleanupTyped(ctx, c, &inferenceextv1.InferencePool{ObjectMeta: objMeta(ms.Namespace, InferencePoolName(ms.Name))})
	}

	metadataLabels := LabelsForRole(ms.Name, ComponentDecode, version)
	desired := BuildInferencePool(ms, metadataLabels)

	return ensureResource(ctx, c, scheme, ms, desired, func(existing, desired *inferenceextv1.InferencePool) {
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
	})
}

// EnsureHTTPRoute creates or updates the HTTPRoute if configured.
func EnsureHTTPRoute(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	version string,
) error {
	if ms.Spec.HTTPRoute == nil || ms.Spec.InferencePool == nil {
		return cleanupTyped(ctx, c, &gatewayv1.HTTPRoute{ObjectMeta: objMeta(ms.Namespace, HTTPRouteName(ms.Name))})
	}

	metadataLabels := LabelsForRole(ms.Name, ComponentDecode, version)
	desired := BuildHTTPRoute(ms, metadataLabels)

	return ensureResource(ctx, c, scheme, ms, desired, func(existing, desired *gatewayv1.HTTPRoute) {
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
	})
}

// EnsurePodMonitors creates or updates PodMonitors for decode (and optionally prefill).
func EnsurePodMonitors(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	version string,
) error {
	enabled := ms.Spec.Monitoring != nil &&
		ms.Spec.Monitoring.PodMonitor != nil &&
		ms.Spec.Monitoring.PodMonitor.Enabled

	if !enabled {
		if err := cleanupTyped(ctx, c, &monitoringv1.PodMonitor{ObjectMeta: objMeta(ms.Namespace, PodMonitorName(ms.Name, RoleDecode))}); err != nil {
			return err
		}
		return cleanupTyped(ctx, c, &monitoringv1.PodMonitor{ObjectMeta: objMeta(ms.Namespace, PodMonitorName(ms.Name, RolePrefill))})
	}

	decodeSelectorLabels := SelectorLabelsForRole(ms.Name, ComponentDecode)
	decodeLabels := LabelsForRole(ms.Name, ComponentDecode, version)
	decodeMonitor := BuildPodMonitor(ms, RoleDecode, decodeSelectorLabels, decodeLabels)
	if err := ensureResource(ctx, c, scheme, ms, decodeMonitor, func(existing, desired *monitoringv1.PodMonitor) {
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
	}); err != nil {
		return err
	}

	if ms.Spec.Prefill != nil {
		prefillSelectorLabels := SelectorLabelsForRole(ms.Name, ComponentPrefill)
		prefillLabels := LabelsForRole(ms.Name, ComponentPrefill, version)
		prefillMonitor := BuildPodMonitor(ms, RolePrefill, prefillSelectorLabels, prefillLabels)
		return ensureResource(ctx, c, scheme, ms, prefillMonitor, func(existing, desired *monitoringv1.PodMonitor) {
			existing.Spec = desired.Spec
			existing.Labels = desired.Labels
		})
	}

	return cleanupTyped(ctx, c, &monitoringv1.PodMonitor{ObjectMeta: objMeta(ms.Namespace, PodMonitorName(ms.Name, RolePrefill))})
}

// EnsureEPPServiceAccount creates or updates the EPP ServiceAccount.
func EnsureEPPServiceAccount(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	version string,
) error {
	if ms.Spec.InferencePool == nil {
		return cleanupTyped(ctx, c, &corev1.ServiceAccount{ObjectMeta: objMeta(ms.Namespace, EPPName(ms.Name))})
	}
	metadataLabels := EPPLabels(ms.Name, version)
	desired := BuildEPPServiceAccount(ms, metadataLabels)
	return ensureResource(ctx, c, scheme, ms, desired, func(existing, desired *corev1.ServiceAccount) {
		existing.Labels = desired.Labels
	})
}

// EnsureEPPConfigMap creates or updates the EPP ConfigMap.
func EnsureEPPConfigMap(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	version string,
) error {
	if ms.Spec.InferencePool == nil {
		return cleanupTyped(ctx, c, &corev1.ConfigMap{ObjectMeta: objMeta(ms.Namespace, EPPConfigMapName(ms.Name))})
	}
	metadataLabels := EPPLabels(ms.Name, version)
	desired := BuildEPPConfigMap(ms, metadataLabels)
	return ensureResource(ctx, c, scheme, ms, desired, func(existing, desired *corev1.ConfigMap) {
		existing.Data = desired.Data
		existing.Labels = desired.Labels
	})
}

// EnsureEPPDeployment creates or updates the EPP Deployment.
func EnsureEPPDeployment(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	eppConfig EPPConfig,
	version string,
	configHash string,
) error {
	name := EPPName(ms.Name)
	if ms.Spec.InferencePool == nil {
		return cleanupDeployment(ctx, c, ms.Namespace, name)
	}
	metadataLabels := EPPLabels(ms.Name, version)
	selectorLabels := EPPSelectorLabels(ms.Name)
	desired := BuildEPPDeployment(ms, eppConfig, metadataLabels, selectorLabels, configHash)
	if err := ctrlutil.SetControllerReference(ms, desired, scheme); err != nil {
		return fmt.Errorf("setting EPP Deployment owner reference: %w", err)
	}

	var existing appsv1.Deployment
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: ms.Namespace}, &existing)
	if apierrors.IsNotFound(err) {
		if err := c.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating EPP Deployment %s: %w", name, err)
		}
		log.FromContext(ctx).Info("Created EPP Deployment", "name", name)
		return nil
	}
	if err != nil {
		return err
	}
	if !deploymentNeedsUpdate(&existing, desired) {
		return nil
	}
	existing.Spec = desired.Spec
	existing.Labels = desired.Labels
	if err := c.Update(ctx, &existing); err != nil {
		return fmt.Errorf("updating EPP Deployment %s: %w", name, err)
	}
	log.FromContext(ctx).Info("Updated EPP Deployment", "name", name)
	return nil
}

// EnsureEPPService creates or updates the EPP Service.
func EnsureEPPService(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	version string,
) error {
	if ms.Spec.InferencePool == nil {
		return cleanupTyped(ctx, c, &corev1.Service{ObjectMeta: objMeta(ms.Namespace, EPPName(ms.Name))})
	}
	metadataLabels := EPPLabels(ms.Name, version)
	selectorLabels := EPPSelectorLabels(ms.Name)
	desired := BuildEPPService(ms, metadataLabels, selectorLabels)
	return ensureResource(ctx, c, scheme, ms, desired, func(existing, desired *corev1.Service) {
		existing.Spec.Ports = desired.Spec.Ports
		existing.Spec.Selector = desired.Spec.Selector
		existing.Labels = desired.Labels
	})
}

// EnsureEPPSATokenSecret creates or updates the EPP SA token Secret.
func EnsureEPPSATokenSecret(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	version string,
) error {
	if ms.Spec.InferencePool == nil {
		return cleanupTyped(ctx, c, &corev1.Secret{ObjectMeta: objMeta(ms.Namespace, EPPSecretName(ms.Name))})
	}
	metadataLabels := EPPLabels(ms.Name, version)
	desired := BuildEPPSATokenSecret(ms, metadataLabels)
	return ensureResource(ctx, c, scheme, ms, desired, func(existing, desired *corev1.Secret) {
		existing.Annotations = desired.Annotations
		existing.Labels = desired.Labels
	})
}

// EnsureEPPRBAC creates or updates the EPP Role and RoleBinding.
func EnsureEPPRBAC(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	version string,
) error {
	name := EPPName(ms.Name)
	if ms.Spec.InferencePool == nil {
		if err := cleanupTyped(ctx, c, &rbacv1.Role{ObjectMeta: objMeta(ms.Namespace, name)}); err != nil {
			return err
		}
		return cleanupTyped(ctx, c, &rbacv1.RoleBinding{ObjectMeta: objMeta(ms.Namespace, name)})
	}
	metadataLabels := EPPLabels(ms.Name, version)
	replicas := DefaultReplicas(ms.Spec.InferencePool.EndpointPicker.Replicas)
	role, binding := BuildEPPRBAC(ms, metadataLabels, replicas)

	if err := ensureResource(ctx, c, scheme, ms, role, func(existing, desired *rbacv1.Role) {
		existing.Rules = desired.Rules
		existing.Labels = desired.Labels
	}); err != nil {
		return err
	}
	return ensureResource(ctx, c, scheme, ms, binding, func(existing, desired *rbacv1.RoleBinding) {
		existing.Subjects = desired.Subjects
		existing.RoleRef = desired.RoleRef
		existing.Labels = desired.Labels
	})
}

// EnsureEPPClusterRBAC creates or updates the cluster-scoped ClusterRole and
// ClusterRoleBinding for EPP metrics authentication. Since these are
// cluster-scoped they cannot carry an OwnerReference; the controller manages
// cleanup via a Finalizer on the ModelService.
func EnsureEPPClusterRBAC(
	ctx context.Context,
	c client.Client,
	ms *modelv1alpha1.ModelService,
	eppConfig EPPConfig,
	version string,
) error {
	name := EPPClusterRBACName(ms.Namespace, ms.Name)

	needsClusterRBAC := ms.Spec.InferencePool != nil && eppConfig.MetricsEndpointAuth
	if !needsClusterRBAC {
		return cleanupClusterRBAC(ctx, c, name)
	}

	metadataLabels := EPPLabels(ms.Name, version)
	desiredRole, desiredBinding := BuildEPPClusterRBAC(ms, metadataLabels)

	if err := ensureClusterResource(ctx, c, desiredRole, func(existing, desired *rbacv1.ClusterRole) {
		existing.Rules = desired.Rules
		existing.Labels = desired.Labels
	}); err != nil {
		return err
	}
	return ensureClusterResource(ctx, c, desiredBinding, func(existing, desired *rbacv1.ClusterRoleBinding) {
		existing.Subjects = desired.Subjects
		existing.RoleRef = desired.RoleRef
		existing.Labels = desired.Labels
	})
}

// CleanupEPPClusterRBAC removes the cluster-scoped RBAC resources. Called from
// the controller's Finalizer path.
func CleanupEPPClusterRBAC(ctx context.Context, c client.Client, namespace, msName string) error {
	return cleanupClusterRBAC(ctx, c, EPPClusterRBACName(namespace, msName))
}

func cleanupClusterRBAC(ctx context.Context, c client.Client, name string) error {
	if err := cleanupClusterTyped(ctx, c, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: name}}); err != nil {
		return err
	}
	return cleanupClusterTyped(ctx, c, &rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: name}})
}

func cleanupClusterTyped(ctx context.Context, c client.Client, obj client.Object) error {
	err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	kind := obj.GetObjectKind().GroupVersionKind().Kind
	if kind == "" {
		kind = fmt.Sprintf("%T", obj)
	}
	log.FromContext(ctx).Info("Cleaning up cluster-scoped "+kind, "name", obj.GetName())
	return client.IgnoreNotFound(c.Delete(ctx, obj))
}

// ensureClusterResource is a create-or-update for cluster-scoped resources
// (no OwnerReference since owner is namespaced).
func ensureClusterResource[T client.Object](
	ctx context.Context,
	c client.Client,
	desired T,
	mutateFn func(existing, desired T),
) error {
	kind := desired.GetObjectKind().GroupVersionKind().Kind
	if kind == "" {
		kind = fmt.Sprintf("%T", desired)
	}
	name := desired.GetName()

	existing := desired.DeepCopyObject().(T)
	err := c.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if apierrors.IsNotFound(err) {
		if err := c.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating %s %s: %w", kind, name, err)
		}
		log.FromContext(ctx).Info("Created "+kind, "name", name)
		return nil
	}
	if err != nil {
		return err
	}

	mutateFn(existing, desired)
	if err := c.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating %s %s: %w", kind, name, err)
	}
	log.FromContext(ctx).Info("Updated "+kind, "name", name)
	return nil
}

// EnsureDestinationRule creates or updates the Istio DestinationRule.
func EnsureDestinationRule(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	eppConfig EPPConfig,
	version string,
) error {
	name := EPPName(ms.Name)
	if ms.Spec.InferencePool == nil || eppConfig.Provider != ProviderIstio {
		return cleanupTyped(ctx, c, &istionetworkingv1beta1.DestinationRule{ObjectMeta: objMeta(ms.Namespace, name)})
	}
	metadataLabels := EPPLabels(ms.Name, version)
	desired := BuildDestinationRule(ms, metadataLabels)
	return ensureResource(ctx, c, scheme, ms, desired, func(existing, desired *istionetworkingv1beta1.DestinationRule) {
		existing.Spec.Host = desired.Spec.Host
		existing.Spec.TrafficPolicy = desired.Spec.TrafficPolicy
		existing.Spec.Subsets = desired.Spec.Subsets
		existing.Spec.ExportTo = desired.Spec.ExportTo
		existing.Labels = desired.Labels
	})
}

// EnsureEPPServiceMonitor creates or updates the EPP ServiceMonitor.
func EnsureEPPServiceMonitor(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	ms *modelv1alpha1.ModelService,
	version string,
) error {
	name := EPPServiceMonitorName(ms.Name)
	monitoringEnabled := ms.Spec.Monitoring != nil &&
		ms.Spec.Monitoring.PodMonitor != nil &&
		ms.Spec.Monitoring.PodMonitor.Enabled

	if ms.Spec.InferencePool == nil || !monitoringEnabled {
		return cleanupTyped(ctx, c, &monitoringv1.ServiceMonitor{ObjectMeta: objMeta(ms.Namespace, name)})
	}
	metadataLabels := EPPLabels(ms.Name, version)
	desired := BuildEPPServiceMonitor(ms, metadataLabels)
	return ensureResource(ctx, c, scheme, ms, desired, func(existing, desired *monitoringv1.ServiceMonitor) {
		existing.Spec = desired.Spec
		existing.Labels = desired.Labels
	})
}

// ensureResource is a generic create-or-update for typed resources.
// The mutateFn copies desired fields into the existing object.
func ensureResource[T client.Object](
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	owner *modelv1alpha1.ModelService,
	desired T,
	mutateFn func(existing, desired T),
) error {
	kind := desired.GetObjectKind().GroupVersionKind().Kind
	if kind == "" {
		kind = fmt.Sprintf("%T", desired)
	}
	name := desired.GetName()
	namespace := desired.GetNamespace()

	if err := ctrlutil.SetControllerReference(owner, desired, scheme); err != nil {
		return fmt.Errorf("setting %s owner reference: %w", kind, err)
	}

	existing := desired.DeepCopyObject().(T)
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, existing)
	if apierrors.IsNotFound(err) {
		if err := c.Create(ctx, desired); err != nil {
			return fmt.Errorf("creating %s %s: %w", kind, name, err)
		}
		log.FromContext(ctx).Info("Created "+kind, "name", name)
		return nil
	}
	if err != nil {
		return err
	}

	mutateFn(existing, desired)
	existing.SetResourceVersion(existing.GetResourceVersion())
	if err := c.Update(ctx, existing); err != nil {
		return fmt.Errorf("updating %s %s: %w", kind, name, err)
	}
	log.FromContext(ctx).Info("Updated "+kind, "name", name)
	return nil
}

// cleanupTyped deletes a typed resource if it exists. The obj must be a
// zero-value instance of the target type with Name and Namespace set.
func cleanupTyped(ctx context.Context, c client.Client, obj client.Object) error {
	err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj)
	if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
		return nil
	}
	if err != nil {
		return err
	}
	kind := obj.GetObjectKind().GroupVersionKind().Kind
	if kind == "" {
		kind = fmt.Sprintf("%T", obj)
	}
	log.FromContext(ctx).Info("Cleaning up orphaned "+kind, "name", obj.GetName())
	return client.IgnoreNotFound(c.Delete(ctx, obj))
}

func objMeta(namespace, name string) metav1.ObjectMeta {
	return metav1.ObjectMeta{Name: name, Namespace: namespace}
}
