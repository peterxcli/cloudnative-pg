# 🚀 Super Simple Guide - Run Chaos Tests NOW!

## Option 1: One-Line Commands (Easiest!)

```bash
# 1. List all available tests
./hack/run-chaos-tests.sh -t list

# 2. Run a basic test (kills a pod)
./hack/run-chaos-tests.sh -t basic

# 3. Run network chaos test
./hack/run-chaos-tests.sh -t network

# 4. Run all tests
./hack/run-chaos-tests.sh -t all
```

That's it! The script handles everything for you.

---

## Option 2: Use YAML Files (No Code!)

If you have Chaos Mesh installed:

```bash
# Apply chaos directly with kubectl
kubectl apply -f tests/chaos/scenarios/kill-primary.yaml

# Wait 30 seconds...

# Remove chaos
kubectl delete -f tests/chaos/scenarios/kill-primary.yaml
```

Available YAML tests:
- `kill-primary.yaml` - Kills the primary database
- `network-delay.yaml` - Makes network slow
- `io-stress.yaml` - Makes disk slow

---

## Option 3: Run Existing Go Tests

We already have complete tests written! Just run them:

```bash
# Run ALL chaos tests that already exist
go test ./tests/e2e -run "Chaos" -v

# Run specific existing test
go test ./tests/e2e -run "TestPrimaryFailover" -v

# Run with timeout
go test ./tests/e2e -run "Chaos" -v -timeout 30m
```

---

## What Tests Already Exist?

### E2E Tests (Complete scenarios):
1. **chaos_mesh_failover_test.go** - Full failover scenarios with Chaos Mesh
2. **chaos_primary_failure_test.go** - Primary pod failure handling  
3. **failover_test.go** - Comprehensive failover testing
4. **fastfailover_test.go** - Fast failover performance testing

### Unit Tests:
1. **tests/chaos/core** - Core framework tests (93% coverage)
2. **tests/chaos/chaosmesh** - Chaos Mesh adapter tests (60% coverage)
3. **tests/chaos/safety** - Safety mechanism tests

---

## Quick Setup (If Starting Fresh)

```bash
# 1. Install prerequisites (one time only)
curl -sSL https://mirrors.chaos-mesh.org/v2.6.2/install.sh | bash

# 2. Create a test cluster
kubectl apply -f - <<EOF
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: test-cluster
spec:
  instances: 3
  storage:
    size: 1Gi
EOF

# 3. Wait for cluster to be ready
kubectl wait --for=condition=Ready cluster/test-cluster --timeout=300s

# 4. Run tests!
./hack/run-chaos-tests.sh -t basic
```

---

## Examples Output

When you run a test, you'll see:

```
[INFO] Checking prerequisites...
[INFO] Prerequisites check completed ✓
[INFO] Using existing cluster 'test-cluster'
[INFO] Running basic pod chaos test...
[INFO] Found primary pod: test-cluster-1
[INFO] Killing primary pod to test failover...
[INFO] Waiting for new primary election...
[INFO] New primary elected: test-cluster-2
[INFO] Failover completed in 8 seconds ✓
[INFO] Test execution completed!
```

---

## Troubleshooting

**"kubectl not found"**
```bash
# Install kubectl first
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/darwin/amd64/kubectl"
chmod +x kubectl
sudo mv kubectl /usr/local/bin/
```

**"Not connected to cluster"**
```bash
# Use kind for local testing
kind create cluster
```

**"Permission denied"**
```bash
chmod +x ./hack/run-chaos-tests.sh
```

---

## Summary

You have **3 ways** to run chaos tests:

1. **Shell script** - `./hack/run-chaos-tests.sh -t basic` (easiest!)
2. **YAML files** - `kubectl apply -f tests/chaos/scenarios/kill-primary.yaml` (simple!)
3. **Go tests** - `go test ./tests/e2e -run "Chaos" -v` (most complete!)

All the tests are **already written**. You just need to **run** them!