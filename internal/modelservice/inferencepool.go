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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	inferenceextv1 "sigs.k8s.io/gateway-api-inference-extension/api/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// ptrToGroup returns a pointer to the given Group (Core API group when s is "").
func ptrToGroup(s string) *inferenceextv1.Group {
	g := inferenceextv1.Group(s)
	return &g
}

// BuildDefaultInferencePool constructs a default InferencePool when ms.Spec.InferencePool is nil.
// The pool name is InferencePoolName(ms.Name) (no -epp suffix) so it matches the default
// HTTPRoute backendRef.name and the EPP --pool-name flag.
func BuildDefaultInferencePool(
	ms *modelv1alpha1.ModelService,
	metadataLabels map[string]string,
) *inferenceextv1.InferencePool {
	poolName := InferencePoolName(ms.Name)
	eppName := EPPName(ms.Name)
	selectorLabels := InferencePoolSelectorLabels(ms.Name)
	matchLabels := make(map[inferenceextv1.LabelKey]inferenceextv1.LabelValue, len(selectorLabels))
	for k, v := range selectorLabels {
		matchLabels[inferenceextv1.LabelKey(k)] = inferenceextv1.LabelValue(v)
	}
	port := enginePort(ms)
	return &inferenceextv1.InferencePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      poolName,
			Namespace: ms.Namespace,
			Labels:    metadataLabels,
		},
		Spec: inferenceextv1.InferencePoolSpec{
			TargetPorts: []inferenceextv1.Port{
				{Number: inferenceextv1.PortNumber(port)},
			},
			Selector: inferenceextv1.LabelSelector{MatchLabels: matchLabels},
			EndpointPickerRef: inferenceextv1.EndpointPickerRef{
				Group:       ptrToGroup(""),
				Kind:        inferenceextv1.Kind("Service"),
				Name:        inferenceextv1.ObjectName(eppName),
				Port:        &inferenceextv1.Port{Number: inferenceextv1.PortNumber(9002)},
				FailureMode: inferenceextv1.EndpointPickerFailClose,
			},
		},
	}
}

// BuildInferencePool constructs a typed InferencePool resource.
//
// The InferencePool selector matches serving pods via the common label set
// (without role), so both decode and prefill pods are included in the pool.
// The endpointPickerRef points to the EPP Service managed by this operator.
func BuildInferencePool(
	ms *modelv1alpha1.ModelService,
	metadataLabels map[string]string,
) *inferenceextv1.InferencePool {
	pool := &ms.Spec.InferencePool.EndpointPicker
	port := enginePort(ms)

	eppPort := pool.Port
	if eppPort == 0 {
		eppPort = 9002
	}
	failureMode := inferenceextv1.EndpointPickerFailureMode(pool.FailureMode)
	if failureMode == "" {
		failureMode = inferenceextv1.EndpointPickerFailClose
	}

	selectorLabels := InferencePoolSelectorLabels(ms.Name)
	matchLabels := make(map[inferenceextv1.LabelKey]inferenceextv1.LabelValue, len(selectorLabels))
	for k, v := range selectorLabels {
		matchLabels[inferenceextv1.LabelKey(k)] = inferenceextv1.LabelValue(v)
	}

	eppName := EPPName(ms.Name)

	return &inferenceextv1.InferencePool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      InferencePoolName(ms.Name),
			Namespace: ms.Namespace,
			Labels:    metadataLabels,
		},
		Spec: inferenceextv1.InferencePoolSpec{
			TargetPorts: []inferenceextv1.Port{
				{Number: inferenceextv1.PortNumber(port)},
			},
			Selector: inferenceextv1.LabelSelector{
				MatchLabels: matchLabels,
			},
			EndpointPickerRef: inferenceextv1.EndpointPickerRef{
				Group:       ptrToGroup(""),
				Kind:        inferenceextv1.Kind("Service"),
				Name:        inferenceextv1.ObjectName(eppName),
				Port:        &inferenceextv1.Port{Number: inferenceextv1.PortNumber(eppPort)},
				FailureMode: failureMode,
			},
		},
	}
}
