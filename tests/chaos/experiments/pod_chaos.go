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
	"math/rand"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/cloudnative-pg/cloudnative-pg/tests/chaos/core"
)

// PodChaosExperiment implements chaos experiments targeting pods
type PodChaosExperiment struct {
	*core.BaseExperiment
	targetPods    []corev1.Pod
	affectedPods  []corev1.Pod
	originalState map[string]interface{}
}

// NewPodChaosExperiment creates a new pod chaos experiment
func NewPodChaosExperiment(config core.ExperimentConfig, k8sClient client.Client) *PodChaosExperiment {
	return &PodChaosExperiment{
		BaseExperiment: core.NewBaseExperiment(config, k8sClient),
		targetPods:     []corev1.Pod{},
		affectedPods:   []corev1.Pod{},
		originalState:  make(map[string]interface{}),
	}
}

// Setup prepares the pod chaos experiment
func (e *PodChaosExperiment) Setup(ctx context.Context) error {
	if err := e.BaseExperiment.Setup(ctx); err != nil {
		return err
	}

	// Find target pods
	if err := e.selectTargetPods(ctx); err != nil {
		e.SetStatus(core.ExperimentStatusFailed)
		return fmt.Errorf("failed to select target pods: %w", err)
	}

	if len(e.targetPods) == 0 {
		e.SetStatus(core.ExperimentStatusFailed)
		return fmt.Errorf("no pods matched the target selector")
	}

	e.AddEvent("Setup", fmt.Sprintf("Found %d target pods", len(e.targetPods)), core.EventSeverityInfo)

	// Store original state for recovery
	for _, pod := range e.targetPods {
		e.originalState[pod.Name] = map[string]interface{}{
			"status": pod.Status.Phase,
			"ready":  isPodReady(&pod),
		}
	}

	return nil
}

// Run executes the pod chaos injection
func (e *PodChaosExperiment) Run(ctx context.Context) error {
	e.SetStatus(core.ExperimentStatusRunning)
	e.AddEvent("Execution", fmt.Sprintf("Starting %s chaos injection", e.Config.Action), core.EventSeverityInfo)

	// Start safety monitoring in background
	go e.MonitorSafety(ctx, 5*time.Second)

	switch e.Config.Action {
	case core.ChaosActionPodKill:
		return e.runPodKill(ctx)
	case core.ChaosActionPodFailure:
		return e.runPodFailure(ctx)
	default:
		return fmt.Errorf("unsupported pod chaos action: %s", e.Config.Action)
	}
}

// Cleanup restores the environment after the experiment
func (e *PodChaosExperiment) Cleanup(ctx context.Context) error {
	e.AddEvent("Cleanup", "Starting pod recovery", core.EventSeverityInfo)

	// Verify pods have recovered
	for _, pod := range e.affectedPods {
		if err := e.waitForPodRecovery(ctx, pod.Namespace, pod.Name); err != nil {
			e.AddEvent("Cleanup", fmt.Sprintf("Pod %s recovery failed: %v", pod.Name, err), core.EventSeverityWarning)
		} else {
			e.AddEvent("Cleanup", fmt.Sprintf("Pod %s recovered", pod.Name), core.EventSeverityInfo)
		}
	}

	return e.BaseExperiment.Cleanup(ctx)
}

// selectTargetPods finds pods matching the target selector
func (e *PodChaosExperiment) selectTargetPods(ctx context.Context) error {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(e.Config.Target.Namespace),
	}

	if e.Config.Target.LabelSelector != nil {
		listOpts = append(listOpts, client.MatchingLabelsSelector{Selector: e.Config.Target.LabelSelector})
	}

	if err := e.Client.List(ctx, podList, listOpts...); err != nil {
		return err
	}

	// Filter by specific pod name if provided
	if e.Config.Target.PodName != "" {
		for _, pod := range podList.Items {
			if pod.Name == e.Config.Target.PodName {
				e.targetPods = []corev1.Pod{pod}
				return nil
			}
		}
		return fmt.Errorf("pod %s not found", e.Config.Target.PodName)
	}

	// Filter by node name if provided
	if e.Config.Target.NodeName != "" {
		var filteredPods []corev1.Pod
		for _, pod := range podList.Items {
			if pod.Spec.NodeName == e.Config.Target.NodeName {
				filteredPods = append(filteredPods, pod)
			}
		}
		e.targetPods = filteredPods
	} else {
		e.targetPods = podList.Items
	}

	// Apply count or percentage limits
	e.targetPods = e.applyTargetLimits(e.targetPods)

	return nil
}

// applyTargetLimits applies count or percentage limits to target selection
func (e *PodChaosExperiment) applyTargetLimits(pods []corev1.Pod) []corev1.Pod {
	if e.Config.Target.Count > 0 && e.Config.Target.Count < len(pods) {
		// Randomly select Count pods
		rand.Shuffle(len(pods), func(i, j int) {
			pods[i], pods[j] = pods[j], pods[i]
		})
		return pods[:e.Config.Target.Count]
	}

	if e.Config.Target.Percentage > 0 && e.Config.Target.Percentage < 100 {
		count := (len(pods) * e.Config.Target.Percentage) / 100
		if count == 0 {
			count = 1
		}
		rand.Shuffle(len(pods), func(i, j int) {
			pods[i], pods[j] = pods[j], pods[i]
		})
		return pods[:count]
	}

	return pods
}

// runPodKill implements the pod-kill chaos action
func (e *PodChaosExperiment) runPodKill(ctx context.Context) error {
	for _, pod := range e.targetPods {
		e.AddEvent("PodKill", fmt.Sprintf("Deleting pod %s", pod.Name), core.EventSeverityInfo)

		// Record as affected
		e.affectedPods = append(e.affectedPods, pod)

		// Delete the pod
		deletePolicy := metav1.DeletePropagationForeground
		deleteOpts := client.DeleteOptions{
			PropagationPolicy: &deletePolicy,
		}

		if err := e.Client.Delete(ctx, &pod, &deleteOpts); err != nil {
			e.AddEvent("PodKill", fmt.Sprintf("Failed to delete pod %s: %v", pod.Name, err), core.EventSeverityError)
			return fmt.Errorf("failed to delete pod %s: %w", pod.Name, err)
		}

		e.AddEvent("PodKill", fmt.Sprintf("Successfully deleted pod %s", pod.Name), core.EventSeverityInfo)

		// Record deletion time in metrics
		e.Result.Metrics[fmt.Sprintf("pod.%s.deletionTime", pod.Name)] = time.Now().Unix()
	}

	// Wait for the specified duration
	e.AddEvent("Duration", fmt.Sprintf("Waiting for %v", e.Config.Duration), core.EventSeverityInfo)

	select {
	case <-time.After(e.Config.Duration):
		e.AddEvent("Duration", "Chaos duration completed", core.EventSeverityInfo)
	case <-ctx.Done():
		e.AddEvent("Duration", "Context cancelled", core.EventSeverityWarning)
		return ctx.Err()
	}

	return nil
}

// runPodFailure implements the pod-failure chaos action
func (e *PodChaosExperiment) runPodFailure(ctx context.Context) error {
	for _, pod := range e.targetPods {
		e.AddEvent("PodFailure", fmt.Sprintf("Injecting failure into pod %s", pod.Name), core.EventSeverityInfo)

		// Record as affected
		e.affectedPods = append(e.affectedPods, pod)

		// Execute failure injection command in the pod
		failureCmd := e.getFailureCommand()
		if err := e.executePodCommand(ctx, &pod, failureCmd); err != nil {
			e.AddEvent("PodFailure", fmt.Sprintf("Failed to inject failure into pod %s: %v", pod.Name, err), core.EventSeverityError)
			// Continue with other pods
		} else {
			e.AddEvent("PodFailure", fmt.Sprintf("Successfully injected failure into pod %s", pod.Name), core.EventSeverityInfo)
		}
	}

	// Wait for the specified duration
	select {
	case <-time.After(e.Config.Duration):
		e.AddEvent("Duration", "Chaos duration completed", core.EventSeverityInfo)
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// getFailureCommand returns the command to inject based on parameters
func (e *PodChaosExperiment) getFailureCommand() []string {
	// Default to a simple exit command
	cmd := []string{"sh", "-c", "exit 1"}

	// Check for custom command in parameters
	if cmdParam, ok := e.Config.Parameters["command"]; ok {
		if cmdStr, ok := cmdParam.(string); ok {
			cmd = []string{"sh", "-c", cmdStr}
		}
	}

	return cmd
}

// executePodCommand executes a command in a pod (simplified for POC)
func (e *PodChaosExperiment) executePodCommand(ctx context.Context, pod *corev1.Pod, command []string) error {
	// In a real implementation, this would use the Kubernetes exec API
	// For POC, we'll simulate the execution
	e.AddEvent("Execute", fmt.Sprintf("Would execute command in pod %s: %v", pod.Name, command), core.EventSeverityInfo)
	return nil
}

// waitForPodRecovery waits for a pod to recover after chaos injection
func (e *PodChaosExperiment) waitForPodRecovery(ctx context.Context, namespace, name string) error {
	timeout := time.After(5 * time.Minute)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for pod %s to recover", name)
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			pod := &corev1.Pod{}
			key := client.ObjectKey{Namespace: namespace, Name: name}

			// Check if pod exists and is ready
			if err := e.Client.Get(ctx, key, pod); err != nil {
				// Pod might be recreating
				continue
			}

			if isPodReady(pod) {
				// Record recovery time
				e.Result.Metrics[fmt.Sprintf("pod.%s.recoveryTime", name)] = time.Now().Unix()
				return nil
			}
		}
	}
}

// isPodReady checks if a pod is in ready state
func isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}

	return false
}
