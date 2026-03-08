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

func TestBuildEPPRBAC_SingleReplica(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qwen3",
			Namespace: "ml-serving",
		},
	}

	role, binding := BuildEPPRBAC(ms, nil, 1)

	if role.Name != "qwen3-epp" {
		t.Errorf("Role name = %q, want qwen3-epp", role.Name)
	}
	if len(role.Rules) != 3 {
		t.Fatalf("Role rules = %d, want 3 (pods + experimental + GA)", len(role.Rules))
	}

	// Rule 0: pods
	r0 := role.Rules[0]
	if r0.APIGroups[0] != "" {
		t.Errorf("Rule[0] APIGroup = %q, want empty (core)", r0.APIGroups[0])
	}
	if r0.Resources[0] != "pods" {
		t.Errorf("Rule[0] resource = %q, want pods", r0.Resources[0])
	}

	// Rule 1: experimental GAIE resources
	r1 := role.Rules[1]
	if r1.APIGroups[0] != "inference.networking.x-k8s.io" {
		t.Errorf("Rule[1] APIGroup = %q", r1.APIGroups[0])
	}
	if len(r1.Resources) != 2 {
		t.Errorf("Rule[1] resources = %v", r1.Resources)
	}

	// Rule 2: GA InferencePool
	r2 := role.Rules[2]
	if r2.APIGroups[0] != "inference.networking.k8s.io" {
		t.Errorf("Rule[2] APIGroup = %q", r2.APIGroups[0])
	}
	if r2.Resources[0] != "inferencepools" {
		t.Errorf("Rule[2] resource = %q", r2.Resources[0])
	}

	// No leader election rules for single replica
	for _, rule := range role.Rules {
		for _, g := range rule.APIGroups {
			if g == "coordination.k8s.io" {
				t.Error("Single replica should not have leader election RBAC")
			}
		}
	}

	// RoleBinding checks
	if binding.Name != "qwen3-epp" {
		t.Errorf("RoleBinding name = %q, want qwen3-epp", binding.Name)
	}
	if len(binding.Subjects) != 1 {
		t.Fatalf("Subjects = %d, want 1", len(binding.Subjects))
	}
	if binding.Subjects[0].Name != "qwen3-epp" {
		t.Errorf("Subject name = %q, want qwen3-epp", binding.Subjects[0].Name)
	}
	if binding.RoleRef.Name != "qwen3-epp" {
		t.Errorf("RoleRef name = %q, want qwen3-epp", binding.RoleRef.Name)
	}
}

func TestBuildEPPRBAC_MultipleReplicas(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qwen3",
			Namespace: "ml-serving",
		},
	}

	role, _ := BuildEPPRBAC(ms, nil, 3)

	if len(role.Rules) != 5 {
		t.Fatalf("Role rules = %d, want 5 (pods + experimental + GA + leases + events)", len(role.Rules))
	}

	hasLeases := false
	hasEvents := false
	for _, rule := range role.Rules {
		for _, g := range rule.APIGroups {
			if g == "coordination.k8s.io" {
				for _, r := range rule.Resources {
					if r == "leases" {
						hasLeases = true
					}
				}
			}
		}
		for _, r := range rule.Resources {
			if r == "events" {
				hasEvents = true
			}
		}
	}
	if !hasLeases {
		t.Error("Multi-replica should have leases RBAC")
	}
	if !hasEvents {
		t.Error("Multi-replica should have events RBAC")
	}
}
