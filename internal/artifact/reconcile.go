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
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlutil "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// EnsurePVC creates the workspace PVC if it does not exist.
// Caller must pass a client and scheme for OwnerReference.
func EnsurePVC(ctx context.Context, c client.Client, scheme *runtime.Scheme, ma *modelv1alpha1.ModelArtifact, labels map[string]string) error {
	pvcName := PVCName(ma.Name)
	var existing corev1.PersistentVolumeClaim
	err := c.Get(ctx, types.NamespacedName{Name: pvcName, Namespace: ma.Namespace}, &existing)
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}

	pvc := BuildPVC(ma, labels)
	if err := ctrlutil.SetControllerReference(ma, pvc, scheme); err != nil {
		return fmt.Errorf("setting PVC owner reference: %w", err)
	}
	if err := c.Create(ctx, pvc); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil
		}
		return err
	}
	log.FromContext(ctx).Info("Created workspace PVC", "pvc", pvcName)
	return nil
}

// EnsureJob finds an existing owned Job or creates a new one.
// Returns the job (existing or newly created), whether it was created, and any error.
func EnsureJob(ctx context.Context, c client.Client, scheme *runtime.Scheme, ma *modelv1alpha1.ModelArtifact, kitImage string, labels map[string]string) (*batchv1.Job, bool, error) {
	job, err := FindOwnedJob(ctx, c, ma.Namespace, labels, ma)
	if err != nil {
		return nil, false, err
	}
	if job != nil {
		return job, false, nil
	}

	job = BuildJob(ma, kitImage, labels)
	if err := ctrlutil.SetControllerReference(ma, job, scheme); err != nil {
		return nil, false, fmt.Errorf("setting Job owner reference: %w", err)
	}
	if err := c.Create(ctx, job); err != nil {
		return nil, false, err
	}
	log.FromContext(ctx).Info("Created pipeline Job", "job", job.Name)
	return job, true, nil
}

// FindOwnedJob returns the first non-deleted Job owned by owner with given labels.
func FindOwnedJob(ctx context.Context, c client.Client, namespace string, labels map[string]string, owner metav1.Object) (*batchv1.Job, error) {
	var list batchv1.JobList
	if err := c.List(ctx, &list, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
		return nil, err
	}
	for i := range list.Items {
		j := &list.Items[i]
		if j.DeletionTimestamp.IsZero() && isOwnedBy(j, owner) {
			return j, nil
		}
	}
	return nil, nil
}

// CleanupStaleJobs deletes all owned Jobs that are not yet marked for deletion.
// Called when generation changes to remove Jobs from the previous generation.
func CleanupStaleJobs(ctx context.Context, c client.Client, ma *modelv1alpha1.ModelArtifact, labels map[string]string) error {
	var jobList batchv1.JobList
	if err := c.List(ctx, &jobList, client.InNamespace(ma.Namespace), client.MatchingLabels(labels)); err != nil {
		return err
	}

	propagation := metav1.DeletePropagationBackground
	for i := range jobList.Items {
		j := &jobList.Items[i]
		// Only delete jobs we own that are not yet marked for deletion
		if !isOwnedBy(j, ma) || !j.DeletionTimestamp.IsZero() {
			continue
		}
		if err := c.Delete(ctx, j, &client.DeleteOptions{PropagationPolicy: &propagation}); err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		log.FromContext(ctx).Info("Deleted stale Job", "job", j.Name)
	}
	return nil
}

// DeletePVC removes the workspace PVC. Non-existence is ignored.
func DeletePVC(ctx context.Context, c client.Client, ma *modelv1alpha1.ModelArtifact) error {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PVCName(ma.Name),
			Namespace: ma.Namespace,
		},
	}
	if err := c.Delete(ctx, pvc); err != nil {
		return client.IgnoreNotFound(err)
	}
	log.FromContext(ctx).Info("Deleted workspace PVC", "pvc", pvc.Name)
	return nil
}

// isOwnedBy checks whether obj has an ownerReference pointing to the given owner.
func isOwnedBy(obj metav1.Object, owner metav1.Object) bool {
	for _, ref := range obj.GetOwnerReferences() {
		if ref.UID == owner.GetUID() {
			return true
		}
	}
	return false
}
