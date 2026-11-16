package k8s

import (
	"context"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

func TestNewClientsNilConfig(t *testing.T) {
	_, err := NewClients(nil)
	if err == nil || !strings.Contains(err.Error(), "no Kubernetes config") {
		t.Fatalf("NewClients(nil) err = %v, want a 'no Kubernetes config' error", err)
	}
}

func TestNewClientsDoesNotDial(t *testing.T) {
	// Building clients must not contact the API server.
	c, err := NewClients(&rest.Config{Host: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Core == nil {
		t.Fatal("Core client is nil")
	}
}

func TestReadsAndDeleteWithFakeClient(t *testing.T) {
	ctx := context.Background()
	c := &Clients{Core: fake.NewSimpleClientset(
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "demo"}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "demo", Labels: map[string]string{"app": "a"}}},
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "demo", Labels: map[string]string{"app": "b"}}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "demo"}, Data: map[string]string{"k": "v"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "d", Namespace: "demo"}},
	)}

	ns, err := c.ListNamespaces(ctx)
	if err != nil || len(ns) != 1 {
		t.Fatalf("ListNamespaces = %d, %v", len(ns), err)
	}
	pods, err := c.ListPods(ctx, "demo", "app=a")
	if err != nil || len(pods) != 1 || pods[0].Name != "a" {
		t.Fatalf("ListPods(app=a) = %v, %v", pods, err)
	}
	if _, err := c.GetPod(ctx, "demo", "missing"); err == nil || !strings.Contains(err.Error(), "demo/missing") {
		t.Fatalf("GetPod(missing) err = %v", err)
	}
	cm, err := c.GetConfigMap(ctx, "demo", "cm")
	if err != nil || cm.Data["k"] != "v" {
		t.Fatalf("GetConfigMap = %v, %v", cm, err)
	}
	deps, err := c.ListDeployments(ctx, "demo")
	if err != nil || len(deps) != 1 {
		t.Fatalf("ListDeployments = %d, %v", len(deps), err)
	}
	if err := c.DeletePod(ctx, "demo", "a"); err != nil {
		t.Fatal(err)
	}
	if pods, _ := c.ListPods(ctx, "demo", ""); len(pods) != 1 {
		t.Fatalf("after DeletePod, %d pods left, want 1", len(pods))
	}
}

func TestMetricsUnavailable(t *testing.T) {
	c := &Clients{Core: fake.NewSimpleClientset()}
	if _, err := c.ListPodMetrics(context.Background(), ""); err == nil {
		t.Fatal("ListPodMetrics without metrics client should error")
	}
	if _, err := c.ListNodeMetrics(context.Background()); err == nil {
		t.Fatal("ListNodeMetrics without metrics client should error")
	}
}
