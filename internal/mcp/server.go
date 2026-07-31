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

	"github.com/warble-tech/heron-ops/internal/config"
	"github.com/warble-tech/heron-ops/internal/k8s"
)

// Server is the MCP server instance.
type Server struct {
	cfg     *config.Config
	clients *k8s.Clients
	mu      sync.Mutex
}

// NewServer creates a Server and initialises the Kubernetes clients.
func NewServer(cfg *config.Config) (*Server, error) {
	clients, err := k8s.NewClients(cfg.RESTConfig)
	if err != nil {
		return nil, err
	}
	return &Server{cfg: cfg, clients: clients}, nil
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
		resp := s.handle(context.Background(), line)
		s.mu.Lock()
		if err := enc.Encode(resp); err != nil {
			log.Printf("[mcp] encode error: %v", err)
		}
		s.mu.Unlock()
	}
	return scanner.Err()
}

// ── HTTP transport ────────────────────────────────────────────────────────────

// ServeHTTP starts an HTTP server exposing a single POST /mcp endpoint.
// Useful for integration testing and non-stdio clients.
func (s *Server) ServeHTTP(addr string) error {
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
		resp := s.handle(r.Context(), raw)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"status":"ok","server":"heron-ops"}`)
	})
	return http.ListenAndServe(addr, mux)
}

// ── Core dispatcher ───────────────────────────────────────────────────────────

func (s *Server) handle(ctx context.Context, raw []byte) Response {
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return errResponse(nil, -32700, "parse error")
	}

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
