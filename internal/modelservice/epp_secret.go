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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// BuildEPPSATokenSecret constructs a ServiceAccountToken Secret for the EPP.
// This token is used by Prometheus to authenticate when scraping metrics.
func BuildEPPSATokenSecret(
	ms *modelv1alpha1.ModelService,
	metadataLabels map[string]string,
) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      EPPSecretName(ms.Name),
			Namespace: ms.Namespace,
			Labels:    metadataLabels,
			Annotations: map[string]string{
				"kubernetes.io/service-account.name": EPPName(ms.Name),
			},
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
}
