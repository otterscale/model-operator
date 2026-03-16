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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

var _ = Describe("PluginsConfigFile", func() {
	It("should return default config for non-PD mode", func() {
		ms := &modelv1alpha1.ModelService{
			Spec: modelv1alpha1.ModelServiceSpec{
				Prefill: nil,
			},
		}
		Expect(PluginsConfigFile(ms)).To(Equal(DefaultPluginsConfigFile))
	})

	It("should return PD config when prefill is configured", func() {
		ms := &modelv1alpha1.ModelService{
			Spec: modelv1alpha1.ModelServiceSpec{
				Prefill: &modelv1alpha1.RoleSpec{Replicas: new(int32(1))},
			},
		}
		Expect(PluginsConfigFile(ms)).To(Equal(PDPluginsConfigFile))
	})
})

var _ = Describe("BuildEPPConfigMap", func() {
	Context("non-PD mode", func() {
		It("should contain default-plugins.yaml with expected plugins", func() {
			ms := &modelv1alpha1.ModelService{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: modelv1alpha1.ModelServiceSpec{
					Prefill: nil,
				},
			}

			cm := BuildEPPConfigMap(ms, nil)

			Expect(cm.Name).To(Equal("test-epp-config"))

			data, ok := cm.Data[DefaultPluginsConfigFile]
			Expect(ok).To(BeTrue(), "Non-PD ConfigMap should contain default-plugins.yaml")
			Expect(data).To(ContainSubstring("EndpointPickerConfig"))
			Expect(data).To(ContainSubstring("queue-scorer"))
			Expect(data).To(ContainSubstring("kv-cache-utilization-scorer"))
			Expect(data).To(ContainSubstring("prefix-cache-scorer"))
			Expect(data).To(ContainSubstring("schedulingProfiles"))
		})
	})

	Context("PD mode", func() {
		It("should contain pd-config.yaml with PD-specific plugins", func() {
			ms := &modelv1alpha1.ModelService{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
				Spec: modelv1alpha1.ModelServiceSpec{
					Prefill: &modelv1alpha1.RoleSpec{Replicas: new(int32(1))},
				},
			}

			cm := BuildEPPConfigMap(ms, nil)

			data, ok := cm.Data[PDPluginsConfigFile]
			Expect(ok).To(BeTrue(), "PD ConfigMap should contain pd-config.yaml")
			Expect(data).To(ContainSubstring("EndpointPickerConfig"))
			Expect(data).To(ContainSubstring("prefill-header-handler"))
			Expect(data).To(ContainSubstring("prefix-cache-scorer"))
			Expect(data).To(ContainSubstring("pd-profile-handler"))
			Expect(data).To(ContainSubstring("schedulingProfiles"))
			Expect(cm.Data).NotTo(HaveKey(DefaultPluginsConfigFile))
		})
	})
})

var _ = Describe("ConfigMapHash", func() {
	It("should produce a non-empty 64-char SHA-256 hex for non-PD mode", func() {
		ms := &modelv1alpha1.ModelService{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: modelv1alpha1.ModelServiceSpec{
				Prefill: nil,
			},
		}
		hash := ConfigMapHash(ms)
		Expect(hash).NotTo(BeEmpty())
		Expect(hash).To(HaveLen(64))
	})

	It("should produce a non-empty 64-char SHA-256 hex for PD mode", func() {
		ms := &modelv1alpha1.ModelService{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: modelv1alpha1.ModelServiceSpec{
				Prefill: &modelv1alpha1.RoleSpec{Replicas: new(int32(1))},
			},
		}
		hash := ConfigMapHash(ms)
		Expect(hash).NotTo(BeEmpty())
		Expect(hash).To(HaveLen(64))
	})

	It("should be deterministic", func() {
		ms := &modelv1alpha1.ModelService{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: modelv1alpha1.ModelServiceSpec{
				Prefill: &modelv1alpha1.RoleSpec{Replicas: new(int32(1))},
			},
		}
		Expect(ConfigMapHash(ms)).To(Equal(ConfigMapHash(ms)))
	})

	It("should differ between non-PD and PD modes", func() {
		msNonPD := &modelv1alpha1.ModelService{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
		}
		msPD := &modelv1alpha1.ModelService{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: modelv1alpha1.ModelServiceSpec{
				Prefill: &modelv1alpha1.RoleSpec{Replicas: new(int32(1))},
			},
		}
		Expect(ConfigMapHash(msNonPD)).NotTo(Equal(ConfigMapHash(msPD)))
	})
})

// Verify that the config YAML content is reasonable by checking key sections.
var _ = Describe("Config YAML content", func() {
	It("should have valid default plugins config", func() {
		Expect(strings.Contains(defaultPluginsConfig, "kind: EndpointPickerConfig")).To(BeTrue())
	})
	It("should have valid PD plugins config", func() {
		Expect(strings.Contains(pdPluginsConfig, "kind: EndpointPickerConfig")).To(BeTrue())
	})
})
