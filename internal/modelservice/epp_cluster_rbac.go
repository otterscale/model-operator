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
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// BuildEPPClusterRBAC constructs a ClusterRole and ClusterRoleBinding that
// grant the EPP ServiceAccount permissions for metrics authentication:
//   - tokenreviews (authenticate bearer tokens from Prometheus)
//   - subjectaccessreviews (authorise metrics scraping)
//   - /metrics non-resource URL access
//
// These are cluster-scoped and cannot carry an OwnerReference to the
// namespace-scoped ModelService; the controller uses a Finalizer to clean them up.
func BuildEPPClusterRBAC(
	ms *modelv1alpha1.ModelService,
	metadataLabels map[string]string,
) (*rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding) {
	name := EPPClusterRBACName(ms.Namespace, ms.Name)
	saName := EPPName(ms.Name)

	role := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: metadataLabels,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"authentication.k8s.io"},
				Resources: []string{"tokenreviews"},
				Verbs:     []string{"create"},
			},
			{
				APIGroups: []string{"authorization.k8s.io"},
				Resources: []string{"subjectaccessreviews"},
				Verbs:     []string{"create"},
			},
			{
				NonResourceURLs: []string{"/metrics"},
				Verbs:           []string{"get"},
			},
		},
	}

	binding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: metadataLabels,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: ms.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     name,
		},
	}

	return role, binding
}
