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
	"fmt"
	"sync"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BaseExperiment provides common functionality for all experiments
type BaseExperiment struct {
	Config       ExperimentConfig
	Result       *ExperimentResult
	Client       client.Client
	collectors   []MetricsCollector
	safetyChecks []SafetyCheck
	mu           sync.RWMutex
	stopCh       chan struct{}
}

// NewBaseExperiment creates a new base experiment
func NewBaseExperiment(config ExperimentConfig, k8sClient client.Client) *BaseExperiment {
	return &BaseExperiment{
		Config: config,
		Client: k8sClient,
		Result: &ExperimentResult{
			ExperimentName: config.Name,
			Status:         ExperimentStatusPending,
			Events:         []ExperimentEvent{},
			Metrics:        make(map[string]interface{}),
		},
		collectors: []MetricsCollector{},
		stopCh:     make(chan struct{}),
	}
}

// Name returns the experiment name
func (e *BaseExperiment) Name() string {
	return e.Config.Name
}

// Validate checks if the experiment configuration is valid
func (e *BaseExperiment) Validate() error {
	if e.Config.Name == "" {
		return fmt.Errorf("experiment name is required")
	}
	if e.Config.Target.Namespace == "" {
		return fmt.Errorf("target namespace is required")
	}
	if e.Config.Duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	if e.Config.Action == "" {
		return fmt.Errorf("chaos action is required")
	}
	return nil
}

// AddMetricsCollector adds a metrics collector to the experiment
func (e *BaseExperiment) AddMetricsCollector(collector MetricsCollector) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.collectors = append(e.collectors, collector)
}

// AddSafetyCheck adds a safety check to the experiment
func (e *BaseExperiment) AddSafetyCheck(check SafetyCheck) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.safetyChecks = append(e.safetyChecks, check)
}

// AddEvent adds an event to the experiment result
func (e *BaseExperiment) AddEvent(eventType, message string, severity EventSeverity) {
	e.mu.Lock()
	defer e.mu.Unlock()
	event := ExperimentEvent{
		Timestamp: time.Now(),
		Type:      eventType,
		Message:   message,
		Severity:  severity,
	}
	e.Result.Events = append(e.Result.Events, event)
}

// SetStatus updates the experiment status
func (e *BaseExperiment) SetStatus(status ExperimentStatus) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Result.Status = status
}

// GetResult returns the experiment result
func (e *BaseExperiment) GetResult() *ExperimentResult {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.Result
}

// RunSafetyChecks executes all safety checks
func (e *BaseExperiment) RunSafetyChecks(ctx context.Context) error {
	e.mu.RLock()
	checks := e.safetyChecks
	e.mu.RUnlock()

	for _, check := range checks {
		passed, reason, err := check.Check(ctx, e.Client)
		if err != nil {
			e.AddEvent("SafetyCheck", fmt.Sprintf("Safety check %s failed: %v", check.Name(), err), EventSeverityError)
			if check.IsCritical() {
				return fmt.Errorf("critical safety check %s failed: %w", check.Name(), err)
			}
		}
		if !passed {
			e.AddEvent("SafetyCheck", fmt.Sprintf("Safety check %s failed: %s", check.Name(), reason), EventSeverityWarning)
			if check.IsCritical() {
				e.Result.SafetyAborted = true
				e.Result.AbortReason = reason
				return fmt.Errorf("critical safety check %s failed: %s", check.Name(), reason)
			}
		}
		e.AddEvent("SafetyCheck", fmt.Sprintf("Safety check %s passed", check.Name()), EventSeverityInfo)
	}
	return nil
}

// StartMetricsCollection starts all metrics collectors
func (e *BaseExperiment) StartMetricsCollection(ctx context.Context) error {
	e.mu.RLock()
	collectors := e.collectors
	e.mu.RUnlock()

	for _, collector := range collectors {
		if err := collector.Start(ctx); err != nil {
			e.AddEvent("Metrics", fmt.Sprintf("Failed to start collector %s: %v", collector.Name(), err), EventSeverityWarning)
			// Continue with other collectors even if one fails
		} else {
			e.AddEvent("Metrics", fmt.Sprintf("Started metrics collector %s", collector.Name()), EventSeverityInfo)
		}
	}
	return nil
}

// StopMetricsCollection stops all metrics collectors and collects results
func (e *BaseExperiment) StopMetricsCollection() {
	e.mu.RLock()
	collectors := e.collectors
	e.mu.RUnlock()

	for _, collector := range collectors {
		if err := collector.Stop(); err != nil {
			e.AddEvent("Metrics", fmt.Sprintf("Failed to stop collector %s: %v", collector.Name(), err), EventSeverityWarning)
		}
		
		metrics, err := collector.Collect()
		if err != nil {
			e.AddEvent("Metrics", fmt.Sprintf("Failed to collect metrics from %s: %v", collector.Name(), err), EventSeverityWarning)
		} else {
			e.mu.Lock()
			for k, v := range metrics {
				e.Result.Metrics[fmt.Sprintf("%s.%s", collector.Name(), k)] = v
			}
			e.mu.Unlock()
			e.AddEvent("Metrics", fmt.Sprintf("Collected metrics from %s", collector.Name()), EventSeverityInfo)
		}
	}
}

// Setup prepares the experiment environment
func (e *BaseExperiment) Setup(ctx context.Context) error {
	e.SetStatus(ExperimentStatusPending)
	e.Result.StartTime = time.Now()
	e.AddEvent("Setup", "Starting experiment setup", EventSeverityInfo)
	
	// Run initial safety checks
	if err := e.RunSafetyChecks(ctx); err != nil {
		e.SetStatus(ExperimentStatusFailed)
		return err
	}
	
	// Start metrics collection
	if err := e.StartMetricsCollection(ctx); err != nil {
		e.AddEvent("Setup", fmt.Sprintf("Warning: metrics collection setup failed: %v", err), EventSeverityWarning)
		// Continue even if metrics fail
	}
	
	e.AddEvent("Setup", "Experiment setup completed", EventSeverityInfo)
	return nil
}

// Cleanup removes any injected failures
func (e *BaseExperiment) Cleanup(ctx context.Context) error {
	e.AddEvent("Cleanup", "Starting experiment cleanup", EventSeverityInfo)
	
	// Stop metrics collection
	e.StopMetricsCollection()
	
	// Update result
	e.Result.EndTime = time.Now()
	e.Result.Duration = e.Result.EndTime.Sub(e.Result.StartTime)
	
	if e.Result.Status == ExperimentStatusRunning {
		e.SetStatus(ExperimentStatusCompleted)
	}
	
	e.AddEvent("Cleanup", "Experiment cleanup completed", EventSeverityInfo)
	return nil
}

// MonitorSafety continuously monitors safety conditions during the experiment
func (e *BaseExperiment) MonitorSafety(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			if err := e.RunSafetyChecks(ctx); err != nil {
				e.AddEvent("SafetyMonitor", fmt.Sprintf("Safety check failed during monitoring: %v", err), EventSeverityCritical)
				e.SetStatus(ExperimentStatusAborted)
				close(e.stopCh)
				return
			}
		}
	}
}

// Stop signals the experiment to stop
func (e *BaseExperiment) Stop() {
	close(e.stopCh)
}