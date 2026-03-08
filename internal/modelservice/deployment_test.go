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
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

const testMountPath = "/models"

func newTestModelService() *modelv1alpha1.ModelService {
	replicas := int32(2)
	return &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qwen3-32b",
			Namespace: TestNamespace,
		},
		Spec: modelv1alpha1.ModelServiceSpec{
			Model: modelv1alpha1.ModelSpec{
				Name:  "qwen/Qwen3-32B",
				Image: "registry.example.com/models/qwen3-32b:v1",
			},
			Engine: modelv1alpha1.EngineSpec{
				Image: "vllm/vllm-openai:v0.8.0",
				Args:  []string{"--max-model-len=8192"},
				Port:  8000,
			},
			Accelerator: modelv1alpha1.AcceleratorSpec{
				Type: modelv1alpha1.AcceleratorNvidia,
			},
			Decode: modelv1alpha1.RoleSpec{
				Replicas: &replicas,
				Parallelism: modelv1alpha1.ParallelismSpec{
					Tensor: 4,
				},
			},
		},
	}
}

func TestBuildDeployment_Basic(t *testing.T) {
	ms := newTestModelService()
	role := &ms.Spec.Decode
	podLabels := map[string]string{"role": "decode"}
	metaLabels := map[string]string{"app": "qwen3"}
	selLabels := map[string]string{"app": "qwen3"}

	dep := BuildDeployment(ms, role, RoleDecode, "qwen3-32b-decode", podLabels, metaLabels, selLabels, TracingConfig{})

	if dep.Name != "qwen3-32b-decode" {
		t.Errorf("Name = %q", dep.Name)
	}
	if dep.Namespace != TestNamespace {
		t.Errorf("Namespace = %q", dep.Namespace)
	}
	if *dep.Spec.Replicas != 2 {
		t.Errorf("Replicas = %d", *dep.Spec.Replicas)
	}

	containers := dep.Spec.Template.Spec.Containers
	if len(containers) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(containers))
	}

	vllm := containers[0]
	if vllm.Name != "vllm" {
		t.Errorf("Container name = %q", vllm.Name)
	}
	if vllm.Image != "vllm/vllm-openai:v0.8.0" {
		t.Errorf("Image = %q", vllm.Image)
	}

	if !slices.Contains(vllm.Args, "--tensor-parallel-size") {
		t.Error("Missing --tensor-parallel-size in args")
	}
	if !slices.Contains(vllm.Args, "--max-model-len=8192") {
		t.Error("Missing user-provided arg --max-model-len=8192")
	}

	gpuRes, ok := vllm.Resources.Limits["nvidia.com/gpu"]
	if !ok {
		t.Fatal("Missing nvidia.com/gpu in limits")
	}
	if gpuRes.Value() != 4 {
		t.Errorf("GPU limits = %d, want 4", gpuRes.Value())
	}
}

func TestBuildDeployment_ImageVolume(t *testing.T) {
	ms := newTestModelService()
	role := &ms.Spec.Decode
	dep := BuildDeployment(ms, role, RoleDecode, "test-decode", nil, nil, nil, TracingConfig{})

	volumes := dep.Spec.Template.Spec.Volumes
	if len(volumes) != 1 {
		t.Fatalf("Expected 1 volume, got %d", len(volumes))
	}

	v := volumes[0]
	if v.Name != ModelVolumeName {
		t.Errorf("Volume name = %q", v.Name)
	}
	if v.Image == nil {
		t.Fatal("Expected image volume source")
	}
	if v.Image.Reference != "registry.example.com/models/qwen3-32b:v1" {
		t.Errorf("Image reference = %q", v.Image.Reference)
	}
	if v.Image.PullPolicy != corev1.PullIfNotPresent {
		t.Errorf("PullPolicy = %q", v.Image.PullPolicy)
	}

	vllm := dep.Spec.Template.Spec.Containers[0]
	if len(vllm.VolumeMounts) != 1 {
		t.Fatalf("Expected 1 volume mount, got %d", len(vllm.VolumeMounts))
	}
	if vllm.VolumeMounts[0].MountPath != testMountPath {
		t.Errorf("MountPath = %q", vllm.VolumeMounts[0].MountPath)
	}
	if !vllm.VolumeMounts[0].ReadOnly {
		t.Error("Model volume mount should be read-only")
	}
}

func TestBuildDeployment_WithRoutingProxy(t *testing.T) {
	ms := newTestModelService()
	ms.Spec.RoutingProxy = &modelv1alpha1.RoutingProxySpec{
		Connector:  "nixlv2",
		TargetPort: 8200,
	}
	role := &ms.Spec.Decode
	dep := BuildDeployment(ms, role, RoleDecode, "test-decode", nil, nil, nil, TracingConfig{})

	initContainers := dep.Spec.Template.Spec.InitContainers
	if len(initContainers) != 1 {
		t.Fatalf("Expected 1 init container, got %d", len(initContainers))
	}

	proxy := initContainers[0]
	if proxy.Name != "routing-proxy" {
		t.Errorf("Init container name = %q", proxy.Name)
	}
	if proxy.RestartPolicy == nil || *proxy.RestartPolicy != corev1.ContainerRestartPolicyAlways {
		t.Error("Routing proxy should be a native sidecar (RestartPolicy=Always)")
	}

	if !slices.Contains(proxy.Args, "--connector") {
		t.Error("Missing --connector in proxy args")
	}

	vllm := dep.Spec.Template.Spec.Containers[0]
	foundPort := false
	for _, p := range vllm.Ports {
		if p.ContainerPort == 8200 {
			foundPort = true
		}
	}
	if !foundPort {
		t.Error("vLLM should listen on targetPort 8200 when proxy is enabled")
	}
}

func TestBuildDeployment_PrefillNoRoutingProxy(t *testing.T) {
	ms := newTestModelService()
	ms.Spec.RoutingProxy = &modelv1alpha1.RoutingProxySpec{
		Connector:  "nixlv2",
		TargetPort: 8200,
	}
	role := &ms.Spec.Decode
	dep := BuildDeployment(ms, role, RolePrefill, "test-prefill", nil, nil, nil, TracingConfig{})

	initContainers := dep.Spec.Template.Spec.InitContainers
	if len(initContainers) != 0 {
		t.Fatalf("Prefill should have 0 init containers, got %d", len(initContainers))
	}

	vllm := dep.Spec.Template.Spec.Containers[0]
	for _, p := range vllm.Ports {
		if p.ContainerPort == 8200 {
			t.Error("Prefill vLLM should listen on engine port, not routing proxy target port")
		}
	}
}

func TestBuildDeployment_SecurityContext(t *testing.T) {
	ms := newTestModelService()
	role := &ms.Spec.Decode
	dep := BuildDeployment(ms, role, RoleDecode, "test", nil, nil, nil, TracingConfig{})

	podSec := dep.Spec.Template.Spec.SecurityContext
	if podSec == nil {
		t.Fatal("Missing pod security context")
	}
	if !*podSec.RunAsNonRoot {
		t.Error("RunAsNonRoot should be true")
	}

	vllm := dep.Spec.Template.Spec.Containers[0]
	if vllm.SecurityContext == nil {
		t.Fatal("Missing container security context")
	}
	if *vllm.SecurityContext.AllowPrivilegeEscalation {
		t.Error("AllowPrivilegeEscalation should be false")
	}
}

func TestBuildDeployment_CPUAccelerator(t *testing.T) {
	ms := newTestModelService()
	ms.Spec.Accelerator.Type = modelv1alpha1.AcceleratorCPU
	role := &ms.Spec.Decode
	dep := BuildDeployment(ms, role, RoleDecode, "test", nil, nil, nil, TracingConfig{})

	vllm := dep.Spec.Template.Spec.Containers[0]
	for k := range vllm.Resources.Limits {
		if k == "nvidia.com/gpu" || k == "amd.com/gpu" {
			t.Errorf("CPU mode should not have GPU resource %q", k)
		}
	}
}
