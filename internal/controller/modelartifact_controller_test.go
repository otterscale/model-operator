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

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
	"github.com/otterscale/model-operator/internal/artifact"
	"github.com/otterscale/model-operator/internal/labels"
)

var _ = Describe("ModelArtifact Controller", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	var (
		ctx          context.Context
		reconciler   *ModelArtifactReconciler
		ma           *modelv1alpha1.ModelArtifact
		resourceName string
		namespace    *corev1.Namespace
	)

	// --- Helpers ---

	makeModelArtifact := func(name, ns string, mods ...func(*modelv1alpha1.ModelArtifact)) *modelv1alpha1.ModelArtifact {
		a := &modelv1alpha1.ModelArtifact{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: modelv1alpha1.ModelArtifactSpec{
				Source: modelv1alpha1.ModelSource{
					HuggingFace: &modelv1alpha1.HuggingFaceSource{
						Model: "microsoft/phi-4",
					},
				},
				Target: modelv1alpha1.OCITarget{
					Registry:   "ghcr.io",
					Repository: "test/phi-4",
					Tag:        "latest",
				},
				Format: modelv1alpha1.PackFormatModelPack,
				Storage: modelv1alpha1.StorageSpec{
					Size: resource.MustParse("50Gi"),
				},
			},
		}
		for _, mod := range mods {
			mod(a)
		}
		return a
	}

	executeReconcile := func() {
		nsName := types.NamespacedName{Name: resourceName, Namespace: namespace.Name}
		_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: nsName})
		Expect(err).NotTo(HaveOccurred())
	}

	fetchResource := func(obj client.Object, name, ns string) {
		key := types.NamespacedName{Name: name, Namespace: ns}
		Eventually(func() error {
			return k8sClient.Get(ctx, key, obj)
		}, timeout, interval).Should(Succeed())
	}

	// --- Lifecycle ---

	BeforeEach(func() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		DeferCleanup(cancel)
		resourceName = "ma-" + string(uuid.NewUUID())[:8]

		namespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "ns-" + string(uuid.NewUUID())[:8]},
		}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		reconciler = &ModelArtifactReconciler{
			Client:   k8sClient,
			Scheme:   k8sClient.Scheme(),
			Version:  "test",
			KitImage: "ghcr.io/jozu-ai/kit:latest",
			Recorder: events.NewFakeRecorder(100),
		}
		ma = makeModelArtifact(resourceName, namespace.Name)
	})

	JustBeforeEach(func() {
		Expect(k8sClient.Create(ctx, ma)).To(Succeed())
	})

	AfterEach(func() {
		key := types.NamespacedName{Name: resourceName, Namespace: namespace.Name}
		if err := k8sClient.Get(ctx, key, ma); err == nil {
			Expect(k8sClient.Delete(ctx, ma)).To(Succeed())
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, key, ma))
			}, timeout, interval).Should(BeTrue())
		}
	})

	// --- Tests ---

	Context("Basic Reconciliation", func() {
		It("should create a workspace PVC and a Job", func() {
			executeReconcile()

			By("Verifying the PVC is created")
			var pvc corev1.PersistentVolumeClaim
			fetchResource(&pvc, artifact.PVCName(resourceName), namespace.Name)
			Expect(pvc.Spec.Resources.Requests[corev1.ResourceStorage]).To(Equal(resource.MustParse("50Gi")))
			Expect(pvc.Spec.AccessModes).To(ContainElement(corev1.ReadWriteOnce))
			Expect(pvc.Labels).To(HaveKeyWithValue(labels.ManagedBy, labels.Operator))
			Expect(pvc.Labels).To(HaveKeyWithValue(labels.Component, "model-artifact"))

			By("Verifying the Job is created")
			var jobList batchv1.JobList
			Expect(k8sClient.List(ctx, &jobList,
				client.InNamespace(namespace.Name),
				client.MatchingLabels(artifact.LabelsForArtifact(resourceName, "test")),
			)).To(Succeed())
			Expect(jobList.Items).To(HaveLen(1))
			job := jobList.Items[0]

			Expect(job.Spec.Template.Spec.Containers).To(HaveLen(1))
			container := job.Spec.Template.Spec.Containers[0]
			Expect(container.Image).To(Equal("ghcr.io/jozu-ai/kit:latest"))
			Expect(container.TerminationMessagePath).To(Equal("/dev/termination-log"))
			Expect(container.VolumeMounts).To(ContainElement(
				corev1.VolumeMount{
					Name:      artifact.WorkspaceVolumeName,
					MountPath: artifact.WorkspaceMountPath,
				},
			))

			By("Verifying OwnerReference is set on the Job")
			Expect(job.OwnerReferences).To(HaveLen(1))
			Expect(job.OwnerReferences[0].Name).To(Equal(resourceName))

			By("Verifying status updates")
			fetchResource(ma, resourceName, namespace.Name)
			Expect(ma.Status.ObservedGeneration).To(Equal(ma.Generation))
			Expect(ma.Status.JobRef).NotTo(BeNil())
			Expect(ma.Status.JobRef.Name).To(Equal(job.Name))
			Expect(ma.Status.JobRef.Namespace).To(Equal(namespace.Name))
		})
	})

	Context("Idempotency", func() {
		It("should not create duplicate resources on subsequent reconciles", func() {
			executeReconcile()
			executeReconcile()

			var jobList batchv1.JobList
			Expect(k8sClient.List(ctx, &jobList,
				client.InNamespace(namespace.Name),
				client.MatchingLabels(artifact.LabelsForArtifact(resourceName, "test")),
			)).To(Succeed())
			Expect(jobList.Items).To(HaveLen(1))
		})
	})

	Context("Deleted Resource", func() {
		It("should not reconcile a deleted ModelArtifact", func() {
			Expect(k8sClient.Delete(ctx, ma)).To(Succeed())
			Eventually(func() bool {
				return errors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{
					Name: resourceName, Namespace: namespace.Name,
				}, ma))
			}, timeout, interval).Should(BeTrue())

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: resourceName, Namespace: namespace.Name},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("Domain Helpers", func() {
		It("should generate correct labels", func() {
			artifactLabels := artifact.LabelsForArtifact("my-model", "v1")
			Expect(artifactLabels).To(HaveKeyWithValue(labels.Name, "my-model"))
			Expect(artifactLabels).To(HaveKeyWithValue(labels.Version, "v1"))
			Expect(artifactLabels).To(HaveKeyWithValue(labels.Component, "model-artifact"))
			Expect(artifactLabels).To(HaveKeyWithValue(labels.PartOf, "otterscale-system"))
			Expect(artifactLabels).To(HaveKeyWithValue(labels.ManagedBy, "model-operator"))
		})
	})
})
