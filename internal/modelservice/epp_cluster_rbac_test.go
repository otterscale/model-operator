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

func TestBuildEPPClusterRBAC(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qwen3",
			Namespace: "ml-serving",
		},
	}
	labels := map[string]string{"app": "epp"}

	role, binding := BuildEPPClusterRBAC(ms, labels)

	expectedName := "ml-serving-qwen3-epp"
	if role.Name != expectedName {
		t.Errorf("ClusterRole name = %q, want %q", role.Name, expectedName)
	}
	if binding.Name != expectedName {
		t.Errorf("ClusterRoleBinding name = %q, want %q", binding.Name, expectedName)
	}

	if len(role.Rules) != 3 {
		t.Fatalf("ClusterRole rules = %d, want 3", len(role.Rules))
	}

	hasTokenReviews := false
	hasSAR := false
	hasMetrics := false
	for _, rule := range role.Rules {
		for _, r := range rule.Resources {
			if r == "tokenreviews" {
				hasTokenReviews = true
			}
			if r == "subjectaccessreviews" {
				hasSAR = true
			}
		}
		for _, url := range rule.NonResourceURLs {
			if url == "/metrics" {
				hasMetrics = true
			}
		}
	}
	if !hasTokenReviews {
		t.Error("Missing tokenreviews rule")
	}
	if !hasSAR {
		t.Error("Missing subjectaccessreviews rule")
	}
	if !hasMetrics {
		t.Error("Missing /metrics nonResourceURL rule")
	}

	if len(binding.Subjects) != 1 {
		t.Fatalf("Subjects = %d, want 1", len(binding.Subjects))
	}
	if binding.Subjects[0].Name != "qwen3-epp" {
		t.Errorf("Subject name = %q, want qwen3-epp", binding.Subjects[0].Name)
	}
	if binding.Subjects[0].Namespace != "ml-serving" {
		t.Errorf("Subject namespace = %q, want ml-serving", binding.Subjects[0].Namespace)
	}
	if binding.RoleRef.Kind != "ClusterRole" {
		t.Errorf("RoleRef kind = %q, want ClusterRole", binding.RoleRef.Kind)
	}
	if binding.RoleRef.Name != expectedName {
		t.Errorf("RoleRef name = %q, want %q", binding.RoleRef.Name, expectedName)
	}
}

func TestEPPClusterRBACName(t *testing.T) {
	name := EPPClusterRBACName("my-namespace", "my-model")
	if name != "my-namespace-my-model-epp" {
		t.Errorf("EPPClusterRBACName = %q, want my-namespace-my-model-epp", name)
	}
}
