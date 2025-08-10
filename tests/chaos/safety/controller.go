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
	"fmt"
	"os"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/core"
)

// Controller manages safety checks and abort mechanisms for chaos experiments
type Controller struct {
	client             client.Client
	config             SafetyConfig
	emergencyStopFile  string
	abortSignal        chan struct{}
	mu                 sync.RWMutex
	checks             []core.SafetyCheck
	monitoringInterval time.Duration
	recoveryTimeCheck  *RecoveryTimeCheck
	lastClusterState   *ClusterState
}

// SafetyConfig holds configuration for the safety controller
type SafetyConfig struct {
	// MaxFailurePercent is the maximum percentage of instances that can fail
	MaxFailurePercent float64
	// MinHealthyReplicas is the minimum number of healthy replicas required
	MinHealthyReplicas int
	// MaxDataLagBytes is the maximum acceptable replication lag in bytes
	MaxDataLagBytes int64
	// MaxRecoveryTime is the maximum time allowed for recovery
	MaxRecoveryTime time.Duration
	// EnableEmergencyStop enables the emergency stop file mechanism
	EnableEmergencyStop bool
	// ClusterNamespace is the namespace of the target cluster
	ClusterNamespace string
	// ClusterName is the name of the target cluster
	ClusterName string
}

// NewController creates a new safety controller
func NewController(client client.Client, config SafetyConfig) *Controller {
	return &Controller{
		client:             client,
		config:             config,
		emergencyStopFile:  "/tmp/chaos-emergency-stop",
		abortSignal:        make(chan struct{}),
		checks:             []core.SafetyCheck{},
		monitoringInterval: 5 * time.Second,
		recoveryTimeCheck:  nil,
		lastClusterState:   nil,
	}
}

// RegisterCheck adds a safety check to the controller
func (c *Controller) RegisterCheck(check core.SafetyCheck) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checks = append(c.checks, check)
}

// Start begins continuous safety monitoring
func (c *Controller) Start(ctx context.Context) error {
	// Register default checks
	c.registerDefaultChecks()

	// Start monitoring goroutine
	go c.monitorSafety(ctx)

	return nil
}

// Stop halts safety monitoring
func (c *Controller) Stop() {
	close(c.abortSignal)
}

// ShouldAbort checks if an experiment should be aborted
func (c *Controller) ShouldAbort(ctx context.Context) (bool, string) {
	// Check emergency stop file
	if c.config.EnableEmergencyStop {
		if _, err := os.Stat(c.emergencyStopFile); err == nil {
			return true, "emergency stop file detected"
		}
	}

	// Check abort signal
	select {
	case <-c.abortSignal:
		return true, "abort signal received"
	default:
	}

	// Run all safety checks
	c.mu.RLock()
	checks := c.checks
	c.mu.RUnlock()

	for _, check := range checks {
		passed, reason, err := check.Check(ctx, c.client)
		if err != nil {
			if check.IsCritical() {
				return true, fmt.Sprintf("critical check %s error: %v", check.Name(), err)
			}
		}
		if !passed && check.IsCritical() {
			return true, fmt.Sprintf("critical check %s failed: %s", check.Name(), reason)
		}
	}

	return false, ""
}

// TriggerEmergencyStop creates the emergency stop file
func (c *Controller) TriggerEmergencyStop(reason string) error {
	if !c.config.EnableEmergencyStop {
		return fmt.Errorf("emergency stop is not enabled")
	}

	file, err := os.Create(c.emergencyStopFile)
	if err != nil {
		return fmt.Errorf("failed to create emergency stop file: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString(fmt.Sprintf("Emergency stop triggered at %s\nReason: %s\n",
		time.Now().Format(time.RFC3339), reason))
	return err
}

// ClearEmergencyStop removes the emergency stop file
func (c *Controller) ClearEmergencyStop() error {
	return os.Remove(c.emergencyStopFile)
}

// TriggerRecovery manually triggers recovery timing (for external components)
func (c *Controller) TriggerRecovery(reason string) {
	if c.recoveryTimeCheck != nil {
		fmt.Printf("Manually triggering recovery timer: %s\n", reason)
		c.recoveryTimeCheck.StartRecovery()
	}
}

// ResetRecovery manually resets recovery timing (for external components)
func (c *Controller) ResetRecovery(reason string) {
	if c.recoveryTimeCheck != nil {
		fmt.Printf("Manually resetting recovery timer: %s\n", reason)
		c.recoveryTimeCheck.ResetRecovery()
	}
}

// detectRecoveryScenarios monitors the cluster for recovery scenarios and triggers recovery timing
func (c *Controller) detectRecoveryScenarios(ctx context.Context) {
	if c.recoveryTimeCheck == nil {
		return
	}

	// Get current cluster state
	currentState, err := c.getClusterState(ctx)
	if err != nil {
		fmt.Printf("Failed to get cluster state for recovery detection: %v\n", err)
		return
	}

	// If this is the first check, just store the state
	if c.lastClusterState == nil {
		c.lastClusterState = currentState
		return
	}

	// Check for recovery scenarios
	if c.isRecoveryScenario(c.lastClusterState, currentState) {
		fmt.Printf("Recovery scenario detected, starting recovery timer\n")
		c.recoveryTimeCheck.StartRecovery()
	}

	// Check if cluster has recovered to healthy state
	if c.isClusterHealthy(currentState) && !c.isClusterHealthy(c.lastClusterState) {
		fmt.Printf("Cluster recovered to healthy state, resetting recovery timer\n")
		c.recoveryTimeCheck.ResetRecovery()
	}

	// Update the last known state
	c.lastClusterState = currentState
}

// getClusterState retrieves the current state of the cluster
func (c *Controller) getClusterState(ctx context.Context) (*ClusterState, error) {
	cluster := &apiv1.Cluster{}
	key := types.NamespacedName{Namespace: c.config.ClusterNamespace, Name: c.config.ClusterName}

	if err := c.client.Get(ctx, key, cluster); err != nil {
		return nil, fmt.Errorf("failed to get cluster: %w", err)
	}

	state := &ClusterState{
		ReadyInstances: cluster.Status.ReadyInstances,
		CurrentPrimary: cluster.Status.CurrentPrimary,
		TargetPrimary:  cluster.Status.TargetPrimary,
		HasPrimary:     cluster.Status.CurrentPrimary != "",
		IsHealthy:      cluster.Status.ReadyInstances >= c.config.MinHealthyReplicas,
	}

	return state, nil
}

// isRecoveryScenario determines if the cluster is in a recovery scenario
func (c *Controller) isRecoveryScenario(previous, current *ClusterState) bool {
	// Scenario 1: Primary switchover in progress
	if current.CurrentPrimary != current.TargetPrimary && current.TargetPrimary != "" {
		return true
	}

	// Scenario 2: Primary was lost and now we have one again
	if !previous.HasPrimary && current.HasPrimary {
		return true
	}

	// Scenario 3: Cluster health degraded and then recovered
	if !previous.IsHealthy && current.IsHealthy {
		return true
	}

	// Scenario 4: Ready instances dropped below minimum and then recovered
	if previous.ReadyInstances >= c.config.MinHealthyReplicas &&
		current.ReadyInstances < c.config.MinHealthyReplicas {
		return true
	}

	return false
}

// isClusterHealthy determines if the cluster is in a healthy state
func (c *Controller) isClusterHealthy(state *ClusterState) bool {
	return state.IsHealthy &&
		state.HasPrimary &&
		state.CurrentPrimary == state.TargetPrimary
}

// monitorSafety continuously monitors safety conditions
func (c *Controller) monitorSafety(ctx context.Context) {
	ticker := time.NewTicker(c.monitoringInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check for recovery scenarios before running safety checks
			c.detectRecoveryScenarios(ctx)

			if shouldAbort, reason := c.ShouldAbort(ctx); shouldAbort {
				fmt.Printf("Safety controller triggered abort: %s\n", reason)
				close(c.abortSignal)
				return
			}
		}
	}
}

// registerDefaultChecks registers the default safety checks
func (c *Controller) registerDefaultChecks() {
	// Cluster health check
	c.RegisterCheck(&ClusterHealthCheck{
		Namespace:          c.config.ClusterNamespace,
		ClusterName:        c.config.ClusterName,
		MinHealthyReplicas: c.config.MinHealthyReplicas,
	})

	// Data consistency check
	c.RegisterCheck(&DataConsistencyCheck{
		Namespace:       c.config.ClusterNamespace,
		ClusterName:     c.config.ClusterName,
		MaxDataLagBytes: c.config.MaxDataLagBytes,
	})

	// Recovery time check
	recoveryCheck := &RecoveryTimeCheck{
		maxRecoveryTime: c.config.MaxRecoveryTime,
		startTime:       time.Now(),
	}
	c.RegisterCheck(recoveryCheck)
	c.recoveryTimeCheck = recoveryCheck
}

// ClusterHealthCheck validates cluster health
type ClusterHealthCheck struct {
	Namespace          string
	ClusterName        string
	MinHealthyReplicas int
}

// Name returns the check name
func (c *ClusterHealthCheck) Name() string {
	return "ClusterHealth"
}

// Check performs the cluster health validation
func (c *ClusterHealthCheck) Check(ctx context.Context, client client.Client) (bool, string, error) {
	cluster := &apiv1.Cluster{}
	key := types.NamespacedName{Namespace: c.Namespace, Name: c.ClusterName}

	if err := client.Get(ctx, key, cluster); err != nil {
		return false, "", fmt.Errorf("failed to get cluster: %w", err)
	}

	// Check ready instances
	if cluster.Status.ReadyInstances < c.MinHealthyReplicas {
		return false, fmt.Sprintf("only %d ready instances, need %d",
			cluster.Status.ReadyInstances, c.MinHealthyReplicas), nil
	}

	// Check if primary exists
	if cluster.Status.CurrentPrimary == "" {
		return false, "no primary instance available", nil
	}

	// Check if cluster is operational
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary && cluster.Status.TargetPrimary != "" {
		return false, "primary switchover in progress", nil
	}

	return true, "", nil
}

// IsCritical indicates this is a critical check
func (c *ClusterHealthCheck) IsCritical() bool {
	return true
}

// DataConsistencyCheck validates data consistency
type DataConsistencyCheck struct {
	Namespace       string
	ClusterName     string
	MaxDataLagBytes int64
}

// Name returns the check name
func (c *DataConsistencyCheck) Name() string {
	return "DataConsistency"
}

// Check performs the data consistency validation
func (c *DataConsistencyCheck) Check(ctx context.Context, client client.Client) (bool, string, error) {
	cluster := &apiv1.Cluster{}
	key := types.NamespacedName{Namespace: c.Namespace, Name: c.ClusterName}

	if err := client.Get(ctx, key, cluster); err != nil {
		return false, "", fmt.Errorf("failed to get cluster: %w", err)
	}

	// Check if there are enough ready instances for replication
	if cluster.Status.ReadyInstances < 2 {
		return false, "insufficient replicas for data consistency", nil
	}

	// Check if primary and target primary are aligned
	if cluster.Status.CurrentPrimary != cluster.Status.TargetPrimary && cluster.Status.TargetPrimary != "" {
		return false, "primary transition in progress, data consistency uncertain", nil
	}

	// In a real implementation, we would check actual replication lag
	// For POC, we'll simulate this check by assuming consistency if we have replicas
	return true, "", nil
}

// IsCritical indicates this is a critical check
func (c *DataConsistencyCheck) IsCritical() bool {
	return true
}

// RecoveryTimeCheck validates recovery time constraints
type RecoveryTimeCheck struct {
	maxRecoveryTime time.Duration
	startTime       time.Time
	recoveryStart   *time.Time
	mu              sync.RWMutex
}

// Name returns the check name
func (c *RecoveryTimeCheck) Name() string {
	return "RecoveryTime"
}

// Check performs the recovery time validation
func (c *RecoveryTimeCheck) Check(ctx context.Context, client client.Client) (bool, string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.recoveryStart != nil {
		elapsed := time.Since(*c.recoveryStart)
		if elapsed > c.maxRecoveryTime {
			return false, fmt.Sprintf("recovery time exceeded: %v > %v", elapsed, c.maxRecoveryTime), nil
		}
	}

	return true, "", nil
}

// IsCritical indicates this is not a critical check
func (c *RecoveryTimeCheck) IsCritical() bool {
	return false
}

// StartRecovery marks the beginning of recovery
func (c *RecoveryTimeCheck) StartRecovery() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.recoveryStart = &now
}

// ResetRecovery clears the recovery timer
func (c *RecoveryTimeCheck) ResetRecovery() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recoveryStart = nil
}

// ClusterState represents the state of the cluster for recovery detection
type ClusterState struct {
	ReadyInstances int
	CurrentPrimary string
	TargetPrimary  string
	HasPrimary     bool
	IsHealthy      bool
}
