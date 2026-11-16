package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	k8stesting "k8s.io/client-go/testing"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"

	"github.com/warbletech/heron-ops/internal/config"
	"github.com/warbletech/heron-ops/internal/k8s"
)

func int32Ptr(v int32) *int32 { return &v }

// fixtureServer is a readwrite server backed by fake core and metrics
// clientsets holding one of every object the read tools render.
func fixtureServer(t *testing.T) *Server {
	t.Helper()
	objs := []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "demo"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "gone"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating}},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "cp-1", Labels: map[string]string{"node-role.kubernetes.io/control-plane": ""}},
			Status: corev1.NodeStatus{
				Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
				NodeInfo:   corev1.NodeSystemInfo{KubeletVersion: "v1.29.3", OSImage: "Ubuntu 22.04"},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "worker-1"},
			Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse},
				{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
			}},
		},
		&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "new-1"}},
		crashLoopingPod(),
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "demo"},
			Status: corev1.PodStatus{
				Phase:      corev1.PodRunning,
				PodIP:      "10.0.0.7",
				Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}},
				ContainerStatuses: []corev1.ContainerStatus{
					{Name: "app", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
					{Name: "init-done", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Completed"}}},
					{Name: "mystery"},
				},
			},
		},
		&corev1.Event{
			ObjectMeta:     metav1.ObjectMeta{Name: "e1", Namespace: "demo"},
			Type:           "Warning",
			Reason:         "BackOff",
			InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "memory-hog-1", Namespace: "demo"},
			Message:        strings.Repeat("x", 120),
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "demo"},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP, ClusterIP: "10.96.0.10",
				Ports: []corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolTCP}, {Port: 53, Protocol: corev1.ProtocolUDP}},
			},
		},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "app-config", Namespace: "demo"}, Data: map[string]string{"LOG_LEVEL": "debug"}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "demo"}, Spec: appsv1.DeploymentSpec{Replicas: int32Ptr(2)}},
		&autoscalingv2.HorizontalPodAutoscaler{
			ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "demo"},
			Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
				ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "web"},
				MinReplicas:    int32Ptr(2), MaxReplicas: 10,
			},
			Status: autoscalingv2.HorizontalPodAutoscalerStatus{CurrentReplicas: 3, DesiredReplicas: 4},
		},
	}
	core := fake.NewSimpleClientset(objs...)
	// The fake object tracker has no scale subresource; emulate it.
	core.PrependReactor("get", "deployments", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "scale" {
			return false, nil, nil
		}
		name := a.(k8stesting.GetAction).GetName()
		if name != "web" {
			return true, nil, fmt.Errorf("deployments.apps %q not found", name)
		}
		return true, &autoscalingv1.Scale{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: a.GetNamespace()}}, nil
	})
	core.PrependReactor("update", "deployments", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetSubresource() != "scale" {
			return false, nil, nil
		}
		return true, a.(k8stesting.UpdateAction).GetObject(), nil
	})

	metrics := metricsfake.NewSimpleClientset()
	metrics.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &metricsv1beta1.PodMetricsList{Items: []metricsv1beta1.PodMetrics{{
			ObjectMeta: metav1.ObjectMeta{Name: "web-1", Namespace: "demo"},
			Containers: []metricsv1beta1.ContainerMetrics{{
				Name: "app",
				Usage: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("250m"),
					corev1.ResourceMemory: resource.MustParse("64Mi"),
				},
			}},
		}}}, nil
	})
	metrics.PrependReactor("list", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &metricsv1beta1.NodeMetricsList{Items: []metricsv1beta1.NodeMetrics{{
			ObjectMeta: metav1.ObjectMeta{Name: "cp-1"},
			Usage: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		}}}, nil
	})
	return &Server{
		cfg:     &config.Config{Mode: config.ModeReadWrite},
		clients: &k8s.Clients{Core: core, Metrics: metrics},
	}
}

func TestReadAndWriteTools(t *testing.T) {
	s := fixtureServer(t)
	for _, tc := range []struct {
		tool    string
		args    map[string]any
		want    []string
		notWant []string
	}{
		{"list_namespaces", nil, []string{"NAMESPACES (2)", "demo", "Active", "Terminating"}, nil},
		{"list_pods", map[string]any{"namespace": "demo"}, []string{"PODS (2)", "web-1", "1/3"}, nil},
		{"get_pod", map[string]any{"namespace": "demo", "name": "web-1"}, []string{
			"Pod: demo/web-1", "IP:     10.0.0.7", "Ready", "state=Running",
			"state=Terminated(Completed)", "state=Unknown",
		}, []string{"lastState"}},
		{"get_pod_logs", map[string]any{"namespace": "demo", "name": "web-1", "container": "app", "tail_lines": float64(5)}, []string{"fake logs"}, nil},
		{"list_nodes", nil, []string{
			"NODES (3)", "cp-1", "control-plane", "v1.29.3", "Ubuntu 22.04",
			"worker-1", "NotReady", "<none>", "new-1", "Unknown",
		}, nil},
		{"list_events", map[string]any{"namespace": "demo"}, []string{"EVENTS (1)", "BackOff", "Pod/memory-hog-1", strings.Repeat("x", 77) + "..."}, []string{strings.Repeat("x", 78)}},
		{"list_deployments", map[string]any{"namespace": "demo"}, []string{`"name": "web"`, `"replicas": 2`}, nil},
		{"list_hpas", map[string]any{"namespace": "demo"}, []string{"HPAs (1)", "Deployment/web", "2 ", "10 "}, nil},
		{"list_pod_metrics", map[string]any{"namespace": "demo"}, []string{"web-1", "250m", "64Mi"}, nil},
		{"list_node_metrics", nil, []string{"cp-1", "2Gi"}, nil},
		{"list_services", map[string]any{"namespace": "demo"}, []string{"web", "ClusterIP", "10.96.0.10", "80/TCP,53/UDP"}, nil},
		{"get_configmap", map[string]any{"namespace": "demo", "name": "app-config"}, []string{"ConfigMap: demo/app-config", "=== LOG_LEVEL ===", "debug"}, nil},
		{"scale_deployment", map[string]any{"namespace": "demo", "name": "web", "replicas": float64(5)}, []string{"Scaled demo/web to 5 replicas"}, nil},
		{"scale_deployment", map[string]any{"namespace": "demo", "name": "web"}, []string{"Scaled demo/web to 1 replicas"}, nil},
		{"delete_pod", map[string]any{"namespace": "demo", "name": "web-1"}, []string{"Deleted pod demo/web-1"}, nil},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			r := s.CallTool(context.Background(), tc.tool, tc.args)
			out := text(r)
			if r.IsError {
				t.Fatalf("%s returned an error: %s", tc.tool, out)
			}
			if len(r.Content) != 1 || r.Content[0].Type != "text" {
				t.Fatalf("%s content = %+v, want one text block", tc.tool, r.Content)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("%s output missing %q:\n%s", tc.tool, w, out)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(out, w) {
					t.Errorf("%s output should not contain %q:\n%s", tc.tool, w, out)
				}
			}
		})
	}
}

func TestToolErrorsBecomeErrorResults(t *testing.T) {
	core := fake.NewSimpleClientset()
	core.PrependReactor("*", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver unavailable")
	})
	metrics := metricsfake.NewSimpleClientset()
	metrics.PrependReactor("*", "*", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("apiserver unavailable")
	})
	s := &Server{
		cfg:     &config.Config{Mode: config.ModeReadWrite},
		clients: &k8s.Clients{Core: core, Metrics: metrics},
	}
	for _, d := range toolDefs(string(config.ModeReadWrite)) {
		switch d.Name {
		case "cluster_summary":
			continue // degrades gracefully instead, see below
		case "get_pod_logs":
			continue // the fake clientset serves logs via a fake REST client that ignores reactors
		}
		t.Run(d.Name, func(t *testing.T) {
			r := s.CallTool(context.Background(), d.Name, map[string]any{"namespace": "demo", "name": "x"})
			if !r.IsError || !strings.Contains(text(r), "apiserver unavailable") {
				t.Fatalf("%s: got %+v, want an error result carrying the API error", d.Name, r)
			}
		})
	}

	// cluster_summary skips sections it cannot read rather than failing.
	r := s.CallTool(context.Background(), "cluster_summary", nil)
	if r.IsError {
		t.Fatalf("cluster_summary should not fail outright: %s", text(r))
	}
	if out := text(r); out != "" {
		t.Errorf("cluster_summary with every API call failing = %q, want empty", out)
	}
}

func TestClusterSummaryTruncatesWarnings(t *testing.T) {
	var objs []runtime.Object
	for i := 0; i < 13; i++ {
		objs = append(objs, &corev1.Event{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("e%02d", i), Namespace: "demo"},
			Type:       "Warning", Reason: "BackOff",
		})
	}
	objs = append(objs, &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "demo"},
		Spec:       autoscalingv2.HorizontalPodAutoscalerSpec{MaxReplicas: 5},
		Status:     autoscalingv2.HorizontalPodAutoscalerStatus{CurrentReplicas: 2},
	})
	s := newTestServer(config.ModeReadOnly, objs...)
	out := text(s.CallTool(context.Background(), "cluster_summary", map[string]any{"namespace": "demo"}))
	for _, want := range []string{"WARNING EVENTS (13)", "... and 3 more", "demo/api current=2 max=5"} {
		if !strings.Contains(out, want) {
			t.Errorf("cluster_summary missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "AT MAX") {
		t.Errorf("HPA below max must not be flagged:\n%s", out)
	}
}

// Every advertised tool must be routed by dispatch, and each schema's
// required fields must be declared properties.
func TestToolRegistration(t *testing.T) {
	s := newTestServer(config.ModeReadWrite)
	for _, tc := range []struct {
		mode      string
		wantCount int
		wantWrite bool
	}{
		{"readonly", 13, false},
		{"", 13, false},
		{"bogus", 13, false},
		{"readwrite", 15, true},
	} {
		t.Run("mode="+tc.mode, func(t *testing.T) {
			defs := toolDefs(tc.mode)
			if len(defs) != tc.wantCount {
				t.Fatalf("toolDefs(%q) has %d tools, want %d", tc.mode, len(defs), tc.wantCount)
			}
			seen := map[string]bool{}
			for _, d := range defs {
				if seen[d.Name] {
					t.Errorf("duplicate tool %q", d.Name)
				}
				seen[d.Name] = true
				props, _ := d.InputSchema["properties"].(map[string]any)
				if req, ok := d.InputSchema["required"].([]string); ok {
					for _, r := range req {
						if _, ok := props[r]; !ok {
							t.Errorf("%s: required field %q is not a declared property", d.Name, r)
						}
					}
				}
				r := s.CallTool(context.Background(), d.Name, map[string]any{"namespace": "demo", "name": "x"})
				if strings.Contains(text(r), "unknown tool") {
					t.Errorf("advertised tool %q is not handled by dispatch", d.Name)
				}
			}
			if seen["scale_deployment"] != tc.wantWrite || seen["delete_pod"] != tc.wantWrite {
				t.Errorf("write tools present = %v, want %v", seen["scale_deployment"], tc.wantWrite)
			}
		})
	}
}

func TestToolNamesFollowsMode(t *testing.T) {
	for mode, want := range map[config.Mode]int{config.ModeReadOnly: 13, config.ModeReadWrite: 15} {
		if got := len(newTestServer(mode).ToolNames()); got != want {
			t.Errorf("ToolNames() in %s = %d tools, want %d", mode, got, want)
		}
	}
}

func TestNewServer(t *testing.T) {
	if _, err := NewServer(&config.Config{}); err == nil || !strings.Contains(err.Error(), "no Kubernetes config") {
		t.Fatalf("NewServer without RESTConfig err = %v", err)
	}
	s, err := NewServer(&config.Config{Mode: config.ModeReadOnly, RESTConfig: &rest.Config{Host: "http://127.0.0.1:1"}})
	if err != nil {
		t.Fatal(err)
	}
	if s.clients == nil || s.clients.Core == nil || s.cfg.Mode != config.ModeReadOnly {
		t.Fatalf("NewServer returned an incomplete server: %+v", s)
	}
}

func TestInt64ArgTypes(t *testing.T) {
	for _, tc := range []struct {
		name string
		val  any
		want int64
	}{
		{"float64", float64(3), 3},
		{"int64", int64(4), 4},
		{"json.Number", jsonNumber("5"), 5},
		{"bad json.Number", jsonNumber("5.5"), 42},
		{"string", "6", 42},
		{"int", 7, 42}, // only JSON-decoded numeric types are accepted
		{"nil", nil, 42},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := int64Arg(map[string]any{"k": tc.val}, "k", 42); got != tc.want {
				t.Fatalf("int64Arg(%v) = %d, want %d", tc.val, got, tc.want)
			}
		})
	}
}

func jsonNumber(s string) any { return json.Number(s) }
