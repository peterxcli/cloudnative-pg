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

// Package core provides the foundational types and interfaces for chaos testing
package core

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ExperimentStatus represents the current state of a chaos experiment
type ExperimentStatus string

const (
	// ExperimentStatusPending indicates the experiment is waiting to start
	ExperimentStatusPending ExperimentStatus = "Pending"
	// ExperimentStatusRunning indicates the experiment is currently executing
	ExperimentStatusRunning ExperimentStatus = "Running"
	// ExperimentStatusCompleted indicates the experiment finished successfully
	ExperimentStatusCompleted ExperimentStatus = "Completed"
	// ExperimentStatusFailed indicates the experiment encountered an error
	ExperimentStatusFailed ExperimentStatus = "Failed"
	// ExperimentStatusAborted indicates the experiment was stopped by safety mechanisms
	ExperimentStatusAborted ExperimentStatus = "Aborted"
)

// ChaosAction defines the type of chaos to inject
type ChaosAction string

const (
	// ChaosActionPodKill terminates a pod
	ChaosActionPodKill ChaosAction = "pod-kill"
	// ChaosActionPodFailure simulates pod failures
	ChaosActionPodFailure ChaosAction = "pod-failure"
	// ChaosActionNetworkDelay introduces network latency
	ChaosActionNetworkDelay ChaosAction = "network-delay"
	// ChaosActionNetworkPartition creates network partitions
	ChaosActionNetworkPartition ChaosAction = "network-partition"
	// ChaosActionIODelay introduces storage I/O delays
	ChaosActionIODelay ChaosAction = "io-delay"
	// ChaosActionCPUStress creates CPU pressure
	ChaosActionCPUStress ChaosAction = "cpu-stress"
	// ChaosActionMemoryStress creates memory pressure
	ChaosActionMemoryStress ChaosAction = "memory-stress"
)

// TargetSelector defines how to select targets for chaos injection
type TargetSelector struct {
	// Namespace to target
	Namespace string `json:"namespace"`
	// LabelSelector for pod selection
	LabelSelector labels.Selector `json:"labelSelector,omitempty"`
	// PodName for specific pod targeting
	PodName string `json:"podName,omitempty"`
	// NodeName for node-level chaos
	NodeName string `json:"nodeName,omitempty"`
	// Count of targets to affect
	Count int `json:"count,omitempty"`
	// Percentage of targets to affect
	Percentage int `json:"percentage,omitempty"`
}

// ExperimentConfig holds the configuration for a chaos experiment
type ExperimentConfig struct {
	// Name of the experiment
	Name string `json:"name"`
	// Description of what the experiment tests
	Description string `json:"description"`
	// Target selection criteria
	Target TargetSelector `json:"target"`
	// Action to perform
	Action ChaosAction `json:"action"`
	// Duration of the chaos injection
	Duration time.Duration `json:"duration"`
	// GracePeriod before starting the experiment
	GracePeriod time.Duration `json:"gracePeriod,omitempty"`
	// Parameters specific to the chaos action
	Parameters map[string]interface{} `json:"parameters,omitempty"`
	// SafetyChecks to run before and during the experiment
	SafetyChecks []string `json:"safetyChecks,omitempty"`
	// MetricsToCollect during the experiment
	MetricsToCollect []string `json:"metricsToCollect,omitempty"`
}

// ExperimentResult contains the outcome of a chaos experiment
type ExperimentResult struct {
	// ExperimentName that was executed
	ExperimentName string `json:"experimentName"`
	// Status of the experiment
	Status ExperimentStatus `json:"status"`
	// StartTime when the experiment began
	StartTime time.Time `json:"startTime"`
	// EndTime when the experiment completed
	EndTime time.Time `json:"endTime"`
	// Duration of the actual execution
	Duration time.Duration `json:"duration"`
	// Error if the experiment failed
	Error error `json:"error,omitempty"`
	// Metrics collected during the experiment
	Metrics map[string]interface{} `json:"metrics,omitempty"`
	// Events that occurred during the experiment
	Events []ExperimentEvent `json:"events,omitempty"`
	// SafetyAborted indicates if safety mechanisms stopped the experiment
	SafetyAborted bool `json:"safetyAborted"`
	// AbortReason if the experiment was aborted
	AbortReason string `json:"abortReason,omitempty"`
}

// ExperimentEvent represents a significant event during an experiment
type ExperimentEvent struct {
	// Timestamp when the event occurred
	Timestamp time.Time `json:"timestamp"`
	// Type of event
	Type string `json:"type"`
	// Message describing the event
	Message string `json:"message"`
	// Severity of the event
	Severity EventSeverity `json:"severity"`
	// Details with additional context
	Details map[string]interface{} `json:"details,omitempty"`
}

// EventSeverity indicates the importance of an experiment event
type EventSeverity string

const (
	// EventSeverityInfo is for informational events
	EventSeverityInfo EventSeverity = "Info"
	// EventSeverityWarning indicates potential issues
	EventSeverityWarning EventSeverity = "Warning"
	// EventSeverityError indicates failures
	EventSeverityError EventSeverity = "Error"
	// EventSeverityCritical indicates severe failures requiring immediate attention
	EventSeverityCritical EventSeverity = "Critical"
)

// Experiment defines the interface for all chaos experiments
type Experiment interface {
	// Name returns the experiment name
	Name() string
	// Validate checks if the experiment configuration is valid
	Validate() error
	// Setup prepares the experiment environment
	Setup(ctx context.Context) error
	// Run executes the chaos injection
	Run(ctx context.Context) error
	// Cleanup removes any injected failures
	Cleanup(ctx context.Context) error
	// GetResult returns the experiment results
	GetResult() *ExperimentResult
}

// ExperimentRunner orchestrates chaos experiment execution
type ExperimentRunner interface {
	// RunExperiment executes a single experiment
	RunExperiment(ctx context.Context, exp Experiment) (*ExperimentResult, error)
	// RunExperiments executes multiple experiments
	RunExperiments(ctx context.Context, exps []Experiment) ([]*ExperimentResult, error)
	// StopExperiment stops a running experiment
	StopExperiment(ctx context.Context, name string) error
	// GetStatus returns the current status of an experiment
	GetStatus(name string) (ExperimentStatus, error)
}

// SafetyCheck defines the interface for safety validations
type SafetyCheck interface {
	// Name returns the check name
	Name() string
	// Check performs the safety validation
	Check(ctx context.Context, client client.Client) (bool, string, error)
	// IsCritical indicates if failing this check should abort the experiment
	IsCritical() bool
}

// MetricsCollector defines the interface for collecting experiment metrics
type MetricsCollector interface {
	// Name returns the collector name
	Name() string
	// Start begins metric collection
	Start(ctx context.Context) error
	// Stop ends metric collection
	Stop() error
	// Collect returns the collected metrics
	Collect() (map[string]interface{}, error)
	// Reset clears collected metrics
	Reset()
}