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

package metrics

import (
	"context"
	"fmt"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// ResilienceMetrics tracks key resilience metrics during chaos experiments
type ResilienceMetrics struct {
	// Recovery Metrics
	TimeToDetection time.Duration `json:"ttd"`      // Time to detect failure
	TimeToRecovery  time.Duration `json:"ttr"`      // Time to full recovery
	DataLossBytes   int64         `json:"dataLoss"` // Amount of data lost

	// Availability Metrics
	DowntimeDuration time.Duration `json:"downtime"`       // Total downtime
	FailedRequests   int64         `json:"failedRequests"` // Number of failed requests
	SuccessRate      float64       `json:"successRate"`    // Percentage of successful requests

	// Performance Impact
	LatencyP50 time.Duration `json:"p50Latency"` // 50th percentile latency
	LatencyP99 time.Duration `json:"p99Latency"` // 99th percentile latency
	Throughput float64       `json:"throughput"` // Requests per second

	// Consistency Metrics
	ReplicationLag    time.Duration `json:"replicationLag"`    // Maximum replication lag observed
	SplitBrainEvents  int           `json:"splitBrainEvents"`  // Number of split-brain scenarios
	DataInconsistency bool          `json:"dataInconsistency"` // Whether data inconsistency was detected
}

// ClusterMetricsCollector collects metrics from a CloudNativePG cluster
type ClusterMetricsCollector struct {
	client       client.Client
	namespace    string
	clusterName  string
	metrics      *ResilienceMetrics
	samples      []MetricSample
	mu           sync.RWMutex
	stopCh       chan struct{}
	ticker       *time.Ticker
	startTime    time.Time
	failureTime  *time.Time
	recoveryTime *time.Time
}

// MetricSample represents a single metric measurement
type MetricSample struct {
	Timestamp         time.Time
	ReadyInstances    int
	TotalInstances    int
	CurrentPrimary    string
	TargetPrimary     string
	ReplicationLag    time.Duration
	ConnectionsActive int32
	ConnectionsFailed int32
}

// NewClusterMetricsCollector creates a new cluster metrics collector
func NewClusterMetricsCollector(client client.Client, namespace, clusterName string) *ClusterMetricsCollector {
	return &ClusterMetricsCollector{
		client:      client,
		namespace:   namespace,
		clusterName: clusterName,
		metrics:     &ResilienceMetrics{},
		samples:     []MetricSample{},
		stopCh:      make(chan struct{}),
	}
}

// Name returns the collector name
func (c *ClusterMetricsCollector) Name() string {
	return fmt.Sprintf("cluster-%s", c.clusterName)
}

// Start begins metric collection
func (c *ClusterMetricsCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	c.startTime = time.Now()
	c.ticker = time.NewTicker(1 * time.Second)
	c.mu.Unlock()

	// Start collection goroutine
	go c.collectMetrics(ctx)

	return nil
}

// Stop ends metric collection
func (c *ClusterMetricsCollector) Stop() error {
	close(c.stopCh)
	if c.ticker != nil {
		c.ticker.Stop()
	}
	return nil
}

// Collect returns the collected metrics
func (c *ClusterMetricsCollector) Collect() (map[string]interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Calculate final metrics
	c.calculateMetrics()

	// Convert to map
	result := map[string]interface{}{
		"resilience": c.metrics,
		"samples":    len(c.samples),
		"duration":   time.Since(c.startTime).Seconds(),
	}

	if c.failureTime != nil {
		result["failureDetectedAt"] = c.failureTime.Unix()
	}
	if c.recoveryTime != nil {
		result["recoveryCompletedAt"] = c.recoveryTime.Unix()
	}

	return result, nil
}

// Reset clears collected metrics
func (c *ClusterMetricsCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.metrics = &ResilienceMetrics{}
	c.samples = []MetricSample{}
	c.failureTime = nil
	c.recoveryTime = nil
}

// collectMetrics continuously collects cluster metrics
func (c *ClusterMetricsCollector) collectMetrics(ctx context.Context) {
	var lastHealthyState *MetricSample
	failureDetected := false

	for {
		select {
		case <-c.stopCh:
			return
		case <-ctx.Done():
			return
		case <-c.ticker.C:
			sample, err := c.collectSample(ctx)
			if err != nil {
				continue
			}

			c.mu.Lock()
			c.samples = append(c.samples, *sample)

			// Detect failure
			if !failureDetected && lastHealthyState != nil {
				if c.isFailureState(sample, lastHealthyState) {
					failureDetected = true
					now := time.Now()
					c.failureTime = &now
					c.metrics.TimeToDetection = now.Sub(c.startTime)
				}
			}

			// Detect recovery
			if failureDetected && c.failureTime != nil {
				if c.isRecoveredState(sample) {
					now := time.Now()
					c.recoveryTime = &now
					c.metrics.TimeToRecovery = now.Sub(*c.failureTime)
					failureDetected = false
				}
			}

			// Track healthy state
			if c.isHealthyState(sample) {
				lastHealthyState = sample
			}

			c.mu.Unlock()
		}
	}
}

// collectSample collects a single metric sample
func (c *ClusterMetricsCollector) collectSample(ctx context.Context) (*MetricSample, error) {
	cluster := &apiv1.Cluster{}
	key := client.ObjectKey{Namespace: c.namespace, Name: c.clusterName}

	if err := c.client.Get(ctx, key, cluster); err != nil {
		return nil, err
	}

	sample := &MetricSample{
		Timestamp:      time.Now(),
		ReadyInstances: cluster.Status.ReadyInstances,
		TotalInstances: cluster.Status.Instances,
		CurrentPrimary: cluster.Status.CurrentPrimary,
		TargetPrimary:  cluster.Status.TargetPrimary,
	}

	// Collect pod-level metrics
	c.collectPodMetrics(ctx, sample)

	return sample, nil
}

// collectPodMetrics collects metrics from cluster pods
func (c *ClusterMetricsCollector) collectPodMetrics(ctx context.Context, sample *MetricSample) {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(c.namespace),
		client.MatchingLabels{"cnpg.io/cluster": c.clusterName},
	}

	if err := c.client.List(ctx, podList, listOpts...); err != nil {
		return
	}

	var activeConnections, failedConnections int32
	for _, pod := range podList.Items {
		// In a real implementation, we would query pod metrics
		// For POC, we'll use pod status as a proxy
		if pod.Status.Phase == corev1.PodRunning {
			activeConnections += 10 // Simulated value
		} else {
			failedConnections += 5 // Simulated value
		}
	}

	sample.ConnectionsActive = activeConnections
	sample.ConnectionsFailed = failedConnections
}

// isHealthyState checks if the cluster is in a healthy state
func (c *ClusterMetricsCollector) isHealthyState(sample *MetricSample) bool {
	return sample.ReadyInstances == sample.TotalInstances &&
		sample.CurrentPrimary == sample.TargetPrimary &&
		sample.CurrentPrimary != ""
}

// isFailureState checks if a failure has occurred
func (c *ClusterMetricsCollector) isFailureState(current, lastHealthy *MetricSample) bool {
	// Failure detected if:
	// - Ready instances decreased
	// - Primary changed unexpectedly
	// - No primary available
	return current.ReadyInstances < lastHealthy.ReadyInstances ||
		(current.CurrentPrimary != lastHealthy.CurrentPrimary && current.CurrentPrimary != current.TargetPrimary) ||
		current.CurrentPrimary == ""
}

// isRecoveredState checks if the cluster has recovered
func (c *ClusterMetricsCollector) isRecoveredState(sample *MetricSample) bool {
	return sample.ReadyInstances == sample.TotalInstances &&
		sample.CurrentPrimary == sample.TargetPrimary &&
		sample.CurrentPrimary != ""
}

// calculateMetrics calculates final metrics from samples
func (c *ClusterMetricsCollector) calculateMetrics() {
	if len(c.samples) == 0 {
		return
	}

	var totalDowntime time.Duration
	var failedRequests, successfulRequests int64
	var latencies []time.Duration
	var maxReplicationLag time.Duration

	for i, sample := range c.samples {
		// Calculate downtime
		if !c.isHealthyState(&sample) {
			if i > 0 {
				downtime := sample.Timestamp.Sub(c.samples[i-1].Timestamp)
				totalDowntime += downtime
			}
		}

		// Track requests
		failedRequests += int64(sample.ConnectionsFailed)
		successfulRequests += int64(sample.ConnectionsActive)

		// Track replication lag
		if sample.ReplicationLag > maxReplicationLag {
			maxReplicationLag = sample.ReplicationLag
		}

		// Simulate latency (in real implementation, would get from actual metrics)
		latencies = append(latencies, time.Duration(100+i*10)*time.Millisecond)
	}

	// Update metrics
	c.metrics.DowntimeDuration = totalDowntime
	c.metrics.FailedRequests = failedRequests
	if total := failedRequests + successfulRequests; total > 0 {
		c.metrics.SuccessRate = float64(successfulRequests) / float64(total) * 100
	}
	c.metrics.ReplicationLag = maxReplicationLag

	// Calculate latency percentiles (simplified)
	if len(latencies) > 0 {
		c.metrics.LatencyP50 = latencies[len(latencies)/2]
		c.metrics.LatencyP99 = latencies[len(latencies)*99/100]
	}

	// Calculate throughput
	duration := time.Since(c.startTime).Seconds()
	if duration > 0 {
		c.metrics.Throughput = float64(successfulRequests) / duration
	}
}

// BaseMetricsCollector provides a simple implementation of MetricsCollector
type BaseMetricsCollector struct {
	name    string
	metrics map[string]interface{}
	mu      sync.RWMutex
}

// NewBaseMetricsCollector creates a new base metrics collector
func NewBaseMetricsCollector(name string) *BaseMetricsCollector {
	return &BaseMetricsCollector{
		name:    name,
		metrics: make(map[string]interface{}),
	}
}

// Name returns the collector name
func (c *BaseMetricsCollector) Name() string {
	return c.name
}

// Start begins metric collection
func (c *BaseMetricsCollector) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics["startTime"] = time.Now().Unix()
	return nil
}

// Stop ends metric collection
func (c *BaseMetricsCollector) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics["endTime"] = time.Now().Unix()
	return nil
}

// Collect returns the collected metrics
func (c *BaseMetricsCollector) Collect() (map[string]interface{}, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]interface{})
	for k, v := range c.metrics {
		result[k] = v
	}
	return result, nil
}

// Reset clears collected metrics
func (c *BaseMetricsCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics = make(map[string]interface{})
}

// Record adds a metric value
func (c *BaseMetricsCollector) Record(key string, value interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics[key] = value
}
