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
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

func newEPPTestModelService() *modelv1alpha1.ModelService {
	return &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qwen3",
			Namespace: "ml-serving",
		},
		Spec: modelv1alpha1.ModelServiceSpec{
			Engine: modelv1alpha1.EngineSpec{Port: 8000},
			InferencePool: &modelv1alpha1.InferencePoolSpec{
				EndpointPicker: modelv1alpha1.EndpointPickerSpec{
					Image:    "ghcr.io/llm-d/llm-d-inference-scheduler:v0.6.0",
					Replicas: ptr.To(int32(2)),
					Port:     9002,
				},
			},
		},
	}
}

func TestBuildEPPDeployment_Basics(t *testing.T) {
	ms := newEPPTestModelService()
	eppConfig := EPPConfig{Provider: ProviderIstio, MetricsEndpointAuth: true}
	labels := map[string]string{"app": "epp"}
	selLabels := map[string]string{"selector": "epp"}

	dep := BuildEPPDeployment(ms, eppConfig, labels, selLabels, "abc123")

	if dep.Name != "qwen3-epp" {
		t.Errorf("Name = %q, want qwen3-epp", dep.Name)
	}
	if dep.Namespace != "ml-serving" {
		t.Errorf("Namespace = %q, want ml-serving", dep.Namespace)
	}
	if *dep.Spec.Replicas != 2 {
		t.Errorf("Replicas = %d, want 2", *dep.Spec.Replicas)
	}
}

func TestBuildEPPDeployment_RecreateStrategy(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

	if dep.Spec.Strategy.Type != appsv1.RecreateDeploymentStrategyType {
		t.Errorf("Strategy = %q, want Recreate", dep.Spec.Strategy.Type)
	}
}

func TestBuildEPPDeployment_TerminationGracePeriod(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

	grace := dep.Spec.Template.Spec.TerminationGracePeriodSeconds
	if grace == nil || *grace != 130 {
		t.Errorf("TerminationGracePeriodSeconds = %v, want 130", grace)
	}
}

func TestBuildEPPDeployment_Container(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

	if len(dep.Spec.Template.Spec.Containers) != 1 {
		t.Fatalf("Containers = %d, want 1", len(dep.Spec.Template.Spec.Containers))
	}

	c := dep.Spec.Template.Spec.Containers[0]
	if c.Name != "epp" {
		t.Errorf("Container name = %q, want epp", c.Name)
	}
	if c.Image != "ghcr.io/llm-d/llm-d-inference-scheduler:v0.6.0" {
		t.Errorf("Image = %q", c.Image)
	}

	hasExtProcPort := false
	hasMetricsPort := false
	hasHealthPort := false
	for _, p := range c.Ports {
		switch {
		case p.Name == "grpc" && p.ContainerPort == 9002:
			hasExtProcPort = true
		case p.Name == "metrics" && p.ContainerPort == 9090:
			hasMetricsPort = true
		case p.Name == "grpc-health" && p.ContainerPort == 9003:
			hasHealthPort = true
		}
	}
	if !hasExtProcPort {
		t.Error("Missing grpc port 9002")
	}
	if !hasMetricsPort {
		t.Error("Missing metrics port 9090")
	}
	if !hasHealthPort {
		t.Error("Missing grpc-health port 9003")
	}
}

func TestBuildEPPDeployment_GRPCProbes(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
	c := dep.Spec.Template.Spec.Containers[0]

	if c.ReadinessProbe == nil || c.ReadinessProbe.GRPC == nil {
		t.Fatal("ReadinessProbe should use gRPC")
	}
	if c.ReadinessProbe.GRPC.Port != 9003 {
		t.Errorf("ReadinessProbe gRPC port = %d, want 9003", c.ReadinessProbe.GRPC.Port)
	}

	if c.LivenessProbe == nil || c.LivenessProbe.GRPC == nil {
		t.Fatal("LivenessProbe should use gRPC")
	}
	if c.LivenessProbe.GRPC.Port != 9003 {
		t.Errorf("LivenessProbe gRPC port = %d, want 9003", c.LivenessProbe.GRPC.Port)
	}
}

func TestBuildEPPDeployment_GRPCProbeService_HA(t *testing.T) {
	ms := newEPPTestModelService()
	ms.Spec.InferencePool.EndpointPicker.Replicas = ptr.To(int32(3))
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
	c := dep.Spec.Template.Spec.Containers[0]

	if c.ReadinessProbe.GRPC.Service != nil {
		t.Error("HA mode readiness probe should have nil service (empty string)")
	}
	if c.LivenessProbe.GRPC.Service != nil {
		t.Error("HA mode liveness probe should have nil service (empty string)")
	}
}

func TestBuildEPPDeployment_GRPCProbeService_Single(t *testing.T) {
	ms := newEPPTestModelService()
	ms.Spec.InferencePool.EndpointPicker.Replicas = ptr.To(int32(1))
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
	c := dep.Spec.Template.Spec.Containers[0]

	if c.ReadinessProbe.GRPC.Service == nil || *c.ReadinessProbe.GRPC.Service != "inference-extension" {
		t.Error("Single-replica readiness probe should have service=inference-extension")
	}
}

func TestBuildEPPDeployment_EnvVars(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
	c := dep.Spec.Template.Spec.Containers[0]

	envMap := make(map[string]corev1.EnvVar)
	for _, e := range c.Env {
		envMap[e.Name] = e
	}

	ns, ok := envMap["NAMESPACE"]
	if !ok {
		t.Fatal("Missing NAMESPACE env var")
	}
	if ns.ValueFrom == nil || ns.ValueFrom.FieldRef == nil || ns.ValueFrom.FieldRef.FieldPath != "metadata.namespace" {
		t.Error("NAMESPACE should use fieldRef metadata.namespace")
	}

	pod, ok := envMap["POD_NAME"]
	if !ok {
		t.Fatal("Missing POD_NAME env var")
	}
	if pod.ValueFrom == nil || pod.ValueFrom.FieldRef == nil || pod.ValueFrom.FieldRef.FieldPath != "metadata.name" {
		t.Error("POD_NAME should use fieldRef metadata.name")
	}
}

func TestBuildEPPDeployment_ArgsKebabCase(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

	args := dep.Spec.Template.Spec.Containers[0].Args
	hasPoolName := false
	hasPoolNamespace := false
	hasConfigFile := false
	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--pool-name="):
			hasPoolName = true
		case strings.HasPrefix(a, "--pool-namespace="):
			hasPoolNamespace = true
		case strings.HasPrefix(a, "--config-file=/config/"):
			hasConfigFile = true
		}
	}
	if !hasPoolName {
		t.Errorf("Missing --pool-name flag, args: %v", args)
	}
	if !hasPoolNamespace {
		t.Errorf("Missing --pool-namespace flag, args: %v", args)
	}
	if !hasConfigFile {
		t.Errorf("Missing --config-file=/config/ flag, args: %v", args)
	}
}

func TestBuildEPPDeployment_ArgsNonPD(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

	args := dep.Spec.Template.Spec.Containers[0].Args
	found := false
	for _, a := range args {
		if strings.Contains(a, "config-file=/config/default-plugins.yaml") {
			found = true
		}
	}
	if !found {
		t.Errorf("Non-PD deployment should use default-plugins.yaml, args: %v", args)
	}
}

func TestBuildEPPDeployment_ArgsPD(t *testing.T) {
	ms := newEPPTestModelService()
	ms.Spec.Prefill = &modelv1alpha1.RoleSpec{Replicas: ptr.To(int32(1))}
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "hash")

	args := dep.Spec.Template.Spec.Containers[0].Args
	found := false
	for _, a := range args {
		if strings.Contains(a, "config-file=/config/pd-config.yaml") {
			found = true
		}
	}
	if !found {
		t.Errorf("PD deployment should use pd-config.yaml, args: %v", args)
	}
}

func TestBuildEPPDeployment_ZapEncoder(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
	args := dep.Spec.Template.Spec.Containers[0].Args
	found := false
	for _, a := range args {
		if a == "--zap-encoder=json" {
			found = true
		}
	}
	if !found {
		t.Errorf("Should include --zap-encoder=json, args: %v", args)
	}
}

func TestBuildEPPDeployment_LeaderElection(t *testing.T) {
	ms := newEPPTestModelService()
	ms.Spec.InferencePool.EndpointPicker.Replicas = ptr.To(int32(3))
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

	args := dep.Spec.Template.Spec.Containers[0].Args
	found := false
	for _, a := range args {
		if a == "--ha-enable-leader-election" {
			found = true
		}
	}
	if !found {
		t.Errorf("Replicas > 1 should include --ha-enable-leader-election, args: %v", args)
	}
}

func TestBuildEPPDeployment_NoLeaderElectionSingleReplica(t *testing.T) {
	ms := newEPPTestModelService()
	ms.Spec.InferencePool.EndpointPicker.Replicas = ptr.To(int32(1))
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

	args := dep.Spec.Template.Spec.Containers[0].Args
	for _, a := range args {
		if a == "--ha-enable-leader-election" {
			t.Error("Single replica should not include --ha-enable-leader-election")
		}
	}
}

func TestBuildEPPDeployment_TracingEnabled(t *testing.T) {
	ms := newEPPTestModelService()
	eppConfig := EPPConfig{
		Tracing: TracingConfig{
			Enabled:              true,
			OtelExporterEndpoint: "http://otel:4317",
			Sampler:              "parentbased_traceidratio",
			SamplerArg:           "0.5",
		},
	}
	dep := BuildEPPDeployment(ms, eppConfig, nil, nil, "")
	c := dep.Spec.Template.Spec.Containers[0]

	hasTracingFlag := false
	for _, a := range c.Args {
		if a == "--tracing=true" {
			hasTracingFlag = true
		}
	}
	if !hasTracingFlag {
		t.Errorf("Tracing enabled should include --tracing=true, args: %v", c.Args)
	}

	envMap := make(map[string]corev1.EnvVar)
	for _, e := range c.Env {
		envMap[e.Name] = e
	}
	if envMap["OTEL_EXPORTER_OTLP_ENDPOINT"].Value != "http://otel:4317" {
		t.Errorf("OTEL_EXPORTER_OTLP_ENDPOINT = %q", envMap["OTEL_EXPORTER_OTLP_ENDPOINT"].Value)
	}
	if envMap["OTEL_TRACES_SAMPLER"].Value != "parentbased_traceidratio" {
		t.Errorf("OTEL_TRACES_SAMPLER = %q", envMap["OTEL_TRACES_SAMPLER"].Value)
	}
	if envMap["OTEL_TRACES_SAMPLER_ARG"].Value != "0.5" {
		t.Errorf("OTEL_TRACES_SAMPLER_ARG = %q", envMap["OTEL_TRACES_SAMPLER_ARG"].Value)
	}
}

func TestBuildEPPDeployment_TracingDisabled(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
	c := dep.Spec.Template.Spec.Containers[0]

	hasTracingFalse := false
	for _, a := range c.Args {
		if a == "--tracing=false" {
			hasTracingFalse = true
		}
	}
	if !hasTracingFalse {
		t.Errorf("Tracing disabled should include --tracing=false, args: %v", c.Args)
	}

	for _, e := range c.Env {
		if e.Name == "OTEL_EXPORTER_OTLP_ENDPOINT" {
			t.Error("OTEL env vars should not be set when tracing is disabled")
		}
	}
}

func TestBuildEPPDeployment_MetricsAuthDisabled(t *testing.T) {
	ms := newEPPTestModelService()
	eppConfig := EPPConfig{MetricsEndpointAuth: false}
	dep := BuildEPPDeployment(ms, eppConfig, nil, nil, "")

	args := dep.Spec.Template.Spec.Containers[0].Args
	found := false
	for _, a := range args {
		if a == "--metrics-endpoint-auth=false" {
			found = true
		}
	}
	if !found {
		t.Errorf("MetricsEndpointAuth=false should include --metrics-endpoint-auth=false, args: %v", args)
	}
}

func TestBuildEPPDeployment_MetricsAuthEnabled(t *testing.T) {
	ms := newEPPTestModelService()
	eppConfig := EPPConfig{MetricsEndpointAuth: true}
	dep := BuildEPPDeployment(ms, eppConfig, nil, nil, "")

	args := dep.Spec.Template.Spec.Containers[0].Args
	for _, a := range args {
		if strings.Contains(a, "metrics-endpoint-auth") {
			t.Errorf("MetricsEndpointAuth=true should not include --metrics-endpoint-auth flag, args: %v", args)
		}
	}
}

func TestBuildEPPDeployment_CustomFlags(t *testing.T) {
	ms := newEPPTestModelService()
	eppConfig := EPPConfig{
		Flags: map[string]string{
			"v": "2",
		},
	}

	dep := BuildEPPDeployment(ms, eppConfig, nil, nil, "")
	args := dep.Spec.Template.Spec.Containers[0].Args
	found := false
	for _, a := range args {
		if a == "--v=2" {
			found = true
		}
	}
	if !found {
		t.Errorf("Should include custom flag --v=2, args: %v", args)
	}
}

func TestBuildEPPDeployment_ConfigHashAnnotation(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "deadbeef")

	ann := dep.Spec.Template.Annotations
	if ann["checksum/config"] != "deadbeef" {
		t.Errorf("checksum/config = %q, want deadbeef", ann["checksum/config"])
	}
}

func TestBuildEPPDeployment_NoConfigHash(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

	ann := dep.Spec.Template.Annotations
	if _, ok := ann["checksum/config"]; ok {
		t.Error("Empty config hash should not produce annotation")
	}
}

func TestBuildEPPDeployment_DefaultResources(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
	c := dep.Spec.Template.Spec.Containers[0]

	expectedCPU := resource.MustParse("4")
	expectedMem := resource.MustParse("8Gi")
	expectedMemLimit := resource.MustParse("16Gi")

	if !c.Resources.Requests.Cpu().Equal(expectedCPU) {
		t.Errorf("CPU request = %s, want 4", c.Resources.Requests.Cpu().String())
	}
	if !c.Resources.Requests.Memory().Equal(expectedMem) {
		t.Errorf("Memory request = %s, want 8Gi", c.Resources.Requests.Memory().String())
	}
	if !c.Resources.Limits.Memory().Equal(expectedMemLimit) {
		t.Errorf("Memory limit = %s, want 16Gi", c.Resources.Limits.Memory().String())
	}
	if _, ok := c.Resources.Limits[corev1.ResourceCPU]; ok {
		t.Error("CPU limit should not be set by default (allow bursting)")
	}
}

func TestBuildEPPDeployment_CustomResources(t *testing.T) {
	ms := newEPPTestModelService()
	ms.Spec.InferencePool.EndpointPicker.Resources = corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("2"),
			corev1.ResourceMemory: resource.MustParse("4Gi"),
		},
	}

	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
	c := dep.Spec.Template.Spec.Containers[0]

	if !c.Resources.Requests.Cpu().Equal(resource.MustParse("2")) {
		t.Errorf("Custom CPU request = %s, want 2", c.Resources.Requests.Cpu().String())
	}
}

func TestBuildEPPDeployment_ServiceAccount(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

	if dep.Spec.Template.Spec.ServiceAccountName != "qwen3-epp" {
		t.Errorf("ServiceAccountName = %q, want qwen3-epp", dep.Spec.Template.Spec.ServiceAccountName)
	}
}

func TestBuildEPPDeployment_ConfigMapVolume(t *testing.T) {
	ms := newEPPTestModelService()
	dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

	found := false
	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.Name == "epp-config" && v.ConfigMap != nil && v.ConfigMap.Name == "qwen3-epp-config" {
			found = true
		}
	}
	if !found {
		t.Error("Missing epp-config volume from ConfigMap")
	}
}
