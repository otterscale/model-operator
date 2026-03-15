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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
	"github.com/otterscale/model-operator/internal/modelservice"
)

var _ = Describe("ModelService Controller", func() {
	const (
		msName      = "test-modelservice"
		msNamespace = "default"
	)

	ctx := context.Background()

	Context("When reconciling a ModelService", func() {
		var ms *modelv1alpha1.ModelService

		BeforeEach(func() {
			replicas := int32(1)
			ms = &modelv1alpha1.ModelService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      msName,
					Namespace: msNamespace,
				},
				Spec: modelv1alpha1.ModelServiceSpec{
					Model: modelv1alpha1.ModelSpec{
						Name:  "test/model",
						Image: "registry.example.com/models/test:v1",
					},
					Engine: modelv1alpha1.EngineSpec{
						Image: "vllm/vllm-openai:latest",
						Port:  8000,
					},
					Accelerator: modelv1alpha1.AcceleratorSpec{
						Type: modelv1alpha1.AcceleratorNvidia,
					},
					Decode: modelv1alpha1.RoleSpec{
						Replicas: &replicas,
						Parallelism: modelv1alpha1.ParallelismSpec{
							Tensor: 1,
						},
					},
				},
			}

			Expect(k8sClient.Create(ctx, ms)).To(Succeed())
		})

		AfterEach(func() {
			var latest modelv1alpha1.ModelService
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ms), &latest); err != nil {
				return
			}
			if ctrlutil.ContainsFinalizer(&latest, modelservice.FinalizerClusterRBAC) {
				ctrlutil.RemoveFinalizer(&latest, modelservice.FinalizerClusterRBAC)
				Expect(k8sClient.Update(ctx, &latest)).To(Succeed())
			}
			Expect(k8sClient.Delete(ctx, &latest)).To(Succeed())
		})

		It("should create the decode Deployment", func() {
			reconciler := &ModelServiceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Version:  "test",
				Recorder: &events.FakeRecorder{},
				KitImage: "ghcr.io/kitops-ml/kitops:v1.11.0",
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      msName,
					Namespace: msNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			decodeName := modelservice.DecodeName(msName)
			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      decodeName,
				Namespace: msNamespace,
			}, &dep)).To(Succeed())

			Expect(*dep.Spec.Replicas).To(Equal(int32(1)))
			Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("vllm/vllm-openai:latest"))

			Expect(dep.Spec.Template.Spec.Volumes).NotTo(BeEmpty())
			var modelVol *corev1.Volume
			for i := range dep.Spec.Template.Spec.Volumes {
				v := &dep.Spec.Template.Spec.Volumes[i]
				if v.Name == modelservice.ModelVolumeName {
					modelVol = v
					break
				}
			}
			Expect(modelVol).NotTo(BeNil())
			Expect(modelVol.EmptyDir).NotTo(BeNil())
			Expect(dep.Spec.Template.Spec.InitContainers).NotTo(BeEmpty())
			Expect(dep.Spec.Template.Spec.InitContainers[0].Name).To(Equal("model-unpack"))
		})

		It("should not create prefill Deployment when not specified", func() {
			reconciler := &ModelServiceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Version:  "test",
				Recorder: &events.FakeRecorder{},
				KitImage: "ghcr.io/kitops-ml/kitops:v1.11.0",
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      msName,
					Namespace: msNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			prefillName := modelservice.PrefillName(msName)
			var dep appsv1.Deployment
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name:      prefillName,
				Namespace: msNamespace,
			}, &dep)
			Expect(err).To(HaveOccurred())
		})

		It("should update status after reconciliation", func() {
			reconciler := &ModelServiceReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Version:  "test",
				Recorder: &events.FakeRecorder{},
				KitImage: "ghcr.io/kitops-ml/kitops:v1.11.0",
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      msName,
					Namespace: msNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			var updated modelv1alpha1.ModelService
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      msName,
				Namespace: msNamespace,
			}, &updated)).To(Succeed())

			Expect(updated.Status.ObservedGeneration).To(Equal(updated.Generation))
			Expect(updated.Status.Phase).NotTo(BeEmpty())
		})
	})
})
