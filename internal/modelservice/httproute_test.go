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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

const testModelServiceName = "qwen3-32b"

func TestBuildHTTPRoute(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testModelServiceName,
			Namespace: "ml-serving",
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

	if route.Name != testModelServiceName {
		t.Errorf("Name = %q, want %q", route.Name, testModelServiceName)
	}
	if route.Namespace != "ml-serving" {
		t.Errorf("Namespace = %q, want ml-serving", route.Namespace)
	}

	if len(route.Spec.ParentRefs) != 1 {
		t.Fatalf("ParentRefs len = %d, want 1", len(route.Spec.ParentRefs))
	}
	if string(route.Spec.ParentRefs[0].Name) != "inference-gateway" {
		t.Errorf("Gateway name = %q, want inference-gateway", route.Spec.ParentRefs[0].Name)
	}
	if route.Spec.ParentRefs[0].Namespace != nil {
		t.Errorf("Same-namespace gateway should not set Namespace, got %q", *route.Spec.ParentRefs[0].Namespace)
	}

	if len(route.Spec.Hostnames) != 1 || string(route.Spec.Hostnames[0]) != "models.example.com" {
		t.Errorf("Hostnames = %v, want [models.example.com]", route.Spec.Hostnames)
	}

	if len(route.Spec.Rules) != 1 {
		t.Fatalf("Rules len = %d, want 1", len(route.Spec.Rules))
	}
	backends := route.Spec.Rules[0].BackendRefs
	if len(backends) != 1 {
		t.Fatalf("BackendRefs len = %d, want 1", len(backends))
	}
	ref := backends[0].BackendRef.BackendObjectReference
	if ref.Kind == nil || string(*ref.Kind) != "InferencePool" {
		t.Errorf("Backend kind = %v, want InferencePool", ref.Kind)
	}
	if ref.Group == nil || string(*ref.Group) != "inference.networking.k8s.io" {
		t.Errorf("Backend group = %v, want inference.networking.k8s.io", ref.Group)
	}
	if string(ref.Name) != testModelServiceName {
		t.Errorf("Backend name = %q, want %q", ref.Name, testModelServiceName)
	}
}

func TestBuildHTTPRoute_CrossNamespaceGateway(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "ml-serving",
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
	if ref.Namespace == nil {
		t.Fatal("Cross-namespace gateway should set Namespace")
	}
	if string(*ref.Namespace) != "infra" {
		t.Errorf("Gateway namespace = %q, want infra", *ref.Namespace)
	}
}

func TestBuildHTTPRoute_NoHostnames(t *testing.T) {
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

	if len(route.Spec.Hostnames) != 0 {
		t.Errorf("Expected no hostnames, got %v", route.Spec.Hostnames)
	}

	// BackendRef should still reference InferencePool
	if string(route.Spec.Rules[0].BackendRefs[0].Name) != "test" {
		t.Errorf("Backend name = %q, want test", route.Spec.Rules[0].BackendRefs[0].Name)
	}

	_ = gatewayv1.HTTPRoute{} // verify import
}
