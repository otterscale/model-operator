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
	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// DefaultImages holds default container images used when the CR does not
// specify an image. These are set via operator CLI flags so that platform
// administrators can control image versions at the cluster level without
// baking them into the CRD schema.
type DefaultImages struct {
	// Engine is the default vLLM container image (--default-engine-image).
	Engine string
	// EPP is the default Endpoint Picker image (--default-epp-image).
	EPP string
	// RoutingProxy is the default routing proxy sidecar image (--default-routing-proxy-image).
	RoutingProxy string
}

// ApplyImageDefaults fills in empty image fields on the ModelService with
// the operator-level defaults. The caller should pass a DeepCopy of the CR
// to avoid mutating the original API object.
func ApplyImageDefaults(ms *modelv1alpha1.ModelService, defaults DefaultImages) {
	if ms.Spec.Engine.Image == "" {
		ms.Spec.Engine.Image = defaults.Engine
	}
	if ms.Spec.RoutingProxy != nil && ms.Spec.RoutingProxy.Image == "" {
		ms.Spec.RoutingProxy.Image = defaults.RoutingProxy
	}
	if ms.Spec.InferencePool != nil && ms.Spec.InferencePool.EndpointPicker.Image == "" {
		ms.Spec.InferencePool.EndpointPicker.Image = defaults.EPP
	}
}
