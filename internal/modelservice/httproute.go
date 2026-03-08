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
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// BuildHTTPRoute constructs a typed HTTPRoute that routes traffic
// from a Gateway to the InferencePool backend.
//
// The HTTPRoute uses the InferencePool as its backend reference, allowing the
// Gateway API Inference Extension EPP to perform intelligent model-aware routing.
func BuildHTTPRoute(
	ms *modelv1alpha1.ModelService,
	metadataLabels map[string]string,
) *gatewayv1.HTTPRoute {
	route := ms.Spec.HTTPRoute

	gwNamespace := route.GatewayRef.Namespace
	if gwNamespace == "" {
		gwNamespace = ms.Namespace
	}

	parentRef := gatewayv1.ParentReference{
		Name: gatewayv1.ObjectName(route.GatewayRef.Name),
	}
	if gwNamespace != ms.Namespace {
		ns := gatewayv1.Namespace(gwNamespace)
		parentRef.Namespace = &ns
	}

	poolName := InferencePoolName(ms.Name)
	backendGroup := gatewayv1.Group("inference.networking.k8s.io")
	backendKind := gatewayv1.Kind("InferencePool")

	rule := gatewayv1.HTTPRouteRule{
		BackendRefs: []gatewayv1.HTTPBackendRef{
			{
				BackendRef: gatewayv1.BackendRef{
					BackendObjectReference: gatewayv1.BackendObjectReference{
						Group: &backendGroup,
						Kind:  &backendKind,
						Name:  gatewayv1.ObjectName(poolName),
					},
				},
			},
		},
	}

	spec := gatewayv1.HTTPRouteSpec{
		CommonRouteSpec: gatewayv1.CommonRouteSpec{
			ParentRefs: []gatewayv1.ParentReference{parentRef},
		},
		Rules: []gatewayv1.HTTPRouteRule{rule},
	}

	if len(route.Hostnames) > 0 {
		hostnames := make([]gatewayv1.Hostname, len(route.Hostnames))
		for i, h := range route.Hostnames {
			hostnames[i] = gatewayv1.Hostname(h)
		}
		spec.Hostnames = hostnames
	}

	return &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      HTTPRouteName(ms.Name),
			Namespace: ms.Namespace,
			Labels:    metadataLabels,
		},
		Spec: spec,
	}
}
