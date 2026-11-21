// Package mcpserver exposes Heron Ops (legacy: SteadyHelm) Kubernetes MCP
// server for embedding (e.g. Warble Army Starling Lite). Prefer this package
// over importing internal/.
package mcpserver

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/warbletech/heron-ops/internal/config"
	"github.com/warbletech/heron-ops/internal/mcp"
	"github.com/warbletech/heron-ops/internal/metrics"
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
	// MetricsRegisterer, when set, receives the heron_ops_* tool and
	// Kubernetes API metrics (see docs/metrics.md). Nil disables metrics.
	MetricsRegisterer prometheus.Registerer
}

// New builds a cluster MCP backend. Fails if Kubernetes config cannot be resolved.
func New(opts Options) (*Server, error) {
	mode := opts.Mode
	if mode == "" {
		mode = string(config.ModeReadOnly)
	}
	cfg := config.New(opts.Kubeconfig, opts.Namespace, mode)
	var srvOpts []mcp.Option
	if opts.MetricsRegisterer != nil {
		m, err := metrics.New(opts.MetricsRegisterer)
		if err != nil {
			return nil, err
		}
		srvOpts = append(srvOpts, mcp.WithMetrics(m))
	}
	inner, err := mcp.NewServer(cfg, srvOpts...)
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
