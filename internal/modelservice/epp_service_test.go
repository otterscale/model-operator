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

var _ = Describe("BuildEPPService", func() {
	It("should expose gRPC and metrics ports with correct selector", func() {
		ms := &modelv1alpha1.ModelService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "qwen3",
				Namespace: TestNamespace,
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

		Expect(svc.Name).To(Equal(TestEPPName))
		Expect(svc.Spec.Ports).To(HaveLen(2))

		hasGRPC := false
		hasMetrics := false
		for _, p := range svc.Spec.Ports {
			if p.Name == eppExtProcPortName && p.Port == 9002 {
				hasGRPC = true
			}
			if p.Name == "http-metrics" && p.Port == 9090 {
				hasMetrics = true
			}
		}
		Expect(hasGRPC).To(BeTrue(), "Missing grpc-ext-proc port 9002")
		Expect(hasMetrics).To(BeTrue(), "Missing http-metrics port 9090")
		Expect(svc.Spec.Selector["sel"]).To(Equal("epp"))
	})

	It("should use custom port when specified", func() {
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
			if p.Name == eppExtProcPortName {
				Expect(p.Port).To(Equal(int32(9999)))
			}
		}
	})
})
