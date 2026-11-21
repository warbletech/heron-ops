package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/warbletech/heron-ops/internal/config"
	"github.com/warbletech/heron-ops/internal/k8s"
	"github.com/warbletech/heron-ops/internal/metrics"
)

// Transport names used as the "transport" metric label.
const (
	TransportStdio = "stdio"
	TransportHTTP  = "http"
)

// Server is the MCP server instance.
type Server struct {
	cfg     *config.Config
	clients *k8s.Clients
	metrics *metrics.Metrics // nil disables instrumentation
	mu      sync.Mutex
}

// Option customises a Server.
type Option func(*Server)

// WithMetrics instruments tool calls, JSON-RPC requests and Kubernetes API
// calls with m, and serves m on GET /metrics in the HTTP transport.
func WithMetrics(m *metrics.Metrics) Option {
	return func(s *Server) {
		if m == nil {
			return
		}
		s.metrics = m
		s.clients.Recorder = m
	}
}

// NewServer creates a Server and initialises the Kubernetes clients.
func NewServer(cfg *config.Config, opts ...Option) (*Server, error) {
	clients, err := k8s.NewClients(cfg.RESTConfig)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, clients: clients}
	for _, o := range opts {
		o(s)
	}
	return s, nil
}

// ── stdio transport ───────────────────────────────────────────────────────────

// ServeStdio reads newline-delimited JSON-RPC from stdin and writes to stdout.
// This is the standard MCP transport used by Claude Desktop, Cursor, etc.
func (s *Server) ServeStdio() error {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4*1024*1024), 4*1024*1024)
	enc := json.NewEncoder(os.Stdout)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		resp := s.handle(context.Background(), line, TransportStdio)
		if isNotificationAck(resp) {
			// JSON-RPC 2.0: notifications must not be answered.
			continue
		}
		s.mu.Lock()
		if err := enc.Encode(resp); err != nil {
			log.Printf("[mcp] encode error: %v", err)
		}
		s.mu.Unlock()
	}
	return scanner.Err()
}

// ── HTTP transport ────────────────────────────────────────────────────────────

// ServeHTTP starts an HTTP server exposing POST /mcp, GET /health and, when
// metrics are enabled, GET /metrics.
// Useful for integration testing and non-stdio clients.
func (s *Server) ServeHTTP(addr string) error {
	srv := &http.Server{Addr: addr, Handler: s.httpHandler(), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}

// httpHandler builds the HTTP transport's routes.
func (s *Server) httpHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		var raw json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "bad JSON", http.StatusBadRequest)
			return
		}
		resp := s.handle(r.Context(), raw, TransportHTTP)
		if isNotificationAck(resp) {
			// JSON-RPC 2.0: notifications get no response body.
			w.WriteHeader(http.StatusAccepted)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("[heron-ops] write HTTP response: %v", err)
		}
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"ok","server":"heron-ops"}`)
	})
	if s.metrics != nil {
		mux.Handle("/metrics", s.metrics.Handler())
	}
	return mux
}

// isNotificationAck reports whether handle produced the empty Response it
// uses for notifications, which must not be written to the client.
func isNotificationAck(r Response) bool {
	return r.JSONRPC == ""
}

// ── Core dispatcher ───────────────────────────────────────────────────────────

// rpcMethods are the JSON-RPC methods reported by name in metrics; anything
// else is reported as "other" (or "invalid" for unparsable input) to keep the
// method label bounded.
var rpcMethods = map[string]bool{
	"initialize": true, "notifications/initialized": true,
	"tools/list": true, "tools/call": true, "ping": true,
}

// handle decodes and answers one JSON-RPC message received on transport,
// recording request metrics.
func (s *Server) handle(ctx context.Context, raw []byte, transport string) Response {
	start := time.Now()
	method := "invalid"
	var req Request
	var resp Response
	if err := json.Unmarshal(raw, &req); err != nil {
		resp = errResponse(nil, -32700, "parse error")
	} else {
		method = "other"
		if rpcMethods[req.Method] {
			method = req.Method
		}
		resp = s.route(ctx, req)
	}
	s.metrics.ObserveRPC(method, transport, resp.Error != nil, time.Since(start))
	return resp
}

// route answers a decoded JSON-RPC request.
func (s *Server) route(ctx context.Context, req Request) Response {
	switch req.Method {

	case "initialize":
		var params InitializeParams
		if req.Params != nil {
			_ = json.Unmarshal(req.Params, &params)
		}
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: InitializeResult{
				ProtocolVersion: "2024-11-05",
				ServerInfo:      ServerInfo{Name: "heron-ops", Version: "0.1.0"},
				Capabilities: map[string]any{
					"tools": map[string]any{},
				},
			},
		}

	case "notifications/initialized":
		return Response{}

	case "tools/list":
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  ToolsListResult{Tools: toolDefs(string(s.cfg.Mode))},
		}

	case "tools/call":
		var params ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return errResponse(req.ID, -32602, "invalid params")
		}
		result := s.dispatch(ctx, params)
		return Response{JSONRPC: "2.0", ID: req.ID, Result: result}

	case "ping":
		return Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}}

	default:
		return errResponse(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}
