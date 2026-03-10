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

var _ = Describe("BuildEPPClusterRBAC", func() {
	var (
		ms     *modelv1alpha1.ModelService
		labels map[string]string
	)

	BeforeEach(func() {
		ms = &modelv1alpha1.ModelService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "qwen3",
				Namespace: TestNamespace,
			},
		}
		labels = map[string]string{"app": "epp"}
	})

	It("should set correct names for ClusterRole and ClusterRoleBinding", func() {
		role, binding := BuildEPPClusterRBAC(ms, labels)

		expectedName := TestNamespace + "-" + TestEPPName
		Expect(role.Name).To(Equal(expectedName))
		Expect(binding.Name).To(Equal(expectedName))
	})

	It("should include tokenreviews, subjectaccessreviews, and /metrics rules", func() {
		role, _ := BuildEPPClusterRBAC(ms, labels)

		Expect(role.Rules).To(HaveLen(3))

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
		Expect(hasTokenReviews).To(BeTrue(), "Missing tokenreviews rule")
		Expect(hasSAR).To(BeTrue(), "Missing subjectaccessreviews rule")
		Expect(hasMetrics).To(BeTrue(), "Missing /metrics nonResourceURL rule")
	})

	It("should bind to the EPP ServiceAccount", func() {
		_, binding := BuildEPPClusterRBAC(ms, labels)

		expectedName := TestNamespace + "-" + TestEPPName
		Expect(binding.Subjects).To(HaveLen(1))
		Expect(binding.Subjects[0].Name).To(Equal(TestEPPName))
		Expect(binding.Subjects[0].Namespace).To(Equal(TestNamespace))
		Expect(binding.RoleRef.Kind).To(Equal("ClusterRole"))
		Expect(binding.RoleRef.Name).To(Equal(expectedName))
	})
})

var _ = Describe("EPPClusterRBACName", func() {
	It("should embed namespace to avoid collisions", func() {
		Expect(EPPClusterRBACName("my-namespace", "my-model")).To(Equal("my-namespace-my-model-epp"))
	})
})
