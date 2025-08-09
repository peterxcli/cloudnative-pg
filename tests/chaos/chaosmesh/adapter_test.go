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
	"testing"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNewAdapter(t *testing.T) {
	client := fake.NewClientBuilder().Build()
	adapter := NewAdapter(client, "test-namespace")

	assert.NotNil(t, adapter)
	assert.Equal(t, "test-namespace", adapter.namespace)
	assert.Equal(t, client, adapter.client)
}

func TestInjectPodChaos(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name           string
		config         core.ExperimentConfig
		expectedAction PodChaosAction
		expectedMode   SelectorMode
		expectedValue  string
	}{
		{
			name: "pod kill with fixed count",
			config: core.ExperimentConfig{
				Name:     "test-pod-kill",
				Action:   core.ChaosActionPodKill,
				Duration: 30 * time.Second,
				Target: core.TargetSelector{
					Namespace: "test-ns",
					Count:     2,
					LabelSelector: labels.SelectorFromSet(labels.Set{
						"app": "test",
					}),
				},
			},
			expectedAction: PodKillAction,
			expectedMode:   FixedMode,
			expectedValue:  "2",
		},
		{
			name: "pod failure with percentage",
			config: core.ExperimentConfig{
				Name:     "test-pod-failure",
				Action:   core.ChaosActionPodFailure,
				Duration: 60 * time.Second,
				Target: core.TargetSelector{
					Namespace:  "test-ns",
					Percentage: 50,
				},
			},
			expectedAction: PodFailureAction,
			expectedMode:   FixedPercentMode,
			expectedValue:  "50",
		},
		{
			name: "specific pod targeting",
			config: core.ExperimentConfig{
				Name:     "test-specific-pod",
				Action:   core.ChaosActionPodKill,
				Duration: 10 * time.Second,
				Target: core.TargetSelector{
					Namespace: "test-ns",
					PodName:   "test-pod-1",
				},
			},
			expectedAction: PodKillAction,
			expectedMode:   OneMode,
			expectedValue:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create fake client with scheme
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			
			client := fake.NewClientBuilder().
				WithScheme(scheme).
				Build()

			adapter := NewAdapter(client, "test-namespace")
			
			// Mock the create operation
			podChaos, err := adapter.InjectPodChaos(ctx, tt.config)
			
			require.NoError(t, err)
			assert.NotNil(t, podChaos)
			assert.Equal(t, tt.config.Name, podChaos.Name)
			assert.Equal(t, "test-namespace", podChaos.Namespace)
			assert.Equal(t, tt.expectedAction, podChaos.Spec.Action)
			assert.Equal(t, tt.expectedMode, podChaos.Spec.Mode)
			
			if tt.expectedValue != "" {
				assert.Equal(t, tt.expectedValue, podChaos.Spec.Value)
			}
		})
	}
}

func TestInjectNetworkChaos(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	adapter := NewAdapter(client, "test-namespace")

	config := NetworkChaosConfig{
		Name:      "test-network-delay",
		Action:    NetworkDelayAction,
		Mode:      AllMode,
		Duration:  30 * time.Second,
		Direction: Both,
		Selector: PodSelectorSpec{
			Namespaces: []string{"test-ns"},
			LabelSelectors: map[string]string{
				"app": "test",
			},
		},
		Delay: &DelaySpec{
			Latency:     "100ms",
			Jitter:      "10ms",
			Correlation: "25",
		},
	}

	networkChaos, err := adapter.InjectNetworkChaos(ctx, config)
	
	require.NoError(t, err)
	assert.NotNil(t, networkChaos)
	assert.Equal(t, config.Name, networkChaos.Name)
	assert.Equal(t, NetworkDelayAction, networkChaos.Spec.Action)
	assert.Equal(t, AllMode, networkChaos.Spec.Mode)
	assert.NotNil(t, networkChaos.Spec.TcParameter)
	assert.NotNil(t, networkChaos.Spec.TcParameter.Delay)
	assert.Equal(t, "100ms", networkChaos.Spec.TcParameter.Delay.Latency)
}

func TestInjectIOChaos(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	
	client := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	adapter := NewAdapter(client, "test-namespace")

	config := IOChaosConfig{
		Name:     "test-io-delay",
		Action:   IODelayAction,
		Mode:     OneMode,
		Duration: 60 * time.Second,
		Selector: PodSelectorSpec{
			Namespaces: []string{"test-ns"},
		},
		Delay:   "50ms",
		Path:    "/var/lib/postgresql/data",
		Percent: 50,
		Methods: []string{"read", "write"},
	}

	ioChaos, err := adapter.InjectIOChaos(ctx, config)
	
	require.NoError(t, err)
	assert.NotNil(t, ioChaos)
	assert.Equal(t, config.Name, ioChaos.Name)
	assert.Equal(t, IODelayAction, ioChaos.Spec.Action)
	assert.Equal(t, OneMode, ioChaos.Spec.Mode)
	assert.Equal(t, "50ms", ioChaos.Spec.Delay)
	assert.Equal(t, "/var/lib/postgresql/data", ioChaos.Spec.Path)
	assert.Equal(t, 50, ioChaos.Spec.Percent)
	assert.Equal(t, []string{"read", "write"}, ioChaos.Spec.Methods)
}

func TestDeleteChaos(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name          string
		kind          string
		resourceName  string
		existingChaos bool
		expectedError bool
	}{
		{
			name:          "delete existing chaos",
			kind:          "PodChaos",
			resourceName:  "test-chaos",
			existingChaos: true,
			expectedError: false,
		},
		{
			name:          "delete non-existing chaos returns no error",
			kind:          "PodChaos",
			resourceName:  "non-existing",
			existingChaos: false,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme)
			
			if tt.existingChaos {
				// Create an unstructured object to simulate existing chaos
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "chaos-mesh.org",
					Version: "v1alpha1",
					Kind:    tt.kind,
				})
				u.SetName(tt.resourceName)
				u.SetNamespace("test-namespace")
				builder = builder.WithObjects(u)
			}
			
			client := builder.Build()
			adapter := NewAdapter(client, "test-namespace")
			
			err := adapter.DeleteChaos(ctx, tt.kind, tt.resourceName)
			
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			
			// Verify the resource is deleted
			if tt.existingChaos {
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "chaos-mesh.org",
					Version: "v1alpha1",
					Kind:    tt.kind,
				})
				key := types.NamespacedName{
					Name:      tt.resourceName,
					Namespace: "test-namespace",
				}
				err = client.Get(ctx, key, u)
				assert.True(t, errors.IsNotFound(err))
			}
		})
	}
}

func TestGetChaosStatus(t *testing.T) {
	ctx := context.Background()
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	tests := []struct {
		name           string
		chaosName      string
		chaosExists    bool
		chaosPhase     string
		expectedStatus string
		expectedError  bool
	}{
		{
			name:           "get status of running chaos",
			chaosName:      "test-chaos",
			chaosExists:    true,
			chaosPhase:     "Running",
			expectedStatus: "Running",
			expectedError:  false,
		},
		{
			name:           "get status of completed chaos",
			chaosName:      "test-chaos",
			chaosExists:    true,
			chaosPhase:     "Completed",
			expectedStatus: "Completed",
			expectedError:  false,
		},
		{
			name:           "get status without phase field",
			chaosName:      "test-chaos",
			chaosExists:    true,
			chaosPhase:     "",
			expectedStatus: "Unknown",
			expectedError:  false,
		},
		{
			name:           "get status of non-existing chaos",
			chaosName:      "non-existing",
			chaosExists:    false,
			expectedStatus: "",
			expectedError:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := fake.NewClientBuilder().WithScheme(scheme)
			
			if tt.chaosExists {
				// Create an unstructured object with status
				u := &unstructured.Unstructured{}
				u.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "chaos-mesh.org",
					Version: "v1alpha1",
					Kind:    "PodChaos",
				})
				u.SetName(tt.chaosName)
				u.SetNamespace("test-namespace")
				
				if tt.chaosPhase != "" {
					_ = unstructured.SetNestedField(u.Object, tt.chaosPhase, "status", "phase")
				}
				
				builder = builder.WithObjects(u)
			}
			
			client := builder.Build()
			adapter := NewAdapter(client, "test-namespace")
			
			status, err := adapter.GetChaosStatus(ctx, "PodChaos", tt.chaosName)
			
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedStatus, status)
			}
		})
	}
}

func TestMapChaosAction(t *testing.T) {
	adapter := &Adapter{}

	tests := []struct {
		input    core.ChaosAction
		expected PodChaosAction
	}{
		{core.ChaosActionPodKill, PodKillAction},
		{core.ChaosActionPodFailure, PodFailureAction},
		{core.ChaosAction("unknown"), PodKillAction}, // Default
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := adapter.mapChaosAction(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapSelectorMode(t *testing.T) {
	adapter := &Adapter{}

	tests := []struct {
		name     string
		config   core.ExperimentConfig
		expected SelectorMode
	}{
		{
			name: "fixed count mode",
			config: core.ExperimentConfig{
				Target: core.TargetSelector{Count: 3},
			},
			expected: FixedMode,
		},
		{
			name: "percentage mode",
			config: core.ExperimentConfig{
				Target: core.TargetSelector{Percentage: 50},
			},
			expected: FixedPercentMode,
		},
		{
			name: "one mode with pod name",
			config: core.ExperimentConfig{
				Target: core.TargetSelector{PodName: "test-pod"},
			},
			expected: OneMode,
		},
		{
			name:     "all mode by default",
			config:   core.ExperimentConfig{},
			expected: AllMode,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := adapter.mapSelectorMode(tt.config)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildPodSelector(t *testing.T) {
	adapter := &Adapter{}

	tests := []struct {
		name     string
		target   core.TargetSelector
		validate func(t *testing.T, selector PodSelectorSpec)
	}{
		{
			name: "namespace selector",
			target: core.TargetSelector{
				Namespace: "test-ns",
			},
			validate: func(t *testing.T, selector PodSelectorSpec) {
				assert.Equal(t, []string{"test-ns"}, selector.Namespaces)
				assert.Contains(t, selector.PodPhaseSelectors, string(corev1.PodRunning))
			},
		},
		{
			name: "label selector",
			target: core.TargetSelector{
				Namespace: "test-ns",
				LabelSelector: labels.SelectorFromSet(labels.Set{
					"app":  "test",
					"tier": "backend",
				}),
			},
			validate: func(t *testing.T, selector PodSelectorSpec) {
				assert.Equal(t, []string{"test-ns"}, selector.Namespaces)
				// Since we simplified label selector parsing, check that map was created
				assert.NotNil(t, selector.LabelSelectors)
			},
		},
		{
			name: "node selector",
			target: core.TargetSelector{
				Namespace: "test-ns",
				NodeName:  "node-1",
			},
			validate: func(t *testing.T, selector PodSelectorSpec) {
				assert.Equal(t, []string{"test-ns"}, selector.Namespaces)
				assert.Equal(t, "node-1", selector.NodeSelectors["kubernetes.io/hostname"])
			},
		},
		{
			name: "specific pod",
			target: core.TargetSelector{
				Namespace: "test-ns",
				PodName:   "test-pod-1",
			},
			validate: func(t *testing.T, selector PodSelectorSpec) {
				assert.Equal(t, []string{"test-pod-1"}, selector.Pods["test-ns"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selector := adapter.buildPodSelector(tt.target)
			tt.validate(t, selector)
		})
	}
}

func TestSetDuration(t *testing.T) {
	tests := []struct {
		duration time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m0s"},
		{time.Hour, "1h0m0s"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := SetDuration(tt.duration)
			assert.NotNil(t, result)
			assert.Equal(t, tt.expected, *result)
		})
	}
}

func TestGetDuration(t *testing.T) {
	tests := []struct {
		name          string
		input         *string
		expected      time.Duration
		expectedError bool
	}{
		{
			name:          "valid duration",
			input:         stringPtr("30s"),
			expected:      30 * time.Second,
			expectedError: false,
		},
		{
			name:          "nil duration",
			input:         nil,
			expected:      0,
			expectedError: false,
		},
		{
			name:          "empty duration",
			input:         stringPtr(""),
			expected:      0,
			expectedError: false,
		},
		{
			name:          "invalid duration",
			input:         stringPtr("invalid"),
			expected:      0,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			duration, err := GetDuration(tt.input)
			
			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, duration)
			}
		})
	}
}

// Helper function for string pointers
func stringPtr(s string) *string {
	return &s
}