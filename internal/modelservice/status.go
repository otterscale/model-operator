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
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	modelv1alpha1 "github.com/otterscale/api/model/v1alpha1"
)

// ObservationResult holds the derived status from observing the current cluster state.
type ObservationResult struct {
	Phase           modelv1alpha1.ModelServicePhase
	DecodeReady     int32
	DecodeReplicas  int32
	PrefillReady    int32
	PrefillReplicas int32
	Ready           metav1.ConditionStatus
	Reason          string
	Message         string
}

// ObserveStatus reads the current Deployment states and derives the ModelService status.
func ObserveStatus(
	ctx context.Context,
	c client.Client,
	ms *modelv1alpha1.ModelService,
) (ObservationResult, error) {
	obs := ObservationResult{
		Phase: modelv1alpha1.ModelServicePending,
	}

	decodeStatus, err := getDeploymentStatus(ctx, c, ms.Namespace, DecodeName(ms.Name))
	if err != nil {
		return obs, err
	}

	obs.DecodeReady = decodeStatus.readyReplicas
	obs.DecodeReplicas = decodeStatus.desiredReplicas

	if ms.Spec.Prefill != nil {
		prefillStatus, err := getDeploymentStatus(ctx, c, ms.Namespace, PrefillName(ms.Name))
		if err != nil {
			return obs, err
		}
		obs.PrefillReady = prefillStatus.readyReplicas
		obs.PrefillReplicas = prefillStatus.desiredReplicas
	}

	obs.Phase, obs.Ready, obs.Reason, obs.Message = derivePhase(obs, decodeStatus)

	return obs, nil
}

type deploymentStatus struct {
	found           bool
	desiredReplicas int32
	readyReplicas   int32
	updatedReplicas int32
	available       bool
	progressing     bool
}

func getDeploymentStatus(ctx context.Context, c client.Client, namespace, name string) (deploymentStatus, error) {
	var dep appsv1.Deployment
	err := c.Get(ctx, types.NamespacedName{Name: name, Namespace: namespace}, &dep)
	if apierrors.IsNotFound(err) {
		return deploymentStatus{}, nil
	}
	if err != nil {
		return deploymentStatus{}, err
	}

	ds := deploymentStatus{
		found:           true,
		desiredReplicas: ptrInt32OrZero(dep.Spec.Replicas),
		readyReplicas:   dep.Status.ReadyReplicas,
		updatedReplicas: dep.Status.UpdatedReplicas,
	}

	for _, cond := range dep.Status.Conditions {
		switch cond.Type {
		case appsv1.DeploymentAvailable:
			ds.available = cond.Status == "True"
		case appsv1.DeploymentProgressing:
			ds.progressing = cond.Status == "True"
		}
	}

	return ds, nil
}

func derivePhase(obs ObservationResult, decode deploymentStatus) (
	modelv1alpha1.ModelServicePhase, metav1.ConditionStatus, string, string,
) {
	if !decode.found {
		return modelv1alpha1.ModelServicePending, metav1.ConditionFalse,
			"DeploymentNotFound", "Decode Deployment has not been created yet"
	}

	totalDesired := obs.DecodeReplicas + obs.PrefillReplicas
	totalReady := obs.DecodeReady + obs.PrefillReady

	if totalReady == 0 && totalDesired > 0 {
		return modelv1alpha1.ModelServiceRunning, metav1.ConditionFalse,
			"PodsStarting", "Waiting for serving pods to become ready"
	}

	if totalReady < totalDesired {
		return modelv1alpha1.ModelServiceRunning, metav1.ConditionFalse,
			"PartiallyReady", fmt.Sprintf("%d/%d replicas ready", totalReady, totalDesired)
	}

	return modelv1alpha1.ModelServiceReady, metav1.ConditionTrue,
		"AllReplicasReady", fmt.Sprintf("All %d replicas are ready", totalReady)
}

func ptrInt32OrZero(p *int32) int32 {
	if p != nil {
		return *p
	}
	return 0
}
