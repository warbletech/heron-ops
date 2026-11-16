# Grafana dashboard

`heron-ops-dashboard.json` is a Grafana dashboard for the metrics described in
[docs/metrics.md](../../docs/metrics.md).

Import it with **Dashboards → New → Import → Upload JSON file**, or provision
it from a ConfigMap / file provider. The Prometheus data source is chosen with
the `datasource` variable at the top of the dashboard, so nothing in the JSON
is tied to a specific data source UID. `job` and `instance` variables filter by
scrape target (populated from `heron_ops_build_info`).

Panels:

- **Overview**: version and mode, JSON-RPC request rate, tool error ratio,
  Kubernetes API error ratio.
- **MCP tools**: calls/s per tool, failed and denied calls, p95 and mean latency.
- **JSON-RPC requests**: requests/s by method and transport, p50/p95/p99
  latency, protocol errors.
- **Kubernetes API**: calls/s, errors/s and p95 latency by verb and resource.
- **Process**: CPU, resident memory, goroutines.

`internal/metrics` has a test that parses this file and fails if a panel
queries a metric heron-ops does not export.
