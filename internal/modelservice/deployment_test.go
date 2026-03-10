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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

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

var _ = Describe("BuildDeployment", func() {
	It("should set basic fields correctly", func() {
		ms := newTestModelService()
		role := &ms.Spec.Decode
		podLabels := map[string]string{"role": "decode"}
		metaLabels := map[string]string{"app": "qwen3"}
		selLabels := map[string]string{"app": "qwen3"}

		dep := BuildDeployment(ms, role, RoleDecode, "qwen3-32b-decode", podLabels, metaLabels, selLabels, TracingConfig{})

		Expect(dep.Name).To(Equal("qwen3-32b-decode"))
		Expect(dep.Namespace).To(Equal(TestNamespace))
		Expect(*dep.Spec.Replicas).To(Equal(int32(2)))

		Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
		vllm := dep.Spec.Template.Spec.Containers[0]
		Expect(vllm.Name).To(Equal("vllm"))
		Expect(vllm.Image).To(Equal("vllm/vllm-openai:v0.8.0"))
		Expect(slices.Contains(vllm.Args, "--tensor-parallel-size")).To(BeTrue(), "Missing --tensor-parallel-size in args")
		Expect(slices.Contains(vllm.Args, "--max-model-len=8192")).To(BeTrue(), "Missing user-provided arg --max-model-len=8192")

		gpuRes, ok := vllm.Resources.Limits["nvidia.com/gpu"]
		Expect(ok).To(BeTrue(), "Missing nvidia.com/gpu in limits")
		Expect(gpuRes.Value()).To(Equal(int64(4)))
	})

	It("should mount the model as an image volume", func() {
		ms := newTestModelService()
		role := &ms.Spec.Decode
		dep := BuildDeployment(ms, role, RoleDecode, "test-decode", nil, nil, nil, TracingConfig{})

		volumes := dep.Spec.Template.Spec.Volumes
		Expect(volumes).To(HaveLen(1))

		v := volumes[0]
		Expect(v.Name).To(Equal(ModelVolumeName))
		Expect(v.Image).NotTo(BeNil())
		Expect(v.Image.Reference).To(Equal("registry.example.com/models/qwen3-32b:v1"))
		Expect(v.Image.PullPolicy).To(Equal(corev1.PullIfNotPresent))

		vllm := dep.Spec.Template.Spec.Containers[0]
		Expect(vllm.VolumeMounts).To(HaveLen(1))
		Expect(vllm.VolumeMounts[0].MountPath).To(Equal(testMountPath))
		Expect(vllm.VolumeMounts[0].ReadOnly).To(BeTrue())
	})

	It("should inject routing proxy as native sidecar for decode role", func() {
		ms := newTestModelService()
		ms.Spec.RoutingProxy = &modelv1alpha1.RoutingProxySpec{
			Connector:  "nixlv2",
			TargetPort: 8200,
		}
		role := &ms.Spec.Decode
		dep := BuildDeployment(ms, role, RoleDecode, "test-decode", nil, nil, nil, TracingConfig{})

		initContainers := dep.Spec.Template.Spec.InitContainers
		Expect(initContainers).To(HaveLen(1))

		proxy := initContainers[0]
		Expect(proxy.Name).To(Equal("routing-proxy"))
		Expect(proxy.RestartPolicy).NotTo(BeNil())
		Expect(*proxy.RestartPolicy).To(Equal(corev1.ContainerRestartPolicyAlways))
		Expect(slices.Contains(proxy.Args, "--connector")).To(BeTrue(), "Missing --connector in proxy args")

		vllm := dep.Spec.Template.Spec.Containers[0]
		foundPort := false
		for _, p := range vllm.Ports {
			if p.ContainerPort == 8200 {
				foundPort = true
			}
		}
		Expect(foundPort).To(BeTrue(), "vLLM should listen on targetPort 8200 when proxy is enabled")
	})

	It("should not inject routing proxy for prefill role", func() {
		ms := newTestModelService()
		ms.Spec.RoutingProxy = &modelv1alpha1.RoutingProxySpec{
			Connector:  "nixlv2",
			TargetPort: 8200,
		}
		role := &ms.Spec.Decode
		dep := BuildDeployment(ms, role, RolePrefill, "test-prefill", nil, nil, nil, TracingConfig{})

		Expect(dep.Spec.Template.Spec.InitContainers).To(BeEmpty())

		vllm := dep.Spec.Template.Spec.Containers[0]
		for _, p := range vllm.Ports {
			Expect(p.ContainerPort).NotTo(Equal(int32(8200)), "Prefill vLLM should listen on engine port, not routing proxy target port")
		}
	})

	It("should set security context correctly", func() {
		ms := newTestModelService()
		role := &ms.Spec.Decode
		dep := BuildDeployment(ms, role, RoleDecode, "test", nil, nil, nil, TracingConfig{})

		podSec := dep.Spec.Template.Spec.SecurityContext
		Expect(podSec).NotTo(BeNil())
		Expect(*podSec.RunAsNonRoot).To(BeTrue())

		vllm := dep.Spec.Template.Spec.Containers[0]
		Expect(vllm.SecurityContext).NotTo(BeNil())
		Expect(*vllm.SecurityContext.AllowPrivilegeEscalation).To(BeFalse())
	})

	It("should not inject GPU resources for CPU accelerator", func() {
		ms := newTestModelService()
		ms.Spec.Accelerator.Type = modelv1alpha1.AcceleratorCPU
		role := &ms.Spec.Decode
		dep := BuildDeployment(ms, role, RoleDecode, "test", nil, nil, nil, TracingConfig{})

		vllm := dep.Spec.Template.Spec.Containers[0]
		for k := range vllm.Resources.Limits {
			Expect(k).NotTo(BeElementOf(corev1.ResourceName("nvidia.com/gpu"), corev1.ResourceName("amd.com/gpu")),
				"CPU mode should not have GPU resources")
		}
	})
})
