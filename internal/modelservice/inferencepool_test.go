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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	inferenceextv1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

func TestBuildInferencePool(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qwen3-32b",
			Namespace: TestNamespace,
		},
		Spec: modelv1alpha1.ModelServiceSpec{
			Engine: modelv1alpha1.EngineSpec{Port: 8000},
			InferencePool: &modelv1alpha1.InferencePoolSpec{
				EndpointPicker: modelv1alpha1.EndpointPickerSpec{
					Port:        9002,
					FailureMode: modelv1alpha1.EPPFailureModeOpen,
				},
			},
		},
	}

	labels := map[string]string{"app": "qwen3-32b"}
	pool := BuildInferencePool(ms, labels)

	if pool.Name != "qwen3-32b" {
		t.Errorf("Name = %q, want qwen3-32b", pool.Name)
	}
	if pool.Namespace != TestNamespace {
		t.Errorf("Namespace = %q, want %s", pool.Namespace, TestNamespace)
	}

	if len(pool.Spec.TargetPorts) != 1 {
		t.Fatalf("TargetPorts len = %d, want 1", len(pool.Spec.TargetPorts))
	}
	if pool.Spec.TargetPorts[0].Number != 8000 {
		t.Errorf("TargetPort = %d, want 8000", pool.Spec.TargetPorts[0].Number)
	}

	eppRef := pool.Spec.EndpointPickerRef
	if string(eppRef.Name) != "qwen3-32b-epp" {
		t.Errorf("EPP name = %q, want qwen3-32b-epp", eppRef.Name)
	}
	if eppRef.Port == nil || eppRef.Port.Number != 9002 {
		t.Errorf("EPP port = %v, want 9002", eppRef.Port)
	}
	if eppRef.FailureMode != inferenceextv1.EndpointPickerFailOpen {
		t.Errorf("FailureMode = %q, want FailOpen", eppRef.FailureMode)
	}
}

func TestBuildInferencePool_DefaultValues(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: modelv1alpha1.ModelServiceSpec{
			Engine: modelv1alpha1.EngineSpec{},
			InferencePool: &modelv1alpha1.InferencePoolSpec{
				EndpointPicker: modelv1alpha1.EndpointPickerSpec{},
			},
		},
	}

	pool := BuildInferencePool(ms, nil)

	// Default engine port = 8000
	if pool.Spec.TargetPorts[0].Number != 8000 {
		t.Errorf("Default TargetPort = %d, want 8000", pool.Spec.TargetPorts[0].Number)
	}

	// Default EPP port = 9002
	if pool.Spec.EndpointPickerRef.Port.Number != 9002 {
		t.Errorf("Default EPP port = %d, want 9002", pool.Spec.EndpointPickerRef.Port.Number)
	}

	// Default failure mode = FailOpen
	if pool.Spec.EndpointPickerRef.FailureMode != inferenceextv1.EndpointPickerFailOpen {
		t.Errorf("Default FailureMode = %q, want FailOpen", pool.Spec.EndpointPickerRef.FailureMode)
	}
}

func TestBuildInferencePool_SelectorLabels(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "default",
		},
		Spec: modelv1alpha1.ModelServiceSpec{
			Engine: modelv1alpha1.EngineSpec{},
			InferencePool: &modelv1alpha1.InferencePoolSpec{
				EndpointPicker: modelv1alpha1.EndpointPickerSpec{},
			},
		},
	}

	pool := BuildInferencePool(ms, nil)

	if len(pool.Spec.Selector.MatchLabels) == 0 {
		t.Fatal("Selector.MatchLabels should not be empty")
	}

	val, ok := pool.Spec.Selector.MatchLabels[inferenceextv1.LabelKey(LabelInferenceServer)]
	if !ok {
		t.Error("Missing llm-d.ai/inference-serving label in selector")
	}
	if string(val) != LabelValueTrue {
		t.Errorf("LabelInferenceServer = %q, want %q", val, LabelValueTrue)
	}
}
