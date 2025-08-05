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

package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// MockSafetyCheck is a mock implementation of SafetyCheck
type MockSafetyCheck struct {
	mock.Mock
}

func (m *MockSafetyCheck) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockSafetyCheck) Check(ctx context.Context, k8sClient client.Client) (bool, string, error) {
	args := m.Called(ctx, k8sClient)
	return args.Bool(0), args.String(1), args.Error(2)
}

func (m *MockSafetyCheck) IsCritical() bool {
	args := m.Called()
	return args.Bool(0)
}

// MockMetricsCollector is a mock implementation of MetricsCollector
type MockMetricsCollector struct {
	mock.Mock
}

func (m *MockMetricsCollector) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockMetricsCollector) Start(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockMetricsCollector) Stop() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockMetricsCollector) Collect() (map[string]interface{}, error) {
	args := m.Called()
	return args.Get(0).(map[string]interface{}), args.Error(1)
}

func (m *MockMetricsCollector) Reset() {
	m.Called()
}

func TestBaseExperiment_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  ExperimentConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid configuration",
			config: ExperimentConfig{
				Name: "test-experiment",
				Target: TargetSelector{
					Namespace: "default",
				},
				Duration: 30 * time.Second,
				Action:   ChaosActionPodKill,
			},
			wantErr: false,
		},
		{
			name: "missing name",
			config: ExperimentConfig{
				Target: TargetSelector{
					Namespace: "default",
				},
				Duration: 30 * time.Second,
				Action:   ChaosActionPodKill,
			},
			wantErr: true,
			errMsg:  "experiment name is required",
		},
		{
			name: "missing namespace",
			config: ExperimentConfig{
				Name:     "test-experiment",
				Duration: 30 * time.Second,
				Action:   ChaosActionPodKill,
			},
			wantErr: true,
			errMsg:  "target namespace is required",
		},
		{
			name: "invalid duration",
			config: ExperimentConfig{
				Name: "test-experiment",
				Target: TargetSelector{
					Namespace: "default",
				},
				Duration: 0,
				Action:   ChaosActionPodKill,
			},
			wantErr: true,
			errMsg:  "duration must be positive",
		},
		{
			name: "missing action",
			config: ExperimentConfig{
				Name: "test-experiment",
				Target: TargetSelector{
					Namespace: "default",
				},
				Duration: 30 * time.Second,
			},
			wantErr: true,
			errMsg:  "chaos action is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewClientBuilder().Build()
			exp := NewBaseExperiment(tt.config, client)
			
			err := exp.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestBaseExperiment_AddEvent(t *testing.T) {
	client := fake.NewClientBuilder().Build()
	config := ExperimentConfig{
		Name: "test-experiment",
		Target: TargetSelector{
			Namespace: "default",
		},
		Duration: 30 * time.Second,
		Action:   ChaosActionPodKill,
	}
	
	exp := NewBaseExperiment(config, client)
	
	// Add events
	exp.AddEvent("TestEvent", "Test message", EventSeverityInfo)
	exp.AddEvent("ErrorEvent", "Error occurred", EventSeverityError)
	
	// Verify events were added
	result := exp.GetResult()
	assert.Len(t, result.Events, 2)
	
	assert.Equal(t, "TestEvent", result.Events[0].Type)
	assert.Equal(t, "Test message", result.Events[0].Message)
	assert.Equal(t, EventSeverityInfo, result.Events[0].Severity)
	
	assert.Equal(t, "ErrorEvent", result.Events[1].Type)
	assert.Equal(t, "Error occurred", result.Events[1].Message)
	assert.Equal(t, EventSeverityError, result.Events[1].Severity)
}

func TestBaseExperiment_SetStatus(t *testing.T) {
	client := fake.NewClientBuilder().Build()
	config := ExperimentConfig{
		Name: "test-experiment",
		Target: TargetSelector{
			Namespace: "default",
		},
		Duration: 30 * time.Second,
		Action:   ChaosActionPodKill,
	}
	
	exp := NewBaseExperiment(config, client)
	
	// Initial status should be Pending
	assert.Equal(t, ExperimentStatusPending, exp.GetResult().Status)
	
	// Update status
	exp.SetStatus(ExperimentStatusRunning)
	assert.Equal(t, ExperimentStatusRunning, exp.GetResult().Status)
	
	exp.SetStatus(ExperimentStatusCompleted)
	assert.Equal(t, ExperimentStatusCompleted, exp.GetResult().Status)
}

func TestBaseExperiment_RunSafetyChecks(t *testing.T) {
	ctx := context.Background()
	client := fake.NewClientBuilder().Build()
	config := ExperimentConfig{
		Name: "test-experiment",
		Target: TargetSelector{
			Namespace: "default",
		},
		Duration: 30 * time.Second,
		Action:   ChaosActionPodKill,
	}
	
	t.Run("all checks pass", func(t *testing.T) {
		exp := NewBaseExperiment(config, client)
		
		mockCheck := new(MockSafetyCheck)
		mockCheck.On("Name").Return("test-check")
		mockCheck.On("Check", ctx, client).Return(true, "", nil)
		mockCheck.On("IsCritical").Return(true).Maybe() // May not be called if check passes
		
		exp.AddSafetyCheck(mockCheck)
		
		err := exp.RunSafetyChecks(ctx)
		require.NoError(t, err)
		
		// Verify event was added
		events := exp.GetResult().Events
		assert.Len(t, events, 1)
		assert.Contains(t, events[0].Message, "passed")
		
		mockCheck.AssertExpectations(t)
	})
	
	t.Run("critical check fails", func(t *testing.T) {
		exp := NewBaseExperiment(config, client)
		
		mockCheck := new(MockSafetyCheck)
		mockCheck.On("Name").Return("critical-check")
		mockCheck.On("Check", ctx, client).Return(false, "cluster unhealthy", nil)
		mockCheck.On("IsCritical").Return(true)
		
		exp.AddSafetyCheck(mockCheck)
		
		err := exp.RunSafetyChecks(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "critical safety check")
		assert.Contains(t, err.Error(), "cluster unhealthy")
		
		// Verify abort reason was set
		result := exp.GetResult()
		assert.True(t, result.SafetyAborted)
		assert.Equal(t, "cluster unhealthy", result.AbortReason)
		
		mockCheck.AssertExpectations(t)
	})
	
	t.Run("non-critical check fails", func(t *testing.T) {
		exp := NewBaseExperiment(config, client)
		
		mockCheck := new(MockSafetyCheck)
		mockCheck.On("Name").Return("warning-check")
		mockCheck.On("Check", ctx, client).Return(false, "minor issue", nil)
		mockCheck.On("IsCritical").Return(false).Maybe() // May be called once or twice
		
		exp.AddSafetyCheck(mockCheck)
		
		err := exp.RunSafetyChecks(ctx)
		require.NoError(t, err)
		
		// Verify warning event was added (may have multiple events)
		events := exp.GetResult().Events
		assert.GreaterOrEqual(t, len(events), 1)
		// Check at least one warning event exists
		hasWarning := false
		for _, event := range events {
			if event.Severity == EventSeverityWarning {
				hasWarning = true
				break
			}
		}
		assert.True(t, hasWarning, "Should have at least one warning event")
		
		mockCheck.AssertExpectations(t)
	})
	
	t.Run("check returns error", func(t *testing.T) {
		exp := NewBaseExperiment(config, client)
		
		mockCheck := new(MockSafetyCheck)
		mockCheck.On("Name").Return("error-check")
		mockCheck.On("Check", ctx, client).Return(false, "", errors.New("connection failed"))
		mockCheck.On("IsCritical").Return(true)
		
		exp.AddSafetyCheck(mockCheck)
		
		err := exp.RunSafetyChecks(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "connection failed")
		
		mockCheck.AssertExpectations(t)
	})
}

func TestBaseExperiment_MetricsCollection(t *testing.T) {
	ctx := context.Background()
	client := fake.NewClientBuilder().Build()
	config := ExperimentConfig{
		Name: "test-experiment",
		Target: TargetSelector{
			Namespace: "default",
		},
		Duration: 30 * time.Second,
		Action:   ChaosActionPodKill,
	}
	
	t.Run("successful metrics collection", func(t *testing.T) {
		exp := NewBaseExperiment(config, client)
		
		mockCollector := new(MockMetricsCollector)
		mockCollector.On("Name").Return("test-collector")
		mockCollector.On("Start", ctx).Return(nil)
		mockCollector.On("Stop").Return(nil)
		mockCollector.On("Collect").Return(map[string]interface{}{
			"metric1": 100,
			"metric2": "value",
		}, nil)
		
		exp.AddMetricsCollector(mockCollector)
		
		// Start collection
		err := exp.StartMetricsCollection(ctx)
		require.NoError(t, err)
		
		// Stop collection
		exp.StopMetricsCollection()
		
		// Verify metrics were collected
		result := exp.GetResult()
		assert.Equal(t, 100, result.Metrics["test-collector.metric1"])
		assert.Equal(t, "value", result.Metrics["test-collector.metric2"])
		
		mockCollector.AssertExpectations(t)
	})
	
	t.Run("collector start failure", func(t *testing.T) {
		exp := NewBaseExperiment(config, client)
		
		mockCollector := new(MockMetricsCollector)
		mockCollector.On("Name").Return("failing-collector")
		mockCollector.On("Start", ctx).Return(errors.New("start failed"))
		
		exp.AddMetricsCollector(mockCollector)
		
		// Start should not return error but add warning event
		err := exp.StartMetricsCollection(ctx)
		require.NoError(t, err)
		
		// Verify warning event was added
		events := exp.GetResult().Events
		found := false
		for _, event := range events {
			if event.Type == "Metrics" && event.Severity == EventSeverityWarning {
				assert.Contains(t, event.Message, "Failed to start collector")
				found = true
				break
			}
		}
		assert.True(t, found, "Expected warning event not found")
		
		mockCollector.AssertExpectations(t)
	})
}

func TestBaseExperiment_Setup(t *testing.T) {
	ctx := context.Background()
	client := fake.NewClientBuilder().Build()
	config := ExperimentConfig{
		Name: "test-experiment",
		Target: TargetSelector{
			Namespace: "default",
		},
		Duration: 30 * time.Second,
		Action:   ChaosActionPodKill,
	}
	
	t.Run("successful setup", func(t *testing.T) {
		exp := NewBaseExperiment(config, client)
		
		// Add passing safety check
		mockCheck := new(MockSafetyCheck)
		mockCheck.On("Name").Return("setup-check")
		mockCheck.On("Check", ctx, client).Return(true, "", nil)
		mockCheck.On("IsCritical").Return(true).Maybe()
		exp.AddSafetyCheck(mockCheck)
		
		// Add metrics collector
		mockCollector := new(MockMetricsCollector)
		mockCollector.On("Name").Return("setup-collector")
		mockCollector.On("Start", ctx).Return(nil)
		exp.AddMetricsCollector(mockCollector)
		
		err := exp.Setup(ctx)
		require.NoError(t, err)
		
		result := exp.GetResult()
		assert.Equal(t, ExperimentStatusPending, result.Status)
		assert.NotZero(t, result.StartTime)
		
		mockCheck.AssertExpectations(t)
		mockCollector.AssertExpectations(t)
	})
	
	t.Run("setup fails on safety check", func(t *testing.T) {
		exp := NewBaseExperiment(config, client)
		
		mockCheck := new(MockSafetyCheck)
		mockCheck.On("Name").Return("failing-check")
		mockCheck.On("Check", ctx, client).Return(false, "not safe", nil)
		mockCheck.On("IsCritical").Return(true)
		exp.AddSafetyCheck(mockCheck)
		
		err := exp.Setup(ctx)
		require.Error(t, err)
		
		result := exp.GetResult()
		assert.Equal(t, ExperimentStatusFailed, result.Status)
		
		mockCheck.AssertExpectations(t)
	})
}

func TestBaseExperiment_Cleanup(t *testing.T) {
	ctx := context.Background()
	client := fake.NewClientBuilder().Build()
	config := ExperimentConfig{
		Name: "test-experiment",
		Target: TargetSelector{
			Namespace: "default",
		},
		Duration: 30 * time.Second,
		Action:   ChaosActionPodKill,
	}
	
	exp := NewBaseExperiment(config, client)
	
	// Add metrics collector
	mockCollector := new(MockMetricsCollector)
	mockCollector.On("Name").Return("cleanup-collector")
	mockCollector.On("Stop").Return(nil)
	mockCollector.On("Collect").Return(map[string]interface{}{
		"final": "metrics",
	}, nil)
	exp.AddMetricsCollector(mockCollector)
	
	// Set status to running
	exp.SetStatus(ExperimentStatusRunning)
	exp.Result.StartTime = time.Now().Add(-1 * time.Minute)
	
	err := exp.Cleanup(ctx)
	require.NoError(t, err)
	
	result := exp.GetResult()
	assert.Equal(t, ExperimentStatusCompleted, result.Status)
	assert.NotZero(t, result.EndTime)
	assert.NotZero(t, result.Duration)
	assert.Equal(t, "metrics", result.Metrics["cleanup-collector.final"])
	
	mockCollector.AssertExpectations(t)
}

func TestBaseExperiment_MonitorSafety(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	client := fake.NewClientBuilder().Build()
	config := ExperimentConfig{
		Name: "test-experiment",
		Target: TargetSelector{
			Namespace: "default",
		},
		Duration: 30 * time.Second,
		Action:   ChaosActionPodKill,
	}
	
	t.Run("safety check triggers abort", func(t *testing.T) {
		exp := NewBaseExperiment(config, client)
		
		// Mock check that fails after some calls
		mockCheck := new(MockSafetyCheck)
		mockCheck.On("Name").Return("monitor-check")
		// First call passes
		mockCheck.On("Check", mock.Anything, client).Return(true, "", nil).Once()
		// Second call fails
		mockCheck.On("Check", mock.Anything, client).Return(false, "safety violation", nil).Once()
		mockCheck.On("IsCritical").Return(true)
		
		exp.AddSafetyCheck(mockCheck)
		
		// Start monitoring with short interval
		go exp.MonitorSafety(ctx, 10*time.Millisecond)
		
		// Wait for abort
		select {
		case <-exp.stopCh:
			// Successfully aborted
			result := exp.GetResult()
			assert.Equal(t, ExperimentStatusAborted, result.Status)
		case <-time.After(1 * time.Second):
			t.Fatal("Expected abort did not occur")
		}
		
		mockCheck.AssertExpectations(t)
	})
	
	t.Run("context cancellation stops monitoring", func(t *testing.T) {
		exp := NewBaseExperiment(config, client)
		
		mockCheck := new(MockSafetyCheck)
		mockCheck.On("Name").Return("context-check")
		mockCheck.On("Check", mock.Anything, client).Return(true, "", nil)
		mockCheck.On("IsCritical").Return(true)
		
		exp.AddSafetyCheck(mockCheck)
		
		monitorCtx, monitorCancel := context.WithCancel(context.Background())
		
		// Start monitoring
		done := make(chan struct{})
		go func() {
			exp.MonitorSafety(monitorCtx, 10*time.Millisecond)
			close(done)
		}()
		
		// Cancel context
		monitorCancel()
		
		// Verify monitoring stopped
		select {
		case <-done:
			// Successfully stopped
		case <-time.After(1 * time.Second):
			t.Fatal("Monitoring did not stop after context cancellation")
		}
	})
}