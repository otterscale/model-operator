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
	"github.com/otterscale/model-operator/internal/labels"
)

const (
	// ConditionTypeReady indicates whether the artifact pipeline has completed
	// and the OCI artifact is available in the target registry.
	ConditionTypeReady = "Ready"

	// ComponentArtifact is the Kubernetes recommended label component value.
	ComponentArtifact = "model-artifact"

	// WorkspaceVolumeName is the name of the PVC volume mounted into the Job Pod.
	WorkspaceVolumeName = "workspace"

	// WorkspaceMountPath is the mount path for the workspace volume inside the Job Pod.
	WorkspaceMountPath = "/workspace"

	// PVCSuffix is appended to the ModelArtifact name to form the PVC name.
	PVCSuffix = "-workspace"

	// DefaultJobTTLSeconds is the time after Job completion (succeeded or failed)
	// before Kubernetes automatically deletes it. Allows users to view logs via kubectl
	// before cleanup; after deletion, the PVC can be released.
	DefaultJobTTLSeconds = int32(3600)
)

// LabelsForArtifact returns the standard set of labels for resources managed
// by a ModelArtifact (Jobs, PVCs). It builds on the shared labels.Standard() base.
func LabelsForArtifact(artifactName, version string) map[string]string {
	return labels.Standard(artifactName, ComponentArtifact, version)
}

// PVCName returns the deterministic PVC name for a given ModelArtifact.
func PVCName(artifactName string) string {
	return artifactName + PVCSuffix
}
