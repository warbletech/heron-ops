package mcp

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	"github.com/warbletech/heron-ops/internal/config"
	"github.com/warbletech/heron-ops/internal/metrics"
)

func newMetricsServer(t *testing.T, mode config.Mode, objs ...runtime.Object) (*Server, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m, err := metrics.New(reg)
	if err != nil {
		t.Fatal(err)
	}
	s := newTestServer(mode, objs...)
	WithMetrics(m)(s)
	return s, reg
}

// sample returns the value of the series name{labels}, or 0 if absent.
func sample(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
	next:
		for _, m := range mf.GetMetric() {
			got := map[string]string{}
			for _, lp := range m.GetLabel() {
				got[lp.GetName()] = lp.GetValue()
			}
			for k, v := range labels {
				if got[k] != v {
					continue next
				}
			}
			if len(got) != len(labels) {
				continue
			}
			switch {
			case m.GetCounter() != nil:
				return m.GetCounter().GetValue()
			case m.GetHistogram() != nil:
				return float64(m.GetHistogram().GetSampleCount())
			}
		}
	}
	return 0
}

func TestToolAndRPCMetrics(t *testing.T) {
	demo := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "demo"}}
	for _, tc := range []struct {
		name    string
		mode    config.Mode
		req     string
		tool    map[string]string // tool_calls_total labels, nil if no tool runs
		rpc     map[string]string // rpc_requests_total labels
		k8s     map[string]string // k8s_api_requests_total labels, nil if no API call
		wantErr string            // text expected in the tool result
	}{
		{
			name: "successful read", mode: config.ModeReadOnly,
			req:  `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_namespaces"}}`,
			tool: map[string]string{"tool": "list_namespaces", "result": "success"},
			rpc:  map[string]string{"method": "tools/call", "transport": "stdio", "result": "success"},
			k8s:  map[string]string{"verb": "list", "resource": "namespaces", "result": "success"},
		},
		{
			name: "failed read", mode: config.ModeReadOnly,
			req:     `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"get_pod","arguments":{"namespace":"demo","name":"nope"}}}`,
			tool:    map[string]string{"tool": "get_pod", "result": "error"},
			rpc:     map[string]string{"method": "tools/call", "transport": "stdio", "result": "success"},
			k8s:     map[string]string{"verb": "get", "resource": "pods", "result": "error"},
			wantErr: "not found",
		},
		{
			name: "write denied in readonly mode", mode: config.ModeReadOnly,
			req:     `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete_pod","arguments":{"namespace":"demo","name":"p"}}}`,
			tool:    map[string]string{"tool": "delete_pod", "result": "denied"},
			rpc:     map[string]string{"method": "tools/call", "transport": "stdio", "result": "success"},
			wantErr: "requires readwrite mode",
		},
		{
			name: "write allowed in readwrite mode", mode: config.ModeReadWrite,
			req:     `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"delete_pod","arguments":{"namespace":"demo","name":"p"}}}`,
			tool:    map[string]string{"tool": "delete_pod", "result": "error"},
			rpc:     map[string]string{"method": "tools/call", "transport": "stdio", "result": "success"},
			k8s:     map[string]string{"verb": "delete", "resource": "pods", "result": "error"},
			wantErr: "delete pod demo/p",
		},
		{
			name: "unknown tool is bounded", mode: config.ModeReadOnly,
			req:     `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"rm -rf"}}`,
			tool:    map[string]string{"tool": "unknown", "result": "error"},
			rpc:     map[string]string{"method": "tools/call", "transport": "stdio", "result": "success"},
			wantErr: "unknown tool",
		},
		{
			name: "invalid tool params", mode: config.ModeReadOnly,
			req: `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":[1]}`,
			rpc: map[string]string{"method": "tools/call", "transport": "stdio", "result": "error"},
		},
		{
			name: "unknown method is bounded", mode: config.ModeReadOnly,
			req: `{"jsonrpc":"2.0","id":7,"method":"resources/list"}`,
			rpc: map[string]string{"method": "other", "transport": "stdio", "result": "error"},
		},
		{
			name: "parse error", mode: config.ModeReadOnly,
			req: `{not json`,
			rpc: map[string]string{"method": "invalid", "transport": "stdio", "result": "error"},
		},
		{
			name: "notification", mode: config.ModeReadOnly,
			req: `{"jsonrpc":"2.0","method":"notifications/initialized"}`,
			rpc: map[string]string{"method": "notifications/initialized", "transport": "stdio", "result": "success"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, reg := newMetricsServer(t, tc.mode, demo)
			resp := rpc(t, s, tc.req)
			if tc.wantErr != "" {
				res, ok := resp.Result.(ToolCallResult)
				if !ok || !res.IsError || !strings.Contains(text(res), tc.wantErr) {
					t.Fatalf("result = %+v, want an error containing %q", resp.Result, tc.wantErr)
				}
			}
			if got := sample(t, reg, "heron_ops_rpc_requests_total", tc.rpc); got != 1 {
				t.Errorf("rpc_requests_total%v = %v, want 1", tc.rpc, got)
			}
			rpcDur := map[string]string{"method": tc.rpc["method"], "transport": "stdio"}
			if got := sample(t, reg, "heron_ops_rpc_request_duration_seconds", rpcDur); got != 1 {
				t.Errorf("rpc duration count%v = %v, want 1", rpcDur, got)
			}
			if tc.tool != nil {
				if got := sample(t, reg, "heron_ops_tool_calls_total", tc.tool); got != 1 {
					t.Errorf("tool_calls_total%v = %v, want 1", tc.tool, got)
				}
				dur := map[string]string{"tool": tc.tool["tool"]}
				if got := sample(t, reg, "heron_ops_tool_call_duration_seconds", dur); got != 1 {
					t.Errorf("tool duration count%v = %v, want 1", dur, got)
				}
			} else if n := testutil.CollectAndCount(reg, "heron_ops_tool_calls_total"); n != 0 {
				t.Errorf("no tool should have run, got %d tool series", n)
			}
			if tc.k8s != nil {
				if got := sample(t, reg, "heron_ops_k8s_api_requests_total", tc.k8s); got != 1 {
					t.Errorf("k8s_api_requests_total%v = %v, want 1", tc.k8s, got)
				}
			} else if n := testutil.CollectAndCount(reg, "heron_ops_k8s_api_requests_total"); n != 0 {
				t.Errorf("no Kubernetes call expected (readonly guard must stop writes first), got %d series", n)
			}
		})
	}
}

func TestCallToolRecordsToolMetrics(t *testing.T) {
	s, reg := newMetricsServer(t, config.ModeReadOnly)
	s.CallTool(t.Context(), "list_nodes", nil)
	if got := sample(t, reg, "heron_ops_tool_calls_total", map[string]string{"tool": "list_nodes", "result": "success"}); got != 1 {
		t.Fatalf("CallTool not counted: %v", got)
	}
	// Embedded calls bypass JSON-RPC, so no request is recorded.
	if n := testutil.CollectAndCount(reg, "heron_ops_rpc_requests_total"); n != 0 {
		t.Fatalf("rpc series = %d, want 0", n)
	}
}

func TestHTTPMetricsEndpoint(t *testing.T) {
	s, _ := newMetricsServer(t, config.ModeReadOnly)
	srv := httptest.NewServer(s.httpHandler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	resp, err = http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/metrics status %d", resp.StatusCode)
	}
	if want := `heron_ops_rpc_requests_total{method="ping",result="success",transport="http"} 1`; !strings.Contains(string(b), want) {
		t.Fatalf("/metrics missing %q:\n%s", want, b)
	}
}

func TestNoMetricsEndpointWithoutMetrics(t *testing.T) {
	srv := httptest.NewServer(newTestServer(config.ModeReadOnly).httpHandler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("/metrics without metrics: status %d, want 404", resp.StatusCode)
	}
}

func TestNewServerWithMetrics(t *testing.T) {
	m, err := metrics.New(prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Mode: config.ModeReadOnly, RESTConfig: &rest.Config{Host: "http://127.0.0.1:1"}}
	s, err := NewServer(cfg, WithMetrics(m), WithMetrics(nil))
	if err != nil {
		t.Fatal(err)
	}
	if s.metrics != m || s.clients.Recorder != m {
		t.Fatal("WithMetrics did not wire the server and Kubernetes clients")
	}
}
