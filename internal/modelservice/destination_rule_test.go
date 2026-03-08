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

	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

func TestBuildDestinationRule(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qwen3",
			Namespace: "ml-serving",
		},
	}

	labels := map[string]string{"component": "epp"}
	dr := BuildDestinationRule(ms, labels)

	if dr.Name != "qwen3-epp" {
		t.Errorf("Name = %q, want qwen3-epp", dr.Name)
	}
	if dr.Namespace != "ml-serving" {
		t.Errorf("Namespace = %q, want ml-serving", dr.Namespace)
	}

	expectedHost := "qwen3-epp.ml-serving.svc.cluster.local"
	if dr.Spec.Host != expectedHost {
		t.Errorf("Host = %q, want %q", dr.Spec.Host, expectedHost)
	}

	if dr.Spec.TrafficPolicy == nil {
		t.Fatal("TrafficPolicy should not be nil")
	}
	if dr.Spec.TrafficPolicy.Tls == nil {
		t.Fatal("TrafficPolicy.Tls should not be nil")
	}
	if dr.Spec.TrafficPolicy.Tls.Mode != networkingv1alpha3.ClientTLSSettings_SIMPLE {
		t.Errorf("TLS mode = %v, want SIMPLE", dr.Spec.TrafficPolicy.Tls.Mode)
	}
	if dr.Spec.TrafficPolicy.Tls.InsecureSkipVerify == nil || !dr.Spec.TrafficPolicy.Tls.InsecureSkipVerify.Value {
		t.Error("InsecureSkipVerify should be true")
	}
}
