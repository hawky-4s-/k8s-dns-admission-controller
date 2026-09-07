# Task 3.3: Prometheus Metrics

**Phase**: 3 - Configuration & Observability  
**Estimate**: 2-3 hours  
**Dependencies**: Task 3.1

## Objective

Implement Prometheus metrics for monitoring webhook performance and mutation statistics.

## Deliverables

- [ ] Metrics endpoint on separate port
- [ ] Mutation counters
- [ ] Latency histograms
- [ ] Error tracking

## Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `dns_webhook_mutations_total` | Counter | namespace, action | Total mutations |
| `dns_webhook_errors_total` | Counter | type | Total errors |
| `dns_webhook_request_duration_seconds` | Histogram | | Request latency |

## Acceptance Criteria

- [ ] `/metrics` endpoint returns Prometheus format
- [ ] Mutations counted by namespace and action
- [ ] Errors categorized and counted
- [ ] Latency tracked with histograms
