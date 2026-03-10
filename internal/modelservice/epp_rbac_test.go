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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

var _ = Describe("BuildEPPRBAC", func() {
	var ms *modelv1alpha1.ModelService

	BeforeEach(func() {
		ms = &modelv1alpha1.ModelService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "qwen3",
				Namespace: TestNamespace,
			},
		}
	})

	Context("single replica", func() {
		It("should create Role with 3 rules (pods + experimental + GA)", func() {
			role, _ := BuildEPPRBAC(ms, nil, 1)

			Expect(role.Name).To(Equal(TestEPPName))
			Expect(role.Rules).To(HaveLen(3))

			// Rule 0: pods
			r0 := role.Rules[0]
			Expect(r0.APIGroups[0]).To(BeEmpty(), "Rule[0] should be core API group")
			Expect(r0.Resources[0]).To(Equal("pods"))

			// Rule 1: experimental GAIE resources
			r1 := role.Rules[1]
			Expect(r1.APIGroups[0]).To(Equal("inference.networking.x-k8s.io"))
			Expect(r1.Resources).To(HaveLen(2))

			// Rule 2: GA InferencePool
			r2 := role.Rules[2]
			Expect(r2.APIGroups[0]).To(Equal("inference.networking.k8s.io"))
			Expect(r2.Resources[0]).To(Equal("inferencepools"))
		})

		It("should not include leader election RBAC", func() {
			role, _ := BuildEPPRBAC(ms, nil, 1)

			for _, rule := range role.Rules {
				for _, g := range rule.APIGroups {
					Expect(g).NotTo(Equal("coordination.k8s.io"),
						"Single replica should not have leader election RBAC")
				}
			}
		})

		It("should create correct RoleBinding", func() {
			_, binding := BuildEPPRBAC(ms, nil, 1)

			Expect(binding.Name).To(Equal(TestEPPName))
			Expect(binding.Subjects).To(HaveLen(1))
			Expect(binding.Subjects[0].Name).To(Equal(TestEPPName))
			Expect(binding.RoleRef.Name).To(Equal(TestEPPName))
		})
	})

	Context("multiple replicas", func() {
		It("should include leader election RBAC (leases + events)", func() {
			role, _ := BuildEPPRBAC(ms, nil, 3)

			Expect(role.Rules).To(HaveLen(5))

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
			Expect(hasLeases).To(BeTrue(), "Multi-replica should have leases RBAC")
			Expect(hasEvents).To(BeTrue(), "Multi-replica should have events RBAC")
		})
	})
})
