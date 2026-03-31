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
	"maps"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

const (
	defaultMountPath = "/models"

	// ModelTmpVolumeName is the name of the emptyDir volume used by the model-unpack
	// init container for OCI pull cache (kit cache / scratch).
	ModelTmpVolumeName = "model-tmp"
	// ModelTmpMountPath is the mount path for the OCI cache inside the init container.
	// KITOPS_HOME is set to this path so kit stores its cache here.
	ModelTmpMountPath = "/tmp/model-oci"
)

// BuildDeployment constructs an apps/v1 Deployment for a serving role (decode or prefill).
//
// The model artifact is provisioned by an init container that pulls the OCI ModelPack
// from spec.model.image into a tmp emptyDir and runs "kit unpack" into spec.model.mountPath.
// The main vLLM container mounts the unpacked model read-only. GPU resources are
// injected automatically based on accelerator type and parallelism.
func BuildDeployment(
	ms *modelv1alpha1.ModelService,
	role *modelv1alpha1.RoleSpec,
	roleName string,
	deployName string,
	podLabels map[string]string,
	metadataLabels map[string]string,
	selectorLabels map[string]string,
	tracing TracingConfig,
	kitImage string,
) *appsv1.Deployment {
	replicas := DefaultReplicas(role.Replicas)

	isDecodeRole := roleName == RoleDecode
	vllmContainer := buildVLLMContainer(ms, role, isDecodeRole)
	initContainers := buildInitContainers(ms, isDecodeRole, tracing, kitImage)
	volumes := buildVolumes(ms)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployName,
			Namespace: ms.Namespace,
			Labels:    metadataLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      podLabels,
					Annotations: maps.Clone(role.Annotations),
				},
				Spec: corev1.PodSpec{
					InitContainers:               initContainers,
					Containers:                   []corev1.Container{vllmContainer},
					Volumes:                      volumes,
					ImagePullSecrets:             ms.Spec.Model.ImagePullSecrets,
					AutomountServiceAccountToken: new(false),
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: new(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					NodeSelector: role.NodeSelector,
					Tolerations:  role.Tolerations,
				},
			},
		},
	}

	return dep
}

// buildVLLMContainer constructs the main vLLM inference engine container.
// The routing proxy is only used for decode pods; prefill pods listen directly
// on the engine port.
func buildVLLMContainer(
	ms *modelv1alpha1.ModelService,
	role *modelv1alpha1.RoleSpec,
	isDecodeRole bool,
) corev1.Container {
	port := enginePort(ms)
	vllmPort := port
	if isDecodeRole && ms.Spec.RoutingProxy != nil {
		vllmPort = routingProxyTargetPort(ms)
	}

	args := buildVLLMArgs(ms, role, vllmPort)
	env := buildVLLMEnv(ms, role)

	res := role.Resources.DeepCopy()
	gpuCount := GPUCount(role.Parallelism)
	InjectGPUResources(res, ms.Spec.Accelerator.Type, gpuCount)

	mountPath := ms.Spec.Model.MountPath
	if mountPath == "" {
		mountPath = defaultMountPath
	}

	c := corev1.Container{
		Name:            "vllm",
		Image:           ms.Spec.Engine.Image,
		ImagePullPolicy: ms.Spec.Engine.ImagePullPolicy,
		Command:         []string{"vllm", "serve"},
		Args:            args,
		Env:             env,
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: vllmPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      ModelVolumeName,
				MountPath: mountPath,
				ReadOnly:  true,
			},
		},
		Resources: *res,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: new(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}

	// If VLLM_NIXL_SIDE_CHANNEL_PORT is provided via engine.env, expose it as a
	// container port for both decode and prefill pods.
	var nixlPort int32
	for _, e := range ms.Spec.Engine.Env {
		if e.Name == "VLLM_NIXL_SIDE_CHANNEL_PORT" {
			if p, err := strconv.ParseInt(e.Value, 10, 32); err == nil && p > 0 && p <= 65535 {
				nixlPort = int32(p)
			}
			break
		}
	}
	if nixlPort != 0 {
		exists := false
		for _, p := range c.Ports {
			if p.ContainerPort == nixlPort && p.Protocol == corev1.ProtocolTCP {
				exists = true
				break
			}
		}
		if !exists {
			c.Ports = append(c.Ports, corev1.ContainerPort{
				Name:          "nixl-side",
				ContainerPort: nixlPort,
				Protocol:      corev1.ProtocolTCP,
			})
		}
	}

	return c
}

// buildVLLMArgs constructs the vLLM command-line arguments.
// Auto-generated args are placed first; user-provided args are appended so they
// can override defaults when vLLM supports last-wins semantics.
func buildVLLMArgs(
	ms *modelv1alpha1.ModelService,
	role *modelv1alpha1.RoleSpec,
	vllmPort int32,
) []string {
	mountPath := ms.Spec.Model.MountPath
	if mountPath == "" {
		mountPath = defaultMountPath
	}

	args := []string{
		mountPath,
		"--port", fmt.Sprintf("%d", vllmPort),
		"--served-model-name", ms.Spec.Model.Name,
	}

	tensor := max(role.Parallelism.Tensor, 1)
	data := max(role.Parallelism.Data, 1)
	dataLocal := max(role.Parallelism.DataLocal, 1)

	if tensor > 1 {
		args = append(args, "--tensor-parallel-size", fmt.Sprintf("%d", tensor))
	}
	if data > 1 {
		args = append(args, "--data-parallel-size", fmt.Sprintf("%d", data))
	}
	if dataLocal > 1 {
		args = append(args, "--data-parallel-size-local", fmt.Sprintf("%d", dataLocal))
	}

	args = append(args, ms.Spec.Engine.Args...)

	return args
}

// buildVLLMEnv constructs environment variables for the vLLM container.
func buildVLLMEnv(
	ms *modelv1alpha1.ModelService,
	role *modelv1alpha1.RoleSpec,
) []corev1.EnvVar {
	envs := make([]corev1.EnvVar, 0, 3+len(ms.Spec.Engine.Env))

	envs = append(envs,
		corev1.EnvVar{Name: "TP_SIZE", Value: fmt.Sprintf("%d", max(role.Parallelism.Tensor, 1))},
		corev1.EnvVar{Name: "DP_SIZE", Value: fmt.Sprintf("%d", max(role.Parallelism.Data, 1))},
		corev1.EnvVar{Name: "DP_SIZE_LOCAL", Value: fmt.Sprintf("%d", max(role.Parallelism.DataLocal, 1))},
	)

	envs = append(envs, ms.Spec.Engine.Env...)

	return envs
}

// buildInitContainers constructs init containers. The first init container (model-unpack)
// pulls the OCI ModelPack from spec.model.image to a tmp emptyDir and unpacks to
// spec.model.mountPath using kit. The routing proxy sidecar is only injected for
// decode pods — prefill pods receive requests directly from the routing proxy on
// decode pods, so they don't need their own proxy.
func buildInitContainers(ms *modelv1alpha1.ModelService, isDecodeRole bool, tracing TracingConfig, kitImage string) []corev1.Container {
	mountPath := ms.Spec.Model.MountPath
	if mountPath == "" {
		mountPath = defaultMountPath
	}

	modelUnpack := buildModelUnpackInitContainer(ms, mountPath, kitImage)
	var inits []corev1.Container
	if modelUnpack != nil {
		inits = append(inits, *modelUnpack)
	}

	if !isDecodeRole || ms.Spec.RoutingProxy == nil {
		return inits
	}

	proxy := ms.Spec.RoutingProxy
	port := enginePort(ms)
	targetPort := routingProxyTargetPort(ms)
	always := corev1.ContainerRestartPolicyAlways

	args := []string{
		"--port", fmt.Sprintf("%d", port),
		"--vllm-port", fmt.Sprintf("%d", targetPort),
		"--connector", proxy.Connector,
	}
	if proxy.ZapEncoder != "" {
		args = append(args, "--zap-encoder", proxy.ZapEncoder)
	}
	if proxy.ZapLogLevel != "" {
		args = append(args, "--zap-log-level", proxy.ZapLogLevel)
	}
	secureProxy := proxy.SecureProxy != nil && *proxy.SecureProxy
	args = append(args, fmt.Sprintf("--secure-proxy=%t", secureProxy))
	if proxy.PrefillerUseTLS != nil {
		args = append(args, fmt.Sprintf("--prefiller-use-tls=%t", *proxy.PrefillerUseTLS))
	}
	if proxy.CertPath != "" {
		args = append(args, "--cert-path", proxy.CertPath)
	}

	var env []corev1.EnvVar
	if tracing.Enabled {
		env = append(env,
			corev1.EnvVar{Name: "OTEL_SERVICE_NAME", Value: "llm-d-routing-sidecar"},
			corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: tracing.OtelExporterEndpoint},
			corev1.EnvVar{Name: "OTEL_TRACES_EXPORTER", Value: "otlp"},
		)
		if tracing.Sampler != "" {
			env = append(env, corev1.EnvVar{Name: "OTEL_TRACES_SAMPLER", Value: tracing.Sampler})
		}
		if tracing.SamplerArg != "" {
			env = append(env, corev1.EnvVar{Name: "OTEL_TRACES_SAMPLER_ARG", Value: tracing.SamplerArg})
		}
	}

	return append(inits, corev1.Container{
		Name:  "routing-proxy",
		Image: proxy.Image,
		Args:  args,
		Env:   env,
		Ports: []corev1.ContainerPort{
			{
				Name:          "proxy",
				ContainerPort: port,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		RestartPolicy: &always,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: new(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	})
}

// modelUnpackScript runs in the model-unpack init container: set KITOPS_HOME to the
// tmp emptyDir, then kit unpack the OCI ModelPack from spec.model.image to the
// model mount path. --plain-http allows connecting to registries over HTTP (e.g. Harbor without TLS).
// Registry auth uses DOCKER_CONFIG when imagePullSecrets are set.
const modelUnpackScript = `set -euo pipefail
export KITOPS_HOME="${KITOPS_HOME:-/tmp/model-oci}"
mkdir -p "$KITOPS_HOME"
kit unpack "$OCI_MODEL_IMAGE" --plain-http -o -d "$MODEL_MOUNT_PATH"
`

// buildModelUnpackInitContainer returns the init container that pulls the OCI ModelPack
// from spec.model.image into the tmp emptyDir and unpacks to mountPath using kit.
// Returns nil if kitImage or ms.Spec.Model.Image is empty.
func buildModelUnpackInitContainer(ms *modelv1alpha1.ModelService, mountPath, kitImage string) *corev1.Container {
	if kitImage == "" || ms.Spec.Model.Image == "" {
		return nil
	}

	volumeMounts := []corev1.VolumeMount{
		{Name: ModelTmpVolumeName, MountPath: ModelTmpMountPath},
		{Name: ModelVolumeName, MountPath: mountPath},
	}
	env := []corev1.EnvVar{
		{Name: "OCI_MODEL_IMAGE", Value: ms.Spec.Model.Image},
		{Name: "MODEL_MOUNT_PATH", Value: mountPath},
		{Name: "KITOPS_HOME", Value: ModelTmpMountPath},
	}

	if len(ms.Spec.Model.ImagePullSecrets) > 0 {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      DockerConfigVolumeName,
			MountPath: DockerConfigMountPath,
			ReadOnly:  true,
		})
		env = append(env, corev1.EnvVar{Name: "DOCKER_CONFIG", Value: DockerConfigMountPath})
	}

	c := &corev1.Container{
		Name:         "model-unpack",
		Image:        kitImage,
		Command:      []string{"/bin/sh", "-c"},
		Args:         []string{modelUnpackScript},
		Env:          env,
		VolumeMounts: volumeMounts,
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: new(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		},
	}
	return c
}

// buildVolumes constructs the volume list. The model artifact is provisioned by the
// model-unpack init container: model-tmp is an emptyDir for OCI pull cache (kit cache),
// and model is an emptyDir that receives the unpacked content at spec.model.mountPath.
// When spec.model.imagePullSecrets is set, the first secret is mounted for registry auth.
func buildVolumes(ms *modelv1alpha1.ModelService) []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: ModelVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: ModelTmpVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}
	if len(ms.Spec.Model.ImagePullSecrets) > 0 {
		volumes = append(volumes, corev1.Volume{
			Name: DockerConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: ms.Spec.Model.ImagePullSecrets[0].Name,
					Items: []corev1.KeyToPath{
						{Key: ".dockerconfigjson", Path: "config.json"},
					},
				},
			},
		})
	}
	return volumes
}

func enginePort(ms *modelv1alpha1.ModelService) int32 {
	if ms.Spec.Engine.Port > 0 {
		return ms.Spec.Engine.Port
	}
	return 8000
}

func routingProxyTargetPort(ms *modelv1alpha1.ModelService) int32 {
	if ms.Spec.RoutingProxy != nil && ms.Spec.RoutingProxy.TargetPort > 0 {
		return ms.Spec.RoutingProxy.TargetPort
	}
	return 8200
}
