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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

const defaultMountPath = "/models"

// BuildDeployment constructs an apps/v1 Deployment for a serving role (decode or prefill).
//
// The Deployment uses a Kubernetes image volume (K8s >= 1.35) to mount the OCI
// model artifact directly — no init containers or PVC provisioning required.
// GPU resources are injected automatically based on accelerator type and parallelism.
func BuildDeployment(
	ms *modelv1alpha1.ModelService,
	role *modelv1alpha1.RoleSpec,
	roleName string,
	deployName string,
	podLabels map[string]string,
	metadataLabels map[string]string,
	selectorLabels map[string]string,
) *appsv1.Deployment {
	replicas := DefaultReplicas(role.Replicas)

	vllmContainer := buildVLLMContainer(ms, role)
	initContainers := buildInitContainers(ms)
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
					Labels: podLabels,
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
func buildVLLMContainer(
	ms *modelv1alpha1.ModelService,
	role *modelv1alpha1.RoleSpec,
) corev1.Container {
	port := enginePort(ms)
	vllmPort := port
	if ms.Spec.RoutingProxy != nil {
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

// buildInitContainers constructs init containers. When a routing proxy is configured,
// it runs as a native sidecar (restartable init container) that proxies traffic
// between the external port and vLLM.
func buildInitContainers(ms *modelv1alpha1.ModelService) []corev1.Container {
	if ms.Spec.RoutingProxy == nil {
		return nil
	}

	proxy := ms.Spec.RoutingProxy
	port := enginePort(ms)
	targetPort := routingProxyTargetPort(ms)
	always := corev1.ContainerRestartPolicyAlways

	return []corev1.Container{
		{
			Name:  "routing-proxy",
			Image: proxy.Image,
			Args: []string{
				"--port", fmt.Sprintf("%d", port),
				"--vllm-port", fmt.Sprintf("%d", targetPort),
				"--connector", proxy.Connector,
			},
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
		},
	}
}

// buildVolumes constructs the volume list. The model artifact is mounted via
// a Kubernetes image volume — the kubelet pulls the OCI image and exposes it
// as a read-only filesystem, leveraging the node's container image cache.
func buildVolumes(ms *modelv1alpha1.ModelService) []corev1.Volume {
	return []corev1.Volume{
		{
			Name: ModelVolumeName,
			VolumeSource: corev1.VolumeSource{
				Image: &corev1.ImageVolumeSource{
					Reference:  ms.Spec.Model.Image,
					PullPolicy: corev1.PullIfNotPresent,
				},
			},
		},
	}
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
