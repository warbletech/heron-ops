// Package metrics defines the Prometheus metrics exported by heron-ops.
//
// Metrics are registered on a caller-supplied registry rather than the global
// default one, so embedders and tests stay isolated from each other. Every
// method on *Metrics is safe to call on a nil receiver, which turns
// instrumentation into a no-op.
package metrics

import (
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Namespace is the prefix of every heron-ops metric name.
const Namespace = "heron_ops"

// Label values used for the result label.
const (
	ResultSuccess = "success"
	ResultError   = "error"
	// ResultDenied marks a write tool rejected by the readonly-mode guard.
	ResultDenied = "denied"
)

// Metric names (without the Namespace prefix), exported for docs and tests.
const (
	NameBuildInfo          = "build_info"
	NameToolCallsTotal     = "tool_calls_total"
	NameToolCallDuration   = "tool_call_duration_seconds"
	NameRPCRequestsTotal   = "rpc_requests_total"
	NameRPCRequestDuration = "rpc_request_duration_seconds"
	NameK8sRequestsTotal   = "k8s_api_requests_total"
	NameK8sRequestDuration = "k8s_api_request_duration_seconds"
)

// durationBuckets covers fast in-memory calls up to slow log fetches.
var durationBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30}

// Metrics holds the heron-ops collectors.
type Metrics struct {
	gatherer prometheus.Gatherer

	buildInfo    *prometheus.GaugeVec
	toolCalls    *prometheus.CounterVec
	toolDuration *prometheus.HistogramVec
	rpcRequests  *prometheus.CounterVec
	rpcDuration  *prometheus.HistogramVec
	k8sRequests  *prometheus.CounterVec
	k8sDuration  *prometheus.HistogramVec
}

// NewRegistry returns a fresh registry with the Go runtime and process
// collectors already registered, as used by the heron-ops binary.
func NewRegistry() *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
	return reg
}

// New creates the heron-ops metrics and registers them on reg. If reg also
// implements prometheus.Gatherer (a *prometheus.Registry does), Handler serves
// its contents.
func New(reg prometheus.Registerer) (*Metrics, error) {
	if reg == nil {
		return nil, errors.New("metrics: nil registerer")
	}
	m := &Metrics{
		buildInfo: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: Namespace,
			Name:      NameBuildInfo,
			Help:      "Always 1; labels carry the heron-ops version and server mode.",
		}, []string{"version", "mode"}),
		toolCalls: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      NameToolCallsTotal,
			Help:      "MCP tool calls, by tool and result (success, error, denied).",
		}, []string{"tool", "result"}),
		toolDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      NameToolCallDuration,
			Help:      "Time spent executing MCP tool calls, by tool.",
			Buckets:   durationBuckets,
		}, []string{"tool"}),
		rpcRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      NameRPCRequestsTotal,
			Help:      "JSON-RPC requests handled, by method, transport and result (success, error).",
		}, []string{"method", "transport", "result"}),
		rpcDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      NameRPCRequestDuration,
			Help:      "JSON-RPC request handling latency, by method and transport.",
			Buckets:   durationBuckets,
		}, []string{"method", "transport"}),
		k8sRequests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: Namespace,
			Name:      NameK8sRequestsTotal,
			Help:      "Kubernetes API calls made by heron-ops, by verb, resource and result (success, error).",
		}, []string{"verb", "resource", "result"}),
		k8sDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Namespace: Namespace,
			Name:      NameK8sRequestDuration,
			Help:      "Kubernetes API call latency, by verb and resource.",
			Buckets:   durationBuckets,
		}, []string{"verb", "resource"}),
	}
	for _, c := range []prometheus.Collector{
		m.buildInfo, m.toolCalls, m.toolDuration,
		m.rpcRequests, m.rpcDuration, m.k8sRequests, m.k8sDuration,
	} {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}
	if g, ok := reg.(prometheus.Gatherer); ok {
		m.gatherer = g
	}
	return m, nil
}

// SetBuildInfo records the running version and mode.
func (m *Metrics) SetBuildInfo(version, mode string) {
	if m == nil {
		return
	}
	m.buildInfo.Reset()
	m.buildInfo.WithLabelValues(version, mode).Set(1)
}

// ObserveTool records one tool call.
func (m *Metrics) ObserveTool(tool, result string, d time.Duration) {
	if m == nil {
		return
	}
	m.toolCalls.WithLabelValues(tool, result).Inc()
	m.toolDuration.WithLabelValues(tool).Observe(d.Seconds())
}

// ObserveRPC records one JSON-RPC request.
func (m *Metrics) ObserveRPC(method, transport string, failed bool, d time.Duration) {
	if m == nil {
		return
	}
	m.rpcRequests.WithLabelValues(method, transport, resultOf(failed)).Inc()
	m.rpcDuration.WithLabelValues(method, transport).Observe(d.Seconds())
}

// ObserveK8s records one Kubernetes API call. It satisfies k8s.Recorder.
func (m *Metrics) ObserveK8s(verb, resource string, err error, d time.Duration) {
	if m == nil {
		return
	}
	m.k8sRequests.WithLabelValues(verb, resource, resultOf(err != nil)).Inc()
	m.k8sDuration.WithLabelValues(verb, resource).Observe(d.Seconds())
}

// Handler serves the registry in the Prometheus exposition format. It returns
// 404 when the metrics were registered on something that cannot be gathered.
func (m *Metrics) Handler() http.Handler {
	if m == nil || m.gatherer == nil {
		return http.NotFoundHandler()
	}
	return promhttp.HandlerFor(m.gatherer, promhttp.HandlerOpts{})
}

func resultOf(failed bool) string {
	if failed {
		return ResultError
	}
	return ResultSuccess
}

// NewServeMux returns a mux that serves only GET /metrics.
func NewServeMux(m *Metrics) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/metrics", m.Handler())
	return mux
}

// ListenAndServe serves NewServeMux(m) on addr until the listener fails. It is
// used for the optional standalone metrics listener (e.g. in stdio mode).
func ListenAndServe(addr string, m *Metrics) error {
	srv := &http.Server{Addr: addr, Handler: NewServeMux(m), ReadHeaderTimeout: 10 * time.Second}
	return srv.ListenAndServe()
}
