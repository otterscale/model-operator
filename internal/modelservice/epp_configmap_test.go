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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

func TestPluginsConfigFile_NonPD(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		Spec: modelv1alpha1.ModelServiceSpec{
			Prefill: nil,
		},
	}
	if got := PluginsConfigFile(ms); got != DefaultPluginsConfigFile {
		t.Errorf("PluginsConfigFile = %q, want %q", got, DefaultPluginsConfigFile)
	}
}

func TestPluginsConfigFile_PD(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		Spec: modelv1alpha1.ModelServiceSpec{
			Prefill: &modelv1alpha1.RoleSpec{Replicas: ptr.To(int32(1))},
		},
	}
	if got := PluginsConfigFile(ms); got != PDPluginsConfigFile {
		t.Errorf("PluginsConfigFile = %q, want %q", got, PDPluginsConfigFile)
	}
}

func TestBuildEPPConfigMap_NonPD(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: modelv1alpha1.ModelServiceSpec{
			Prefill: nil,
		},
	}

	cm := BuildEPPConfigMap(ms, nil)

	if cm.Name != "test-epp-config" {
		t.Errorf("Name = %q, want test-epp-config", cm.Name)
	}

	data, ok := cm.Data[DefaultPluginsConfigFile]
	if !ok {
		t.Fatal("Non-PD ConfigMap should contain default-plugins.yaml")
	}
	if !strings.Contains(data, "EndpointPickerConfig") {
		t.Error("Default config should contain EndpointPickerConfig")
	}
	if !strings.Contains(data, "queue-scorer") {
		t.Error("Default config should contain queue-scorer plugin")
	}
	if !strings.Contains(data, "kv-cache-utilization-scorer") {
		t.Error("Default config should contain kv-cache-utilization-scorer plugin")
	}
	if !strings.Contains(data, "prefix-cache-scorer") {
		t.Error("Default config should contain prefix-cache-scorer plugin")
	}
	if !strings.Contains(data, "metrics-data-source") {
		t.Error("Default config should contain metrics-data-source plugin")
	}
	if !strings.Contains(data, "core-metrics-extractor") {
		t.Error("Default config should contain core-metrics-extractor plugin")
	}
	if !strings.Contains(data, "schedulingProfiles") {
		t.Error("Default config should contain schedulingProfiles")
	}
}

func TestBuildEPPConfigMap_PD(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: modelv1alpha1.ModelServiceSpec{
			Prefill: &modelv1alpha1.RoleSpec{Replicas: ptr.To(int32(1))},
		},
	}

	cm := BuildEPPConfigMap(ms, nil)

	data, ok := cm.Data[PDPluginsConfigFile]
	if !ok {
		t.Fatal("PD ConfigMap should contain pd-config.yaml")
	}
	if !strings.Contains(data, "EndpointPickerConfig") {
		t.Error("PD config should contain EndpointPickerConfig")
	}
	if !strings.Contains(data, "prefill-header-handler") {
		t.Error("PD config should contain prefill-header-handler plugin")
	}
	if !strings.Contains(data, "prefix-cache-scorer") {
		t.Error("PD config should contain prefix-cache-scorer plugin")
	}
	if !strings.Contains(data, "pd-profile-handler") {
		t.Error("PD config should contain pd-profile-handler plugin")
	}
	if !strings.Contains(data, "schedulingProfiles") {
		t.Error("PD config should contain schedulingProfiles")
	}
	if _, hasDefault := cm.Data[DefaultPluginsConfigFile]; hasDefault {
		t.Error("PD ConfigMap should not contain default-plugins.yaml")
	}
}

func TestConfigMapHash_NonPD(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: modelv1alpha1.ModelServiceSpec{
			Prefill: nil,
		},
	}
	hash := ConfigMapHash(ms)
	if hash == "" {
		t.Fatal("Non-PD hash should not be empty (default config is now explicit)")
	}
	if len(hash) != 64 {
		t.Errorf("SHA-256 hex should be 64 chars, got %d", len(hash))
	}
}

func TestConfigMapHash_PD(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: modelv1alpha1.ModelServiceSpec{
			Prefill: &modelv1alpha1.RoleSpec{Replicas: ptr.To(int32(1))},
		},
	}
	hash := ConfigMapHash(ms)
	if hash == "" {
		t.Fatal("PD hash should not be empty")
	}
	if len(hash) != 64 {
		t.Errorf("SHA-256 hex should be 64 chars, got %d", len(hash))
	}
}

func TestConfigMapHash_Deterministic(t *testing.T) {
	ms := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: modelv1alpha1.ModelServiceSpec{
			Prefill: &modelv1alpha1.RoleSpec{Replicas: ptr.To(int32(1))},
		},
	}
	h1 := ConfigMapHash(ms)
	h2 := ConfigMapHash(ms)
	if h1 != h2 {
		t.Error("ConfigMapHash should be deterministic")
	}
}

func TestConfigMapHash_DiffersBetweenModes(t *testing.T) {
	msNonPD := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
	}
	msPD := &modelv1alpha1.ModelService{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		Spec: modelv1alpha1.ModelServiceSpec{
			Prefill: &modelv1alpha1.RoleSpec{Replicas: ptr.To(int32(1))},
		},
	}
	if ConfigMapHash(msNonPD) == ConfigMapHash(msPD) {
		t.Error("Non-PD and PD hashes should differ")
	}
}
