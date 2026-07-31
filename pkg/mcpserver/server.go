// Package mcpserver exposes Heron Ops (legacy: SteadyHelm) Kubernetes MCP
// server for embedding (e.g. Warble Army Starling Lite). Prefer this package
// over importing internal/.
package mcpserver

import (
	"context"

	"github.com/warble-tech/heron-ops/internal/config"
	"github.com/warble-tech/heron-ops/internal/mcp"
)

// Mode mirrors config.Mode for external callers.
type Mode = config.Mode

const (
	ModeReadOnly  = config.ModeReadOnly
	ModeReadWrite = config.ModeReadWrite
)

// Server is a SteadyHelm MCP server (cluster tools).
type Server struct {
	inner *mcp.Server
	mode  Mode
}

// Options for New.
type Options struct {
	Kubeconfig string
	Namespace  string
	Mode       string // "readonly" | "readwrite"
}

// New builds a cluster MCP backend. Fails if Kubernetes config cannot be resolved.
func New(opts Options) (*Server, error) {
	mode := opts.Mode
	if mode == "" {
		mode = string(config.ModeReadOnly)
	}
	cfg := config.New(opts.Kubeconfig, opts.Namespace, mode)
	inner, err := mcp.NewServer(cfg)
	if err != nil {
		return nil, err
	}
	return &Server{inner: inner, mode: cfg.Mode}, nil
}

// CallTool invokes a SteadyHelm tool by name.
func (s *Server) CallTool(ctx context.Context, name string, args map[string]any) (text string, isError bool) {
	res := s.inner.CallTool(ctx, name, args)
	var b stringsBuilder
	for _, c := range res.Content {
		if c.Text != "" {
			b.WriteString(c.Text)
		}
	}
	return b.String(), res.IsError
}

// Mode returns the effective mode.
func (s *Server) Mode() Mode { return s.mode }

// IsClusterTool reports whether name is handled by SteadyHelm.
func IsClusterTool(name string) bool {
	switch name {
	case "list_namespaces", "list_pods", "get_pod", "get_pod_logs",
		"list_nodes", "list_events", "list_deployments", "list_hpas",
		"list_pod_metrics", "list_node_metrics", "list_services",
		"get_configmap", "cluster_summary", "scale_deployment", "delete_pod":
		return true
	default:
		return false
	}
}

// minimal strings builder without importing strings for tiny package surface
type stringsBuilder struct{ b []byte }

func (s *stringsBuilder) WriteString(v string) {
	s.b = append(s.b, v...)
}
func (s *stringsBuilder) String() string { return string(s.b) }
