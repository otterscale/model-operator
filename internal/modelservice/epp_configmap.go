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
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

const (
	DefaultPluginsConfigFile = "default-plugins.yaml"
	PDPluginsConfigFile      = "pd-config.yaml"
)

// defaultPluginsConfig is the standard EndpointPickerConfig for non-PD mode.
// It mirrors the upstream epplib default-plugins.yaml with queue, KV-cache,
// prefix-cache scorers, a metrics-data-source, and a core-metrics-extractor.
const defaultPluginsConfig = `apiVersion: inference.networking.x-k8s.io/v1alpha1
kind: EndpointPickerConfig
plugins:
- type: queue-scorer
- type: kv-cache-utilization-scorer
- type: prefix-cache-scorer
- type: metrics-data-source
  parameters:
    scheme: "http"
    path: "/metrics"
    insecureSkipVerify: true
- type: core-metrics-extractor
schedulingProfiles:
- name: default
  plugins:
  - pluginRef: queue-scorer
    weight: 2
  - pluginRef: kv-cache-utilization-scorer
    weight: 2
  - pluginRef: prefix-cache-scorer
    weight: 3
`

// pdPluginsConfig is the EndpointPickerConfig for Prefill/Decode disaggregation.
// It includes plugins for prefix-cache-aware scheduling and PD profile routing.
const pdPluginsConfig = `apiVersion: inference.networking.x-k8s.io/v1alpha1
kind: EndpointPickerConfig
featureGates:
- prepareDataPlugins
plugins:
- type: prefill-header-handler
- type: prefix-cache-scorer
  parameters:
    maxPrefixBlocksToMatch: 256
    lruCapacityPerServer: 31250
- type: queue-scorer
- type: prefill-filter
- type: decode-filter
- type: max-score-picker
- type: prefix-based-pd-decider
  parameters:
    nonCachedTokens: 16
- type: pd-profile-handler
  parameters:
    primaryPort: 0
    deciderPluginName: prefix-based-pd-decider
schedulingProfiles:
- name: prefill
  plugins:
  - pluginRef: prefill-filter
  - pluginRef: max-score-picker
  - pluginRef: prefix-cache-scorer
    weight: 2
  - pluginRef: queue-scorer
    weight: 1
- name: decode
  plugins:
  - pluginRef: decode-filter
  - pluginRef: max-score-picker
  - pluginRef: prefix-cache-scorer
    weight: 2
  - pluginRef: queue-scorer
    weight: 1
`

// PluginsConfigFile returns the plugins config filename based on whether
// Prefill/Decode disaggregation is enabled.
func PluginsConfigFile(ms *modelv1alpha1.ModelService) string {
	if ms.Spec.Prefill != nil {
		return PDPluginsConfigFile
	}
	return DefaultPluginsConfigFile
}

// ConfigMapHash returns a SHA-256 hash of the EPP ConfigMap data.
// When the hash changes (e.g. switching from non-PD to PD mode), the EPP
// Deployment's pod template annotation triggers an automatic rollout.
func ConfigMapHash(ms *modelv1alpha1.ModelService) string {
	cm := BuildEPPConfigMap(ms, nil)
	keys := make([]string, 0, len(cm.Data))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	for _, k := range keys {
		sb.WriteString(k)
		sb.WriteString(cm.Data[k])
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(sb.String())))
}

// BuildEPPConfigMap constructs the ConfigMap for the EPP plugins configuration.
//
// Both modes produce an explicit config file so the operator fully controls the
// scheduling behaviour regardless of what the EPP image ships as built-in defaults.
func BuildEPPConfigMap(
	ms *modelv1alpha1.ModelService,
	metadataLabels map[string]string,
) *corev1.ConfigMap {
	data := map[string]string{}

	if ms.Spec.Prefill != nil {
		data[PDPluginsConfigFile] = pdPluginsConfig
	} else {
		data[DefaultPluginsConfigFile] = defaultPluginsConfig
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      EPPConfigMapName(ms.Name),
			Namespace: ms.Namespace,
			Labels:    metadataLabels,
		},
		Data: data,
	}
}
