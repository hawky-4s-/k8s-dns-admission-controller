# Kubernetes ndots Admission Controller

![CI](https://github.com/hawky-4s-/k8s-ndots-admission-controller/actions/workflows/ci.yaml/badge.svg)
![Release](https://github.com/hawky-4s-/k8s-ndots-admission-controller/actions/workflows/release.yaml/badge.svg)
[![codecov](https://codecov.io/gh/hawky-4s-/k8s-ndots-admission-controller/graph/badge.svg?token=CODECOV_TOKEN)](https://codecov.io/gh/hawky-4s-/k8s-ndots-admission-controller)

A Mutating Admission Controller that injects or updates the `ndots` configuration in `Pod.spec.dnsConfig`. This helps improve DNS resolution performance for applications running in Kubernetes, especially when communicating with external services.

## Features

- **Automatic Injection**: Sets `ndots` value in Pod DNS configuration.
- **Configurable Modes**:
    - `opt-in`: Only mutate pods with annotation `change-ndots: "true"`.
    - `opt-out`: Mutate all pods except those with annotation `change-ndots: "false"`.
    - `always`: Mutate all pods regardless of annotations.
- **Namespace Filtering**: configurable list of included/excluded namespaces.
- **Critical Namespace Protection**: automatically excludes `kube-system` and other critical namespaces.
- **Helm Chart**: Easy deployment with Cert Manager integration.
- **Observability**: Prometheus metrics and structured logging.

## Installation

### Prerequisites

- Kubernetes 1.25+
- Helm 3.0+
- [Cert Manager](https://cert-manager.io/) (recommended for TLS)

### Install with Helm

1. Add the repository (if applicable) or clone this repo:
   ```bash
   git clone https://github.com/hawky-4s-/k8s-ndots-admission-controller.git
   cd k8s-ndots-admission-controller
   ```

2. Install the chart:
   ```bash
   helm upgrade --install ndots ./charts/k8s-ndots-admission-controller \
     --namespace ndots-system \
     --create-namespace
   ```

## Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `ndots.value` | The ndots value to set | `2` |
| `ndots.annotationKey` | Annotation key for control | `change-ndots` |
| `ndots.annotationMode` | Mode: `always`, `opt-in`, `opt-out` | `opt-out` |
| `dns.strategy` | How managed DNS settings combine: `merge`, `update`, `unset`, `override` | `merge` |
| `dns.policy` | Pod `dnsPolicy` to set (`""` = leave alone) | `""` |
| `dns.nameservers` | `dnsConfig.nameservers` to apply | `[]` |
| `dns.searches` | `dnsConfig.searches` to apply | `[]` |
| `dns.options` | Extra `dnsConfig.options` beyond ndots | `[]` |
| `dns.annotationKey` | Pod annotation carrying a full DNS spec (JSON/YAML) | `ndots.hawky.dev/dns-config` |
| `dns.strategyAnnotationKey` | Pod annotation overriding the strategy per pod | `ndots.hawky.dev/dns-strategy` |
| `namespace.exclude` | List of namespaces to ignore | `[kube-system, kube-public, kube-node-lease]` |
| `tls.useCertManager` | Use cert-manager for TLS | `true` |

### Annotation Modes

- **opt-out** (Default): Mutations happen automatically. To skip a pod, add:
  ```yaml
  metadata:
    annotations:
      change-ndots: "false"
  ```
- **opt-in**: No mutations happen by default. To enable for a pod, add:
  ```yaml
  metadata:
    annotations:
      change-ndots: "true"
  ```

## DNS settings

Beyond `ndots`, the webhook can manage every pod DNS field: `dnsConfig.options`
(any named option), `dnsConfig.nameservers`, `dnsConfig.searches`, and the
top-level `dnsPolicy`. Out of the box it only seeds `ndots` with the legacy
`merge` behavior, so existing deployments are unaffected.

### Strategies

A strategy decides how the managed settings combine with what a pod already
declares:

| Strategy | Behavior |
|----------|----------|
| `merge` (default) | Add/update managed options; union nameservers and searches; leave everything else. |
| `update` | Only change a field or option that is **already present** on the pod. |
| `unset` | Remove the managed options/fields from the pod. |
| `override` | Replace the whole managed field with the configured value. |

Set the default strategy globally via `dns.strategy` (Helm) / `DNS_STRATEGY`
(env), or per pod via the strategy annotation.

### Global configuration (Helm / env)

```yaml
dns:
  strategy: merge
  nameservers: ["10.0.0.10"]
  searches: ["svc.cluster.local"]
  options:
    - name: edns0        # a flag (no value)
    - name: timeout
      value: "1"         # values are strings — quote them
```

### Per-pod overrides (annotations)

Attach a full DNS spec — JSON or YAML, HashiCorp Vault agent-injector style —
that overlays the global default for that pod:

```yaml
metadata:
  annotations:
    # JSON or YAML both work; option values must be quoted strings.
    ndots.hawky.dev/dns-config: |
      nameservers: ["1.1.1.1"]
      searches: ["team.svc.cluster.local"]
      options:
        - name: ndots
          value: "3"
    # Optionally change the strategy just for this pod.
    ndots.hawky.dev/dns-strategy: override
```

### Runtime safety

The API server remains the authoritative validator; the webhook only enforces
the invariants needed to never emit a rejectable patch:

- **Fail-open**: a malformed spec/strategy annotation is ignored (logged) and
  the pod is admitted with the global default — the webhook never blocks a pod
  because of its own uncertainty.
- **`dnsPolicy: None` guard**: `None` requires at least one nameserver, so the
  policy change is skipped (and logged) unless the effective pod would have one.
- Patches are well-formed and idempotent (parents created first, removals in
  descending index order), so they are safe under `reinvocationPolicy`.

## Examples

### Deployment with Opt-Out

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: my-app
spec:
  template:
    metadata:
      annotations:
        change-ndots: "false" # Prevents ndots modification
    spec:
      containers:
        - name: app
          image: nginx
```

## Monitoring

Metrics are exposed on port `8080` at `/metrics`.

If `metrics.serviceMonitor.enabled` is `false` (default), the Service is automatically annotated with:
- `prometheus.io/scrape: "true"`
- `prometheus.io/port: "8080"` (or configured port)

| Metric | Description |
|--------|-------------|
| `ndots_admission_requests_total` | Total admission requests processed |
| `ndots_pod_mutations_total` | Total number of pod mutations performed |
| `ndots_admission_duration_seconds` | Latency of admission requests |

## Development

This project follows strict development guidelines. See [AGENTS.md](./AGENTS.md) for details.

### Prerequisites

- Go 1.26+
- Docker
- Kind (for local clusters)

### Common Commands

```bash
# Run unit tests
make test

# Run linting
make lint

# Run E2E tests (requires Kind)
make e2e

# Build binary
make build

# Build Docker image
make docker-build
```

## License

This project is licensed under the [MIT License](LICENSE).
