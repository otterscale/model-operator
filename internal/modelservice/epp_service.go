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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// DefaultEPPExtProcPort is the default gRPC extProc port when InferencePool is not set.
const DefaultEPPExtProcPort = int32(9002)

// BuildEPPService constructs the Service exposing the EPP's extProc and
// metrics ports. Safe to call when ms.Spec.InferencePool is nil (uses default port).
func BuildEPPService(
	ms *modelv1alpha1.ModelService,
	metadataLabels map[string]string,
	selectorLabels map[string]string,
) *corev1.Service {
	extProcPort := DefaultEPPExtProcPort
	if ms.Spec.InferencePool != nil && ms.Spec.InferencePool.EndpointPicker.Port > 0 {
		extProcPort = ms.Spec.InferencePool.EndpointPicker.Port
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      EPPNameForService(ms.Name),
			Namespace: ms.Namespace,
			Labels:    metadataLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selectorLabels,
			Ports: []corev1.ServicePort{
				{
					Name:       eppExtProcPortName,
					Port:       extProcPort,
					TargetPort: intstr.FromInt32(extProcPort),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       eppMetricsPortName,
					Port:       eppMetricsPort,
					TargetPort: intstr.FromInt32(eppMetricsPort),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}
