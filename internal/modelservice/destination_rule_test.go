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

	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

var _ = Describe("BuildDestinationRule", func() {
	It("should construct a valid DestinationRule for the EPP Service", func() {
		ms := &modelv1alpha1.ModelService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "qwen3",
				Namespace: TestNamespace,
			},
		}

		labels := map[string]string{"component": "epp"}
		dr := BuildDestinationRule(ms, labels)

		Expect(dr.Name).To(Equal(TestEPPName))
		Expect(dr.Namespace).To(Equal(TestNamespace))

		expectedHost := TestEPPName + "." + TestNamespace + ".svc.cluster.local"
		Expect(dr.Spec.Host).To(Equal(expectedHost))

		Expect(dr.Spec.TrafficPolicy).NotTo(BeNil())
		Expect(dr.Spec.TrafficPolicy.Tls).NotTo(BeNil())
		Expect(dr.Spec.TrafficPolicy.Tls.Mode).To(Equal(networkingv1alpha3.ClientTLSSettings_SIMPLE))
		Expect(dr.Spec.TrafficPolicy.Tls.InsecureSkipVerify).NotTo(BeNil())
		Expect(dr.Spec.TrafficPolicy.Tls.InsecureSkipVerify.Value).To(BeTrue())
	})
})
