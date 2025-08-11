# CloudNativePG Chaos Testing Framework POC

This is a proof-of-concept implementation of a chaos testing framework for CloudNativePG, designed to systematically validate the operator's resilience, fault tolerance, and recovery mechanisms.

## Architecture Overview

The chaos testing framework consists of several key components:

### Core Components (`core/`)

- **Types**: Defines interfaces and data structures for experiments, results, and configurations
- **BaseExperiment**: Provides common functionality for all chaos experiments including setup, execution, cleanup, and safety monitoring
- **Interfaces**: `Experiment`, `SafetyCheck`, `MetricsCollector` for extensibility

### Experiments (`experiments/`)

- **PodChaosExperiment**: Implements chaos injection targeting Kubernetes pods
  - Pod deletion/killing
  - Pod failure injection
  - Support for various target selection strategies (labels, specific pods, percentage-based)

### Safety Controller (`safety/`)

- **Controller**: Manages safety checks and emergency abort mechanisms
- **Built-in Checks**:
  - ClusterHealthCheck: Validates cluster health and minimum replicas
  - DataConsistencyCheck: Ensures data consistency and replication
  - RecoveryTimeCheck: Monitors recovery time constraints
- **Emergency Stop**: File-based kill switch for immediate experiment termination

### Metrics Collection (`metrics/`)

- **ClusterMetricsCollector**: Collects CloudNativePG-specific metrics
- **ResilienceMetrics**: Tracks key resilience indicators:
  - Time to Detection (TTD)
  - Time to Recovery (TTR)
  - Data loss metrics
  - Availability metrics
  - Performance impact

## Usage Example

```go
// Configure experiment
config := core.ExperimentConfig{
    Name: "primary-failure",
    Target: core.TargetSelector{
        Namespace: "default",
        LabelSelector: labels.SelectorFromSet(labels.Set{
            "cnpg.io/cluster": "my-cluster",
            "cnpg.io/instanceRole": "primary",
        }),
    },
    Action: core.ChaosActionPodKill,
    Duration: 30 * time.Second,
}

// Set up safety controller
safetyConfig := safety.SafetyConfig{
    MinHealthyReplicas: 2,
    MaxRecoveryTime: 2 * time.Minute,
    ClusterNamespace: "default",
    ClusterName: "my-cluster",
}
safetyController := safety.NewController(client, safetyConfig)
safetyController.Start(ctx)

// Create and run experiment
experiment := experiments.NewPodChaosExperiment(config, client)
experiment.AddMetricsCollector(metricsCollector)

err := experiment.Setup(ctx)
err = experiment.Run(ctx)
err = experiment.Cleanup(ctx)

// Analyze results
result := experiment.GetResult()
fmt.Printf("Status: %s\n", result.Status)
fmt.Printf("TTR: %v\n", result.Metrics["resilience.ttr"])
```

## Integration with E2E Tests

The framework integrates with CloudNativePG's existing E2E test suite using Ginkgo/Gomega. See `tests/e2e/chaos_primary_failure_test.go` for a complete example.

### Running Chaos Tests

```bash
# Run all chaos tests
cd tests
go test -v ./e2e -ginkgo.focus="Chaos" -timeout=30m

# Run specific chaos experiment
go test -v ./chaos/experiments -run TestPodSelection
```

## Test Coverage

### Unit Tests
- Core experiment lifecycle (`core/experiment_test.go`)
- Safety controller and checks (`safety/controller_test.go`)
- Mock implementations for testing

### Integration Tests
- Pod selection strategies (`experiments/primary_failure_test.go`)
- Primary failure scenarios
- Data consistency validation

## Safety Mechanisms

1. **Pre-flight Checks**: Validate cluster health before starting
2. **Continuous Monitoring**: Safety checks run every 5 seconds during experiments
3. **Automatic Abort**: Critical failures trigger immediate experiment termination
4. **Emergency Stop**: Manual kill switch via file system
5. **Blast Radius Control**: Limit the number/percentage of affected resources

## Extending the Framework

### Adding New Chaos Types

1. Implement the `Experiment` interface:
```go
type NetworkChaosExperiment struct {
    *core.BaseExperiment
    // Custom fields
}

func (e *NetworkChaosExperiment) Run(ctx context.Context) error {
    // Implement chaos injection
}
```

2. Add corresponding safety checks if needed

3. Create metrics collectors for specific measurements

### Adding Safety Checks

Implement the `SafetyCheck` interface:
```go
type CustomCheck struct{}

func (c *CustomCheck) Name() string { return "CustomCheck" }
func (c *CustomCheck) IsCritical() bool { return true }
func (c *CustomCheck) Check(ctx context.Context, client client.Client) (bool, string, error) {
    // Implement validation logic
}
```

## Future Enhancements

1. **Chaos Mesh Integration**: Deploy and manage Chaos Mesh CRDs
2. **Additional Chaos Types**:
   - Network chaos (latency, partition, packet loss)
   - Storage chaos (I/O delays, disk pressure)
   - Resource chaos (CPU/memory stress)
3. **Automated Chaos Testing**: Scheduled chaos experiments in CI/CD
4. **Chaos Dashboard**: Grafana dashboards for chaos metrics
5. **Game Days**: Production-ready chaos scenarios

## Dependencies

The POC uses CloudNativePG's existing dependencies plus:
- `github.com/stretchr/testify` for unit testing mocks
- Existing Ginkgo/Gomega for integration tests
- CloudNativePG API types and utilities

## Contributing

When adding new chaos experiments:
1. Follow the existing patterns in `experiments/`
2. Add comprehensive unit tests
3. Include safety checks for critical operations
4. Document metrics collected
5. Add integration tests using Ginkgo

## License

Licensed under the Apache License, Version 2.0