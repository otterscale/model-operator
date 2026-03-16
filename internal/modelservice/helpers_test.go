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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// Shared test constants for modelservice package tests.
const (
	TestNamespace = "ml-serving"
	TestEPPName   = "qwen3-epp"
)

var _ = Describe("GPUResourceName", func() {
	DescribeTable("maps accelerator types to Kubernetes resource names",
		func(accel modelv1alpha1.AcceleratorType, want corev1.ResourceName) {
			Expect(GPUResourceName(accel)).To(Equal(want))
		},
		Entry("Nvidia", modelv1alpha1.AcceleratorNvidia, corev1.ResourceName("nvidia.com/gpu")),
		Entry("AMD", modelv1alpha1.AcceleratorAMD, corev1.ResourceName("amd.com/gpu")),
		Entry("Intel Gaudi", modelv1alpha1.AcceleratorIntelGaudi, corev1.ResourceName("habana.ai/gaudi")),
		Entry("Google TPU", modelv1alpha1.AcceleratorGoogle, corev1.ResourceName("google.com/tpu")),
		Entry("CPU returns empty", modelv1alpha1.AcceleratorCPU, corev1.ResourceName("")),
	)
})

var _ = Describe("GPUCount", func() {
	DescribeTable("calculates GPU count from parallelism",
		func(p modelv1alpha1.ParallelismSpec, want int32) {
			Expect(GPUCount(p)).To(Equal(want))
		},
		Entry("defaults", modelv1alpha1.ParallelismSpec{}, int32(1)),
		Entry("tensor=4", modelv1alpha1.ParallelismSpec{Tensor: 4}, int32(4)),
		Entry("tensor=4,dataLocal=2", modelv1alpha1.ParallelismSpec{Tensor: 4, DataLocal: 2}, int32(8)),
		Entry("tensor=1,dataLocal=1", modelv1alpha1.ParallelismSpec{Tensor: 1, DataLocal: 1}, int32(1)),
	)
})

var _ = Describe("InjectGPUResources", func() {
	It("should inject nvidia GPU limits and requests", func() {
		res := &corev1.ResourceRequirements{}
		InjectGPUResources(res, modelv1alpha1.AcceleratorNvidia, 4)

		expected := resource.MustParse("4")
		Expect(res.Limits[corev1.ResourceName("nvidia.com/gpu")]).To(Equal(expected))
		Expect(res.Requests[corev1.ResourceName("nvidia.com/gpu")]).To(Equal(expected))
	})

	It("should skip injection for CPU accelerator", func() {
		res := &corev1.ResourceRequirements{}
		InjectGPUResources(res, modelv1alpha1.AcceleratorCPU, 4)

		Expect(res.Limits).To(BeNil())
	})

	It("should skip injection for zero count", func() {
		res := &corev1.ResourceRequirements{}
		InjectGPUResources(res, modelv1alpha1.AcceleratorNvidia, 0)

		Expect(res.Limits).To(BeNil())
	})
})

var _ = Describe("Naming helpers", func() {
	It("should generate correct DecodeName", func() {
		Expect(DecodeName("qwen3")).To(Equal("qwen3-decode"))
	})
	It("should generate correct PrefillName", func() {
		Expect(PrefillName("qwen3")).To(Equal("qwen3-prefill"))
	})
	It("should generate correct InferencePoolName", func() {
		Expect(InferencePoolName("qwen3")).To(Equal("qwen3"))
	})
	It("should generate correct PodMonitorName", func() {
		Expect(PodMonitorName("qwen3", "decode")).To(Equal("qwen3-decode"))
	})
	It("should generate correct EPPName", func() {
		Expect(EPPName("qwen3")).To(Equal("qwen3-epp"))
	})
	It("should generate DNS-1035 EPP Service name (no dots)", func() {
		Expect(EPPNameForService("qwen3-0.6b-fp8-dynamic")).To(Equal("qwen3-0-6b-fp8-dynamic-epp"))
		Expect(EPPNameForService("qwen3")).To(Equal("qwen3-epp"))
	})
	It("should sanitize DNS-1035 label", func() {
		Expect(SanitizeDNS1035Label("qwen3-0.6b-fp8-dynamic")).To(Equal("qwen3-0-6b-fp8-dynamic"))
		Expect(SanitizeDNS1035Label("MyModel_1.0")).To(Equal("mymodel-1-0"))
		Expect(SanitizeDNS1035Label("0.6b")).To(Equal("m-0-6b"))
	})
	It("should generate correct EPPConfigMapName", func() {
		Expect(EPPConfigMapName("qwen3")).To(Equal("qwen3-epp-config"))
	})
	It("should generate correct EPPSecretName", func() {
		Expect(EPPSecretName("qwen3")).To(Equal("qwen3-epp-sa-metrics-reader-secret"))
	})
	It("should generate correct EPPServiceMonitorName", func() {
		Expect(EPPServiceMonitorName("qwen3")).To(Equal("qwen3-epp"))
	})
})

var _ = Describe("InferencePoolSelectorLabels", func() {
	It("should include the inference-serving label", func() {
		selectorLabels := InferencePoolSelectorLabels("qwen3")

		Expect(selectorLabels).To(HaveKeyWithValue(LabelInferenceServer, LabelValueTrue))
	})
})
