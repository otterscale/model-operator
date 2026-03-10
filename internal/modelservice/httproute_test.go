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

const testModelServiceName = "qwen3-32b"

var _ = Describe("BuildHTTPRoute", func() {
	It("should construct a valid HTTPRoute with same-namespace gateway", func() {
		ms := &modelv1alpha1.ModelService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testModelServiceName,
				Namespace: TestNamespace,
			},
			Spec: modelv1alpha1.ModelServiceSpec{
				HTTPRoute: &modelv1alpha1.HTTPRouteSpec{
					GatewayRef: modelv1alpha1.GatewayRef{
						Name: "inference-gateway",
					},
					Hostnames: []string{"models.example.com"},
				},
			},
		}

		labels := map[string]string{"app": testModelServiceName}
		route := BuildHTTPRoute(ms, labels)

		Expect(route.Name).To(Equal(testModelServiceName))
		Expect(route.Namespace).To(Equal(TestNamespace))

		Expect(route.Spec.ParentRefs).To(HaveLen(1))
		Expect(string(route.Spec.ParentRefs[0].Name)).To(Equal("inference-gateway"))
		Expect(route.Spec.ParentRefs[0].Namespace).To(BeNil(), "Same-namespace gateway should not set Namespace")

		Expect(route.Spec.Hostnames).To(HaveLen(1))
		Expect(string(route.Spec.Hostnames[0])).To(Equal("models.example.com"))

		Expect(route.Spec.Rules).To(HaveLen(1))
		backends := route.Spec.Rules[0].BackendRefs
		Expect(backends).To(HaveLen(1))
		ref := backends[0].BackendObjectReference
		Expect(ref.Kind).NotTo(BeNil())
		Expect(string(*ref.Kind)).To(Equal("InferencePool"))
		Expect(ref.Group).NotTo(BeNil())
		Expect(string(*ref.Group)).To(Equal("inference.networking.k8s.io"))
		Expect(string(ref.Name)).To(Equal(testModelServiceName))
	})

	It("should set namespace for cross-namespace gateway", func() {
		ms := &modelv1alpha1.ModelService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: TestNamespace,
			},
			Spec: modelv1alpha1.ModelServiceSpec{
				HTTPRoute: &modelv1alpha1.HTTPRouteSpec{
					GatewayRef: modelv1alpha1.GatewayRef{
						Name:      "shared-gateway",
						Namespace: "infra",
					},
				},
			},
		}

		route := BuildHTTPRoute(ms, nil)

		ref := route.Spec.ParentRefs[0]
		Expect(ref.Namespace).NotTo(BeNil(), "Cross-namespace gateway should set Namespace")
		Expect(string(*ref.Namespace)).To(Equal("infra"))
	})

	It("should omit hostnames when not specified", func() {
		ms := &modelv1alpha1.ModelService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "default",
			},
			Spec: modelv1alpha1.ModelServiceSpec{
				HTTPRoute: &modelv1alpha1.HTTPRouteSpec{
					GatewayRef: modelv1alpha1.GatewayRef{Name: "gw"},
				},
			},
		}

		route := BuildHTTPRoute(ms, nil)

		Expect(route.Spec.Hostnames).To(BeEmpty())
		Expect(string(route.Spec.Rules[0].BackendRefs[0].Name)).To(Equal("test"))
	})
})
