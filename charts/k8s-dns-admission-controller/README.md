# k8s-dns-admission-controller Helm Chart

A Helm chart for deploying the k8s-dns-admission-controller mutating admission controller.

## Installation

```bash
helm upgrade --install ndots . \
  --namespace dns-system \
  --create-namespace
```

## Configuration

| Key | Description | Default |
|-----|-------------|---------|
| `image.repository` | Image repository | `hawky4s/k8s-dns-admission-controller` |
| `image.tag` | Image tag | `""` (chart appVersion) |
| `dns.strategy` | How managed DNS settings combine with the pod's (`merge`, `update`, `unset`, `override`) | `merge` |
| `dns.annotationMode` | Mutation gate (`always`, `opt-in`, `opt-out`) | `opt-out` |
| `dns.enableAnnotationKey` | Pod annotation consulted for opt-in/opt-out gating | `dns.hawky.dev/dns` |
| `dns.policy` | Top-level pod `dnsPolicy` to set (`""` = unmanaged, `None`, `ClusterFirst`, `ClusterFirstWithHostNet`, `Default`) | `""` |
| `dns.nameservers` | `dnsConfig.nameservers` to apply | `[]` |
| `dns.searches` | `dnsConfig.searches` to apply | `[]` |
| `dns.options` | `dnsConfig.options` to apply, including `ndots` (list of `{name, value}`; omit `value` for flags) | `[]` |
| `dns.annotationKey` | Pod annotation carrying a full DNS spec (JSON/YAML) that overlays these defaults | `dns.hawky.dev/dns-config` |
| `dns.strategyAnnotationKey` | Pod annotation overriding `dns.strategy` for a single pod | `dns.hawky.dev/dns-strategy` |
| `tls.useCertManager` | Enable cert-manager integration | `true` |
| `metrics.enabled` | Enable metrics endpoint | `true` |
| `metrics.serviceMonitor.enabled` | Enable Prometheus ServiceMonitor | `false` |

> **Note**: If `metrics.enabled` is `true` and `metrics.serviceMonitor.enabled` is `false`, the Service will be automatically annotated with `prometheus.io/scrape: "true"` and `prometheus.io/port`.

### Setting `ndots`

`ndots` is now an ordinary DNS option — there is no dedicated `ndots.value`.
Manage it like any other option:

```bash
helm upgrade --install ndots . \
  --namespace dns-system --create-namespace \
  --set 'dns.options[0].name=ndots' \
  --set 'dns.options[0].value=2'
```

See `values.yaml` for full configuration options.
