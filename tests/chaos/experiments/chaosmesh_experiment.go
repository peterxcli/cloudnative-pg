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

package experiments

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/chaosmesh"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/core"
	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/safety"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ChaosMeshExperiment wraps our experiment with Chaos Mesh integration
type ChaosMeshExperiment struct {
	*core.BaseExperiment
	adapter          *chaosmesh.Adapter
	chaosName        string
	chaosKind        string
	metricsCollector core.MetricsCollector
}

// NewChaosMeshExperiment creates a new Chaos Mesh integrated experiment
func NewChaosMeshExperiment(
	config core.ExperimentConfig,
	client client.Client,
	safetyChecks []core.SafetyCheck,
	metricsCollector core.MetricsCollector,
) *ChaosMeshExperiment {
	baseExp := core.NewBaseExperiment(config, client)

	// Add safety checks
	for _, check := range safetyChecks {
		baseExp.AddSafetyCheck(check)
	}

	// Add metrics collector
	if metricsCollector != nil {
		baseExp.AddMetricsCollector(metricsCollector)
	}

	return &ChaosMeshExperiment{
		BaseExperiment:   baseExp,
		adapter:          chaosmesh.NewAdapter(client, config.Target.Namespace),
		metricsCollector: metricsCollector,
	}
}

// Run executes the chaos experiment using Chaos Mesh
func (e *ChaosMeshExperiment) Run(ctx context.Context) error {
	// Start metrics collection if available
	if e.metricsCollector != nil {
		if err := e.metricsCollector.Start(ctx); err != nil {
			return fmt.Errorf("failed to start metrics collection: %w", err)
		}
		defer e.metricsCollector.Stop()
	}

	// Pre-experiment safety checks
	if err := e.RunSafetyChecks(ctx); err != nil {
		e.AddEvent("ExperimentAborted",
			fmt.Sprintf("Pre-experiment safety check failed: %v", err),
			core.EventSeverityError)
		return fmt.Errorf("pre-experiment safety check failed: %w", err)
	}

	// Record experiment start
	e.AddEvent("ExperimentStarted",
		fmt.Sprintf("Chaos experiment started: %s", e.Config.Name),
		core.EventSeverityInfo)

	// Inject chaos based on action type
	var err error
	switch e.Config.Action {
	case core.ChaosActionPodKill, core.ChaosActionPodFailure:
		err = e.injectPodChaos(ctx)
	case core.ChaosActionNetworkDelay, core.ChaosActionNetworkPartition:
		err = e.injectNetworkChaos(ctx)
	case core.ChaosActionIODelay, core.ChaosActionIOError:
		err = e.injectIOChaos(ctx)
	default:
		err = fmt.Errorf("unsupported chaos action: %v", e.Config.Action)
	}

	if err != nil {
		e.AddEvent("ExperimentFailed",
			fmt.Sprintf("Failed to inject chaos: %v", err),
			core.EventSeverityError)
		return err
	}

	// Wait for chaos to be ready
	if err := e.adapter.WaitForChaosReady(ctx, e.chaosKind, e.chaosName, 30*time.Second); err != nil {
		return fmt.Errorf("chaos experiment not ready: %w", err)
	}

	// Monitor during chaos
	e.monitorDuringChaos(ctx)

	// Clean up chaos after duration
	select {
	case <-ctx.Done():
		// Context cancelled, clean up immediately
	case <-time.After(e.Config.Duration):
		// Duration passed
	}

	// Cleanup chaos
	if err := e.Cleanup(ctx); err != nil {
		return fmt.Errorf("failed to cleanup chaos: %w", err)
	}

	// Post-experiment validation
	if err := e.RunSafetyChecks(ctx); err != nil {
		e.AddEvent("ExperimentFailed",
			fmt.Sprintf("Post-experiment safety check failed: %v", err),
			core.EventSeverityError)
		return fmt.Errorf("post-experiment safety check failed: %w", err)
	}

	// Record experiment completion
	e.AddEvent("ExperimentCompleted",
		fmt.Sprintf("Chaos experiment completed successfully, duration: %v", e.Config.Duration),
		core.EventSeverityInfo)

	return nil
}

// Cleanup removes the chaos experiment
func (e *ChaosMeshExperiment) Cleanup(ctx context.Context) error {
	if e.chaosName != "" && e.chaosKind != "" {
		return e.adapter.DeleteChaos(ctx, e.chaosKind, e.chaosName)
	}
	return nil
}

// Validate checks if the experiment configuration is valid
func (e *ChaosMeshExperiment) Validate() error {
	if e.Config.Name == "" {
		return fmt.Errorf("experiment name is required")
	}
	if e.Config.Target.Namespace == "" {
		return fmt.Errorf("target namespace is required")
	}
	if e.Config.Duration <= 0 {
		return fmt.Errorf("duration must be positive")
	}
	return nil
}

// injectPodChaos injects pod-level chaos
func (e *ChaosMeshExperiment) injectPodChaos(ctx context.Context) error {
	podChaos, err := e.adapter.InjectPodChaos(ctx, e.Config)
	if err != nil {
		return fmt.Errorf("failed to inject pod chaos: %w", err)
	}
	e.chaosName = podChaos.Name
	e.chaosKind = "PodChaos"
	return nil
}

// injectNetworkChaos injects network-level chaos
func (e *ChaosMeshExperiment) injectNetworkChaos(ctx context.Context) error {
	config := chaosmesh.NetworkChaosConfig{
		Name:     e.Config.Name,
		Mode:     chaosmesh.AllMode,
		Duration: e.Config.Duration,
		Selector: e.buildPodSelector(),
	}

	switch e.Config.Action {
	case core.ChaosActionNetworkDelay:
		config.Action = chaosmesh.NetworkDelayAction
		config.Delay = &chaosmesh.DelaySpec{
			Latency: "100ms",
			Jitter:  "10ms",
		}
	case core.ChaosActionNetworkPartition:
		config.Action = chaosmesh.NetworkPartitionAction
		// Configure partition target if needed
	}

	networkChaos, err := e.adapter.InjectNetworkChaos(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to inject network chaos: %w", err)
	}
	e.chaosName = networkChaos.Name
	e.chaosKind = "NetworkChaos"
	return nil
}

// injectIOChaos injects I/O-level chaos
func (e *ChaosMeshExperiment) injectIOChaos(ctx context.Context) error {
	config := chaosmesh.IOChaosConfig{
		Name:     e.Config.Name,
		Mode:     chaosmesh.AllMode,
		Duration: e.Config.Duration,
		Selector: e.buildPodSelector(),
		Path:     "/var/lib/postgresql/data",
		Percent:  50,
	}

	switch e.Config.Action {
	case core.ChaosActionIODelay:
		config.Action = chaosmesh.IODelayAction
		config.Delay = "100ms"
		config.Methods = []string{"read", "write"}
	case core.ChaosActionIOError:
		config.Action = chaosmesh.IOFaultAction
		config.Methods = []string{"read", "write"}
	}

	ioChaos, err := e.adapter.InjectIOChaos(ctx, config)
	if err != nil {
		return fmt.Errorf("failed to inject IO chaos: %w", err)
	}
	e.chaosName = ioChaos.Name
	e.chaosKind = "IOChaos"
	return nil
}

// buildPodSelector builds a Chaos Mesh pod selector from our target
func (e *ChaosMeshExperiment) buildPodSelector() chaosmesh.PodSelectorSpec {
	selector := chaosmesh.PodSelectorSpec{
		Namespaces: []string{e.Config.Target.Namespace},
	}

	if e.Config.Target.LabelSelector != nil {
		// For simplicity, create a basic label selector
		// In production, parse the selector properly
		selector.LabelSelectors = make(map[string]string)
	}

	if e.Config.Target.PodName != "" {
		selector.Pods = map[string][]string{
			e.Config.Target.Namespace: {e.Config.Target.PodName},
		}
	}

	return selector
}

// monitorDuringChaos monitors the experiment while chaos is active
func (e *ChaosMeshExperiment) monitorDuringChaos(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Get chaos status
				status, err := e.adapter.GetChaosStatus(ctx, e.chaosKind, e.chaosName)
				if err != nil {
					e.AddEvent("StatusCheckError",
						fmt.Sprintf("Failed to get chaos status: %v", err),
						core.EventSeverityWarning)
					continue
				}

				// Check safety during execution
				if err := e.RunSafetyChecks(ctx); err != nil {
					// Safety check failed, abort experiment
					e.AddEvent("ExperimentAborted",
						fmt.Sprintf("Safety check failed during chaos: %v", err),
						core.EventSeverityCritical)
					_ = e.Cleanup(ctx)
					return
				}

				// Record status
				e.AddEvent("StatusUpdate",
					fmt.Sprintf("Chaos status: %s", status),
					core.EventSeverityInfo)
			}
		}
	}()
}

// ChaosMeshExperimentBuilder helps build Chaos Mesh experiments
type ChaosMeshExperimentBuilder struct {
	config           core.ExperimentConfig
	client           client.Client
	safetyChecks     []core.SafetyCheck
	metricsCollector core.MetricsCollector
}

// NewChaosMeshExperimentBuilder creates a new builder
func NewChaosMeshExperimentBuilder(client client.Client) *ChaosMeshExperimentBuilder {
	return &ChaosMeshExperimentBuilder{
		client:           client,
		safetyChecks:     []core.SafetyCheck{},
		metricsCollector: nil, // Will be set by WithMetricsCollector or default to nil
	}
}

// WithConfig sets the experiment configuration
func (b *ChaosMeshExperimentBuilder) WithConfig(config core.ExperimentConfig) *ChaosMeshExperimentBuilder {
	b.config = config
	return b
}

// WithSafetyCheck adds a safety check
func (b *ChaosMeshExperimentBuilder) WithSafetyCheck(check core.SafetyCheck) *ChaosMeshExperimentBuilder {
	b.safetyChecks = append(b.safetyChecks, check)
	return b
}

// WithMetricsCollector sets a custom metrics collector
func (b *ChaosMeshExperimentBuilder) WithMetricsCollector(collector core.MetricsCollector) *ChaosMeshExperimentBuilder {
	b.metricsCollector = collector
	return b
}

// Build creates the experiment
func (b *ChaosMeshExperimentBuilder) Build() *ChaosMeshExperiment {
	// Add default CNPG safety check if none provided
	if len(b.safetyChecks) == 0 {
		b.safetyChecks = append(b.safetyChecks, &safety.ClusterHealthCheck{
			Namespace:          b.config.Target.Namespace,
			ClusterName:        "test-cluster",
			MinHealthyReplicas: 1,
		})
	}

	return NewChaosMeshExperiment(b.config, b.client, b.safetyChecks, b.metricsCollector)
}
