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

// Package labels provides shared Kubernetes recommended label constants and
// builder functions for all operator-managed resources.
//
// See: https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
package labels

const (
	// Name identifies the application name (Kubernetes Recommended Label).
	Name = "app.kubernetes.io/name"

	// Component identifies the component within the architecture (e.g. "module", "workspace").
	Component = "app.kubernetes.io/component"

	// PartOf identifies the higher-level application this resource belongs to.
	PartOf = "app.kubernetes.io/part-of"

	// ManagedBy identifies the tool/operator that manages the resource.
	ManagedBy = "app.kubernetes.io/managed-by"

	// Version identifies the current version of the application.
	Version = "app.kubernetes.io/version"

	// System is the fixed PartOf value shared across all OtterScale operators.
	System = "otterscale-system"

	// Operator is the ManagedBy value for this operator.
	Operator = "model-operator"
)

// Selector returns the minimal label set used for MatchingLabels queries.
// It deliberately excludes Version so that operator upgrades do not break
// resource lookups for in-flight Jobs created by a previous version.
func Selector(name, component string) map[string]string {
	return map[string]string{
		Name:      name,
		Component: component,
		PartOf:    System,
		ManagedBy: Operator,
	}
}

// Standard returns the full set of Kubernetes recommended labels for all
// operator-managed resources. Use this when creating or labelling resources.
// Use Selector() when querying resources via MatchingLabels.
//
// If version is empty, the app.kubernetes.io/version label is omitted, as an
// empty version label carries no semantic meaning per K8s conventions.
func Standard(name, component, version string) map[string]string {
	m := Selector(name, component)
	if version != "" {
		m[Version] = version
	}
	return m
}
