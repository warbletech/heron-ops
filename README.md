# Heron Ops

<p align="center">
  <img src="https://raw.githubusercontent.com/warbletech/warble-army/main/brand/heron-ops/heron-ops-logo.jpg" alt="Heron Ops" width="120" />
</p>

**Kubernetes MCP server** for AI agents — live namespaces, pods, logs, events, HPA metrics, gated writes.

> Formerly **SteadyHelm**. Repository: `warbletech/heron-ops`. Part of the [Warble Army](https://github.com/warbletech/warble-army) flock and **[warbleoss.org](https://warbleoss.org)**.

Pairs with **[ShrikeOps](https://github.com/warbletech/shrikeops)** (scan) and **Starling Lite** / Starling EX Community Edition.

## Features

- **Structured cluster context for LLMs** — resources as clean text models can reason over
- **Read-only & read-write modes** — write tools only when `--mode=readwrite`
- **RBAC gates** — kubeconfig or scoped ServiceAccount
- **stdio + HTTP transports** — Claude Desktop / Cursor via stdio, or HTTP sidecar
- **Metrics-server aware** — live CPU/memory (degrades if metrics-server absent)

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

## Try the flock

Public Starling Lite (includes Heron Ops tools):  
https://mcp.warbleoss.org

## License

Apache 2.0 — see [LICENSE](LICENSE).

Built by [Warble Cloud / ChirpStack LLP](https://warblecloud.com) · Community: [warbleoss.org](https://warbleoss.org)
