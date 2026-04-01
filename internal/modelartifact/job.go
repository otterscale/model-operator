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

package modelartifact

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// pipelineScript executes the kit import → pack (for ModelPack) → push pipeline.
//
// kit import downloads to a temp dir and packs internally; it does NOT populate /workspace.
// - ModelKit: import tags as OCI_TARGET directly, skip pack, push.
// - ModelPack: import creates ModelKit, unpack to /workspace, repack with --use-model-pack.
//
// Registry authentication is handled by mounting a kubernetes.io/dockerconfigjson
// Secret at /.docker/config.json. kit (and the underlying OCI libraries) read
// this file automatically, so no explicit "kit login" step is required.
//
// SECURITY: Env vars (HF_REPO, HF_REVISION, OCI_TARGET, etc.) come from the Artifact CR.
// The CRD schema validates Model, Revision, Registry, Repository, and Tag with Pattern restrictions
// to prevent shell injection. Only users who can create Artifacts have access.
const pipelineScript = `set -euo pipefail
cd /workspace

kit import "$HF_REPO" \
  ${HF_REVISION:+--ref "$HF_REVISION"} \
  ${HF_TOKEN:+--token "$HF_TOKEN"} \
  --tag model:latest

if [ "$FORMAT" = "ModelPack" ]; then
  kit unpack -o model:latest -d /workspace
  kit pack /workspace --use-model-pack -t "$OCI_TARGET"
else
  kit tag model:latest "$OCI_TARGET"
fi

output=$(kit push ${PLAIN_HTTP:+--plain-http} "$OCI_TARGET" 2>&1)
echo "$output" >&2
digest=$(echo "$output" | grep -oE 'sha256:[a-f0-9]{64}' | tail -1)
echo -n "$digest" > /dev/termination-log
`

// BuildPVC constructs a PersistentVolumeClaim for the import/pack/push workspace.
// The PVC name is deterministic (<artifact-name>-workspace) since there is a 1:1
// relationship between an Artifact and its workspace PVC.
func BuildPVC(artifact *modelv1alpha1.ModelArtifact, labels map[string]string) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PVCName(artifact.Name),
			Namespace: artifact.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: artifact.Spec.Storage.Size,
				},
			},
		},
	}

	if artifact.Spec.Storage.StorageClassName != nil {
		pvc.Spec.StorageClassName = artifact.Spec.Storage.StorageClassName
	}

	return pvc
}

// BuildJob constructs a batch/v1 Job that executes the kit import → pack → push pipeline.
//
// Design choices:
//   - GenerateName avoids name collisions when the Artifact spec changes
//   - backoffLimit=0 prevents the Job from retrying on its own; the controller handles retries
//   - terminationMessagePath writes the OCI digest for the controller to read
//   - HuggingFace tokens are injected via env valueFrom, never passing through the controller
//   - OCI registry credentials (kubernetes.io/dockerconfigjson) are mounted at /.docker/config.json
//     so that kit push authenticates transparently without an explicit login step
func BuildJob(artifact *modelv1alpha1.ModelArtifact, kitImage string, labels map[string]string) *batchv1.Job {
	env := buildEnvVars(artifact)
	fsGroup := int64(1000)

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      WorkspaceVolumeName,
			MountPath: WorkspaceMountPath,
		},
	}

	volumes := []corev1.Volume{
		{
			Name: WorkspaceVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: PVCName(artifact.Name),
				},
			},
		},
	}

	if cred := artifact.Spec.Target.CredentialsSecretRef; cred != nil {
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      DockerConfigVolumeName,
			MountPath: DockerConfigMountPath,
			ReadOnly:  true,
		})
		volumes = append(volumes, corev1.Volume{
			Name: DockerConfigVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cred.Name,
					Items: []corev1.KeyToPath{
						{
							Key:  ".dockerconfigjson",
							Path: "config.json",
						},
					},
				},
			},
		})
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: artifact.Name + "-",
			Namespace:    artifact.Namespace,
			Labels:       labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            new(int32),
			TTLSecondsAfterFinished: new(DefaultJobTTLSeconds),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					AutomountServiceAccountToken: new(false),
					RestartPolicy:                corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: new(true),
						FSGroup:      &fsGroup,
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
						},
					},
					Containers: []corev1.Container{
						{
							Name:    "kit",
							Image:   kitImage,
							Command: []string{"/bin/sh", "-c"},
							Args:    []string{pipelineScript},
							Env:     env,
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: new(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
							},
							VolumeMounts:             volumeMounts,
							TerminationMessagePath:   "/dev/termination-log",
							TerminationMessagePolicy: corev1.TerminationMessageReadFile,
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("100m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
					Volumes: volumes,
				},
			},
		},
	}

	return job
}

// OCIReference returns the full OCI target reference for kit pack/push.
func OCIReference(artifact *modelv1alpha1.ModelArtifact) string {
	tag := artifact.Spec.Target.Tag
	if tag == "" {
		tag = "latest"
	}
	return fmt.Sprintf("%s/%s:%s", artifact.Spec.Target.Registry, artifact.Spec.Target.Repository, tag)
}

func buildEnvVars(artifact *modelv1alpha1.ModelArtifact) []corev1.EnvVar {
	envs := []corev1.EnvVar{
		{Name: "OCI_TARGET", Value: OCIReference(artifact)},
		{Name: "FORMAT", Value: string(artifact.Spec.Format)},
		{Name: "DOCKER_CONFIG", Value: DockerConfigMountPath},
	}

	if artifact.Spec.Target.Insecure {
		envs = append(envs, corev1.EnvVar{Name: "PLAIN_HTTP", Value: "true"})
	}

	if hf := artifact.Spec.Source.HuggingFace; hf != nil {
		envs = append(envs, corev1.EnvVar{Name: "HF_REPO", Value: hf.Model})

		if hf.Revision != "" {
			envs = append(envs, corev1.EnvVar{Name: "HF_REVISION", Value: hf.Revision})
		}

		if hf.TokenSecretRef != nil {
			envs = append(envs, corev1.EnvVar{
				Name: "HF_TOKEN",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: hf.TokenSecretRef.Name,
						},
						Key: secretKey(hf.TokenSecretRef),
					},
				},
			})
		}
	}

	return envs
}

func secretKey(ref *modelv1alpha1.SecretKeySelector) string {
	if ref.Key != "" {
		return ref.Key
	}
	return "token"
}
