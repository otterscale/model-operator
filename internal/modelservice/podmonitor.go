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
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// BuildPodMonitor constructs a typed PodMonitor for a serving role.
func BuildPodMonitor(
	ms *modelv1alpha1.ModelService,
	roleName string,
	selectorLabels map[string]string,
	metadataLabels map[string]string,
) *monitoringv1.PodMonitor {
	portName := "http"
	path := "/metrics"
	interval := "30s"

	if ms.Spec.Monitoring != nil && ms.Spec.Monitoring.PodMonitor != nil {
		mon := ms.Spec.Monitoring.PodMonitor
		if mon.PortName != "" {
			portName = mon.PortName
		}
		if mon.Path != "" {
			path = mon.Path
		}
		if mon.Interval != "" {
			interval = mon.Interval
		}
	}

	return &monitoringv1.PodMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name:      PodMonitorName(ms.Name, roleName),
			Namespace: ms.Namespace,
			Labels:    metadataLabels,
		},
		Spec: monitoringv1.PodMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: selectorLabels,
			},
			PodMetricsEndpoints: []monitoringv1.PodMetricsEndpoint{
				{
					Port:     &portName,
					Path:     path,
					Interval: monitoringv1.Duration(interval),
					RelabelConfigs: []monitoringv1.RelabelConfig{
						{
							Action:       "replace",
							SourceLabels: []monitoringv1.LabelName{"__meta_kubernetes_pod_label_app_kubernetes_io_name"},
							TargetLabel:  "model_service",
						},
					},
				},
			},
		},
	}
}
