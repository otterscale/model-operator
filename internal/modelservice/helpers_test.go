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
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// Shared test constants for modelservice package tests.
const (
	TestNamespace = "ml-serving"
	TestEPPName   = "qwen3-epp"
)

func TestGPUResourceName(t *testing.T) {
	tests := []struct {
		accel modelv1alpha1.AcceleratorType
		want  corev1.ResourceName
	}{
		{modelv1alpha1.AcceleratorNvidia, "nvidia.com/gpu"},
		{modelv1alpha1.AcceleratorAMD, "amd.com/gpu"},
		{modelv1alpha1.AcceleratorIntelGaudi, "habana.ai/gaudi"},
		{modelv1alpha1.AcceleratorGoogle, "google.com/tpu"},
		{modelv1alpha1.AcceleratorCPU, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.accel), func(t *testing.T) {
			got := GPUResourceName(tt.accel)
			if got != tt.want {
				t.Errorf("GPUResourceName(%q) = %q, want %q", tt.accel, got, tt.want)
			}
		})
	}
}

func TestGPUCount(t *testing.T) {
	tests := []struct {
		name string
		p    modelv1alpha1.ParallelismSpec
		want int32
	}{
		{"defaults", modelv1alpha1.ParallelismSpec{}, 1},
		{"tensor=4", modelv1alpha1.ParallelismSpec{Tensor: 4}, 4},
		{"tensor=4,dataLocal=2", modelv1alpha1.ParallelismSpec{Tensor: 4, DataLocal: 2}, 8},
		{"tensor=1,dataLocal=1", modelv1alpha1.ParallelismSpec{Tensor: 1, DataLocal: 1}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GPUCount(tt.p)
			if got != tt.want {
				t.Errorf("GPUCount() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestInjectGPUResources(t *testing.T) {
	t.Run("nvidia with 4 GPUs", func(t *testing.T) {
		res := &corev1.ResourceRequirements{}
		InjectGPUResources(res, modelv1alpha1.AcceleratorNvidia, 4)

		expected := resource.MustParse("4")
		if got := res.Limits[corev1.ResourceName("nvidia.com/gpu")]; !got.Equal(expected) {
			t.Errorf("GPU limits = %s, want %s", got.String(), expected.String())
		}
		if got := res.Requests[corev1.ResourceName("nvidia.com/gpu")]; !got.Equal(expected) {
			t.Errorf("GPU requests = %s, want %s", got.String(), expected.String())
		}
	})

	t.Run("CPU skips injection", func(t *testing.T) {
		res := &corev1.ResourceRequirements{}
		InjectGPUResources(res, modelv1alpha1.AcceleratorCPU, 4)

		if res.Limits != nil {
			t.Error("Expected nil Limits for CPU accelerator")
		}
	})

	t.Run("zero count skips injection", func(t *testing.T) {
		res := &corev1.ResourceRequirements{}
		InjectGPUResources(res, modelv1alpha1.AcceleratorNvidia, 0)

		if res.Limits != nil {
			t.Error("Expected nil Limits for zero count")
		}
	})
}

func TestNaming(t *testing.T) {
	if got := DecodeName("qwen3"); got != "qwen3-decode" {
		t.Errorf("DecodeName = %q", got)
	}
	if got := PrefillName("qwen3"); got != "qwen3-prefill" {
		t.Errorf("PrefillName = %q", got)
	}
	if got := InferencePoolName("qwen3"); got != "qwen3" {
		t.Errorf("InferencePoolName = %q", got)
	}
	if got := PodMonitorName("qwen3", "decode"); got != "qwen3-decode" {
		t.Errorf("PodMonitorName = %q", got)
	}
	if got := EPPName("qwen3"); got != "qwen3-epp" {
		t.Errorf("EPPName = %q", got)
	}
	if got := EPPConfigMapName("qwen3"); got != "qwen3-epp-config" {
		t.Errorf("EPPConfigMapName = %q", got)
	}
	if got := EPPSecretName("qwen3"); got != "qwen3-epp-sa-metrics-reader-secret" {
		t.Errorf("EPPSecretName = %q", got)
	}
	if got := EPPServiceMonitorName("qwen3"); got != "qwen3-epp" {
		t.Errorf("EPPServiceMonitorName = %q", got)
	}
}

func TestInferencePoolSelectorLabels(t *testing.T) {
	selectorLabels := InferencePoolSelectorLabels("qwen3")

	val, ok := selectorLabels[LabelInferenceServer]
	if !ok {
		t.Error("Missing llm-d.ai/inference-serving label")
	}
	if val != LabelValueTrue {
		t.Errorf("Expected llm-d.ai/inference-serving=%s, got %q", LabelValueTrue, val)
	}
}
