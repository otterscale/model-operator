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
	"fmt"

	wrapperspb "github.com/golang/protobuf/ptypes/wrappers"
	networkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istionetworkingv1beta1 "istio.io/client-go/pkg/apis/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// BuildDestinationRule constructs an Istio DestinationRule for the EPP Service.
//
// The rule enables TLS SIMPLE mode with insecureSkipVerify so the Istio sidecar
// can communicate with the EPP service over mTLS without needing a custom CA cert.
func BuildDestinationRule(
	ms *modelv1alpha1.ModelService,
	metadataLabels map[string]string,
) *istionetworkingv1beta1.DestinationRule {
	eppName := EPPName(ms.Name)
	host := fmt.Sprintf("%s.%s.svc.cluster.local", eppName, ms.Namespace)

	return &istionetworkingv1beta1.DestinationRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      eppName,
			Namespace: ms.Namespace,
			Labels:    metadataLabels,
		},
		Spec: networkingv1alpha3.DestinationRule{
			Host: host,
			TrafficPolicy: &networkingv1alpha3.TrafficPolicy{
				Tls: &networkingv1alpha3.ClientTLSSettings{
					Mode:               networkingv1alpha3.ClientTLSSettings_SIMPLE,
					InsecureSkipVerify: &wrapperspb.BoolValue{Value: true},
				},
			},
		},
	}
}
