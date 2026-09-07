# Kubernetes DNS Admission Controller

![CI](https://github.com/hawky-4s-/k8s-dns-admission-controller/actions/workflows/ci.yaml/badge.svg)
![Release](https://github.com/hawky-4s-/k8s-dns-admission-controller/actions/workflows/release.yaml/badge.svg)
[![codecov](https://codecov.io/gh/hawky-4s-/k8s-dns-admission-controller/graph/badge.svg?token=CODECOV_TOKEN)](https://codecov.io/gh/hawky-4s-/k8s-dns-admission-controller)

> **Note:** This project was formerly known as `k8s-ndots-admission-controller`. See the [Migration from v1](#migration-from-v1) section for upgrading.

A Mutating Admission Controller that manages DNS settings in `Pod.spec.dnsConfig` and `Pod.spec.dnsPolicy` — including the `ndots` option, which helps improve DNS resolution performance for applications communicating with external services.

## Features

- **Full DNS management**: reconcile `dnsConfig.options` (including `ndots`), `dnsConfig.nameservers`, `dnsConfig.searches`, and the top-level `dnsPolicy`.
- **Combine strategies**: `merge`, `update`, `unset`, or `override` control how managed values combine with what a pod already declares.
- **Configurable gate**:
    - `opt-in`: Only mutate pods with annotation `dns.hawky.dev/dns: "true"`.
    - `opt-out` (default): Mutate all in-scope pods except those with `dns.hawky.dev/dns: "false"`.
    - `always`: Mutate all in-scope pods regardless of annotations.
- **Per-pod overrides**: attach a full DNS spec (JSON/YAML) via a pod annotation.
- **No-op by default**: with no DNS settings configured, the webhook makes no changes.
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
   git clone https://github.com/hawky-4s-/k8s-dns-admission-controller.git
   cd k8s-dns-admission-controller
   ```

2. Install the chart. Nothing is managed out of the box — configure at least one
   DNS setting. To set the classic `ndots` value, add it as an option:
   ```bash
   helm upgrade --install ndots ./charts/k8s-dns-admission-controller \
     --namespace dns-system \
     --create-namespace \
     --set 'dns.options[0].name=ndots' \
     --set 'dns.options[0].value=2'
   ```

## Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `dns.strategy` | How managed DNS settings combine: `merge`, `update`, `unset`, `override` | `merge` |
| `dns.annotationMode` | Gate: `always`, `opt-in`, `opt-out` | `opt-out` |
| `dns.enableAnnotationKey` | Pod annotation consulted for opt-in/opt-out gating | `dns.hawky.dev/dns` |
| `dns.policy` | Pod `dnsPolicy` to set (`""` = leave alone) | `""` |
| `dns.nameservers` | `dnsConfig.nameservers` to apply | `[]` |
| `dns.searches` | `dnsConfig.searches` to apply | `[]` |
| `dns.options` | `dnsConfig.options` to apply, including `ndots` | `[]` |
| `dns.annotationKey` | Pod annotation carrying a full DNS spec (JSON/YAML) | `dns.hawky.dev/dns-config` |
| `dns.strategyAnnotationKey` | Pod annotation overriding the strategy per pod | `dns.hawky.dev/dns-strategy` |
| `namespace.exclude` | List of namespaces to ignore | `[kube-system, kube-public, kube-node-lease]` |
| `tls.useCertManager` | Use cert-manager for TLS | `true` |

### Mutation gate

- **opt-out** (Default): in-scope pods are mutated automatically. To skip a pod, add:
  ```yaml
  metadata:
    annotations:
      dns.hawky.dev/dns: "false"
  ```
- **opt-in**: no mutations happen by default. To enable for a pod, add:
  ```yaml
  metadata:
    annotations:
      dns.hawky.dev/dns: "true"
  ```

## DNS settings

The webhook can manage every pod DNS field: `dnsConfig.options` (any named
option, including `ndots`), `dnsConfig.nameservers`, `dnsConfig.searches`, and
the top-level `dnsPolicy`. `ndots` is not special-cased — set it like any other
option. With no settings configured, the webhook is a no-op.

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
    dns.hawky.dev/dns-config: |
      nameservers: ["1.1.1.1"]
      searches: ["team.svc.cluster.local"]
      options:
        - name: ndots
          value: "3"
    # Optionally change the strategy just for this pod.
    dns.hawky.dev/dns-strategy: override
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
        dns.hawky.dev/dns: "false" # Opt this pod out of DNS mutation
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
| `dns_webhook_mutations_total` | Total number of pod mutations performed |
| `dns_webhook_errors_total` | Total number of mutation errors |
| `dns_webhook_request_duration_seconds` | Latency of admission requests |

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

## Migration from v1

Upgrading from `k8s-ndots-admission-controller` v1? Two things changed:

1. **Set ndots explicitly.** v1 defaulted to `ndots=2` automatically. v2 is a no-op out of the box:
   ```bash
   helm upgrade ... --set 'dns.options[0].name=ndots' --set 'dns.options[0].value=2'
   ```

2. **Rename the opt-out annotation.** If any pods carry the old `change-ndots: "false"` or
   `ndots.hawky.dev/dns: "false"` annotation, rename it to:
   ```yaml
   dns.hawky.dev/dns: "false"
   ```

3. **Namespace.** The default install namespace changed from `ndots-system` to `dns-system`.

## License

This project is licensed under the [MIT License](LICENSE).
