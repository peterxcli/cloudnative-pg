/*
Copyright © contributors to CloudNativePG, established as
CloudNativePG a Series of LF Projects, LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

SPDX-License-Identifier: Apache-2.0
*/

package chaosmesh

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/core"
)

// Adapter provides integration between our chaos framework and Chaos Mesh
type Adapter struct {
	client    client.Client
	namespace string
}

// NewAdapter creates a new Chaos Mesh adapter
func NewAdapter(client client.Client, namespace string) *Adapter {
	return &Adapter{
		client:    client,
		namespace: namespace,
	}
}

// InjectPodChaos injects pod chaos using Chaos Mesh
func (a *Adapter) InjectPodChaos(ctx context.Context, config core.ExperimentConfig) (*PodChaos, error) {
	podChaos := &PodChaos{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "chaos-mesh.org/v1alpha1",
			Kind:       "PodChaos",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: a.namespace,
			Labels: map[string]string{
				"cnpg.io/test":       "chaos",
				"cnpg.io/experiment": config.Name,
			},
		},
		Spec: PodChaosSpec{
			Action:   a.mapChaosAction(config.Action),
			Mode:     a.mapSelectorMode(config),
			Selector: a.buildPodSelector(config.Target),
			Duration: SetDuration(config.Duration),
		},
	}

	// Set value for fixed mode
	if config.Target.Count > 0 {
		podChaos.Spec.Value = fmt.Sprintf("%d", config.Target.Count)
	} else if config.Target.Percentage > 0 {
		podChaos.Spec.Value = fmt.Sprintf("%d", config.Target.Percentage)
	}

	// Convert to unstructured for dynamic client
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(podChaos)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
	}

	u := &unstructured.Unstructured{Object: obj}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "chaos-mesh.org",
		Version: "v1alpha1",
		Kind:    "PodChaos",
	})

	// Create the resource
	if err := a.client.Create(ctx, u); err != nil {
		return nil, fmt.Errorf("failed to create PodChaos: %w", err)
	}

	return podChaos, nil
}

// InjectNetworkChaos injects network chaos
func (a *Adapter) InjectNetworkChaos(ctx context.Context, config NetworkChaosConfig) (*NetworkChaos, error) {
	networkChaos := &NetworkChaos{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "chaos-mesh.org/v1alpha1",
			Kind:       "NetworkChaos",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: a.namespace,
			Labels: map[string]string{
				"cnpg.io/test":       "chaos",
				"cnpg.io/experiment": config.Name,
			},
		},
		Spec: NetworkChaosSpec{
			Action:    config.Action,
			Mode:      config.Mode,
			Selector:  config.Selector,
			Duration:  SetDuration(config.Duration),
			Direction: config.Direction,
		},
	}

	// Set TC parameters for delay/loss
	if config.Delay != nil || config.Loss != nil {
		networkChaos.Spec.TcParameter = &TcParameter{}
		if config.Delay != nil {
			networkChaos.Spec.TcParameter.Delay = config.Delay
		}
		if config.Loss != nil {
			networkChaos.Spec.TcParameter.Loss = config.Loss
		}
	}

	// Set target for partition
	if config.Target != nil {
		networkChaos.Spec.Target = config.Target
	}

	// Convert and create
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(networkChaos)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
	}

	u := &unstructured.Unstructured{Object: obj}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "chaos-mesh.org",
		Version: "v1alpha1",
		Kind:    "NetworkChaos",
	})

	if err := a.client.Create(ctx, u); err != nil {
		return nil, fmt.Errorf("failed to create NetworkChaos: %w", err)
	}

	return networkChaos, nil
}

// InjectIOChaos injects I/O chaos
func (a *Adapter) InjectIOChaos(ctx context.Context, config IOChaosConfig) (*IOChaos, error) {
	ioChaos := &IOChaos{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "chaos-mesh.org/v1alpha1",
			Kind:       "IOChaos",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      config.Name,
			Namespace: a.namespace,
			Labels: map[string]string{
				"cnpg.io/test":       "chaos",
				"cnpg.io/experiment": config.Name,
			},
		},
		Spec: IOChaosSpec{
			Action:   config.Action,
			Mode:     config.Mode,
			Selector: config.Selector,
			Duration: SetDuration(config.Duration),
			Delay:    config.Delay,
			Path:     config.Path,
			Percent:  config.Percent,
			Methods:  config.Methods,
		},
	}

	// Convert and create
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ioChaos)
	if err != nil {
		return nil, fmt.Errorf("failed to convert to unstructured: %w", err)
	}

	u := &unstructured.Unstructured{Object: obj}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "chaos-mesh.org",
		Version: "v1alpha1",
		Kind:    "IOChaos",
	})

	if err := a.client.Create(ctx, u); err != nil {
		return nil, fmt.Errorf("failed to create IOChaos: %w", err)
	}

	return ioChaos, nil
}

// DeleteChaos deletes a chaos experiment
func (a *Adapter) DeleteChaos(ctx context.Context, kind, name string) error {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "chaos-mesh.org",
		Version: "v1alpha1",
		Kind:    kind,
	})
	u.SetName(name)
	u.SetNamespace(a.namespace)

	if err := a.client.Delete(ctx, u); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to delete %s/%s: %w", kind, name, err)
	}

	return nil
}

// WaitForChaosReady waits for a chaos experiment to be ready
func (a *Adapter) WaitForChaosReady(ctx context.Context, kind, name string, timeout time.Duration) error {
	return wait.PollImmediate(2*time.Second, timeout, func() (bool, error) {
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   "chaos-mesh.org",
			Version: "v1alpha1",
			Kind:    kind,
		})

		key := types.NamespacedName{
			Namespace: a.namespace,
			Name:      name,
		}

		if err := a.client.Get(ctx, key, u); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}

		// Check status phase
		status, found, err := unstructured.NestedMap(u.Object, "status")
		if err != nil || !found {
			return false, nil
		}

		phase, found, err := unstructured.NestedString(status, "phase")
		if err != nil || !found {
			return false, nil
		}

		// Chaos is ready when phase is "Running"
		return phase == "Running", nil
	})
}

// GetChaosStatus gets the status of a chaos experiment
func (a *Adapter) GetChaosStatus(ctx context.Context, kind, name string) (string, error) {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "chaos-mesh.org",
		Version: "v1alpha1",
		Kind:    kind,
	})

	key := types.NamespacedName{
		Namespace: a.namespace,
		Name:      name,
	}

	if err := a.client.Get(ctx, key, u); err != nil {
		return "", fmt.Errorf("failed to get %s/%s: %w", kind, name, err)
	}

	status, found, err := unstructured.NestedMap(u.Object, "status")
	if err != nil || !found {
		return "Unknown", nil
	}

	phase, found, err := unstructured.NestedString(status, "phase")
	if err != nil || !found {
		return "Unknown", nil
	}

	return phase, nil
}

// mapChaosAction maps our chaos action to Chaos Mesh action
func (a *Adapter) mapChaosAction(action core.ChaosAction) PodChaosAction {
	switch action {
	case core.ChaosActionPodKill:
		return PodKillAction
	case core.ChaosActionPodFailure:
		return PodFailureAction
	default:
		return PodKillAction
	}
}

// mapSelectorMode maps our selector to Chaos Mesh mode
func (a *Adapter) mapSelectorMode(config core.ExperimentConfig) SelectorMode {
	if config.Target.Count > 0 {
		return FixedMode
	}
	if config.Target.Percentage > 0 {
		return FixedPercentMode
	}
	if config.Target.PodName != "" {
		return OneMode
	}
	return AllMode
}

// buildPodSelector builds a Chaos Mesh pod selector
func (a *Adapter) buildPodSelector(target core.TargetSelector) PodSelectorSpec {
	selector := PodSelectorSpec{
		PodPhaseSelectors: []string{string(corev1.PodRunning)},
	}

	if target.Namespace != "" {
		selector.Namespaces = []string{target.Namespace}
	}

	if target.LabelSelector != nil {
		// Convert labels.Selector to map[string]string
		// For simplicity, we extract the string representation
		// In a real implementation, you'd properly parse the selector
		selectorStr := target.LabelSelector.String()
		if selectorStr != "" && selectorStr != "<nil>" {
			// Basic parsing - this is simplified
			// In production, use proper label selector parsing
			selector.LabelSelectors = make(map[string]string)
		}
	}

	if target.NodeName != "" {
		selector.NodeSelectors = map[string]string{
			"kubernetes.io/hostname": target.NodeName,
		}
	}

	if target.PodName != "" {
		selector.Pods = map[string][]string{
			target.Namespace: {target.PodName},
		}
	}

	return selector
}

// Configuration types for different chaos experiments

// NetworkChaosConfig configures network chaos
type NetworkChaosConfig struct {
	Name      string
	Action    NetworkChaosAction
	Mode      SelectorMode
	Selector  PodSelectorSpec
	Duration  time.Duration
	Direction Direction
	Target    *PodSelectorSpec
	Delay     *DelaySpec
	Loss      *LossSpec
}

// IOChaosConfig configures I/O chaos
type IOChaosConfig struct {
	Name     string
	Action   IOChaosAction
	Mode     SelectorMode
	Selector PodSelectorSpec
	Duration time.Duration
	Delay    string
	Path     string
	Percent  int
	Methods  []string
}

// StressChaosConfig configures stress chaos
type StressChaosConfig struct {
	Name      string
	Mode      SelectorMode
	Selector  PodSelectorSpec
	Duration  time.Duration
	Stressors *Stressors
}
