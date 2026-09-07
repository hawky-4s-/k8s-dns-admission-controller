# k8s-dns-admission-controller Helm Chart

A Helm chart for deploying the `k8s-dns-admission-controller` mutating admission webhook, which
manages DNS settings in `Pod.spec.dnsConfig` and `Pod.spec.dnsPolicy`.

**Nothing is managed out of the box.** The webhook is a no-op until you configure at least one DNS
setting. This means installation is always safe — no pods are changed until you opt in.

## Installation

```bash
helm upgrade --install dns-ac ./charts/k8s-dns-admission-controller \
  --namespace dns-system \
  --create-namespace
```

To set the classic `ndots` value immediately:

```bash
helm upgrade --install dns-ac ./charts/k8s-dns-admission-controller \
  --namespace dns-system \
  --create-namespace \
  --set 'dns.options[0].name=ndots' \
  --set 'dns.options[0].value=2'
```

## Configuration reference

### DNS settings

These control *what* the webhook writes into `Pod.spec.dnsConfig` and `Pod.spec.dnsPolicy`.
With all defaults, the webhook makes no changes.

| Key | Default | Description |
|-----|---------|-------------|
| `dns.options` | `[]` | `dnsConfig.options` entries to apply. A list of `{name, value}` objects. Omit `value` for boolean flags (e.g. `edns0`). Option values **must be quoted strings**. `ndots` is just an ordinary option — there is no separate `ndots.value` key. |
| `dns.nameservers` | `[]` | `dnsConfig.nameservers` to apply. List of IP address strings. |
| `dns.searches` | `[]` | `dnsConfig.searches` to apply. List of search domain strings. |
| `dns.policy` | `""` | Pod-level `dnsPolicy` to set. Valid values: `ClusterFirst`, `ClusterFirstWithHostNet`, `Default`, `None`. Empty means unmanaged. **`None` requires at least one nameserver** — the webhook skips the change and logs a warning if the effective pod would have none. |

Example — set ndots, add a search domain, and use a custom nameserver:

```yaml
dns:
  options:
    - name: ndots
      value: "2"    # always quote option values
    - name: edns0   # boolean flag — no value field
  searches:
    - svc.cluster.local
  nameservers:
    - 10.0.0.10
```

### Combine strategy

The strategy controls how the webhook's managed settings interact with DNS settings the pod
**already declares** in its own spec.

| Key | Default |
|-----|---------|
| `dns.strategy` | `merge` |

| Strategy | What it does |
|----------|--------------|
| `merge` | **Default.** Adds any missing option and updates the value of an existing one. Unions `nameservers` and `searches` with what the pod already has (no duplicates). Leaves everything else on the pod untouched. Use this for most cases. |
| `update` | Only modifies an option or field that is **already present** on the pod. If the pod has no `ndots` option, `merge` would add it — `update` would not. Use this to normalise pods that already configure DNS without imposing settings on pods that don't. |
| `unset` | **Removes** the managed options/fields from the pod. Use to enforce that certain options are absent. |
| `override` | Replaces the **entire** managed field with the configured value, discarding whatever the pod declared. For `options`, the pod ends up with exactly your list and nothing else. |

The strategy can be overridden per pod with the `dns.hawky.dev/dns-strategy` annotation (see
[Per-pod annotation overrides](#per-pod-annotation-overrides)).

### Mutation gate

The gate decides *which* pods the webhook touches. Default is `opt-out`: every pod is mutated
unless it explicitly opts out.

| Key | Default |
|-----|---------|
| `dns.annotationMode` | `opt-out` |
| `dns.enableAnnotationKey` | `dns.hawky.dev/dns` |

| Mode | Behavior |
|------|----------|
| `opt-out` | **Default.** Every in-scope pod is mutated unless it carries `dns.hawky.dev/dns: "false"`. Good for cluster-wide enforcement with escape hatches for specific workloads. |
| `opt-in` | No pod is mutated unless it carries `dns.hawky.dev/dns: "true"`. Good when you want to enable DNS management only for specific workloads and leave everything else alone. |
| `always` | Every in-scope pod is mutated regardless of annotations. The annotation key has no effect. |

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

Two annotations let individual pods deviate from the cluster-wide configuration without touching
any global setting.

| Annotation | Key (Helm) | Default | Description |
|------------|-----------|---------|-------------|
| `dns.hawky.dev/dns-config` | `dns.annotationKey` | `dns.hawky.dev/dns-config` | A full DNS spec in JSON or YAML. Merged on top of the cluster-wide defaults for this pod only. |
| `dns.hawky.dev/dns-strategy` | `dns.strategyAnnotationKey` | `dns.hawky.dev/dns-strategy` | Overrides `dns.strategy` for this pod only (`merge`, `update`, `unset`, `override`). |

The `dns-config` annotation accepts YAML or JSON. Any field you omit falls back to the
cluster-wide default. Option values must be quoted strings.

```yaml
metadata:
  annotations:
    dns.hawky.dev/dns-config: |
      options:
        - name: ndots
          value: "5"          # override ndots for this pod only
        - name: edns0
      searches:
        - team.svc.cluster.local
      nameservers:
        - 1.1.1.1
    dns.hawky.dev/dns-strategy: override   # replace, don't merge
```

If the annotation value is malformed, the webhook ignores it, logs a warning, and falls back to
the cluster-wide default — the pod is never blocked.

### Namespace filtering

| Key | Default | Description |
|-----|---------|-------------|
| `namespace.exclude` | `[kube-system, kube-public, kube-node-lease]` | Pods in these namespaces are never mutated. |
| `namespace.include` | `[]` | When non-empty, only pods in these namespaces are mutated (exclusions still apply). |

### TLS

The webhook requires a TLS certificate. The chart uses cert-manager by default.

| Key | Default | Description |
|-----|---------|-------------|
| `tls.useCertManager` | `true` | Use cert-manager to issue and rotate the certificate. |
| `tls.certManager.duration` | `8760h` | Certificate validity (1 year). |
| `tls.certManager.renewBefore` | `720h` | Renew 30 days before expiry. |
| `tls.certManager.issuer.create` | `true` | Create a self-signed `Issuer` in the release namespace. |
| `tls.certManager.issuer.name` | `""` | Use an existing issuer (set `issuer.create: false`). |
| `tls.certManager.issuer.kind` | `Issuer` | `Issuer` or `ClusterIssuer`. |

### Webhook behaviour

| Key | Default | Description |
|-----|---------|-------------|
| `webhook.failurePolicy` | `Ignore` | `Fail` or `Ignore`. With `Ignore`, a webhook error admits the pod unchanged. With `Fail`, the pod is rejected — use only if DNS mutation is critical to your workloads. |
| `webhook.timeoutSeconds` | `10` | Seconds the API server waits for a response before applying `failurePolicy`. |
| `webhook.reinvocationPolicy` | `Never` | `Never` or `IfNeeded`. Set to `IfNeeded` if other mutating webhooks may undo this webhook's changes. |

### Observability

| Key | Default | Description |
|-----|---------|-------------|
| `logging.level` | `info` | Log level: `debug`, `info`, `warn`, `error`. |
| `logging.format` | `json` | Log format: `json` or `text`. |
| `metrics.enabled` | `true` | Expose a Prometheus `/metrics` endpoint on `metrics.port`. |
| `metrics.port` | `8080` | Port for the metrics server. |
| `metrics.serviceMonitor.enabled` | `false` | Create a Prometheus Operator `ServiceMonitor`. When `false` and `metrics.enabled` is `true`, the Service is annotated with `prometheus.io/scrape: "true"` automatically. |

Exposed metrics:

| Metric | Description |
|--------|-------------|
| `dns_webhook_mutations_total` | Total pod mutations performed |
| `dns_webhook_errors_total` | Total mutation errors |
| `dns_webhook_request_duration_seconds` | Admission request latency histogram |

### Deployment and runtime

| Key | Default | Description |
|-----|---------|-------------|
| `replicaCount` | `1` | Number of webhook pods. Use `≥2` with a `PodDisruptionBudget` for production. |
| `podDisruptionBudget.enabled` | `false` | Create a `PodDisruptionBudget`. |
| `podDisruptionBudget.minAvailable` | `1` | Minimum available pods during disruptions (takes precedence over `maxUnavailable`). |
| `podDisruptionBudget.maxUnavailable` | — | Maximum unavailable pods during disruptions. |
| `priorityClassName` | `""` | `PriorityClass` name for webhook pods. |
| `resources.requests` | `cpu: 50m, memory: 64Mi` | Pod resource requests. |
| `resources.limits` | `cpu: 100m, memory: 128Mi` | Pod resource limits. |
| `nodeSelector` | `{}` | Node selector for webhook pods. |
| `tolerations` | `[]` | Tolerations for webhook pods. |
| `affinity` | `{}` | Affinity/anti-affinity rules for webhook pods. |

### Image and registry

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `hawky4s/k8s-dns-admission-controller` | Container image repository. |
| `image.tag` | `""` | Image tag. Defaults to the chart `appVersion`. |
| `image.pullPolicy` | `IfNotPresent` | Image pull policy. |
| `imagePullSecrets` | `[]` | Image pull secrets for private registries. |
| `nameOverride` | `""` | Override the chart name component in resource names. |
| `fullnameOverride` | `""` | Fully override all resource names. |

### Service account

| Key | Default | Description |
|-----|---------|-------------|
| `serviceAccount.create` | `true` | Create a dedicated `ServiceAccount`. |
| `serviceAccount.name` | `""` | Use this name instead of the generated one. |
| `serviceAccount.annotations` | `{}` | Annotations on the `ServiceAccount` (e.g. AWS IRSA role ARN). |

### Labels and annotations

| Key | Default | Description |
|-----|---------|-------------|
| `commonLabels` | `{}` | Extra labels added to every resource created by this chart. |
| `commonAnnotations` | `{}` | Extra annotations added to every resource created by this chart. |
| `podAnnotations` | `{}` | Extra annotations added only to webhook pods. |

### Pod security

| Key | Default | Description |
|-----|---------|-------------|
| `podSecurityContext` | `runAsNonRoot: true, runAsUser: 65534, fsGroup: 65534` | Security context applied to the pod. |
| `securityContext` | `allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, runAsNonRoot: true, runAsUser: 65534, capabilities.drop: [ALL]` | Security context applied to the container. |

## Setting `ndots`

`ndots` is an ordinary DNS option — there is no special `ndots.value` key:

```bash
helm upgrade --install dns-ac . \
  --namespace dns-system --create-namespace \
  --set 'dns.options[0].name=ndots' \
  --set 'dns.options[0].value=2'
```

See `values.yaml` for the full list of configurable fields with inline documentation.
