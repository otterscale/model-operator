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

// BuildEPPRBAC constructs the Role and RoleBinding for the EPP ServiceAccount.
//
// The EPP needs:
//   - Read access to pods (endpoint selection)
//   - Read access to InferencePool (GA API)
//   - Read access to InferenceObjective / InferenceModelRewrite (GAIE v1alpha2)
//   - When replicas > 1: leases + events for leader election
func BuildEPPRBAC(
	ms *modelv1alpha1.ModelService,
	metadataLabels map[string]string,
	replicas int32,
) (*rbacv1.Role, *rbacv1.RoleBinding) {
	name := EPPName(ms.Name)

	rules := []rbacv1.PolicyRule{
		{
			APIGroups: []string{""},
			Resources: []string{"pods"},
			Verbs:     []string{"get", "watch", "list"},
		},
		{
			APIGroups: []string{"inference.networking.x-k8s.io"},
			Resources: []string{"inferenceobjectives", "inferencemodelrewrites"},
			Verbs:     []string{"get", "watch", "list"},
		},
		{
			APIGroups: []string{"inference.networking.k8s.io"},
			Resources: []string{"inferencepools"},
			Verbs:     []string{"get", "watch", "list"},
		},
	}

	if replicas > 1 {
		rules = append(rules,
			rbacv1.PolicyRule{
				APIGroups: []string{"coordination.k8s.io"},
				Resources: []string{"leases"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			rbacv1.PolicyRule{
				APIGroups: []string{""},
				Resources: []string{"events"},
				Verbs:     []string{"create", "patch"},
			},
		)
	}

	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ms.Namespace,
			Labels:    metadataLabels,
		},
		Rules: rules,
	}

	binding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ms.Namespace,
			Labels:    metadataLabels,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      name,
				Namespace: ms.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     name,
		},
	}

	return role, binding
}
