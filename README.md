# Heron Ops

> **Product rename:** **Heron Ops** (heron bird). Repository and binary still use the legacy name **SteadyHelm** / `steadyhelm` during transition — see [HERON_OPS_RENAME.md](https://github.com/warble-tech/warble-army/blob/main/docs/HERON_OPS_RENAME.md). Logo: [heron-ops mark](https://github.com/warble-tech/warble-army/blob/main/brand/heron-ops/heron-ops-logo.jpg).

**A Kubernetes MCP (Model Context Protocol) server** that bridges AI agents to live cluster state. Lets Gemini, Claude, or GPT-4 reason over your namespaces, pods, events, and HPA metrics — in real time — without `kubectl` proxy hacks.

Part of the [Warble Army](https://github.com/warble-tech/warble-army) flock and upcoming **[warbleoss.org](https://warbleoss.org)** Community Edition. Pairs with **ShrikeOps** (scan) and **Starling Lite** (MCP facade).

---

## Features

- **Structured cluster context for LLMs** — all resources serialised as clean text the model can reason over
- **Read-only & read-write modes** — write tools (`scale_deployment`, `delete_pod`) are only registered when `--mode=readwrite`
- **RBAC gates** — runs under your existing kubeconfig or a scoped ServiceAccount inside the cluster
- **stdio + HTTP transports** — drop into Claude Desktop / Cursor via stdio, or expose as an HTTP sidecar
- **Metrics-server aware** — live CPU/memory per pod and node (gracefully degrades if metrics-server is absent)

---

## Available Tools

| Tool | Mode | Description |
|---|---|---|
| `list_namespaces` | read | All namespaces and their phase |
| `list_pods` | read | Pods with ready/total, phase, restart count |
| `get_pod` | read | Full pod status, conditions, container states |
| `get_pod_logs` | read | Tail N lines from a container |
| `list_nodes` | read | Node status, roles, kernel version |
| `list_events` | read | Events with field-selector support |
| `list_deployments` | read | Deployments with replica counts |
| `list_hpas` | read | HPA min/max/current/desired replicas |
| `list_pod_metrics` | read | Live CPU + memory per pod |
| `list_node_metrics` | read | Live CPU + memory per node |
| `list_services` | read | Services with type and port mappings |
| `get_configmap` | read | ConfigMap key/value data |
| `cluster_summary` | read | Health snapshot: nodes, pod phases, warnings, HPA saturation |
| `scale_deployment` | **write** | Scale a Deployment to N replicas |
| `delete_pod` | **write** | Delete a pod (triggers restart) |

---

## Quick Start

### Homebrew / binary

```bash
go install github.com/warblecloud/steadyhelm@latest
```

### Run against current kubeconfig (read-only, stdio)

```bash
steadyhelm --mode=readonly --transport=stdio
```

### Run as HTTP server (e.g. as a sidecar)

```bash
steadyhelm --mode=readwrite --transport=http --addr=:8811
```

### Restrict to one namespace

```bash
steadyhelm --namespace=production --transport=stdio
```

---

## Claude Desktop / Cursor Configuration

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "steadyhelm": {
      "command": "steadyhelm",
      "args": ["--mode=readonly", "--transport=stdio"],
      "env": {
        "KUBECONFIG": "/Users/you/.kube/config"
      }
    }
  }
}
```

---

## In-Cluster Deployment (minimal RBAC)

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: steadyhelm
  namespace: steadyhelm
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: steadyhelm-readonly
rules:
  - apiGroups: [""]
    resources: ["namespaces","pods","pods/log","nodes","events","services","configmaps"]
    verbs: ["get","list"]
  - apiGroups: ["apps"]
    resources: ["deployments","deployments/scale"]
    verbs: ["get","list"]
  - apiGroups: ["autoscaling"]
    resources: ["horizontalpodautoscalers"]
    verbs: ["get","list"]
  - apiGroups: ["metrics.k8s.io"]
    resources: ["pods","nodes"]
    verbs: ["get","list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: steadyhelm-readonly
subjects:
  - kind: ServiceAccount
    name: steadyhelm
    namespace: steadyhelm
roleRef:
  kind: ClusterRole
  name: steadyhelm-readonly
  apiGroup: rbac.authorization.k8s.io
```

---

## Building

```bash
go build -o steadyhelm .
```

Docker:

```bash
docker build -t steadyhelm:latest .
docker run --rm -v ~/.kube:/root/.kube steadyhelm:latest \
  --mode=readonly --transport=http --addr=:8811
```

---

## Architecture

```
LLM (Gemini / Claude / GPT-4)
        │  tool call (JSON-RPC 2.0)
        ▼
  SteadyHelm MCP Server
  ├── stdio transport   ← Claude Desktop, Cursor
  └── HTTP transport    ← ShrikeOps sidecar, custom agents
        │
        ▼
  k8s client-go
  ├── Core API (pods, nodes, events, services, configmaps)
  ├── Apps API (deployments/scale)
  ├── Autoscaling API (HPAs)
  └── Metrics API (pod + node metrics)
        │
        ▼
  Kubernetes API Server
```

---

## License

Apache 2.0 — see [LICENSE](LICENSE).

Built with ❤️ by [Warble Cloud / ChirpStack LLP](https://warblecloud.com).
