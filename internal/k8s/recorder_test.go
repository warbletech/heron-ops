package k8s

import (
	"context"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

type call struct {
	verb, resource string
	failed         bool
}

type fakeRecorder struct {
	mu    sync.Mutex
	calls []call
}

func (r *fakeRecorder) ObserveK8s(verb, resource string, err error, d time.Duration) {
	if d < 0 {
		panic("negative duration")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, call{verb, resource, err != nil})
}

func (r *fakeRecorder) take() []call {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := r.calls
	r.calls = nil
	return c
}

// Every API call is reported once with a fixed, low-cardinality verb and
// resource, and its result.
func TestRecorderObservesEveryAPICall(t *testing.T) {
	ctx := context.Background()
	rec := &fakeRecorder{}
	ok := &Clients{Core: fake.NewSimpleClientset(), Metrics: metricsfake.NewSimpleClientset(), Recorder: rec}
	bad := failingClients()
	bad.Recorder = rec

	for _, tc := range []struct {
		name string
		call func(c *Clients)
		want []call // for the successful client; the failing one stops at the first call
	}{
		{"ListNamespaces", func(c *Clients) { _, _ = c.ListNamespaces(ctx) }, []call{{"list", "namespaces", false}}},
		{"ListPods", func(c *Clients) { _, _ = c.ListPods(ctx, "demo", "") }, []call{{"list", "pods", false}}},
		{"GetPod", func(c *Clients) { _, _ = c.GetPod(ctx, "demo", "p") }, []call{{"get", "pods", true}}},
		{"GetPodLogs", func(c *Clients) { _, _ = c.GetPodLogs(ctx, "demo", "p", "", 1) }, []call{{"get", "pods/log", false}}},
		{"ListNodes", func(c *Clients) { _, _ = c.ListNodes(ctx) }, []call{{"list", "nodes", false}}},
		{"ListEvents", func(c *Clients) { _, _ = c.ListEvents(ctx, "demo", "") }, []call{{"list", "events", false}}},
		{"ListDeployments", func(c *Clients) { _, _ = c.ListDeployments(ctx, "demo") }, []call{{"list", "deployments", false}}},
		{"ScaleDeployment", func(c *Clients) { _ = c.ScaleDeployment(ctx, "demo", "d", 2) }, []call{{"get", "deployments/scale", true}}},
		{"ListHPAs", func(c *Clients) { _, _ = c.ListHPAs(ctx, "demo") }, []call{{"list", "horizontalpodautoscalers", false}}},
		{"ListPodMetrics", func(c *Clients) { _, _ = c.ListPodMetrics(ctx, "demo") }, []call{{"list", "pods.metrics.k8s.io", false}}},
		{"ListNodeMetrics", func(c *Clients) { _, _ = c.ListNodeMetrics(ctx) }, []call{{"list", "nodes.metrics.k8s.io", false}}},
		{"ListServices", func(c *Clients) { _, _ = c.ListServices(ctx, "demo") }, []call{{"list", "services", false}}},
		{"GetConfigMap", func(c *Clients) { _, _ = c.GetConfigMap(ctx, "demo", "cm") }, []call{{"get", "configmaps", true}}},
		{"DeletePod", func(c *Clients) { _ = c.DeletePod(ctx, "demo", "p") }, []call{{"delete", "pods", true}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.call(ok)
			assertCalls(t, "fake cluster", rec.take(), tc.want)

			tc.call(bad)
			want := []call{{tc.want[0].verb, tc.want[0].resource, true}}
			if tc.name == "GetPodLogs" {
				// client-go's fake log request bypasses reactors and always succeeds.
				want[0].failed = false
			}
			assertCalls(t, "failing cluster", rec.take(), want)
		})
	}
}

func TestRecorderSeesBothScaleCalls(t *testing.T) {
	rec := &fakeRecorder{}
	core := fake.NewSimpleClientset()
	scaleReactor(t, core, 1, nil)
	c := &Clients{Core: core, Recorder: rec}
	if err := c.ScaleDeployment(context.Background(), "demo", "d", 3); err != nil {
		t.Fatal(err)
	}
	assertCalls(t, "scale", rec.take(), []call{{"get", "deployments/scale", false}, {"update", "deployments/scale", false}})
}

func TestNilRecorderIsSafe(t *testing.T) {
	c := &Clients{Core: fake.NewSimpleClientset()}
	if _, err := c.ListNamespaces(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func assertCalls(t *testing.T, what string, got, want []call) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: recorded %+v, want %+v", what, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s: recorded %+v, want %+v", what, got, want)
		}
	}
}
