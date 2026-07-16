# Metrics

tsbridge publishes Prometheus metrics for requests, connections, Whois lookups, service changes, and configuration reloads.

## Enabling Metrics

Set a metrics listen address:

```toml
[global]
metrics_addr = "127.0.0.1:9090"
```

Scrape `http://127.0.0.1:9090/metrics`. The endpoint has no authentication. Use `:9090` only when the surrounding network protects access.

## Available Metrics

### Request Metrics

#### tsbridge_requests_total

Counter labeled by `service` and HTTP `status`. It counts completed requests.

```promql
# Request rate per service
rate(tsbridge_requests_total[5m])

# Error rate (non-2xx responses)
rate(tsbridge_requests_total{status!~"2.."}[5m])
```

#### tsbridge_request_duration_seconds

Histogram labeled by `service`. It measures request handling time in seconds.

```promql
# 95th percentile latency per service
histogram_quantile(0.95, sum by (service, le) (
  rate(tsbridge_request_duration_seconds_bucket[5m])
))

# Average request duration
rate(tsbridge_request_duration_seconds_sum[5m]) / rate(tsbridge_request_duration_seconds_count[5m])
```

### Error Tracking

#### tsbridge_errors_total

Counter labeled by `service` and error `type`. The only emitted type is `panic`, recorded when a request handler panics.

```promql
# Error rate by type
rate(tsbridge_errors_total[5m])
```

### Connection Metrics

#### tsbridge_connections_active

Gauge labeled by `service`. It counts connections managed by the HTTP server and excludes hijacked connections, including WebSockets after upgrade.

#### tsbridge_connection_pool_active

Gauge labeled by `service`. It counts in-flight backend requests.

### Whois Metrics

#### tsbridge_whois_duration_seconds

Histogram labeled by `service`. It measures successful Tailscale Whois lookups in seconds.

```promql
# Whois lookup latency
histogram_quantile(0.99, rate(tsbridge_whois_duration_seconds_bucket[5m]))
```

### Service Lifecycle

#### tsbridge_services_active

Gauge containing the current number of active services.

#### tsbridge_service_operations_total

Counter labeled by `operation` (`add`, `remove`, or `update`) and `status` (`success` or `failure`).

```promql
# Service operation failure rate
rate(tsbridge_service_operations_total{status="failure"}[5m])
```

#### tsbridge_service_operation_duration_seconds

Histogram labeled by `operation`. It measures service add, remove, and update time in seconds.

### Configuration

#### tsbridge_config_reloads_total

Counter labeled by `status` (`success` or `failure`). The Docker provider increments it when container events or polling produce a new configuration. The file provider does not watch TOML files.

#### tsbridge_config_reload_duration_seconds

Histogram measuring provider-driven configuration reload time in seconds.

## Example Queries

### Service Overview

```promql
# Request rate
sum(rate(tsbridge_requests_total[5m])) by (service)

# Error rate
sum(rate(tsbridge_requests_total{status!~"2.."}[5m])) by (service)

# P95 latency
histogram_quantile(0.95, sum by (service, le)(rate(tsbridge_request_duration_seconds_bucket[5m])))

# Active services
tsbridge_services_active
```
