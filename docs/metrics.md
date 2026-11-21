# Metrics

Heron Ops exposes Prometheus metrics in the standard text exposition format.

## Enabling the endpoint

| Transport | Where `/metrics` is served |
|-----------|----------------------------|
| `--transport=http` | Always, on the same listener as `/mcp` and `/health` (`--addr`, default `:8811`). |
| `--transport=stdio` | Off by default. Set `--metrics-addr=:9090` (or `HERON_OPS_METRICS_ADDR=:9090`) to start a separate listener that serves only `GET /metrics`. Nothing is ever written to stdout except MCP responses. |

`--metrics-addr` also works with `--transport=http` if you want metrics on a
separate port. The endpoint is unauthenticated, so bind it to a private
interface (e.g. `127.0.0.1:9090`) or protect it with a network policy.

Example scrape config:

```yaml
scrape_configs:
  - job_name: heron-ops
    static_configs:
      - targets: ["heron-ops.heron-ops.svc:8811"]
```

Embedders using `pkg/mcpserver` can pass their own registry with
`Options.MetricsRegisterer`; the `heron_ops_*` metrics below are registered on
it (no Go/process collectors are added). Nothing is registered on the global
default registry.

## Heron Ops metrics

All labels have a small, fixed set of values. Pod, namespace and object names
are never used as labels.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `heron_ops_build_info` | gauge | `version`, `mode` | Always 1. `mode` is `readonly` or `readwrite`. |
| `heron_ops_tool_calls_total` | counter | `tool`, `result` | MCP tool calls. |
| `heron_ops_tool_call_duration_seconds` | histogram | `tool` | Tool execution time, including Kubernetes API calls. |
| `heron_ops_rpc_requests_total` | counter | `method`, `transport`, `result` | JSON-RPC messages handled. |
| `heron_ops_rpc_request_duration_seconds` | histogram | `method`, `transport` | Time to handle a JSON-RPC message (excludes network I/O). |
| `heron_ops_k8s_api_requests_total` | counter | `verb`, `resource`, `result` | Kubernetes API calls made by Heron Ops. |
| `heron_ops_k8s_api_request_duration_seconds` | histogram | `verb`, `resource` | Kubernetes API call latency. |

Histogram buckets (seconds): 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30.

### Label values

- `tool`: one of the 15 tool names (`list_namespaces`, `list_pods`, `get_pod`,
  `get_pod_logs`, `list_nodes`, `list_events`, `list_deployments`, `list_hpas`,
  `list_pod_metrics`, `list_node_metrics`, `list_services`, `get_configmap`,
  `cluster_summary`, `scale_deployment`, `delete_pod`), or `unknown` for any
  other name a client sends.
- `result` (tool calls): `success`, `error` (the tool returned an error, e.g. the
  object was not found), or `denied` (a write tool was refused because the server
  runs in readonly mode).
- `result` (JSON-RPC, Kubernetes API): `success` or `error`. For JSON-RPC,
  `error` means a protocol error response (parse error, unknown method, invalid
  params); a tool that fails still answers successfully at the JSON-RPC level
  and is counted in `heron_ops_tool_calls_total{result="error"}`.
- `method`: `initialize`, `notifications/initialized`, `tools/list`,
  `tools/call`, `ping`, `other` (unknown method) or `invalid` (unparsable JSON).
- `transport`: `stdio` or `http`.
- `verb`: `list`, `get`, `update`, `delete`.
- `resource`: `namespaces`, `pods`, `pods/log`, `nodes`, `events`,
  `deployments`, `deployments/scale`, `horizontalpodautoscalers`,
  `pods.metrics.k8s.io`, `nodes.metrics.k8s.io`, `services`, `configmaps`.

## Runtime metrics

The `heron-ops` binary also registers the standard Go runtime (`go_*`, e.g.
`go_goroutines`, `go_memstats_*`) and process (`process_*`, e.g.
`process_cpu_seconds_total`, `process_resident_memory_bytes`) collectors from
`client_golang`.

## Example PromQL

```promql
# Tool calls per second, by tool
sum by (tool) (rate(heron_ops_tool_calls_total[5m]))

# Share of tool calls that failed or were denied
sum(rate(heron_ops_tool_calls_total{result!="success"}[5m]))
  / sum(rate(heron_ops_tool_calls_total[5m]))

# Write attempts blocked by readonly mode
sum by (tool) (increase(heron_ops_tool_calls_total{result="denied"}[1h]))

# p95 tool latency, by tool
histogram_quantile(0.95, sum by (le, tool) (rate(heron_ops_tool_call_duration_seconds_bucket[5m])))

# JSON-RPC requests per second, by method and transport
sum by (method, transport) (rate(heron_ops_rpc_requests_total[5m]))

# p99 JSON-RPC handling latency
histogram_quantile(0.99, sum by (le) (rate(heron_ops_rpc_request_duration_seconds_bucket[5m])))

# Kubernetes API error ratio, by verb and resource
sum by (verb, resource) (rate(heron_ops_k8s_api_requests_total{result="error"}[5m]))
  / sum by (verb, resource) (rate(heron_ops_k8s_api_requests_total[5m]))

# p95 Kubernetes API latency
histogram_quantile(0.95, sum by (le, verb, resource) (rate(heron_ops_k8s_api_request_duration_seconds_bucket[5m])))
```

Example alert:

```yaml
- alert: HeronOpsKubernetesAPIErrors
  expr: |
    sum(rate(heron_ops_k8s_api_requests_total{result="error"}[10m]))
      / sum(rate(heron_ops_k8s_api_requests_total[10m])) > 0.2
  for: 10m
  annotations:
    summary: More than 20% of Heron Ops Kubernetes API calls are failing
```

## Grafana

A ready-made dashboard is in
[`deploy/grafana/heron-ops-dashboard.json`](../deploy/grafana/heron-ops-dashboard.json);
see [`deploy/grafana/README.md`](../deploy/grafana/README.md).
