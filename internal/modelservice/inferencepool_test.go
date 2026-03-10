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
	inferenceextv1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

var _ = Describe("BuildInferencePool", func() {
	It("should construct a valid InferencePool with explicit values", func() {
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

		Expect(pool.Name).To(Equal("qwen3-32b"))
		Expect(pool.Namespace).To(Equal(TestNamespace))

		Expect(pool.Spec.TargetPorts).To(HaveLen(1))
		Expect(pool.Spec.TargetPorts[0].Number).To(Equal(inferenceextv1.PortNumber(8000)))

		eppRef := pool.Spec.EndpointPickerRef
		Expect(string(eppRef.Name)).To(Equal("qwen3-32b-epp"))
		Expect(eppRef.Port).NotTo(BeNil())
		Expect(eppRef.Port.Number).To(Equal(inferenceextv1.PortNumber(9002)))
		Expect(eppRef.FailureMode).To(Equal(inferenceextv1.EndpointPickerFailOpen))
	})

	It("should use default values when not specified", func() {
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
		Expect(pool.Spec.TargetPorts[0].Number).To(Equal(inferenceextv1.PortNumber(8000)))
		// Default EPP port = 9002
		Expect(pool.Spec.EndpointPickerRef.Port.Number).To(Equal(inferenceextv1.PortNumber(9002)))
		// Default failure mode = FailOpen
		Expect(pool.Spec.EndpointPickerRef.FailureMode).To(Equal(inferenceextv1.EndpointPickerFailOpen))
	})

	It("should include the inference-serving selector label", func() {
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

		Expect(pool.Spec.Selector.MatchLabels).NotTo(BeEmpty())
		val, ok := pool.Spec.Selector.MatchLabels[inferenceextv1.LabelKey(LabelInferenceServer)]
		Expect(ok).To(BeTrue(), "Missing llm-d.ai/inference-serving label in selector")
		Expect(string(val)).To(Equal(LabelValueTrue))
	})
})
