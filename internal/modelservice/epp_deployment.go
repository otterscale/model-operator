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
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

const (
	eppContainerName   = "epp"
	eppConfigVolume    = "epp-config"
	eppConfigMountPath = "/config"
	eppMetricsPort     = 9090
	eppExtProcPortName = "grpc"
	eppMetricsPortName = "metrics"
	eppHealthPortName  = "grpc-health"
)

// BuildEPPDeployment constructs the EPP Deployment.
func BuildEPPDeployment(
	ms *modelv1alpha1.ModelService,
	eppConfig EPPConfig,
	metadataLabels map[string]string,
	selectorLabels map[string]string,
	configHash string,
) *appsv1.Deployment {
	spec := &ms.Spec.InferencePool.EndpointPicker
	name := EPPName(ms.Name)
	poolName := InferencePoolName(ms.Name)

	replicas := ptr.To(int32(1))
	if spec.Replicas != nil {
		replicas = spec.Replicas
	}

	resources := defaultEPPResources()
	if spec.Resources.Requests != nil || spec.Resources.Limits != nil {
		resources = spec.Resources
	}

	extProcPort := int32(9002)
	if spec.Port > 0 {
		extProcPort = spec.Port
	}

	pluginsConfigFile := PluginsConfigFile(ms)

	args := buildEPPArgs(poolName, ms.Namespace, pluginsConfigFile, extProcPort, *replicas, eppConfig)

	podAnnotations := map[string]string{}
	if configHash != "" {
		podAnnotations["checksum/config"] = configHash
	}

	env := buildEPPEnv(eppConfig)

	grpcServiceName := "inference-extension"
	if *replicas > 1 {
		grpcServiceName = ""
	}

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ms.Namespace,
			Labels:    metadataLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      mergeMaps(selectorLabels, metadataLabels),
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:            name,
					TerminationGracePeriodSeconds: ptr.To(int64(130)),
					Containers: []corev1.Container{
						{
							Name:      eppContainerName,
							Image:     spec.Image,
							Args:      args,
							Env:       env,
							Resources: resources,
							Ports: []corev1.ContainerPort{
								{
									Name:          eppExtProcPortName,
									ContainerPort: extProcPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          eppHealthPortName,
									ContainerPort: eppGRPCHealthPort,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          eppMetricsPortName,
									ContainerPort: eppMetricsPort,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      eppConfigVolume,
									MountPath: eppConfigMountPath,
									ReadOnly:  true,
								},
							},
							ReadinessProbe: grpcProbe(eppGRPCHealthPort, grpcServiceName, 5, 2),
							LivenessProbe:  grpcProbe(eppGRPCHealthPort, grpcServiceName, 5, 10),
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: eppConfigVolume,
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: EPPConfigMapName(ms.Name),
									},
									Optional: ptr.To(true),
								},
							},
						},
					},
				},
			},
		},
	}
}

// buildEPPArgs constructs command-line arguments for the EPP container.
func buildEPPArgs(
	poolName, namespace, pluginsConfigFile string,
	extProcPort, replicas int32,
	eppConfig EPPConfig,
) []string {
	args := []string{
		fmt.Sprintf("--pool-name=%s", poolName),
		fmt.Sprintf("--pool-namespace=%s", namespace),
		fmt.Sprintf("--config-file=%s/%s", eppConfigMountPath, pluginsConfigFile),
		fmt.Sprintf("--extProcPort=%d", extProcPort),
		fmt.Sprintf("--metricsPort=%d", eppMetricsPort),
		"--zap-encoder=json",
	}

	if replicas > 1 {
		args = append(args, "--ha-enable-leader-election")
	}

	if eppConfig.Tracing.Enabled {
		args = append(args, "--tracing=true")
	} else {
		args = append(args, "--tracing=false")
	}

	if !eppConfig.MetricsEndpointAuth {
		args = append(args, "--metrics-endpoint-auth=false")
	}

	flagKeys := make([]string, 0, len(eppConfig.Flags))
	for k := range eppConfig.Flags {
		flagKeys = append(flagKeys, k)
	}
	sort.Strings(flagKeys)
	for _, k := range flagKeys {
		args = append(args, fmt.Sprintf("--%s=%s", k, eppConfig.Flags[k]))
	}

	return args
}

// buildEPPEnv constructs environment variables for the EPP container.
func buildEPPEnv(eppConfig EPPConfig) []corev1.EnvVar {
	env := []corev1.EnvVar{
		{
			Name: "NAMESPACE",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.namespace",
				},
			},
		},
		{
			Name: "POD_NAME",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "metadata.name",
				},
			},
		},
	}

	if eppConfig.Tracing.Enabled {
		env = append(env,
			corev1.EnvVar{Name: "OTEL_SERVICE_NAME", Value: "gateway-api-inference-extension"},
			corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: eppConfig.Tracing.OtelExporterEndpoint},
			corev1.EnvVar{Name: "OTEL_TRACES_EXPORTER", Value: "otlp"},
			corev1.EnvVar{
				Name: "OTEL_RESOURCE_ATTRIBUTES_NODE_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						APIVersion: "v1",
						FieldPath:  "spec.nodeName",
					},
				},
			},
			corev1.EnvVar{
				Name: "OTEL_RESOURCE_ATTRIBUTES_POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						APIVersion: "v1",
						FieldPath:  "metadata.name",
					},
				},
			},
			corev1.EnvVar{
				Name:  "OTEL_RESOURCE_ATTRIBUTES",
				Value: "k8s.namespace.name=$(NAMESPACE),k8s.node.name=$(OTEL_RESOURCE_ATTRIBUTES_NODE_NAME),k8s.pod.name=$(OTEL_RESOURCE_ATTRIBUTES_POD_NAME)",
			},
		)
		if eppConfig.Tracing.Sampler != "" {
			env = append(env, corev1.EnvVar{Name: "OTEL_TRACES_SAMPLER", Value: eppConfig.Tracing.Sampler})
		}
		if eppConfig.Tracing.SamplerArg != "" {
			env = append(env, corev1.EnvVar{Name: "OTEL_TRACES_SAMPLER_ARG", Value: eppConfig.Tracing.SamplerArg})
		}
	}

	return env
}

func grpcProbe(port int32, service string, initialDelay, period int32) *corev1.Probe {
	probe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			GRPC: &corev1.GRPCAction{
				Port: port,
			},
		},
		InitialDelaySeconds: initialDelay,
		PeriodSeconds:       period,
	}
	if service != "" {
		probe.ProbeHandler.GRPC.Service = &service
	}
	return probe
}

func defaultEPPResources() corev1.ResourceRequirements {
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse("4"),
			corev1.ResourceMemory: resource.MustParse("8Gi"),
		},
		Limits: corev1.ResourceList{
			corev1.ResourceMemory: resource.MustParse("16Gi"),
		},
	}
}

func mergeMaps(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			result[k] = v
		}
	}
	return result
}
