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

Add the Helm repository (published to GitHub Pages via
[chart-releaser](https://github.com/helm/chart-releaser-action)):

```bash
helm repo add k8s-dns https://hawky-4s-.github.io/k8s-dns-admission-controller
helm repo update
```

Install the chart. Nothing is managed out of the box — configure at least one
DNS setting. To set the classic `ndots` value, add it as an option:

```bash
helm upgrade --install ndots k8s-dns/k8s-dns-admission-controller \
  --namespace dns-system \
  --create-namespace \
  --set 'dns.options[0].name=ndots' \
  --set 'dns.options[0].value=2'
```

Alternatively, install from a local checkout by pointing at
`./charts/k8s-dns-admission-controller` instead of `k8s-dns/k8s-dns-admission-controller`.

## Configuration

The webhook is configured entirely through environment variables (or, when deployed via Helm,
through `values.yaml`). Every env var has a corresponding Helm key; the table below lists both.

**Nothing is managed out of the box.** With no DNS settings configured, the webhook is a no-op and
admits every pod unchanged. You must configure at least one DNS setting to have the webhook do
anything.

### DNS settings

These control *what* the webhook writes into `Pod.spec.dnsConfig` and `Pod.spec.dnsPolicy`.

| Helm key | Env var | Default | Description |
|----------|---------|---------|-------------|
| `dns.options` | `DNS_OPTIONS` | `[]` / `""` | `dnsConfig.options` entries to apply. In Helm: a list of `{name, value}` objects — omit `value` for boolean flags. In env: a comma-separated `name=value` string (e.g. `ndots=2,edns0`). `ndots` is just another option here — there is no special `ndots.value` key. |
| `dns.nameservers` | `DNS_NAMESERVERS` | `[]` / `""` | `dnsConfig.nameservers` to apply. Helm: list of IP strings. Env: comma-separated IPs. |
| `dns.searches` | `DNS_SEARCHES` | `[]` / `""` | `dnsConfig.searches` to apply. Helm: list of domain strings. Env: comma-separated domains. |
| `dns.policy` | `DNS_POLICY` | `""` | Pod-level `dnsPolicy` to set. Valid values: `ClusterFirst`, `ClusterFirstWithHostNet`, `Default`, `None`. Leave empty to leave `dnsPolicy` unmanaged. **Note:** `None` requires at least one nameserver; the webhook skips the change (and logs a warning) if the effective pod would have none. |

#### Setting `ndots`

`ndots` is an ordinary `dnsConfig.option` — set it the same way as any other option:

```yaml
# values.yaml
dns:
  options:
    - name: ndots
      value: "2"    # values must always be quoted strings
```

```bash
# or via --set
--set 'dns.options[0].name=ndots' --set 'dns.options[0].value=2'
```

```bash
# or via env var
DNS_OPTIONS=ndots=2
```

### Combine strategy

The strategy controls how the webhook's managed settings interact with whatever the pod *already*
declares in its own spec. Set a cluster-wide default; override it per pod with an annotation.

| Helm key | Env var | Default |
|----------|---------|---------|
| `dns.strategy` | `DNS_STRATEGY` | `merge` |

| Strategy | Behavior |
|----------|----------|
| `merge` | **Default.** For `options`: add any missing option; update the value of an existing one. For `nameservers` and `searches`: union the managed list with the pod's existing list (no duplicates). Leaves all other DNS settings on the pod untouched. |
| `update` | Only modify an option/field that is **already present** on the pod. If the pod has no `ndots` option, `merge` would add it — `update` would not. Useful for normalising pods that already configure DNS without imposing settings on pods that don't. |
| `unset` | **Remove** the managed options/fields from the pod. Use to enforce that certain options are absent. |
| `override` | Replace the **entire** managed field with the configured value, discarding whatever the pod declared. For `options`, this means the pod ends up with exactly the configured list and nothing else. |

### Mutation gate

The gate decides *which* pods the webhook touches. Configure cluster-wide with
`dns.annotationMode` / `DNS_ANNOTATION_MODE`; the annotation key can be changed with
`dns.enableAnnotationKey` / `DNS_ANNOTATION_KEY`.

| Helm key | Env var | Default |
|----------|---------|---------|
| `dns.annotationMode` | `DNS_ANNOTATION_MODE` | `opt-out` |
| `dns.enableAnnotationKey` | `DNS_ANNOTATION_KEY` | `dns.hawky.dev/dns` |

| Mode | Behavior |
|------|----------|
| `opt-out` | **Default.** Every in-scope pod is mutated unless it carries the annotation `dns.hawky.dev/dns: "false"`. |
| `opt-in` | No pod is mutated unless it carries the annotation `dns.hawky.dev/dns: "true"`. Useful when you want to enable DNS management only for specific workloads. |
| `always` | Every in-scope pod is mutated, regardless of annotations. The annotation has no effect. |

```yaml
# Opt a single pod out (opt-out mode):
metadata:
  annotations:
    dns.hawky.dev/dns: "false"

# Opt a single pod in (opt-in mode):
metadata:
  annotations:
    dns.hawky.dev/dns: "true"
```

### Per-pod annotation overrides

Two annotations let individual pods deviate from the cluster-wide configuration without changing
any global setting.

| Annotation | Helm key (key name only) | Env var | Default | Description |
|------------|--------------------------|---------|---------|-------------|
| `dns.hawky.dev/dns-config` | `dns.annotationKey` | `DNS_SPEC_ANNOTATION_KEY` | `dns.hawky.dev/dns-config` | A full DNS spec in JSON or YAML. Merged on top of the cluster-wide default for this pod only. |
| `dns.hawky.dev/dns-strategy` | `dns.strategyAnnotationKey` | `DNS_STRATEGY_ANNOTATION_KEY` | `dns.hawky.dev/dns-strategy` | Overrides `dns.strategy` for this pod only. Accepts the same values: `merge`, `update`, `unset`, `override`. |

The DNS spec annotation accepts JSON or YAML (HashiCorp Vault agent-injector style). Any field
you omit falls back to the cluster-wide default. Option values must be quoted strings.

```yaml
metadata:
  annotations:
    dns.hawky.dev/dns-config: |
      options:
        - name: ndots
          value: "5"          # override ndots for this pod only
        - name: edns0         # add a flag option
      searches:
        - team.svc.cluster.local
    dns.hawky.dev/dns-strategy: override   # replace, don't merge
```

### Namespace filtering

| Helm key | Env var | Default |
|----------|---------|---------|
| `namespace.exclude` | `NAMESPACE_EXCLUDE` | `[kube-system, kube-public, kube-node-lease]` |
| `namespace.include` | `NAMESPACE_INCLUDE` | `[]` (all non-excluded) |

Pods in excluded namespaces are never mutated. When `namespace.include` is non-empty, only pods in
those namespaces are mutated (after exclusions are applied). The system namespaces
`kube-system`, `kube-public`, and `kube-node-lease` are always excluded.

### TLS

| Helm key | Default | Description |
|----------|---------|-------------|
| `tls.useCertManager` | `true` | Use cert-manager to issue and rotate the webhook TLS certificate. |
| `tls.certManager.duration` | `8760h` | Certificate validity (1 year). |
| `tls.certManager.renewBefore` | `720h` | Renew 30 days before expiry. |
| `tls.certManager.issuer.create` | `true` | Create a self-signed `Issuer` in the release namespace. |
| `tls.certManager.issuer.name` | `""` | Use an existing issuer instead (requires `issuer.create: false`). |
| `tls.certManager.issuer.kind` | `Issuer` | `Issuer` or `ClusterIssuer`. |
| `TLS_CERT_PATH` (env) | `/certs/tls.crt` | Path to TLS certificate (env-only, for non-Helm deployments). |
| `TLS_KEY_PATH` (env) | `/certs/tls.key` | Path to TLS private key (env-only, for non-Helm deployments). |

### Webhook behaviour

| Helm key | Default | Description |
|----------|---------|-------------|
| `webhook.failurePolicy` | `Ignore` | `Fail` or `Ignore`. With `Ignore`, a webhook error admits the pod unchanged. With `Fail`, the pod is rejected. |
| `webhook.timeoutSeconds` | `10` | Seconds the API server waits for a webhook response before applying `failurePolicy`. |
| `webhook.reinvocationPolicy` | `Never` | `Never` or `IfNeeded`. Set to `IfNeeded` if other mutating webhooks run after this one and may undo its changes. |

### Observability

| Helm key | Env var | Default | Description |
|----------|---------|---------|-------------|
| `logging.level` | `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error`. |
| `logging.format` | `LOG_FORMAT` | `json` | Log format: `json` or `text`. |
| `metrics.enabled` | — | `true` | Expose a Prometheus `/metrics` endpoint. |
| `metrics.port` | `METRICS_PORT` | `8080` | Port for the metrics server. |
| `metrics.serviceMonitor.enabled` | — | `false` | Create a Prometheus Operator `ServiceMonitor`. When `false` and `metrics.enabled` is `true`, the Service is annotated with `prometheus.io/scrape: "true"`. |
| — | `PORT` | `8443` | Webhook HTTPS server port (env-only). |

### Deployment / runtime

| Helm key | Default | Description |
|----------|---------|-------------|
| `replicaCount` | `1` | Number of webhook pods. Use `≥2` with a PodDisruptionBudget for production. |
| `podDisruptionBudget.enabled` | `false` | Create a `PodDisruptionBudget`. |
| `podDisruptionBudget.minAvailable` | `1` | Minimum available pods during disruptions. |
| `priorityClassName` | `""` | Assign a `PriorityClass` to the webhook pods. |
| `resources.requests` | `cpu: 50m, memory: 64Mi` | Pod resource requests. |
| `resources.limits` | `cpu: 100m, memory: 128Mi` | Pod resource limits. |
| `nodeSelector` | `{}` | Node selector for the webhook pods. |
| `tolerations` | `[]` | Tolerations for the webhook pods. |
| `affinity` | `{}` | Affinity rules for the webhook pods. |
| `imagePullSecrets` | `[]` | Image pull secrets for private registries. |
| `serviceAccount.create` | `true` | Create a dedicated `ServiceAccount`. |
| `serviceAccount.annotations` | `{}` | Annotations to add to the `ServiceAccount` (e.g. for IRSA). |
| `podAnnotations` | `{}` | Annotations added to webhook pods. |
| `podSecurityContext` | `runAsNonRoot: true, runAsUser: 65534` | Security context for the pod. |
| `securityContext` | `allowPrivilegeEscalation: false, readOnlyRootFilesystem: true` | Security context for the container. |
| `commonLabels` | `{}` | Extra labels added to all chart resources. |
| `commonAnnotations` | `{}` | Extra annotations added to all chart resources. |

### Runtime safety guarantees

The webhook is designed to never block a pod due to its own errors:

- **Fail-open**: a malformed `dns-config` or `dns-strategy` annotation is silently ignored (a
  warning is logged) and the pod is admitted using the cluster-wide default. The webhook will never
  reject a pod because of a bad annotation.
- **`dnsPolicy: None` guard**: Kubernetes requires `dnsPolicy: None` pods to have at least one
  nameserver. If the effective spec after mutation would leave the pod without nameservers, the
  `dnsPolicy` change is skipped and logged — preventing an API server rejection.
- **Idempotent patches**: parent paths are created before child operations; array removals are
  emitted in descending index order. Patches are safe to re-apply under `reinvocationPolicy: IfNeeded`.

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
