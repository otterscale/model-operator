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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

func newEPPTestModelService() *modelv1alpha1.ModelService {
	return &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "qwen3",
			Namespace: TestNamespace,
		},
		Spec: modelv1alpha1.ModelServiceSpec{
			Engine: modelv1alpha1.EngineSpec{Port: 8000},
			InferencePool: &modelv1alpha1.InferencePoolSpec{
				EndpointPicker: modelv1alpha1.EndpointPickerSpec{
					Image:    "ghcr.io/llm-d/llm-d-inference-scheduler:v0.6.0",
					Replicas: new(int32(2)),
					Port:     9002,
				},
			},
		},
	}
}

var _ = Describe("BuildEPPDeployment", func() {
	Context("basics", func() {
		It("should set name, namespace, and replicas", func() {
			ms := newEPPTestModelService()
			eppConfig := EPPConfig{Provider: ProviderIstio, MetricsEndpointAuth: true}
			labels := map[string]string{"app": "epp"}
			selLabels := map[string]string{"selector": "epp"}

			dep := BuildEPPDeployment(ms, eppConfig, labels, selLabels, "abc123")

			Expect(dep.Name).To(Equal(TestEPPName))
			Expect(dep.Namespace).To(Equal(TestNamespace))
			Expect(*dep.Spec.Replicas).To(Equal(int32(2)))
		})
	})

	Context("deployment strategy", func() {
		It("should use Recreate strategy", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

			Expect(dep.Spec.Strategy.Type).To(Equal(appsv1.RecreateDeploymentStrategyType))
		})
	})

	Context("termination grace period", func() {
		It("should set 130 seconds", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

			grace := dep.Spec.Template.Spec.TerminationGracePeriodSeconds
			Expect(grace).NotTo(BeNil())
			Expect(*grace).To(Equal(int64(130)))
		})
	})

	Context("container", func() {
		It("should configure the EPP container with correct image and ports", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

			Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))

			c := dep.Spec.Template.Spec.Containers[0]
			Expect(c.Name).To(Equal("epp"))
			Expect(c.Image).To(Equal("ghcr.io/llm-d/llm-d-inference-scheduler:v0.6.0"))

			hasExtProcPort := false
			hasMetricsPort := false
			hasHealthPort := false
			for _, p := range c.Ports {
				switch {
				case p.Name == eppExtProcPortName && p.ContainerPort == 9002:
					hasExtProcPort = true
				case p.Name == "http-metrics" && p.ContainerPort == 9090:
					hasMetricsPort = true
				case p.Name == "grpc-health" && p.ContainerPort == 9003:
					hasHealthPort = true
				}
			}
			Expect(hasExtProcPort).To(BeTrue(), "Missing grpc-ext-proc port 9002")
			Expect(hasMetricsPort).To(BeTrue(), "Missing http-metrics port 9090")
			Expect(hasHealthPort).To(BeTrue(), "Missing grpc-health port 9003")
		})
	})

	Context("gRPC probes", func() {
		It("should use gRPC health probes on port 9003", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
			c := dep.Spec.Template.Spec.Containers[0]

			Expect(c.ReadinessProbe).NotTo(BeNil())
			Expect(c.ReadinessProbe.GRPC).NotTo(BeNil())
			Expect(c.ReadinessProbe.GRPC.Port).To(Equal(int32(9003)))

			Expect(c.LivenessProbe).NotTo(BeNil())
			Expect(c.LivenessProbe.GRPC).NotTo(BeNil())
			Expect(c.LivenessProbe.GRPC.Port).To(Equal(int32(9003)))
		})

		It("should use HA probe services when replicas > 1", func() {
			ms := newEPPTestModelService()
			ms.Spec.InferencePool.EndpointPicker.Replicas = new(int32(3))
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
			c := dep.Spec.Template.Spec.Containers[0]

			Expect(c.ReadinessProbe.GRPC.Service).NotTo(BeNil())
			Expect(*c.ReadinessProbe.GRPC.Service).To(Equal("readiness"))
			Expect(c.LivenessProbe.GRPC.Service).NotTo(BeNil())
			Expect(*c.LivenessProbe.GRPC.Service).To(Equal("liveness"))
		})

		It("should use single-replica probe service when replicas = 1", func() {
			ms := newEPPTestModelService()
			ms.Spec.InferencePool.EndpointPicker.Replicas = new(int32(1))
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
			c := dep.Spec.Template.Spec.Containers[0]

			Expect(c.ReadinessProbe.GRPC.Service).NotTo(BeNil())
			Expect(*c.ReadinessProbe.GRPC.Service).To(Equal(eppSingleReplicaProbeService))
		})
	})

	Context("environment variables", func() {
		It("should inject NAMESPACE and POD_NAME from field refs", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
			c := dep.Spec.Template.Spec.Containers[0]

			envMap := make(map[string]corev1.EnvVar)
			for _, e := range c.Env {
				envMap[e.Name] = e
			}

			ns := envMap["NAMESPACE"]
			Expect(ns.ValueFrom).NotTo(BeNil())
			Expect(ns.ValueFrom.FieldRef).NotTo(BeNil())
			Expect(ns.ValueFrom.FieldRef.FieldPath).To(Equal("metadata.namespace"))

			pod := envMap["POD_NAME"]
			Expect(pod.ValueFrom).NotTo(BeNil())
			Expect(pod.ValueFrom.FieldRef).NotTo(BeNil())
			Expect(pod.ValueFrom.FieldRef.FieldPath).To(Equal("metadata.name"))
		})
	})

	Context("command-line arguments", func() {
		It("should include required kebab-case flags", func() {
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
			Expect(hasPoolName).To(BeTrue(), "Missing --pool-name flag")
			Expect(hasPoolNamespace).To(BeTrue(), "Missing --pool-namespace flag")
			Expect(hasConfigFile).To(BeTrue(), "Missing --config-file=/config/ flag")
		})

		It("should use default-plugins.yaml for non-PD mode", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
			args := dep.Spec.Template.Spec.Containers[0].Args

			Expect(args).To(ContainElement(ContainSubstring("config-file=/config/default-plugins.yaml")))
		})

		It("should use pd-config.yaml for PD mode", func() {
			ms := newEPPTestModelService()
			ms.Spec.Prefill = &modelv1alpha1.RoleSpec{Replicas: new(int32(1))}
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "hash")
			args := dep.Spec.Template.Spec.Containers[0].Args

			Expect(args).To(ContainElement(ContainSubstring("config-file=/config/pd-config.yaml")))
		})

		It("should include --zap-encoder=json", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
			args := dep.Spec.Template.Spec.Containers[0].Args

			Expect(args).To(ContainElement("--zap-encoder=json"))
		})

		It("should include --ha-enable-leader-election when replicas > 1", func() {
			ms := newEPPTestModelService()
			ms.Spec.InferencePool.EndpointPicker.Replicas = new(int32(3))
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
			args := dep.Spec.Template.Spec.Containers[0].Args

			Expect(args).To(ContainElement("--ha-enable-leader-election"))
		})

		It("should not include --ha-enable-leader-election for single replica", func() {
			ms := newEPPTestModelService()
			ms.Spec.InferencePool.EndpointPicker.Replicas = new(int32(1))
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
			args := dep.Spec.Template.Spec.Containers[0].Args

			Expect(args).NotTo(ContainElement("--ha-enable-leader-election"))
		})
	})

	Context("tracing", func() {
		It("should include tracing args and env when enabled", func() {
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

			Expect(c.Args).To(ContainElement("--tracing=true"))

			envMap := make(map[string]corev1.EnvVar)
			for _, e := range c.Env {
				envMap[e.Name] = e
			}
			Expect(envMap["OTEL_EXPORTER_OTLP_ENDPOINT"].Value).To(Equal("http://otel:4317"))
			Expect(envMap["OTEL_TRACES_SAMPLER"].Value).To(Equal("parentbased_traceidratio"))
			Expect(envMap["OTEL_TRACES_SAMPLER_ARG"].Value).To(Equal("0.5"))
		})

		It("should include --tracing=false and omit OTEL env when disabled", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
			c := dep.Spec.Template.Spec.Containers[0]

			Expect(c.Args).To(ContainElement("--tracing=false"))

			for _, e := range c.Env {
				Expect(e.Name).NotTo(Equal("OTEL_EXPORTER_OTLP_ENDPOINT"),
					"OTEL env vars should not be set when tracing is disabled")
			}
		})
	})

	Context("metrics endpoint auth", func() {
		It("should include --metrics-endpoint-auth=false when disabled", func() {
			ms := newEPPTestModelService()
			eppConfig := EPPConfig{MetricsEndpointAuth: false}
			dep := BuildEPPDeployment(ms, eppConfig, nil, nil, "")
			args := dep.Spec.Template.Spec.Containers[0].Args

			Expect(args).To(ContainElement("--metrics-endpoint-auth=false"))
		})

		It("should not include --metrics-endpoint-auth flag when enabled", func() {
			ms := newEPPTestModelService()
			eppConfig := EPPConfig{MetricsEndpointAuth: true}
			dep := BuildEPPDeployment(ms, eppConfig, nil, nil, "")
			args := dep.Spec.Template.Spec.Containers[0].Args

			for _, a := range args {
				Expect(a).NotTo(ContainSubstring("metrics-endpoint-auth"))
			}
		})
	})

	Context("custom flags", func() {
		It("should append custom flags from EPPConfig", func() {
			ms := newEPPTestModelService()
			eppConfig := EPPConfig{
				Flags: map[string]string{"v": "2"},
			}

			dep := BuildEPPDeployment(ms, eppConfig, nil, nil, "")
			args := dep.Spec.Template.Spec.Containers[0].Args

			Expect(args).To(ContainElement("--v=2"))
		})
	})

	Context("config hash annotation", func() {
		It("should set checksum/config annotation when hash is provided", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "deadbeef")

			Expect(dep.Spec.Template.Annotations["checksum/config"]).To(Equal("deadbeef"))
		})

		It("should not set checksum/config annotation when hash is empty", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

			Expect(dep.Spec.Template.Annotations).NotTo(HaveKey("checksum/config"))
		})
	})

	Context("resources", func() {
		It("should use default resources when not specified", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
			c := dep.Spec.Template.Spec.Containers[0]

			Expect(c.Resources.Requests.Cpu().Equal(resource.MustParse("4"))).To(BeTrue())
			Expect(c.Resources.Requests.Memory().Equal(resource.MustParse("8Gi"))).To(BeTrue())
			Expect(c.Resources.Limits.Memory().Equal(resource.MustParse("16Gi"))).To(BeTrue())
			Expect(c.Resources.Limits).NotTo(HaveKey(corev1.ResourceCPU), "CPU limit should not be set by default")
		})

		It("should use custom resources when specified", func() {
			ms := newEPPTestModelService()
			ms.Spec.InferencePool.EndpointPicker.Resources = corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("2"),
					corev1.ResourceMemory: resource.MustParse("4Gi"),
				},
			}

			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")
			c := dep.Spec.Template.Spec.Containers[0]

			Expect(c.Resources.Requests.Cpu().Equal(resource.MustParse("2"))).To(BeTrue())
		})
	})

	Context("service account", func() {
		It("should use the EPP service account name", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

			Expect(dep.Spec.Template.Spec.ServiceAccountName).To(Equal(TestEPPName))
		})
	})

	Context("config map volume", func() {
		It("should mount the EPP config ConfigMap", func() {
			ms := newEPPTestModelService()
			dep := BuildEPPDeployment(ms, EPPConfig{}, nil, nil, "")

			found := false
			for _, v := range dep.Spec.Template.Spec.Volumes {
				if v.Name == eppConfigVolume && v.ConfigMap != nil && v.ConfigMap.Name == EPPConfigMapName("qwen3") {
					found = true
				}
			}
			Expect(found).To(BeTrue(), "Missing epp-config volume from ConfigMap")
		})
	})
})
