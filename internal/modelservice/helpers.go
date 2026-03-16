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
	"strings"
	"unicode"

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

	// DockerConfigVolumeName is the name of the volume that mounts OCI registry credentials
	// (when spec.model.imagePullSecrets is set). Aligned with modelartifact for consistency.
	DockerConfigVolumeName = "docker-config"
	// DockerConfigMountPath is the mount path for the Docker config inside the Pod (config at config.json).
	DockerConfigMountPath = "/.docker"

	LabelRole            = "llm-d.ai/role"
	LabelInferenceServer = "llm-d.ai/inference-serving"
	LabelModel           = "llm-d.ai/model"
	// LabelInferencePool is the Pod label key for the EPP deployment name (value = EPPName(msName)).
	LabelInferencePool = "inferencepool"

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

// EPPName returns the EPP resource name (Deployment, Service, SA, Role, RoleBinding, ConfigMap).
// Must be DNS-1035 compliant so Role/SA/Service etc. can be created when msName contains dots.
func EPPName(msName string) string {
	return SanitizeDNS1035Label(msName) + "-epp"
}

// EPPNameForService returns the EPP Service name; same as EPPName (kept for call-site clarity).
func EPPNameForService(msName string) string {
	return EPPName(msName)
}

// SanitizeDNS1035Label returns a string valid as a DNS-1035 label: lowercase alphanumeric and '-',
// must start with a letter, must end with an alphanumeric.
func SanitizeDNS1035Label(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(unicode.ToLower(r))
		case r == '.' || r == '_' || r == '-':
			b.WriteRune('-')
		}
	}
	s = strings.Trim(b.String(), "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	if s == "" {
		return "modelservice"
	}
	if s[0] >= '0' && s[0] <= '9' {
		s = "m-" + s
	}
	if len(s) > 0 && s[len(s)-1] == '-' {
		s = strings.TrimSuffix(s, "-")
	}
	return s
}

// EPPConfigMapName returns the EPP ConfigMap name (DNS-1035 compliant base).
func EPPConfigMapName(msName string) string {
	return EPPName(msName) + "-config"
}

// EPPSecretName returns the EPP SA token Secret name (DNS-1035 compliant base).
func EPPSecretName(msName string) string {
	return EPPName(msName) + "-sa-metrics-reader-secret"
}

// EPPServiceMonitorName returns the EPP ServiceMonitor name (DNS-1035 compliant).
func EPPServiceMonitorName(msName string) string {
	return EPPName(msName)
}

// EPPClusterRBACName returns a cluster-unique name for the EPP ClusterRole /
// ClusterRoleBinding. The namespace is embedded to avoid collisions when
// multiple ModelServices exist across namespaces. Uses sanitized EPP base.
func EPPClusterRBACName(namespace, msName string) string {
	return namespace + "-" + EPPName(msName)
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

// PodLabelsForRole returns labels applied to serving pods, including the llm-d role
// and llm-d.ai/model (ModelService name). Only the Deployment's Pod template gets
// llm-d.ai/model; the Deployment object itself does not.
func PodLabelsForRole(msName, component, version, role string) map[string]string {
	m := LabelsForRole(msName, component, version)
	m[LabelRole] = role
	m[LabelModel] = msName
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

// EPPLabels returns the full label set for EPP resources. app.kubernetes.io/name is set
// to the EPP Deployment name (EPPName(msName)) so it matches deployment.metadata.name.
func EPPLabels(msName, version string) map[string]string {
	return labels.Standard(EPPName(msName), ComponentEPP, version)
}

// EPPSelectorLabels returns labels used for EPP pod selection. Uses EPPName(msName) so
// selector matches the EPP Deployment and its Pod template labels.
func EPPSelectorLabels(msName string) map[string]string {
	return labels.Selector(EPPName(msName), ComponentEPP)
}

// DefaultReplicas returns a pointer to 1 if replicas is nil.
func DefaultReplicas(replicas *int32) int32 {
	if replicas != nil {
		return *replicas
	}
	return 1
}
