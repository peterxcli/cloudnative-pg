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

package safety

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

func createFakeClient(objects ...runtime.Object) client.Client {
	scheme := runtime.NewScheme()
	_ = apiv1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)
	return fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objects...).Build()
}

func TestController_RegisterCheck(t *testing.T) {
	config := SafetyConfig{
		MaxFailurePercent:  50,
		MinHealthyReplicas: 2,
		ClusterNamespace:   "test-ns",
		ClusterName:        "test-cluster",
	}
	
	client := createFakeClient()
	controller := NewController(client, config)
	
	// Initially no custom checks
	assert.Len(t, controller.checks, 0)
	
	// Register a check
	mockCheck := &ClusterHealthCheck{
		Namespace:          "test-ns",
		ClusterName:        "test-cluster",
		MinHealthyReplicas: 2,
	}
	
	controller.RegisterCheck(mockCheck)
	assert.Len(t, controller.checks, 1)
	
	// Register another check
	controller.RegisterCheck(mockCheck)
	assert.Len(t, controller.checks, 2)
}

func TestController_EmergencyStop(t *testing.T) {
	config := SafetyConfig{
		EnableEmergencyStop: true,
		ClusterNamespace:    "test-ns",
		ClusterName:         "test-cluster",
	}
	
	client := createFakeClient()
	controller := NewController(client, config)
	
	// Clean up any existing file
	defer os.Remove(controller.emergencyStopFile)
	
	t.Run("trigger emergency stop", func(t *testing.T) {
		err := controller.TriggerEmergencyStop("test reason")
		require.NoError(t, err)
		
		// Verify file exists
		_, err = os.Stat(controller.emergencyStopFile)
		assert.NoError(t, err)
		
		// Verify content
		content, err := os.ReadFile(controller.emergencyStopFile)
		require.NoError(t, err)
		assert.Contains(t, string(content), "test reason")
		assert.Contains(t, string(content), "Emergency stop triggered")
	})
	
	t.Run("clear emergency stop", func(t *testing.T) {
		err := controller.ClearEmergencyStop()
		require.NoError(t, err)
		
		// Verify file is removed
		_, err = os.Stat(controller.emergencyStopFile)
		assert.True(t, os.IsNotExist(err))
	})
	
	t.Run("emergency stop disabled", func(t *testing.T) {
		config.EnableEmergencyStop = false
		controller = NewController(client, config)
		
		err := controller.TriggerEmergencyStop("test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not enabled")
	})
}

func TestController_ShouldAbort(t *testing.T) {
	ctx := context.Background()
	
	t.Run("emergency stop file triggers abort", func(t *testing.T) {
		config := SafetyConfig{
			EnableEmergencyStop: true,
			ClusterNamespace:    "test-ns",
			ClusterName:         "test-cluster",
		}
		
		client := createFakeClient()
		controller := NewController(client, config)
		
		// Create emergency stop file
		err := controller.TriggerEmergencyStop("test")
		require.NoError(t, err)
		defer controller.ClearEmergencyStop()
		
		shouldAbort, reason := controller.ShouldAbort(ctx)
		assert.True(t, shouldAbort)
		assert.Equal(t, "emergency stop file detected", reason)
	})
	
	t.Run("abort signal triggers abort", func(t *testing.T) {
		config := SafetyConfig{
			ClusterNamespace: "test-ns",
			ClusterName:      "test-cluster",
		}
		
		client := createFakeClient()
		controller := NewController(client, config)
		
		// Close abort signal
		close(controller.abortSignal)
		
		shouldAbort, reason := controller.ShouldAbort(ctx)
		assert.True(t, shouldAbort)
		assert.Equal(t, "abort signal received", reason)
	})
	
	t.Run("critical check failure triggers abort", func(t *testing.T) {
		config := SafetyConfig{
			ClusterNamespace: "test-ns",
			ClusterName:      "test-cluster",
		}
		
		// Create unhealthy cluster (no primary)
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
			},
			Status: apiv1.ClusterStatus{
				ReadyInstances: 0,
				CurrentPrimary: "", // No primary
			},
		}
		
		client := createFakeClient(cluster)
		controller := NewController(client, config)
		
		// Register critical check
		check := &ClusterHealthCheck{
			Namespace:          "test-ns",
			ClusterName:        "test-cluster",
			MinHealthyReplicas: 2,
		}
		controller.RegisterCheck(check)
		
		shouldAbort, reason := controller.ShouldAbort(ctx)
		assert.True(t, shouldAbort)
		assert.Contains(t, reason, "critical check")
		assert.Contains(t, reason, "ClusterHealth")
	})
	
	t.Run("non-critical check failure does not trigger abort", func(t *testing.T) {
		config := SafetyConfig{
			ClusterNamespace: "test-ns",
			ClusterName:      "test-cluster",
		}
		
		client := createFakeClient()
		controller := NewController(client, config)
		
		// Register non-critical check that fails
		check := &RecoveryTimeCheck{
			maxRecoveryTime: 1 * time.Nanosecond, // Will exceed immediately
		}
		check.StartRecovery()
		time.Sleep(10 * time.Millisecond)
		
		controller.RegisterCheck(check)
		
		shouldAbort, reason := controller.ShouldAbort(ctx)
		assert.False(t, shouldAbort)
		assert.Empty(t, reason)
	})
}

func TestClusterHealthCheck(t *testing.T) {
	ctx := context.Background()
	
	t.Run("healthy cluster passes check", func(t *testing.T) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
			},
			Status: apiv1.ClusterStatus{
				ReadyInstances: 3,
				Instances:      3,
				CurrentPrimary: "test-cluster-1",
				TargetPrimary:  "test-cluster-1",
			},
		}
		
		client := createFakeClient(cluster)
		check := &ClusterHealthCheck{
			Namespace:          "test-ns",
			ClusterName:        "test-cluster",
			MinHealthyReplicas: 2,
		}
		
		passed, reason, err := check.Check(ctx, client)
		assert.NoError(t, err)
		assert.True(t, passed)
		assert.Empty(t, reason)
	})
	
	t.Run("no primary fails check", func(t *testing.T) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
			},
			Status: apiv1.ClusterStatus{
				ReadyInstances: 3,
				CurrentPrimary: "", // No primary
			},
		}
		
		client := createFakeClient(cluster)
		check := &ClusterHealthCheck{
			Namespace:          "test-ns",
			ClusterName:        "test-cluster",
			MinHealthyReplicas: 2,
		}
		
		passed, reason, err := check.Check(ctx, client)
		assert.NoError(t, err)
		assert.False(t, passed)
		assert.Contains(t, reason, "no primary instance available")
	})
	
	t.Run("insufficient replicas fails check", func(t *testing.T) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
			},
			Status: apiv1.ClusterStatus{
				ReadyInstances: 1,
				CurrentPrimary: "test-cluster-1",
				TargetPrimary:  "test-cluster-1",
			},
		}
		
		client := createFakeClient(cluster)
		check := &ClusterHealthCheck{
			Namespace:          "test-ns",
			ClusterName:        "test-cluster",
			MinHealthyReplicas: 3,
		}
		
		passed, reason, err := check.Check(ctx, client)
		assert.NoError(t, err)
		assert.False(t, passed)
		assert.Contains(t, reason, "only 1 ready instances, need 3")
	})
	
	t.Run("switchover in progress fails check", func(t *testing.T) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
			},
			Status: apiv1.ClusterStatus{
				ReadyInstances: 3,
				CurrentPrimary: "test-cluster-1",
				TargetPrimary:  "test-cluster-2", // Different from current
			},
		}
		
		client := createFakeClient(cluster)
		check := &ClusterHealthCheck{
			Namespace:          "test-ns",
			ClusterName:        "test-cluster",
			MinHealthyReplicas: 2,
		}
		
		passed, reason, err := check.Check(ctx, client)
		assert.NoError(t, err)
		assert.False(t, passed)
		assert.Contains(t, reason, "primary switchover in progress")
	})
	
	t.Run("cluster not found returns error", func(t *testing.T) {
		client := createFakeClient() // No cluster
		check := &ClusterHealthCheck{
			Namespace:          "test-ns",
			ClusterName:        "missing-cluster",
			MinHealthyReplicas: 2,
		}
		
		passed, _, err := check.Check(ctx, client)
		assert.Error(t, err)
		assert.False(t, passed)
		assert.Contains(t, err.Error(), "failed to get cluster")
	})
	
	t.Run("check is critical", func(t *testing.T) {
		check := &ClusterHealthCheck{}
		assert.True(t, check.IsCritical())
	})
}

func TestDataConsistencyCheck(t *testing.T) {
	ctx := context.Background()
	
	t.Run("sufficient replicas pass check", func(t *testing.T) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
			},
			Status: apiv1.ClusterStatus{
				ReadyInstances: 3,
				CurrentPrimary: "test-cluster-1",
				TargetPrimary:  "test-cluster-1",
			},
		}
		
		client := createFakeClient(cluster)
		check := &DataConsistencyCheck{
			Namespace:       "test-ns",
			ClusterName:     "test-cluster",
			MaxDataLagBytes: 1000000,
		}
		
		passed, reason, err := check.Check(ctx, client)
		assert.NoError(t, err)
		assert.True(t, passed)
		assert.Empty(t, reason)
	})
	
	t.Run("insufficient replicas fails check", func(t *testing.T) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
			},
			Status: apiv1.ClusterStatus{
				ReadyInstances: 1,
				CurrentPrimary: "test-cluster-1",
				TargetPrimary:  "test-cluster-1",
			},
		}
		
		client := createFakeClient(cluster)
		check := &DataConsistencyCheck{
			Namespace:       "test-ns",
			ClusterName:     "test-cluster",
			MaxDataLagBytes: 1000000,
		}
		
		passed, reason, err := check.Check(ctx, client)
		assert.NoError(t, err)
		assert.False(t, passed)
		assert.Contains(t, reason, "insufficient replicas for data consistency")
	})
	
	t.Run("primary transition fails check", func(t *testing.T) {
		cluster := &apiv1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "test-ns",
			},
			Status: apiv1.ClusterStatus{
				ReadyInstances: 3,
				CurrentPrimary: "test-cluster-1",
				TargetPrimary:  "test-cluster-2", // Different from current
			},
		}
		
		client := createFakeClient(cluster)
		check := &DataConsistencyCheck{
			Namespace:       "test-ns",
			ClusterName:     "test-cluster",
			MaxDataLagBytes: 1000000,
		}
		
		passed, reason, err := check.Check(ctx, client)
		assert.NoError(t, err)
		assert.False(t, passed)
		assert.Contains(t, reason, "primary transition in progress")
	})
	
	t.Run("check is critical", func(t *testing.T) {
		check := &DataConsistencyCheck{}
		assert.True(t, check.IsCritical())
	})
}

func TestRecoveryTimeCheck(t *testing.T) {
	ctx := context.Background()
	client := createFakeClient()
	
	t.Run("within recovery time passes", func(t *testing.T) {
		check := &RecoveryTimeCheck{
			maxRecoveryTime: 1 * time.Hour,
		}
		check.StartRecovery()
		
		passed, reason, err := check.Check(ctx, client)
		assert.NoError(t, err)
		assert.True(t, passed)
		assert.Empty(t, reason)
	})
	
	t.Run("exceeds recovery time fails", func(t *testing.T) {
		check := &RecoveryTimeCheck{
			maxRecoveryTime: 1 * time.Nanosecond,
		}
		check.StartRecovery()
		time.Sleep(10 * time.Millisecond)
		
		passed, reason, err := check.Check(ctx, client)
		assert.NoError(t, err)
		assert.False(t, passed)
		assert.Contains(t, reason, "recovery time exceeded")
	})
	
	t.Run("no recovery started passes", func(t *testing.T) {
		check := &RecoveryTimeCheck{
			maxRecoveryTime: 1 * time.Hour,
		}
		// Don't call StartRecovery()
		
		passed, reason, err := check.Check(ctx, client)
		assert.NoError(t, err)
		assert.True(t, passed)
		assert.Empty(t, reason)
	})
	
	t.Run("check is not critical", func(t *testing.T) {
		check := &RecoveryTimeCheck{}
		assert.False(t, check.IsCritical())
	})
}

func TestController_Start(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	config := SafetyConfig{
		MaxFailurePercent:  50,
		MinHealthyReplicas: 2,
		ClusterNamespace:   "test-ns",
		ClusterName:        "test-cluster",
	}
	
	// Create healthy cluster
	cluster := &apiv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "test-ns",
		},
		Status: apiv1.ClusterStatus{
			ReadyInstances: 3,
			CurrentPrimary: "test-cluster-1",
			TargetPrimary:  "test-cluster-1",
		},
	}
	
	client := createFakeClient(cluster)
	controller := NewController(client, config)
	
	err := controller.Start(ctx)
	require.NoError(t, err)
	
	// Verify default checks are registered
	assert.True(t, len(controller.checks) >= 3, "Expected at least 3 default checks")
	
	// Let it run briefly
	time.Sleep(50 * time.Millisecond)
	
	// Stop controller
	controller.Stop()
	
	// Verify abort signal is closed
	select {
	case <-controller.abortSignal:
		// Good, channel is closed
	default:
		t.Error("Expected abort signal to be closed")
	}
}

// MockSafetyCheck for testing
type MockSafetyCheck struct {
	name     string
	critical bool
	result   bool
	reason   string
	err      error
}

func (m *MockSafetyCheck) Name() string {
	return m.name
}

func (m *MockSafetyCheck) Check(ctx context.Context, client client.Client) (bool, string, error) {
	return m.result, m.reason, m.err
}

func (m *MockSafetyCheck) IsCritical() bool {
	return m.critical
}

func TestController_RegisterDefaultChecks(t *testing.T) {
	config := SafetyConfig{
		MaxFailurePercent:  50,
		MinHealthyReplicas: 2,
		MaxDataLagBytes:    1000000,
		MaxRecoveryTime:    5 * time.Minute,
		ClusterNamespace:   "test-ns",
		ClusterName:        "test-cluster",
	}
	
	client := createFakeClient()
	controller := NewController(client, config)
	
	// Register default checks
	controller.registerDefaultChecks()
	
	// Verify correct number of default checks
	assert.Len(t, controller.checks, 3)
	
	// Verify check types
	checkTypes := make(map[string]bool)
	for _, check := range controller.checks {
		checkTypes[check.Name()] = true
	}
	
	assert.True(t, checkTypes["ClusterHealth"])
	assert.True(t, checkTypes["DataConsistency"])
	assert.True(t, checkTypes["RecoveryTime"])
}