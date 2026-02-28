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

package artifact_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
	"github.com/otterscale/model-operator/internal/artifact"
)

func newTestArtifact() *modelv1alpha1.ModelArtifact {
	return &modelv1alpha1.ModelArtifact{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "phi-4",
			Namespace: "ml-team",
		},
		Spec: modelv1alpha1.ModelArtifactSpec{
			Source: modelv1alpha1.ModelSource{
				HuggingFace: &modelv1alpha1.HuggingFaceSource{
					Repository: "microsoft/phi-4",
					Revision:   "main",
					TokenSecretRef: &modelv1alpha1.SecretKeySelector{
						Name: "hf-token",
						Key:  "token",
					},
				},
			},
			Target: modelv1alpha1.OCITarget{
				Repository: "ghcr.io/myorg/models/phi-4",
				Tag:        "v1.0",
				CredentialsSecretRef: &modelv1alpha1.SecretReference{
					Name: "oci-creds",
				},
			},
			Format: modelv1alpha1.PackFormatModelPack,
			Storage: modelv1alpha1.StorageSpec{
				Size: resource.MustParse("100Gi"),
			},
		},
	}
}

var _ = Describe("BuildPVC", func() {
	var (
		ma     *modelv1alpha1.ModelArtifact
		labels map[string]string
	)

	BeforeEach(func() {
		ma = newTestArtifact()
		labels = map[string]string{"app": "test"}
	})

	It("should set the deterministic name and namespace", func() {
		pvc := artifact.BuildPVC(ma, labels)
		Expect(pvc.Name).To(Equal("phi-4-workspace"))
		Expect(pvc.Namespace).To(Equal("ml-team"))
	})

	It("should set the requested storage size", func() {
		pvc := artifact.BuildPVC(ma, labels)
		qty := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		Expect(qty.Cmp(resource.MustParse("100Gi"))).To(Equal(0))
	})

	It("should set ReadWriteOnce access mode", func() {
		pvc := artifact.BuildPVC(ma, labels)
		Expect(pvc.Spec.AccessModes).To(ConsistOf(corev1.ReadWriteOnce))
	})

	It("should leave StorageClassName nil when not specified", func() {
		pvc := artifact.BuildPVC(ma, labels)
		Expect(pvc.Spec.StorageClassName).To(BeNil())
	})

	It("should set StorageClassName when specified", func() {
		ma.Spec.Storage.StorageClassName = new("fast-ssd")
		pvc := artifact.BuildPVC(ma, labels)
		Expect(pvc.Spec.StorageClassName).NotTo(BeNil())
		Expect(*pvc.Spec.StorageClassName).To(Equal("fast-ssd"))
	})

	It("should propagate labels", func() {
		pvc := artifact.BuildPVC(ma, labels)
		Expect(pvc.Labels).To(HaveKeyWithValue("app", "test"))
	})
})

var _ = Describe("BuildJob", func() {
	var (
		ma       *modelv1alpha1.ModelArtifact
		labels   map[string]string
		kitImage string
	)

	BeforeEach(func() {
		ma = newTestArtifact()
		labels = map[string]string{"app": "test"}
		kitImage = "ghcr.io/jozu-ai/kit:latest"
	})

	It("should use GenerateName with the artifact name prefix", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		Expect(job.GenerateName).To(Equal("phi-4-"))
		Expect(job.Name).To(BeEmpty())
	})

	It("should set the correct namespace", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		Expect(job.Namespace).To(Equal("ml-team"))
	})

	It("should set backoffLimit to zero", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		Expect(job.Spec.BackoffLimit).NotTo(BeNil())
		Expect(*job.Spec.BackoffLimit).To(Equal(int32(0)))
	})

	It("should set restart policy to Never", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		Expect(job.Spec.Template.Spec.RestartPolicy).To(Equal(corev1.RestartPolicyNever))
	})

	It("should use the kit image for the container", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
		Expect(job.Spec.Template.Spec.Containers[0].Image).To(Equal(kitImage))
	})

	It("should use /bin/sh -c as command", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		container := job.Spec.Template.Spec.Containers[0]
		Expect(container.Command).To(Equal([]string{"/bin/sh", "-c"}))
	})

	It("should set terminationMessagePath", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		container := job.Spec.Template.Spec.Containers[0]
		Expect(container.TerminationMessagePath).To(Equal("/dev/termination-log"))
	})

	It("should mount the workspace volume", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		container := job.Spec.Template.Spec.Containers[0]
		Expect(container.VolumeMounts).To(ContainElement(
			corev1.VolumeMount{
				Name:      artifact.WorkspaceVolumeName,
				MountPath: artifact.WorkspaceMountPath,
			},
		))
	})

	It("should reference the PVC as volume source", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		volumes := job.Spec.Template.Spec.Volumes
		Expect(volumes).To(ContainElement(SatisfyAll(
			HaveField("Name", artifact.WorkspaceVolumeName),
			HaveField("VolumeSource.PersistentVolumeClaim.ClaimName", "phi-4-workspace"),
		)))
	})

	It("should include HF_REPO env var", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		container := job.Spec.Template.Spec.Containers[0]
		Expect(container.Env).To(ContainElement(
			corev1.EnvVar{Name: "HF_REPO", Value: "microsoft/phi-4"},
		))
	})

	It("should include OCI_TARGET env var", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		container := job.Spec.Template.Spec.Containers[0]
		Expect(container.Env).To(ContainElement(
			corev1.EnvVar{Name: "OCI_TARGET", Value: "ghcr.io/myorg/models/phi-4:v1.0"},
		))
	})

	It("should inject HF_TOKEN from secret", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		container := job.Spec.Template.Spec.Containers[0]
		Expect(container.Env).To(ContainElement(SatisfyAll(
			HaveField("Name", "HF_TOKEN"),
			HaveField("ValueFrom.SecretKeyRef.Name", "hf-token"),
			HaveField("ValueFrom.SecretKeyRef.Key", "token"),
		)))
	})

	It("should inject OCI credentials from secret", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		container := job.Spec.Template.Spec.Containers[0]
		Expect(container.Env).To(ContainElement(SatisfyAll(
			HaveField("Name", "OCI_USER"),
			HaveField("ValueFrom.SecretKeyRef.Name", "oci-creds"),
			HaveField("ValueFrom.SecretKeyRef.Key", "username"),
		)))
		Expect(container.Env).To(ContainElement(SatisfyAll(
			HaveField("Name", "OCI_PASS"),
			HaveField("ValueFrom.SecretKeyRef.Name", "oci-creds"),
			HaveField("ValueFrom.SecretKeyRef.Key", "password"),
		)))
	})

	It("should propagate labels to pod template", func() {
		job := artifact.BuildJob(ma, kitImage, labels)
		Expect(job.Spec.Template.Labels).To(HaveKeyWithValue("app", "test"))
	})

	Context("without optional fields", func() {
		BeforeEach(func() {
			ma = &modelv1alpha1.ModelArtifact{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "small-model",
					Namespace: "default",
				},
				Spec: modelv1alpha1.ModelArtifactSpec{
					Source: modelv1alpha1.ModelSource{
						HuggingFace: &modelv1alpha1.HuggingFaceSource{
							Repository: "org/model",
						},
					},
					Target: modelv1alpha1.OCITarget{
						Repository: "registry.local/models/small",
					},
					Format: modelv1alpha1.PackFormatModelKit,
					Storage: modelv1alpha1.StorageSpec{
						Size: resource.MustParse("10Gi"),
					},
				},
			}
		})

		It("should not include HF_TOKEN env when no secret ref", func() {
			job := artifact.BuildJob(ma, kitImage, nil)
			container := job.Spec.Template.Spec.Containers[0]
			for _, e := range container.Env {
				Expect(e.Name).NotTo(Equal("HF_TOKEN"))
			}
		})

		It("should not include OCI credential envs when no secret ref", func() {
			job := artifact.BuildJob(ma, kitImage, nil)
			container := job.Spec.Template.Spec.Containers[0]
			for _, e := range container.Env {
				Expect(e.Name).NotTo(BeElementOf("OCI_USER", "OCI_PASS"))
			}
		})

		It("should not include HF_REVISION when empty", func() {
			job := artifact.BuildJob(ma, kitImage, nil)
			container := job.Spec.Template.Spec.Containers[0]
			for _, e := range container.Env {
				Expect(e.Name).NotTo(Equal("HF_REVISION"))
			}
		})

		It("should default OCI_TARGET tag to latest", func() {
			job := artifact.BuildJob(ma, kitImage, nil)
			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "OCI_TARGET", Value: "registry.local/models/small:latest"},
			))
		})

		It("should set FORMAT to ModelKit", func() {
			job := artifact.BuildJob(ma, kitImage, nil)
			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "FORMAT", Value: "ModelKit"},
			))
		})
	})

	Context("Insecure", func() {
		It("should set PLAIN_HTTP=true when enabled", func() {
			ma.Spec.Target.Insecure = true
			job := artifact.BuildJob(ma, kitImage, nil)
			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Env).To(ContainElement(
				corev1.EnvVar{Name: "PLAIN_HTTP", Value: "true"},
			))
		})
	})
})

var _ = Describe("OCIReference", func() {
	DescribeTable("tag resolution",
		func(repo, tag, expected string) {
			ma := &modelv1alpha1.ModelArtifact{
				Spec: modelv1alpha1.ModelArtifactSpec{
					Target: modelv1alpha1.OCITarget{
						Repository: repo,
						Tag:        tag,
					},
				},
			}
			Expect(artifact.OCIReference(ma)).To(Equal(expected))
		},
		Entry("with explicit tag", "ghcr.io/org/model", "v1.0", "ghcr.io/org/model:v1.0"),
		Entry("defaults to latest when tag is empty", "ghcr.io/org/model", "", "ghcr.io/org/model:latest"),
	)
})

var _ = Describe("PVCName", func() {
	It("should append -workspace suffix", func() {
		Expect(artifact.PVCName("my-model")).To(Equal("my-model-workspace"))
	})
})
