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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
	"github.com/otterscale/model-operator/internal/labels"
)

const (
	ConditionTypeReady = "Ready"

	ComponentDecode  = "model-decode"
	ComponentPrefill = "model-prefill"
	ComponentEPP     = "epp"

	ModelVolumeName = "model"

	LabelRole            = "llm-d.ai/role"
	LabelInferenceServer = "llm-d.ai/inference-serving"

	LabelValueTrue = "true"

	RoleDecode  = "decode"
	RolePrefill = "prefill"

	FinalizerClusterRBAC = "model.otterscale.io/epp-cluster-rbac"
)

// gpuResourceByAccelerator maps accelerator types to their Kubernetes resource names.
var gpuResourceByAccelerator = map[modelv1alpha1.AcceleratorType]corev1.ResourceName{
	modelv1alpha1.AcceleratorNvidia:     "nvidia.com/gpu",
	modelv1alpha1.AcceleratorAMD:        "amd.com/gpu",
	modelv1alpha1.AcceleratorIntelGaudi: "habana.ai/gaudi",
	modelv1alpha1.AcceleratorGoogle:     "google.com/tpu",
}

// GPUResourceName returns the Kubernetes device plugin resource name for an accelerator.
func GPUResourceName(accel modelv1alpha1.AcceleratorType) corev1.ResourceName {
	if name, ok := gpuResourceByAccelerator[accel]; ok {
		return name
	}
	return ""
}

// GPUCount calculates the number of GPUs required per pod from parallelism settings.
func GPUCount(p modelv1alpha1.ParallelismSpec) int32 {
	tensor := max(p.Tensor, 1)
	dataLocal := max(p.DataLocal, 1)
	return tensor * dataLocal
}

// InjectGPUResources merges GPU resource limits into the given ResourceRequirements.
func InjectGPUResources(res *corev1.ResourceRequirements, accel modelv1alpha1.AcceleratorType, count int32) {
	if accel == modelv1alpha1.AcceleratorCPU || count == 0 {
		return
	}

	resName := GPUResourceName(accel)
	if resName == "" {
		return
	}

	qty := resource.MustParse(intToString(count))

	if res.Limits == nil {
		res.Limits = corev1.ResourceList{}
	}
	res.Limits[resName] = qty

	if res.Requests == nil {
		res.Requests = corev1.ResourceList{}
	}
	res.Requests[resName] = qty
}

func intToString(i int32) string {
	return resource.NewQuantity(int64(i), resource.DecimalSI).String()
}

// DecodeName returns the Deployment name for the decode role.
func DecodeName(msName string) string {
	return msName + "-decode"
}

// PrefillName returns the Deployment name for the prefill role.
func PrefillName(msName string) string {
	return msName + "-prefill"
}

// InferencePoolName returns the InferencePool name.
func InferencePoolName(msName string) string {
	return msName
}

// HTTPRouteName returns the HTTPRoute name.
func HTTPRouteName(msName string) string {
	return msName
}

// EPPName returns the EPP Deployment/Service/SA name.
func EPPName(msName string) string {
	return msName + "-epp"
}

// EPPConfigMapName returns the EPP ConfigMap name.
func EPPConfigMapName(msName string) string {
	return msName + "-epp-config"
}

// EPPSecretName returns the EPP SA token Secret name.
func EPPSecretName(msName string) string {
	return msName + "-epp-sa-metrics-reader-secret"
}

// EPPServiceMonitorName returns the EPP ServiceMonitor name.
func EPPServiceMonitorName(msName string) string {
	return msName + "-epp"
}

// EPPClusterRBACName returns a cluster-unique name for the EPP ClusterRole /
// ClusterRoleBinding. The namespace is embedded to avoid collisions when
// multiple ModelServices exist across namespaces.
func EPPClusterRBACName(namespace, msName string) string {
	return namespace + "-" + msName + "-epp"
}

// PodMonitorName returns the PodMonitor name for a role.
func PodMonitorName(msName, role string) string {
	return msName + "-" + role
}

// SelectorLabelsForRole returns labels used for list/match queries (version-independent).
func SelectorLabelsForRole(msName, component string) map[string]string {
	return labels.Selector(msName, component)
}

// LabelsForRole returns the full label set for resources of a specific role.
func LabelsForRole(msName, component, version string) map[string]string {
	m := labels.Standard(msName, component, version)
	m[LabelInferenceServer] = LabelValueTrue
	return m
}

// PodLabelsForRole returns labels applied to serving pods, including the llm-d role.
func PodLabelsForRole(msName, component, version, role string) map[string]string {
	m := LabelsForRole(msName, component, version)
	m[LabelRole] = role
	return m
}

// InferencePoolSelectorLabels returns the label set that InferencePool uses
// to select serving pods. Uses the common selector (without role) so both
// decode and prefill pods are included.
func InferencePoolSelectorLabels(msName string) map[string]string {
	m := labels.Selector(msName, ComponentDecode)
	delete(m, labels.Component)
	m[LabelInferenceServer] = LabelValueTrue
	return m
}

// EPPSelectorLabels returns labels used for EPP pod selection (version-independent).
func EPPSelectorLabels(msName string) map[string]string {
	return labels.Selector(msName, ComponentEPP)
}

// EPPLabels returns the full label set for EPP resources.
func EPPLabels(msName, version string) map[string]string {
	return labels.Standard(msName, ComponentEPP, version)
}

// DefaultReplicas returns a pointer to 1 if replicas is nil.
func DefaultReplicas(replicas *int32) int32 {
	if replicas != nil {
		return *replicas
	}
	return 1
}
