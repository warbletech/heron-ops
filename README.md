# Heron Ops

<p align="center">
  <img src="https://raw.githubusercontent.com/warbletech/warble-army/main/brand/heron-ops/heron-ops-logo.jpg" alt="Heron Ops" width="120" />
</p>

[![ci](https://github.com/warbletech/heron-ops/actions/workflows/ci.yml/badge.svg)](https://github.com/warbletech/heron-ops/actions/workflows/ci.yml)

**Kubernetes MCP server** for AI agents — live namespaces, pods, logs, events, HPA metrics, gated writes.

> Formerly **SteadyHelm**. Repository: `warbletech/heron-ops`. Part of the [Warble Army](https://github.com/warbletech/warble-army) flock and **[warbleoss.org](https://warbleoss.org)**.

Pairs with **[ShrikeOps](https://github.com/warbletech/shrikeops)** (scan) and **Starling Lite** / Starling EX Community Edition.

## Features

- **Structured cluster context for LLMs** — resources as clean text models can reason over
- **Read-only & read-write modes** — write tools only when `--mode=readwrite`
- **RBAC gates** — kubeconfig or scoped ServiceAccount
- **stdio + HTTP transports** — Claude Desktop / Cursor via stdio, or HTTP sidecar
- **Metrics-server aware** — live CPU/memory (degrades if metrics-server absent)
- **Prometheus metrics** — tool calls, JSON-RPC and Kubernetes API latency on `/metrics`, plus a Grafana dashboard

## Install

```bash
go install github.com/warbletech/heron-ops@latest
# or from army:
git clone --recurse-submodules https://github.com/warbletech/warble-army.git
cd warble-army && make heron-ops
./bin/heron-ops --mode=readonly --transport=stdio
```

Legacy binary name `steadyhelm` remains a symlink during transition.

## MCP client

```json
{
  "mcpServers": {
    "heron-ops": {
      "command": "heron-ops",
      "args": ["--mode=readonly", "--transport=stdio"]
    }
  }
}
```

## Metrics

With `--transport=http`, Prometheus metrics are served on `GET /metrics` next to `/mcp`.
In stdio mode they are off unless you set `--metrics-addr=127.0.0.1:9090` (or `HERON_OPS_METRICS_ADDR`).
See [docs/metrics.md](docs/metrics.md) for every metric, its labels and example PromQL, and
[deploy/grafana](deploy/grafana/) for the Grafana dashboard.

## Try the flock

Public Starling Lite (includes Heron Ops tools):  
https://mcp.warbleoss.org

## Development

```bash
go test -race ./...   # unit tests use client-go's fake clientset; no cluster needed
go vet ./...
go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out | tail -1
```

CI (`.github/workflows/ci.yml`) runs gofmt, `go mod tidy`, build, vet and tests on every push and PR.
It fails if total statement coverage drops below 80% and uploads `coverage.out` as the `coverage` artifact.
Pushing a `v*` tag runs `.github/workflows/release.yml`, which tests, builds binaries for Linux, macOS and
Windows (amd64/arm64), and publishes a GitHub release with `checksums.txt`.

## License

Apache 2.0 — see [LICENSE](LICENSE).

Built by [Warble Cloud / ChirpStack LLP](https://warblecloud.com) · Community: [warbleoss.org](https://warbleoss.org)
