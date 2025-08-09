# Chaos Framework Comparison for CloudNativePG

## Executive Summary

After detailed analysis, **Chaos Mesh** is the recommended choice for CloudNativePG's chaos testing framework, scoring 8.5/10 vs LitmusChaos's 7/10 for this specific use case.

## Detailed Comparison

### 1. Architecture & Design Philosophy

#### Chaos Mesh
- **Architecture**: Controller-based with CRD-centric design
- **Deployment**: Single operator managing multiple experiment types
- **Resource Usage**: ~200MB memory for controller
- **Complexity**: Medium - clean separation of concerns

```yaml
# Chaos Mesh experiment example
apiVersion: chaos-mesh.org/v1alpha1
kind: PodChaos
metadata:
  name: cnpg-primary-kill
spec:
  selector:
    labelSelectors:
      cnpg.io/cluster: my-cluster
      cnpg.io/instanceRole: primary
  mode: one
  action: pod-kill
  duration: 30s
```

#### LitmusChaos
- **Architecture**: Hub-and-spoke with experiment library
- **Deployment**: Operator + ChaosCenter + experiment pods
- **Resource Usage**: ~500MB for full stack
- **Complexity**: Higher - more components to manage

```yaml
# LitmusChaos experiment example
apiVersion: litmuschaos.io/v1alpha1
kind: ChaosEngine
metadata:
  name: cnpg-chaos
spec:
  engineState: active
  appinfo:
    appns: default
    applabel: cnpg.io/cluster=my-cluster
  experiments:
    - name: pod-delete
      spec:
        components:
          env:
            - name: TOTAL_CHAOS_DURATION
              value: '30'
```

### 2. PostgreSQL-Specific Capabilities

#### Chaos Mesh - Better for CNPG
✅ **Native support for StatefulSets** - Critical for CNPG
✅ **JVM/IO chaos** - Can simulate PostgreSQL-specific issues
✅ **Time chaos** - Test time-sensitive operations (backups, WAL archiving)
✅ **Kernel chaos** - Simulate OS-level failures affecting PostgreSQL

```go
// Example: PostgreSQL-specific chaos
type PostgreSQLChaos struct {
    // Simulate WAL corruption
    WalCorruption bool
    // Simulate replication lag
    ReplicationDelay time.Duration
    // Simulate connection exhaustion
    ConnectionChaos bool
}
```

#### LitmusChaos
✅ Generic database chaos experiments
❌ No PostgreSQL-specific experiments out-of-box
❌ Limited StatefulSet targeting
⚠️ Requires custom experiments for PostgreSQL scenarios

### 3. CNPG Integration Suitability

#### Chaos Mesh - Winner for CNPG

**Advantages:**
1. **Direct pod selection via labels** - Perfect for CNPG's labeling scheme
2. **Minimal overhead** - Important for database workloads
3. **Precise timing control** - Critical for failover testing
4. **Network partition support** - Test split-brain scenarios

```yaml
# CNPG-optimized selector
selector:
  namespaces:
    - cnpg-system
  labelSelectors:
    cnpg.io/cluster: my-cluster
    cnpg.io/instanceRole: primary
  podPhaseSelectors:
    - Running
```

#### LitmusChaos
- More complex selector configuration
- Higher resource overhead per experiment
- Less precise timing control
- Generic approach may miss CNPG-specific edge cases

### 4. CI/CD Integration

#### Chaos Mesh - Cleaner Integration
```yaml
# GitHub Actions integration
- name: Install Chaos Mesh
  run: |
    helm install chaos-mesh chaos-mesh/chaos-mesh \
      --namespace=chaos-testing \
      --set dashboard.create=false  # No UI needed in CI
      
- name: Run Chaos Test
  run: |
    kubectl apply -f chaos-experiments/
    kubectl wait --for=condition=complete \
      experiment/cnpg-failover --timeout=5m
```

#### LitmusChaos
```yaml
# More complex setup required
- name: Install LitmusChaos
  run: |
    kubectl apply -f litmus-operator.yaml
    kubectl apply -f litmus-experiments.yaml
    helm install litmus-portal ...  # Optional but recommended
```

### 5. Observability & Metrics

#### Chaos Mesh
- **Prometheus metrics** built-in
- **Events** recorded in Kubernetes
- **Direct integration** with Grafana
- **Experiment status** via CRD status

```prometheus
# Example metrics
chaos_mesh_experiment_duration_seconds
chaos_mesh_experiment_status
chaos_mesh_targets_count
```

#### LitmusChaos
- **ChaosCenter** provides dashboard
- **Prometheus exporter** available
- **More detailed** experiment analytics
- **Better visualization** out-of-box

### 6. Specific Test Scenarios for CNPG

| Scenario | Chaos Mesh | LitmusChaos | Winner |
|----------|------------|-------------|---------|
| Primary pod failure | ✅ Native | ✅ Native | Tie |
| Network partition | ✅ Excellent | ✅ Good | Chaos Mesh |
| Disk I/O delays | ✅ IOChaos | ✅ Generic | Chaos Mesh |
| Time skew | ✅ TimeChaos | ❌ Limited | Chaos Mesh |
| CPU/Memory stress | ✅ StressChaos | ✅ Good | Tie |
| PVC failures | ✅ Good | ⚠️ Basic | Chaos Mesh |
| Kernel panics | ✅ KernelChaos | ❌ No | Chaos Mesh |
| DNS failures | ✅ DNSChaos | ✅ Good | Tie |
| HTTP failures | ✅ HTTPChaos | ✅ Better | LitmusChaos |

### 7. Learning Curve & Documentation

#### Chaos Mesh
- **Documentation**: Excellent, clear examples
- **Learning curve**: 2-3 days for basic, 1 week for advanced
- **Community**: Growing, responsive
- **CNPG-specific guides**: Would need to create

#### LitmusChaos
- **Documentation**: Comprehensive but complex
- **Learning curve**: 3-5 days for basic, 2 weeks for advanced
- **Community**: Larger, more mature
- **CNPG-specific guides**: None available

### 8. Production Readiness

#### Chaos Mesh
- **Maturity**: CNCF Incubating project
- **Adoption**: Used by PingCAP (TiDB), Dailymotion
- **Stability**: v2.6+ very stable
- **Security**: Good RBAC, admission webhooks

#### LitmusChaos
- **Maturity**: CNCF Incubating project
- **Adoption**: Wider adoption, more case studies
- **Stability**: v3.0+ stable
- **Security**: Comprehensive security model

### 9. Resource Requirements

#### Chaos Mesh (Lower - Better for CNPG)
```yaml
resources:
  controller:
    memory: 200Mi
    cpu: 100m
  daemon:
    memory: 100Mi
    cpu: 50m
  # Total: ~300Mi memory per node
```

#### LitmusChaos (Higher)
```yaml
resources:
  operator:
    memory: 300Mi
    cpu: 125m
  chaos-runner:
    memory: 150Mi
    cpu: 50m
  experiments:
    memory: 100-500Mi each
  # Total: ~600Mi+ memory
```

### 10. Implementation Complexity for CNPG

#### Chaos Mesh - Simpler
```go
// Simple integration with our POC
type ChaosMeshAdapter struct {
    client client.Client
}

func (c *ChaosMeshAdapter) InjectChaos(exp core.ExperimentConfig) error {
    podChaos := &v1alpha1.PodChaos{
        ObjectMeta: metav1.ObjectMeta{
            Name: exp.Name,
            Namespace: exp.Target.Namespace,
        },
        Spec: v1alpha1.PodChaosSpec{
            Action: mapAction(exp.Action),
            Mode: v1alpha1.OneMode,
            Selector: v1alpha1.PodSelectorSpec{
                LabelSelectors: exp.Target.LabelSelector.MatchLabels,
            },
            Duration: &exp.Duration,
        },
    }
    return c.client.Create(context.TODO(), podChaos)
}
```

#### LitmusChaos - More Complex
```go
// Requires more setup
type LitmusAdapter struct {
    client client.Client
}

func (l *LitmusAdapter) InjectChaos(exp core.ExperimentConfig) error {
    // Need to create ChaosEngine
    // Need to configure experiment
    // Need to handle experiment pods
    // More boilerplate required
}
```

## Scoring Matrix

| Criteria | Weight | Chaos Mesh | LitmusChaos |
|----------|--------|------------|-------------|
| CNPG Integration | 25% | 9/10 | 7/10 |
| PostgreSQL Features | 20% | 9/10 | 6/10 |
| Ease of Use | 15% | 8/10 | 7/10 |
| Resource Efficiency | 15% | 9/10 | 6/10 |
| CI/CD Integration | 10% | 8/10 | 7/10 |
| Observability | 10% | 7/10 | 9/10 |
| Community/Docs | 5% | 8/10 | 9/10 |
| **Total Score** | | **8.5/10** | **7.0/10** |

## Recommendation: Chaos Mesh

### Why Chaos Mesh for CNPG:

1. **Better StatefulSet Support** - Critical for CNPG's architecture
2. **Lower Resource Overhead** - Important for database workloads
3. **Simpler Architecture** - Easier to integrate with existing POC
4. **PostgreSQL-Relevant Chaos Types** - IOChaos, TimeChaos particularly useful
5. **Cleaner CRD Model** - Aligns with CNPG's CRD-based approach
6. **Precise Targeting** - Better label selector support for CNPG pods

### Implementation Plan with Chaos Mesh

#### Phase 1: Basic Setup (Week 1)
```bash
# Install Chaos Mesh
helm repo add chaos-mesh https://charts.chaos-mesh.org
helm install chaos-mesh chaos-mesh/chaos-mesh \
  --namespace=chaos-testing \
  --create-namespace \
  --set dashboard.create=false \
  --set controllerManager.resources.limits.memory=256Mi
```

#### Phase 2: CNPG-Specific Experiments (Week 2-3)
```yaml
# experiments/cnpg-primary-failure.yaml
apiVersion: chaos-mesh.org/v1alpha1
kind: Workflow
metadata:
  name: cnpg-resilience-test
spec:
  entry: cnpg-chaos-suite
  templates:
    - name: cnpg-chaos-suite
      templateType: Serial
      children:
        - primary-failure
        - network-partition
        - io-delay
        
    - name: primary-failure
      templateType: PodChaos
      deadline: 5m
      podChaos:
        action: pod-kill
        mode: one
        selector:
          labelSelectors:
            cnpg.io/instanceRole: primary
```

#### Phase 3: Integration with POC (Week 4)
```go
// Extend our POC to use Chaos Mesh
package chaos

import (
    chaosmesh "github.com/chaos-mesh/chaos-mesh/api/v1alpha1"
)

func (e *PodChaosExperiment) RunWithChaosMesh(ctx context.Context) error {
    // Convert our experiment to Chaos Mesh CRD
    chaos := e.toChaosMeshPodChaos()
    
    // Apply via Kubernetes client
    if err := e.Client.Create(ctx, chaos); err != nil {
        return err
    }
    
    // Monitor and collect metrics
    return e.monitorChaosMeshExperiment(ctx, chaos)
}
```

### Migration Path if Needed

If we later need LitmusChaos features:
1. Both can coexist in different namespaces
2. Our POC abstraction layer makes switching feasible
3. Experiments can be gradually migrated

## Conclusion

**Chaos Mesh** is the recommended choice for CloudNativePG because:
- Better suited for StatefulSet workloads
- Lower resource overhead
- Simpler integration with our POC
- More relevant chaos types for PostgreSQL
- Cleaner alignment with CNPG's architecture

The main trade-off is slightly less mature tooling around visualization, which can be addressed with Prometheus/Grafana integration.