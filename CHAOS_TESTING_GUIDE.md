# 🚀 CloudNativePG Chaos Testing Framework - User Guide

## Table of Contents
1. [Introduction](#introduction)
2. [Quick Start](#quick-start)
3. [Framework Architecture](#framework-architecture)
4. [Basic Usage Examples](#basic-usage-examples)
5. [Advanced Scenarios](#advanced-scenarios)
6. [Running Tests](#running-tests)
7. [Safety Mechanisms](#safety-mechanisms)
8. [Troubleshooting](#troubleshooting)

---

## Introduction

### What is Chaos Testing?
Chaos testing is like a fire drill for your database. We intentionally cause problems (kill pods, slow networks, fail disks) to ensure CloudNativePG can handle real-world failures gracefully.

### Why Use This Framework?
- **Safety First**: Built-in mechanisms prevent data loss
- **PostgreSQL-Specific**: Designed for database failure scenarios
- **Easy to Use**: Simple API for complex chaos experiments
- **Production-Ready**: Comprehensive testing with metrics

---

## Quick Start

### Prerequisites

1. **Kubernetes Cluster** (local or remote)
```bash
# For local testing, use kind
kind create cluster --name cnpg-chaos
```

2. **CloudNativePG Operator Installed**
```bash
kubectl apply -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.22/releases/cnpg-1.22.0.yaml
```

3. **Chaos Mesh Installation** (optional but recommended)
```bash
# Install Chaos Mesh
curl -sSL https://mirrors.chaos-mesh.org/v2.6.2/install.sh | bash

# Verify installation
kubectl get pods -n chaos-mesh
```

### Your First Chaos Test

Here's a simple example that kills the primary PostgreSQL pod:

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/cloudnative-pg/cloudnative-pg/tests/chaos/core"
    "github.com/cloudnative-pg/cloudnative-pg/tests/chaos/experiments"
    "github.com/cloudnative-pg/cloudnative-pg/tests/chaos/safety"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
    // 1. Setup Kubernetes client
    cfg, _ := config.GetConfig()
    k8sClient, _ := client.New(cfg, client.Options{})
    
    // 2. Define what chaos to inject
    experiment := experiments.NewPodChaosExperiment(
        core.ExperimentConfig{
            Name:     "kill-primary",
            Action:   core.ChaosActionPodKill,
            Duration: 30 * time.Second,
            Target: core.TargetSelector{
                Namespace: "default",
                PodName:   "my-cluster-1", // Primary pod
            },
        },
        k8sClient,
    )
    
    // 3. Add safety checks (prevents data loss)
    experiment.AddSafetyCheck(&safety.ClusterHealthCheck{
        Namespace:          "default",
        ClusterName:        "my-cluster",
        MinHealthyReplicas: 2, // At least 2 replicas must be healthy
    })
    
    // 4. Run the experiment
    ctx := context.Background()
    if err := experiment.Run(ctx); err != nil {
        fmt.Printf("Experiment failed: %v\n", err)
        return
    }
    
    fmt.Println("✅ Chaos experiment completed successfully!")
}
```

---

## Framework Architecture

### Core Components

```
┌─────────────────────────────────────────────────────────┐
│                    Chaos Framework                       │
├───────────────────────────────────────────────────────── │
│                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │     Core     │  │  Experiments │  │    Safety    │ │
│  │              │  │              │  │              │ │
│  │ - Config     │  │ - Pod Chaos  │  │ - Health     │ │
│  │ - Base Exp   │  │ - Network    │  │   Checks     │ │
│  │ - Interfaces │  │ - IO Chaos   │  │ - Abort      │ │
│  └──────────────┘  └──────────────┘  └──────────────┘ │
│                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐ │
│  │   Metrics    │  │  Chaos Mesh  │  │     E2E      │ │
│  │              │  │   Adapter    │  │    Tests     │ │
│  │ - TTD/TTR    │  │              │  │              │ │
│  │ - Prometheus │  │ - CRD Types  │  │ - Failover   │ │
│  │              │  │ - Integration│  │ - Network    │ │
│  └──────────────┘  └──────────────┘  └──────────────┘ │
└─────────────────────────────────────────────────────────┘
```

### Key Interfaces

1. **Experiment Interface**
```go
type Experiment interface {
    Name() string
    Validate() error
    Setup(ctx context.Context) error
    Run(ctx context.Context) error
    Cleanup(ctx context.Context) error
    GetResult() *ExperimentResult
}
```

2. **Safety Check Interface**
```go
type SafetyCheck interface {
    Name() string
    Check(ctx context.Context, client client.Client) (bool, string, error)
    IsCritical() bool  // If true, failure aborts experiment
}
```

3. **Metrics Collector Interface**
```go
type MetricsCollector interface {
    Start(ctx context.Context) error
    Stop() error
    Collect() (map[string]interface{}, error)
}
```

---

## Basic Usage Examples

### Example 1: Simple Pod Failure

Kill a specific PostgreSQL pod and verify automatic recovery:

```go
func testPodFailure() {
    config := core.ExperimentConfig{
        Name:        "test-pod-failure",
        Description: "Kill primary pod to test failover",
        Action:      core.ChaosActionPodKill,
        Duration:    10 * time.Second,
        Target: core.TargetSelector{
            Namespace: "database",
            PodName:   "pg-cluster-1",
        },
    }
    
    experiment := experiments.NewPodChaosExperiment(config, k8sClient)
    
    // Run with safety checks
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
    defer cancel()
    
    if err := experiment.Run(ctx); err != nil {
        log.Fatal(err)
    }
    
    // Check results
    result := experiment.GetResult()
    fmt.Printf("Experiment Status: %s\n", result.Status)
    fmt.Printf("Duration: %v\n", result.Duration)
}
```

### Example 2: Network Delay

Simulate slow network between PostgreSQL nodes:

```go
func testNetworkDelay() {
    config := core.ExperimentConfig{
        Name:     "network-latency-test",
        Action:   core.ChaosActionNetworkDelay,
        Duration: 60 * time.Second,
        Target: core.TargetSelector{
            Namespace: "database",
            LabelSelector: labels.SelectorFromSet(labels.Set{
                "cnpg.io/cluster": "my-cluster",
                "role": "replica",
            }),
        },
        Parameters: map[string]interface{}{
            "delay":      "200ms",
            "jitter":     "50ms",
            "percentage": 100,
        },
    }
    
    experiment := experiments.NewNetworkChaosExperiment(config, k8sClient)
    experiment.Run(context.Background())
}
```

### Example 3: Disk I/O Stress

Test PostgreSQL behavior under slow disk conditions:

```go
func testDiskIOStress() {
    config := core.ExperimentConfig{
        Name:     "io-stress-test",
        Action:   core.ChaosActionIODelay,
        Duration: 2 * time.Minute,
        Target: core.TargetSelector{
            Namespace: "database",
            PodName:   "pg-cluster-1",
        },
        Parameters: map[string]interface{}{
            "path":    "/var/lib/postgresql/data",
            "delay":   "100ms",
            "percent": 50, // Affect 50% of I/O operations
        },
    }
    
    experiment := experiments.NewIOChaosExperiment(config, k8sClient)
    experiment.Run(context.Background())
}
```

---

## Advanced Scenarios

### Using Chaos Mesh Integration

For more sophisticated chaos experiments, use the Chaos Mesh integration:

```go
func advancedChaosWithMesh() {
    // Create builder for Chaos Mesh experiment
    builder := experiments.NewChaosMeshExperimentBuilder(k8sClient)
    
    // Configure the experiment
    experiment := builder.
        WithConfig(core.ExperimentConfig{
            Name:     "advanced-chaos",
            Action:   core.ChaosActionPodKill,
            Duration: 30 * time.Second,
            Target: core.TargetSelector{
                Namespace: "database",
                LabelSelector: labels.SelectorFromSet(labels.Set{
                    "cnpg.io/instanceRole": "primary",
                }),
            },
        }).
        WithSafetyCheck(&safety.ClusterHealthCheck{
            Namespace:          "database",
            ClusterName:        "pg-cluster",
            MinHealthyReplicas: 2,
        }).
        WithMetricsCollector(metrics.NewPrometheusCollector()).
        Build()
    
    // Run the experiment
    if err := experiment.Run(context.Background()); err != nil {
        log.Printf("Experiment failed: %v", err)
    }
}
```

### Custom Safety Checks

Create your own safety checks to protect critical operations:

```go
type DataConsistencyCheck struct {
    connectionString string
}

func (d *DataConsistencyCheck) Name() string {
    return "DataConsistency"
}

func (d *DataConsistencyCheck) Check(ctx context.Context, client client.Client) (bool, string, error) {
    // Connect to PostgreSQL
    db, err := sql.Open("postgres", d.connectionString)
    if err != nil {
        return false, "Cannot connect to database", err
    }
    defer db.Close()
    
    // Check data consistency
    var count int
    err = db.QueryRow("SELECT COUNT(*) FROM critical_table WHERE status = 'pending'").Scan(&count)
    if err != nil {
        return false, "Query failed", err
    }
    
    if count > 0 {
        return false, fmt.Sprintf("%d pending transactions found", count), nil
    }
    
    return true, "No pending transactions", nil
}

func (d *DataConsistencyCheck) IsCritical() bool {
    return true // Abort if check fails
}
```

### Scheduled Chaos Testing

Run chaos experiments on a schedule:

```go
func scheduledChaos() {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            // Run random chaos experiment
            experiments := []core.ChaosAction{
                core.ChaosActionPodKill,
                core.ChaosActionNetworkDelay,
                core.ChaosActionIODelay,
            }
            
            action := experiments[rand.Intn(len(experiments))]
            
            config := core.ExperimentConfig{
                Name:     fmt.Sprintf("scheduled-chaos-%d", time.Now().Unix()),
                Action:   action,
                Duration: 30 * time.Second,
                Target: core.TargetSelector{
                    Namespace: "database",
                    Percentage: 30, // Affect 30% of pods
                },
            }
            
            experiment := experiments.NewPodChaosExperiment(config, k8sClient)
            go experiment.Run(context.Background())
        }
    }
}
```

---

## Running Tests

### Unit Tests

Run the unit tests for the chaos framework:

```bash
# Run all chaos tests
go test ./tests/chaos/... -v

# Run with coverage
go test ./tests/chaos/... -cover

# Run specific package
go test ./tests/chaos/core -v
```

### E2E Tests

Run end-to-end chaos tests:

```bash
# Run all E2E chaos tests
go test ./tests/e2e -run "Chaos" -v

# Run specific chaos test
go test ./tests/e2e -run "TestChaosMeshFailover" -v

# With custom timeout
go test ./tests/e2e -run "Chaos" -timeout 30m -v
```

### In CI/CD Pipeline

Add to your GitHub Actions workflow:

```yaml
name: Chaos Testing
on:
  schedule:
    - cron: '0 2 * * *'  # Run daily at 2 AM
  workflow_dispatch:

jobs:
  chaos-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Setup Kind cluster
        run: |
          kind create cluster --config=tests/e2e/kind-config.yaml
          
      - name: Install CloudNativePG
        run: |
          kubectl apply -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/main/releases/cnpg-1.22.0.yaml
          
      - name: Install Chaos Mesh
        run: |
          curl -sSL https://mirrors.chaos-mesh.org/v2.6.2/install.sh | bash
          
      - name: Run Chaos Tests
        run: |
          go test ./tests/e2e -run "Chaos" -v -timeout 30m
```

---

## Safety Mechanisms

### Built-in Safety Features

1. **Automatic Abort on Critical Failures**
```go
// If any critical safety check fails, experiment stops immediately
if safetyCheck.IsCritical() && !passed {
    experiment.Abort()
}
```

2. **Minimum Replica Protection**
```go
// Ensures minimum replicas are always healthy
safetyCheck := &safety.ClusterHealthCheck{
    MinHealthyReplicas: 2,  // Never go below 2 healthy replicas
}
```

3. **Blast Radius Control**
```go
// Limit chaos to specific percentage of pods
config.Target.Percentage = 30  // Only affect 30% of pods
```

4. **Timeout Protection**
```go
// Experiments auto-cleanup after timeout
ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
defer cancel()
```

### Monitoring During Chaos

The framework continuously monitors during experiments:

```go
// Metrics collected during chaos
- Time to Detect failure (TTD)
- Time to Recovery (TTR)  
- Data consistency status
- Replication lag
- Connection pool status
```

---

## Troubleshooting

### Common Issues and Solutions

#### 1. Experiment Fails to Start
```bash
Error: "failed to create chaos: insufficient permissions"
```
**Solution**: Ensure RBAC permissions:
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: chaos-testing
rules:
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "delete"]
- apiGroups: ["chaos-mesh.org"]
  resources: ["*"]
  verbs: ["*"]
```

#### 2. Safety Check Keeps Failing
```bash
Error: "critical safety check ClusterHealth failed: insufficient replicas"
```
**Solution**: 
- Ensure cluster has enough replicas before testing
- Adjust `MinHealthyReplicas` in safety check
- Wait for cluster to be fully ready

#### 3. Chaos Not Affecting Pods
```bash
Warning: "0 pods selected for chaos injection"
```
**Solution**: 
- Check label selectors match your pods
- Verify namespace is correct
- Use `kubectl get pods -l <your-labels>` to test selectors

#### 4. Cleanup Failed
```bash
Error: "failed to cleanup chaos resources"
```
**Solution**:
```bash
# Manual cleanup
kubectl delete podchaos --all -n <namespace>
kubectl delete networkchaos --all -n <namespace>
kubectl delete iochaos --all -n <namespace>
```

### Debug Mode

Enable detailed logging:

```go
// Set log level
experiment.SetLogLevel(core.LogLevelDebug)

// Or use environment variable
export CHAOS_LOG_LEVEL=debug
```

### Viewing Experiment Results

```go
result := experiment.GetResult()

// Print detailed results
fmt.Printf("Status: %s\n", result.Status)
fmt.Printf("Duration: %v\n", result.Duration)
fmt.Printf("Events: %d\n", len(result.Events))

for _, event := range result.Events {
    fmt.Printf("[%s] %s: %s\n", 
        event.Timestamp.Format(time.RFC3339),
        event.Type,
        event.Message)
}

// Check if aborted by safety
if result.SafetyAborted {
    fmt.Printf("⚠️ Aborted: %s\n", result.AbortReason)
}
```

---

## Best Practices

### DO's ✅

1. **Always use safety checks** - Prevent data loss
2. **Start small** - Test in dev/staging first
3. **Monitor metrics** - Track TTD and TTR
4. **Document experiments** - Keep records of what was tested
5. **Clean up resources** - Ensure experiments clean up properly

### DON'Ts ❌

1. **Don't run in production without approval**
2. **Don't disable safety checks**
3. **Don't run multiple experiments simultaneously** (unless coordinated)
4. **Don't ignore failed safety checks**
5. **Don't forget to set timeouts**

---

## Examples Repository

Find more examples in the `/tests/chaos/examples/` directory:

- `simple_pod_kill.go` - Basic pod failure
- `network_partition.go` - Network split scenarios  
- `io_stress.go` - Disk I/O chaos
- `cascading_failure.go` - Multiple failures
- `recovery_time.go` - Measure recovery metrics

---

## Support and Contributing

### Getting Help
- Check the [troubleshooting](#troubleshooting) section
- Open an issue on GitHub
- Join the CloudNativePG Slack channel

### Contributing
We welcome contributions! To add new chaos experiments:

1. Implement the `Experiment` interface
2. Add safety checks
3. Write unit tests (aim for >80% coverage)
4. Add E2E tests
5. Update documentation

---

## Quick Reference Card

```bash
# Kill primary pod
chaos run --type pod-kill --target primary --duration 30s

# Network delay to replicas  
chaos run --type network-delay --target replicas --delay 100ms

# I/O stress on data directory
chaos run --type io-stress --path /var/lib/postgresql/data --percent 50

# List running experiments
chaos list

# Stop all experiments
chaos stop --all

# View experiment details
chaos describe <experiment-name>
```

---

## Conclusion

The CloudNativePG Chaos Testing Framework provides a safe, controlled way to test database resilience. Start with simple experiments and gradually increase complexity as you gain confidence.

Remember: **The goal is not to break things, but to learn how the system behaves under stress and improve its resilience.**

Happy chaos testing! 🎯