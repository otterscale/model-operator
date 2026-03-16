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

const (
	ProviderIstio = "istio"
	ProviderGKE   = "gke"
	ProviderNone  = "none"

	eppGRPCHealthPort = int32(9003)
)

// EPPConfig holds cluster-level EPP settings that are shared across all
// ModelService instances. Per-ModelService settings (image, replicas,
// resources, port, failureMode) live in the CRD's EndpointPickerSpec.
type EPPConfig struct {
	// Provider is the infrastructure provider: "istio" (default), "gke", "none".
	// Controls whether Istio DestinationRule or GKE-specific resources are created.
	Provider string

	// Flags are additional command-line flags passed to the EPP container.
	Flags map[string]string

	// MetricsEndpointAuth controls whether the EPP metrics endpoint requires
	// authentication. When false, --metrics-endpoint-auth=false is passed.
	MetricsEndpointAuth bool

	// DefaultGatewayName is the Gateway name used when creating an HTTPRoute
	// and spec.httpRoute is not set. When non-empty, a default HTTPRoute is
	// created (parentRef to this gateway, backend to the InferencePool).
	// When empty, HTTPRoute is only created when spec.httpRoute is set.
	DefaultGatewayName string

	// Tracing holds OpenTelemetry tracing configuration.
	Tracing TracingConfig
}

// TracingConfig holds cluster-level tracing settings for the EPP container.
type TracingConfig struct {
	Enabled              bool
	OtelExporterEndpoint string
	Sampler              string
	SamplerArg           string
}
