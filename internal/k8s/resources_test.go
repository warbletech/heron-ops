package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"

	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"
)

// failingClients returns Clients whose every API call fails with errBoom.
func failingClients() *Clients {
	core := fake.NewSimpleClientset()
	core.PrependReactor("*", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errBoom
	})
	metrics := metricsfake.NewSimpleClientset()
	metrics.PrependReactor("*", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errBoom
	})
	return &Clients{Core: core, Metrics: metrics}
}

var errBoom = errors.New("boom")

func TestClientErrorsAreWrapped(t *testing.T) {
	ctx := context.Background()
	c := failingClients()
	for _, tc := range []struct {
		name string
		call func() error
		want string
	}{
		{"ListNamespaces", func() error { _, err := c.ListNamespaces(ctx); return err }, "list namespaces"},
		{"ListPods", func() error { _, err := c.ListPods(ctx, "demo", ""); return err }, "list pods"},
		{"GetPod", func() error { _, err := c.GetPod(ctx, "demo", "p"); return err }, "get pod demo/p"},
		{"ListNodes", func() error { _, err := c.ListNodes(ctx); return err }, "list nodes"},
		{"ListEvents", func() error { _, err := c.ListEvents(ctx, "demo", ""); return err }, "list events"},
		{"ListDeployments", func() error { _, err := c.ListDeployments(ctx, "demo"); return err }, "list deployments"},
		{"ScaleDeployment", func() error { return c.ScaleDeployment(ctx, "demo", "d", 2) }, "get scale demo/d"},
		{"ListHPAs", func() error { _, err := c.ListHPAs(ctx, "demo"); return err }, "list HPAs"},
		{"ListPodMetrics", func() error { _, err := c.ListPodMetrics(ctx, "demo"); return err }, "list pod metrics"},
		{"ListNodeMetrics", func() error { _, err := c.ListNodeMetrics(ctx); return err }, "list node metrics"},
		{"ListServices", func() error { _, err := c.ListServices(ctx, "demo"); return err }, "list services"},
		{"GetConfigMap", func() error { _, err := c.GetConfigMap(ctx, "demo", "cm"); return err }, "get configmap demo/cm"},
		{"DeletePod", func() error { return c.DeletePod(ctx, "demo", "p") }, "delete pod demo/p"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) || !errors.Is(err, errBoom) {
				t.Fatalf("err = %v, want it to contain %q and wrap the API error", err, tc.want)
			}
		})
	}
}

func TestListReads(t *testing.T) {
	ctx := context.Background()
	c := &Clients{Core: fake.NewSimpleClientset(
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n1"}},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "n2"}},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e1", Namespace: "demo"}, Type: "Warning"},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e2", Namespace: "other"}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "svc", Namespace: "demo"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "demo"}},
		&autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "hpa", Namespace: "demo"}},
	)}
	for _, tc := range []struct {
		name string
		call func() (int, error)
		want int
	}{
		{"ListNodes", func() (int, error) { l, err := c.ListNodes(ctx); return len(l), err }, 2},
		{"ListEvents/namespace", func() (int, error) { l, err := c.ListEvents(ctx, "demo", ""); return len(l), err }, 1},
		{"ListEvents/all", func() (int, error) { l, err := c.ListEvents(ctx, "", ""); return len(l), err }, 2},
		{"ListServices", func() (int, error) { l, err := c.ListServices(ctx, "demo"); return len(l), err }, 1},
		{"ListHPAs", func() (int, error) { l, err := c.ListHPAs(ctx, "demo"); return len(l), err }, 1},
		{"GetPod", func() (int, error) {
			p, err := c.GetPod(ctx, "demo", "p")
			if err != nil || p.Name != "p" {
				return 0, err
			}
			return 1, nil
		}, 1},
		{"ListHPAs/empty", func() (int, error) { l, err := c.ListHPAs(ctx, "other"); return len(l), err }, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.call()
			if err != nil || got != tc.want {
				t.Fatalf("got %d items, err %v; want %d", got, err, tc.want)
			}
		})
	}
}

func TestGetPodLogs(t *testing.T) {
	c := &Clients{Core: fake.NewSimpleClientset()}
	for _, container := range []string{"", "worker"} {
		logs, err := c.GetPodLogs(context.Background(), "demo", "p", container, 10)
		if err != nil {
			t.Fatalf("GetPodLogs(container=%q): %v", container, err)
		}
		// client-go's fake REST client always answers with this body.
		if logs != "fake logs" {
			t.Fatalf("GetPodLogs(container=%q) = %q", container, logs)
		}
	}
}

func scaleReactor(t *testing.T, core *fake.Clientset, current int32, updateErr error) *int32 {
	t.Helper()
	var updated int32 = -1
	core.PrependReactor("get", "deployments", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "scale" {
			return false, nil, nil
		}
		return true, &autoscalingv1.Scale{
			ObjectMeta: metav1.ObjectMeta{Name: "d", Namespace: a.GetNamespace()},
			Spec:       autoscalingv1.ScaleSpec{Replicas: current},
		}, nil
	})
	core.PrependReactor("update", "deployments", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "scale" {
			return false, nil, nil
		}
		if updateErr != nil {
			return true, nil, updateErr
		}
		s := a.(k8stesting.UpdateAction).GetObject().(*autoscalingv1.Scale)
		updated = s.Spec.Replicas
		return true, s, nil
	})
	return &updated
}

func TestScaleDeployment(t *testing.T) {
	core := fake.NewSimpleClientset()
	updated := scaleReactor(t, core, 1, nil)
	c := &Clients{Core: core}
	if err := c.ScaleDeployment(context.Background(), "demo", "d", 4); err != nil {
		t.Fatal(err)
	}
	if *updated != 4 {
		t.Fatalf("UpdateScale replicas = %d, want 4", *updated)
	}
}

func TestScaleDeploymentUpdateError(t *testing.T) {
	core := fake.NewSimpleClientset()
	scaleReactor(t, core, 1, errBoom)
	err := (&Clients{Core: core}).ScaleDeployment(context.Background(), "demo", "d", 4)
	if err == nil || !strings.Contains(err.Error(), "update scale demo/d") || !errors.Is(err, errBoom) {
		t.Fatalf("err = %v, want a wrapped update scale error", err)
	}
}

// The generated metrics fake cannot list PodMetrics/NodeMetrics from its object
// tracker (the resource names do not round-trip), so serve the lists via reactors.
func TestMetricsWithMetricsServer(t *testing.T) {
	m := metricsfake.NewSimpleClientset()
	m.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &metricsv1beta1.PodMetricsList{Items: []metricsv1beta1.PodMetrics{
			{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "demo"}},
		}}, nil
	})
	m.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &metricsv1beta1.NodeMetricsList{Items: []metricsv1beta1.NodeMetrics{
			{ObjectMeta: metav1.ObjectMeta{Name: "n1"}}, {ObjectMeta: metav1.ObjectMeta{Name: "n2"}},
		}}, nil
	})
	c := &Clients{Core: fake.NewSimpleClientset(), Metrics: m}
	pods, err := c.ListPodMetrics(context.Background(), "demo")
	if err != nil || len(pods) != 1 {
		t.Fatalf("ListPodMetrics = %v, %v", pods, err)
	}
	nodes, err := c.ListNodeMetrics(context.Background())
	if err != nil || len(nodes) != 2 {
		t.Fatalf("ListNodeMetrics = %v, %v", nodes, err)
	}
}

func TestNewClientsBadConfig(t *testing.T) {
	// A malformed host makes the core clientset constructor fail.
	_, err := NewClients(&restConfigWithBadHost)
	if err == nil || !strings.Contains(err.Error(), "core client") {
		t.Fatalf("NewClients(bad host) err = %v, want a 'core client' error", err)
	}
}

var restConfigWithBadHost = rest.Config{Host: "http://[::1"}
