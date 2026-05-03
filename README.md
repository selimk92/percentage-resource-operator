# Percentage Resource Operator

A Kubernetes operator that allows you to define pod resource **limits as a percentage of node capacity** instead of static values.

When a node grows (more RAM, bigger instance type), pod limits automatically scale with it — no manifest changes needed.

```yaml
requests: memory: 1Gi    # static — scheduler uses this
limits:   memory: 20%    # dynamic — operator calculates from node capacity
```

---

## How It Works

### The Problem
Static resource limits like `limits: memory: 2Gi` break in heterogeneous clusters:
- Too low on large nodes → wasted capacity
- Too high on small nodes → OOM risk
- Different values needed per environment (dev/staging/prod)

### The Solution

1. You define a `PercentageResourcePolicy` with percentage-based limits
2. **Webhook** intercepts pod creation → calculates limits from node capacity → injects into pod spec before it starts
3. **Controller** watches for node capacity changes → updates pod limits when the node grows or shrinks

```
Pod CREATE
    │
    ▼
MutatingWebhook ──► find matching policy
                ──► get node capacity (or min across matching nodes)
                ──► calculate: node_capacity × percentage / 100
                ──► clamp to min/max bounds
                ──► inject limits into pod spec
                    ──► pod starts with correct limits ✓

Node capacity changes
    │
    ▼
Node Reconciler ──► find all pods on this node
                ──► recalculate limits
                ──► apply update strategy (OnRestart / Evict / Disabled)
```

---

## Installation

### Prerequisites
- Kubernetes 1.25+
- Helm 3+

### Install with Helm

```bash
helm install percentage-resource-operator \
  oci://ghcr.io/selimk92/percentage-resource-operator \
  --namespace percentage-system \
  --create-namespace \
  --version 0.1.0
```

### Install from source

```bash
git clone https://github.com/selimk92/percentage-resource-operator
cd percentage-resource-operator

make install   # installs CRD
make run       # runs operator locally (requires kubeconfig)
```

---

## Usage

### 1. Create a PercentageResourcePolicy

```yaml
apiVersion: policy.percentagepolicy.io/v1alpha1
kind: PercentageResourcePolicy
metadata:
  name: my-policy
  namespace: default
spec:
  podSelector:
    matchLabels:
      app: my-app
  resources:
    memory:
      percentage: 20      # 20% of node's allocatable memory
      fallback: "512Mi"   # used if node info is unavailable (required)
      min: "256Mi"        # computed limit cannot go below this
      max: "8Gi"          # computed limit cannot exceed this
    cpu:
      percentage: 30      # 30% of node's allocatable CPU
      fallback: "500m"
      min: "100m"
      max: "4"
  updateStrategy:
    type: OnRestart        # OnRestart | Evict | Disabled
    updateThreshold: "10%"
    debounce: "30s"
```

### 2. Deploy your pods with matching labels

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: my-pod
  labels:
    app: my-app
spec:
  containers:
  - name: app
    image: nginx:alpine
    resources:
      requests:
        memory: "128Mi"   # static — keeps scheduler working correctly
        cpu: "100m"
      # limits intentionally omitted — operator will inject them
```

### 3. Verify limits were injected

```bash
kubectl get pod my-pod -o jsonpath='{.spec.containers[0].resources}' | jq .
```

```json
{
  "limits": {
    "cpu": "1200m",
    "memory": "2449552Ki"
  },
  "requests": {
    "cpu": "100m",
    "memory": "128Mi"
  }
}
```

```bash
kubectl get pod my-pod -o jsonpath='{.metadata.annotations}' | jq .
```

```json
{
  "percentagepolicy.io/policy-ref": "default/my-policy",
  "percentagepolicy.io/limit-source": "percentage",
  "percentagepolicy.io/memory-limit-calculated": "2449552Ki",
  "percentagepolicy.io/memory-limit-applied": "2449552Ki",
  "percentagepolicy.io/cpu-limit-calculated": "1200m",
  "percentagepolicy.io/cpu-limit-applied": "1200m",
  "percentagepolicy.io/update-strategy": "OnRestart",
  "percentagepolicy.io/limit-last-updated": "2026-05-03T19:41:35Z"
}
```

---

## API Reference

### PercentageResourcePolicy Spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `podSelector` | LabelSelector | ✓ | Selects which pods this policy applies to |
| `resources.memory` | ResourceLimitSpec | — | Memory limit configuration |
| `resources.cpu` | ResourceLimitSpec | — | CPU limit configuration |
| `resources.ephemeralStorage` | ResourceLimitSpec | — | Ephemeral storage limit configuration |
| `updateStrategy.type` | string | — | `OnRestart` (default), `Evict`, or `Disabled` |
| `updateStrategy.updateThreshold` | string | — | Minimum change to trigger update (default: `5%`) |
| `updateStrategy.debounce` | string | — | Node event coalescing window (default: `30s`) |

### ResourceLimitSpec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `percentage` | integer (1-100) | ✓ | Percentage of node's allocatable resource |
| `fallback` | Quantity | ✓ | Static value used when node info is unavailable |
| `min` | Quantity | — | Floor: computed limit cannot go below this |
| `max` | Quantity | — | Ceiling: computed limit cannot exceed this |

### Update Strategies

| Strategy | Behavior | Use When |
|----------|----------|----------|
| `OnRestart` | Stores new limits as pending annotation; applies on next pod restart | Production — safe, non-disruptive |
| `Evict` | Evicts pod so it reschedules with updated limits | Fast updates needed; respects PodDisruptionBudget |
| `Disabled` | Never updates running pods; only applies to new pods | Stateful workloads with strict stability requirements |

### Pod Annotations

| Annotation | Description |
|-----------|-------------|
| `percentagepolicy.io/policy-ref` | Policy that manages this pod (`namespace/name`) |
| `percentagepolicy.io/limit-source` | How the limit was determined: `percentage`, `fallback`, `clamped-min`, `clamped-max` |
| `percentagepolicy.io/memory-limit-calculated` | Raw calculated value before clamping |
| `percentagepolicy.io/memory-limit-applied` | Actually applied limit |
| `percentagepolicy.io/memory-limit-pending` | Pending value waiting for pod restart (OnRestart mode) |
| `percentagepolicy.io/limit-last-updated` | Timestamp of last update (RFC3339) |

---

## Fallback Hierarchy

```
1. Node info available    →  percentage × node_allocatable       →  apply
2. Node info unavailable  →  fallback static value               →  apply
3. Computed value < min   →  min value   (limit-source: clamped-min)
4. Computed value > max   →  max value   (limit-source: clamped-max)
5. No matching policy     →  no-op (existing limits preserved)
6. Webhook failure        →  pod starts without limits (fail-open)
```

---

## Kubernetes Version Compatibility

| Version | InPlacePodVerticalScaling | Behavior |
|---------|--------------------------|----------|
| < 1.27 | Not available | OnRestart / Evict strategies only |
| 1.27–1.28 | Alpha (feature gate required) | Opt-in |
| 1.29+ | Beta (enabled by default) | In-place limit updates without pod restart |

---

## Architecture

```
┌─────────────────────────────────────────┐
│           User Manifests                │
│  PercentageResourcePolicy + Pods        │
└──────────────┬──────────────────────────┘
               │
 ┌─────────────▼──────────────┐
 │  MutatingAdmissionWebhook  │  Pod CREATE → inject limits before pod starts
 └─────────────┬──────────────┘
               │
 ┌─────────────▼──────────────┐
 │         Operator           │
 │  ┌─────────────────────┐   │
 │  │   Pod Reconciler    │   │  nodeName set → recalculate and patch
 │  └─────────────────────┘   │
 │  ┌─────────────────────┐   │
 │  │   Node Reconciler   │   │  node capacity changed → update all pods
 │  └─────────────────────┘   │
 └────────────────────────────┘
```

---

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes and add tests
4. Run: `make test && make lint`
5. Submit a pull request

---

## License

Apache License 2.0 — see [LICENSE](LICENSE) for details.
