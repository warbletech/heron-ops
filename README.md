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

## Safety for agents

Prefer `--mode=readonly` when an agent or LLM client can call this server.

Mode is set with the `--mode` flag (`readonly` or `readwrite`). The default is `readonly`. There is no environment variable for mode; any value other than `readwrite` is treated as `readonly`.

**`readonly`** exposes the read tools: `list_namespaces`, `list_pods`, `get_pod`, `get_pod_logs`, `list_nodes`, `list_events`, `list_deployments`, `list_hpas`, `list_pod_metrics`, `list_node_metrics`, `list_services`, `get_configmap`, and `cluster_summary`. The write tools `scale_deployment` and `delete_pod` are omitted from the tool list, and a call to either is refused with an error asking you to restart with `--mode=readwrite`.

**`readwrite`** adds those two write tools: `scale_deployment` changes a Deployment's replica count, and `delete_pod` deletes a pod (which triggers a restart). Enable this only in a tightly scoped setup — namespaced RBAC, short-lived credentials, and human approval.

Mode is an MCP tool gate, not a substitute for Kubernetes RBAC. The server uses `--kubeconfig`, or the `KUBECONFIG` environment variable, or `~/.kube/config`, or the in-cluster ServiceAccount. Give that identity least privilege. A read-only Role or ClusterRole limited to `get`, `list`, and `watch` is enough for the read tools.

See [SECURITY.md](SECURITY.md) for how to report vulnerabilities.

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
