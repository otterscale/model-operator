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

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

func TestBuildEPPService(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qwen3",
			Namespace: "ml-serving",
		},
		Spec: modelv1alpha1.ModelServiceSpec{
			InferencePool: &modelv1alpha1.InferencePoolSpec{
				EndpointPicker: modelv1alpha1.EndpointPickerSpec{
					Port: 9002,
				},
			},
		},
	}

	svc := BuildEPPService(ms, nil, map[string]string{"sel": "epp"})

	if svc.Name != "qwen3-epp" {
		t.Errorf("Name = %q, want qwen3-epp", svc.Name)
	}

	if len(svc.Spec.Ports) != 2 {
		t.Fatalf("Ports = %d, want 2", len(svc.Spec.Ports))
	}

	hasGRPC := false
	hasMetrics := false
	for _, p := range svc.Spec.Ports {
		if p.Name == "grpc-ext-proc" && p.Port == 9002 {
			hasGRPC = true
		}
		if p.Name == "http-metrics" && p.Port == 9090 {
			hasMetrics = true
		}
	}
	if !hasGRPC {
		t.Error("Missing grpc-ext-proc port 9002")
	}
	if !hasMetrics {
		t.Error("Missing http-metrics port 9090")
	}

	if svc.Spec.Selector["sel"] != "epp" {
		t.Error("Selector should match EPP pods")
	}
}

func TestBuildEPPService_CustomPort(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: modelv1alpha1.ModelServiceSpec{
			InferencePool: &modelv1alpha1.InferencePoolSpec{
				EndpointPicker: modelv1alpha1.EndpointPickerSpec{
					Port: 9999,
				},
			},
		},
	}

	svc := BuildEPPService(ms, nil, nil)

	for _, p := range svc.Spec.Ports {
		if p.Name == "grpc-ext-proc" && p.Port != 9999 {
			t.Errorf("gRPC port = %d, want 9999", p.Port)
		}
	}
}
