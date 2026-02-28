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

package artifact

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// pipelineScript executes the kit import → pack → push pipeline.
// SECURITY: Env vars (HF_REPO, HF_REVISION, OCI_TARGET, etc.) come from the ModelArtifact CR.
// The CRD schema validates Model, Revision, Registry, Repository, and Tag with Pattern restrictions
// to prevent shell injection. Only users who can create ModelArtifacts have access.
const pipelineScript = `set -euo pipefail
cd /workspace

import_args="$HF_REPO"
if [ -n "${HF_REVISION:-}" ]; then import_args="$import_args --ref $HF_REVISION"; fi
if [ -n "${HF_TOKEN:-}" ]; then import_args="$import_args --token $HF_TOKEN"; fi
kit import $import_args

pack_flags=""
if [ "$FORMAT" = "ModelPack" ]; then pack_flags="--use-model-pack"; fi
kit pack . $pack_flags -t "$OCI_TARGET"

if [ -n "${OCI_USER:-}" ] && [ -n "${OCI_PASS:-}" ]; then
  kit login "$(echo "$OCI_TARGET" | cut -d'/' -f1)" -u "$OCI_USER" -p "$OCI_PASS"
fi

push_flags=""
if [ "${PLAIN_HTTP:-}" = "true" ]; then push_flags="--plain-http"; fi

output=$(kit push $push_flags "$OCI_TARGET" 2>&1)
echo "$output" >&2
digest=$(echo "$output" | grep -oE 'sha256:[a-f0-9]{64}' | tail -1)
echo -n "$digest" > /dev/termination-log
`

// BuildPVC constructs a PersistentVolumeClaim for the import/pack/push workspace.
// The PVC name is deterministic (<artifact-name>-workspace) since there is a 1:1
// relationship between a ModelArtifact and its workspace PVC.
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
//   - GenerateName avoids name collisions when the ModelArtifact spec changes
//   - backoffLimit=0 prevents the Job from retrying on its own; the controller handles retries
//   - terminationMessagePath writes the OCI digest for the controller to read
//   - Secrets are injected via env valueFrom, never passing through the controller
func BuildJob(artifact *modelv1alpha1.ModelArtifact, kitImage string, labels map[string]string) *batchv1.Job {
	env := buildEnvVars(artifact)

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: artifact.Name + "-",
			Namespace:    artifact.Namespace,
			Labels:       labels,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: new(int32),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:    "kit",
							Image:   kitImage,
							Command: []string{"/bin/sh", "-c"},
							Args:    []string{pipelineScript},
							Env:     env,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      WorkspaceVolumeName,
									MountPath: WorkspaceMountPath,
								},
							},
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
					Volumes: []corev1.Volume{
						{
							Name: WorkspaceVolumeName,
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: PVCName(artifact.Name),
								},
							},
						},
					},
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

	if cred := artifact.Spec.Target.CredentialsSecretRef; cred != nil {
		envs = append(envs,
			corev1.EnvVar{
				Name: "OCI_USER",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: cred.Name},
						Key:                  "username",
					},
				},
			},
			corev1.EnvVar{
				Name: "OCI_PASS",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: cred.Name},
						Key:                  "password",
					},
				},
			},
		)
	}

	return envs
}

func secretKey(ref *modelv1alpha1.SecretKeySelector) string {
	if ref.Key != "" {
		return ref.Key
	}
	return "token"
}
