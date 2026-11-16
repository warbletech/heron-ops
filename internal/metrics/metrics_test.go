package metrics

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// populated returns metrics on a NewRegistry with one observation of each
// vector, so every family shows up in Gather.
func populated(t *testing.T) (*prometheus.Registry, *Metrics) {
	t.Helper()
	reg := NewRegistry()
	m, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	m.SetBuildInfo("1.2.3", "readonly")
	m.ObserveTool("list_pods", ResultSuccess, 10*time.Millisecond)
	m.ObserveRPC("tools/call", "stdio", false, time.Millisecond)
	m.ObserveK8s("list", "pods", nil, 2*time.Millisecond)
	return reg, m
}

func exported(t *testing.T, reg prometheus.Gatherer) map[string]bool {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, mf := range mfs {
		names[mf.GetName()] = true
	}
	return names
}

func TestNewRejectsNilAndDuplicates(t *testing.T) {
	if _, err := New(nil); err == nil {
		t.Fatal("New(nil) should fail")
	}
	reg := prometheus.NewRegistry()
	if _, err := New(reg); err != nil {
		t.Fatal(err)
	}
	var are prometheus.AlreadyRegisteredError
	if _, err := New(reg); !errors.As(err, &are) {
		t.Fatalf("second New on the same registry: err = %v, want AlreadyRegisteredError", err)
	}
}

func TestNilMetricsIsNoop(t *testing.T) {
	var m *Metrics
	m.SetBuildInfo("v", "readonly")
	m.ObserveTool("t", ResultSuccess, time.Second)
	m.ObserveRPC("ping", "http", false, time.Second)
	m.ObserveK8s("get", "pods", errors.New("x"), time.Second)
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("nil Handler status = %d, want 404", rec.Code)
	}
}

func TestObservations(t *testing.T) {
	reg := prometheus.NewRegistry()
	m, err := New(reg)
	if err != nil {
		t.Fatal(err)
	}
	m.SetBuildInfo("old", "readonly")
	m.SetBuildInfo("0.1.0", "readwrite") // replaces, never accumulates series
	m.ObserveTool("get_pod", ResultError, time.Millisecond)
	m.ObserveTool("get_pod", ResultError, time.Millisecond)
	m.ObserveTool("delete_pod", ResultDenied, 0)
	m.ObserveRPC("tools/list", "http", false, time.Millisecond)
	m.ObserveRPC("other", "stdio", true, time.Millisecond)
	m.ObserveK8s("get", "pods", errors.New("not found"), time.Millisecond)
	m.ObserveK8s("list", "nodes", nil, time.Millisecond)

	for _, tc := range []struct {
		name string
		c    prometheus.Collector
		want float64
	}{
		{"build_info", m.buildInfo.WithLabelValues("0.1.0", "readwrite"), 1},
		{"tool error", m.toolCalls.WithLabelValues("get_pod", ResultError), 2},
		{"tool denied", m.toolCalls.WithLabelValues("delete_pod", ResultDenied), 1},
		{"rpc ok", m.rpcRequests.WithLabelValues("tools/list", "http", ResultSuccess), 1},
		{"rpc error", m.rpcRequests.WithLabelValues("other", "stdio", ResultError), 1},
		{"k8s error", m.k8sRequests.WithLabelValues("get", "pods", ResultError), 1},
		{"k8s ok", m.k8sRequests.WithLabelValues("list", "nodes", ResultSuccess), 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := testutil.ToFloat64(tc.c); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
	if n := testutil.CollectAndCount(m.buildInfo); n != 1 {
		t.Errorf("build_info series = %d, want 1", n)
	}
	if n := testutil.CollectAndCount(m.toolDuration); n != 2 {
		t.Errorf("tool duration series = %d, want 2 (get_pod, delete_pod)", n)
	}
}

func TestHandlerServesPrometheusFormat(t *testing.T) {
	_, m := populated(t)
	srv := httptest.NewServer(NewServeMux(m))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("status %d, Content-Type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, want := range []string{
		"# TYPE heron_ops_tool_calls_total counter",
		`heron_ops_tool_calls_total{result="success",tool="list_pods"} 1`,
		"# TYPE heron_ops_tool_call_duration_seconds histogram",
		`heron_ops_rpc_requests_total{method="tools/call",result="success",transport="stdio"} 1`,
		`heron_ops_k8s_api_requests_total{resource="pods",result="success",verb="list"} 1`,
		`heron_ops_build_info{mode="readonly",version="1.2.3"} 1`,
		"go_goroutines",
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("/metrics missing %q", want)
		}
	}

	resp2, err := http.Get(srv.URL + "/other")
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("/other status = %d, want 404", resp2.StatusCode)
	}
}

func TestHandlerWithoutGatherer(t *testing.T) {
	// A wrapping Registerer cannot be gathered, so Handler has nothing to serve.
	m, err := New(prometheus.WrapRegistererWith(prometheus.Labels{"x": "y"}, prometheus.NewRegistry()))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	m.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestListenAndServe(t *testing.T) {
	if err := ListenAndServe("127.0.0.1:-1", nil); err == nil {
		t.Fatal("invalid address should fail")
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	_, m := populated(t)
	go func() { _ = ListenAndServe(addr, m) }() // ends with the test binary
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/metrics")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if !strings.Contains(string(b), "heron_ops_build_info") {
				t.Fatalf("standalone /metrics body: %s", b)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("metrics listener never came up: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// metricRef matches metric names referenced in PromQL or Markdown.
var metricRef = regexp.MustCompile(`\b(?:heron_ops|go|process)_[a-z0-9_]+`)

// baseName strips histogram series suffixes.
func baseName(n string) string {
	for _, suf := range []string{"_bucket", "_sum", "_count"} {
		if strings.HasSuffix(n, suf) {
			return strings.TrimSuffix(n, suf)
		}
	}
	return n
}

func TestGrafanaDashboard(t *testing.T) {
	raw, err := os.ReadFile("../../deploy/grafana/heron-ops-dashboard.json")
	if err != nil {
		t.Fatal(err)
	}
	var dash struct {
		UID        string `json:"uid"`
		Title      string `json:"title"`
		Templating struct {
			List []struct {
				Name  string `json:"name"`
				Type  string `json:"type"`
				Query any    `json:"query"`
			} `json:"list"`
		} `json:"templating"`
		Panels []struct {
			Title      string `json:"title"`
			Type       string `json:"type"`
			Datasource *struct {
				UID string `json:"uid"`
			} `json:"datasource"`
			Targets []struct {
				Expr       string `json:"expr"`
				Datasource struct {
					UID string `json:"uid"`
				} `json:"datasource"`
			} `json:"targets"`
		} `json:"panels"`
	}
	if err := json.Unmarshal(raw, &dash); err != nil {
		t.Fatalf("dashboard is not valid JSON: %v", err)
	}
	if dash.UID == "" || dash.Title == "" {
		t.Fatal("dashboard needs a uid and title")
	}
	hasDS := false
	for _, v := range dash.Templating.List {
		if v.Name == "datasource" && v.Type == "datasource" && v.Query == "prometheus" {
			hasDS = true
		}
	}
	if !hasDS {
		t.Fatal("dashboard must select its Prometheus data source with a $datasource variable")
	}

	reg, _ := populated(t)
	have := exported(t, reg)
	used := map[string]bool{}
	for _, p := range dash.Panels {
		if p.Type == "row" {
			continue
		}
		if p.Datasource == nil || p.Datasource.UID != "${datasource}" {
			t.Errorf("panel %q must use the ${datasource} variable", p.Title)
		}
		if len(p.Targets) == 0 {
			t.Errorf("panel %q has no queries", p.Title)
		}
		for _, tg := range p.Targets {
			if tg.Datasource.UID != "${datasource}" {
				t.Errorf("panel %q query must use the ${datasource} variable", p.Title)
			}
			refs := metricRef.FindAllString(tg.Expr, -1)
			if len(refs) == 0 {
				t.Errorf("panel %q query %q references no known metric", p.Title, tg.Expr)
			}
			for _, n := range refs {
				used[baseName(n)] = true
				if !have[baseName(n)] {
					t.Errorf("panel %q queries %s, which heron-ops does not export", p.Title, n)
				}
			}
		}
	}
	for n := range have {
		if strings.HasPrefix(n, Namespace+"_") && !used[n] {
			t.Errorf("dashboard has no panel for %s", n)
		}
	}
}

func TestDocsListEveryMetric(t *testing.T) {
	raw, err := os.ReadFile("../../docs/metrics.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	reg, _ := populated(t)
	var missing []string
	for n := range exported(t, reg) {
		if strings.HasPrefix(n, Namespace+"_") && !strings.Contains(doc, "`"+n+"`") {
			missing = append(missing, n)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("docs/metrics.md does not document: %v", missing)
	}
	// And nothing documented that does not exist.
	have := exported(t, reg)
	for _, n := range metricRef.FindAllString(doc, -1) {
		if strings.HasPrefix(n, Namespace+"_") && !have[baseName(n)] {
			t.Errorf("docs/metrics.md mentions %s, which is not exported", n)
		}
	}
}

func TestNamesMatchConstants(t *testing.T) {
	reg, _ := populated(t)
	have := exported(t, reg)
	for _, n := range []string{NameBuildInfo, NameToolCallsTotal, NameToolCallDuration, NameRPCRequestsTotal,
		NameRPCRequestDuration, NameK8sRequestsTotal, NameK8sRequestDuration} {
		if !have[Namespace+"_"+n] {
			t.Errorf("%s_%s not exported", Namespace, n)
		}
	}
}
