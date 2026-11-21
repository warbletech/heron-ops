package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/warbletech/heron-ops/internal/config"
	"github.com/warbletech/heron-ops/internal/k8s"
)

func newTestServer(mode config.Mode, objs ...runtime.Object) *Server {
	return &Server{
		cfg:     &config.Config{Mode: mode},
		clients: &k8s.Clients{Core: fake.NewSimpleClientset(objs...)},
	}
}

func rpc(t *testing.T, s *Server, req string) Response {
	t.Helper()
	return s.handle(context.Background(), []byte(req), TransportStdio)
}

func toolNames(t *testing.T, resp Response) map[string]bool {
	t.Helper()
	res, ok := resp.Result.(ToolsListResult)
	if !ok {
		t.Fatalf("tools/list result type %T", resp.Result)
	}
	names := map[string]bool{}
	for _, tl := range res.Tools {
		if tl.Name == "" || tl.Description == "" || tl.InputSchema["type"] != "object" {
			t.Errorf("tool %+v is missing a name, description or object schema", tl)
		}
		names[tl.Name] = true
	}
	return names
}

func text(r ToolCallResult) string {
	var b strings.Builder
	for _, c := range r.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

func TestInitialize(t *testing.T) {
	resp := rpc(t, newTestServer(config.ModeReadOnly), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`)
	if resp.Error != nil {
		t.Fatalf("initialize error: %+v", resp.Error)
	}
	res := resp.Result.(InitializeResult)
	if res.ServerInfo.Name != "heron-ops" || res.ProtocolVersion == "" {
		t.Fatalf("unexpected initialize result %+v", res)
	}
	if string(resp.ID) != "1" {
		t.Fatalf("id = %s, want 1", resp.ID)
	}
}

func TestProtocolErrors(t *testing.T) {
	s := newTestServer(config.ModeReadOnly)
	if r := rpc(t, s, `{not json`); r.Error == nil || r.Error.Code != -32700 {
		t.Errorf("parse error: got %+v", r.Error)
	}
	if r := rpc(t, s, `{"jsonrpc":"2.0","id":2,"method":"nope"}`); r.Error == nil || r.Error.Code != -32601 {
		t.Errorf("unknown method: got %+v", r.Error)
	}
	if r := rpc(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":"bad"}`); r.Error == nil || r.Error.Code != -32602 {
		t.Errorf("invalid params: got %+v", r.Error)
	}
	if r := rpc(t, s, `{"jsonrpc":"2.0","id":4,"method":"ping"}`); r.Error != nil {
		t.Errorf("ping: got %+v", r.Error)
	}
}

func TestToolsListHidesWriteToolsInReadOnly(t *testing.T) {
	ro := toolNames(t, rpc(t, newTestServer(config.ModeReadOnly), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rw := toolNames(t, rpc(t, newTestServer(config.ModeReadWrite), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	for _, w := range []string{"scale_deployment", "delete_pod"} {
		if ro[w] {
			t.Errorf("readonly tools/list exposes write tool %q", w)
		}
		if !rw[w] {
			t.Errorf("readwrite tools/list is missing %q", w)
		}
	}
	for _, r := range []string{"list_namespaces", "list_pods", "get_pod", "get_pod_logs", "cluster_summary", "list_events"} {
		if !ro[r] {
			t.Errorf("readonly tools/list is missing read tool %q", r)
		}
	}
}

func TestWriteToolsBlockedInReadOnly(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "demo"}}
	s := newTestServer(config.ModeReadOnly, pod)
	for _, name := range []string{"delete_pod", "scale_deployment"} {
		r := s.CallTool(context.Background(), name, map[string]any{"namespace": "demo", "name": "p", "replicas": 3})
		if !r.IsError || !strings.Contains(text(r), "requires readwrite mode") {
			t.Errorf("%s in readonly: got %+v", name, r)
		}
	}
	if _, err := s.clients.GetPod(context.Background(), "demo", "p"); err != nil {
		t.Fatalf("pod was deleted in readonly mode: %v", err)
	}
}

func TestDeletePodInReadWrite(t *testing.T) {
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "demo"}}
	s := newTestServer(config.ModeReadWrite, pod)
	r := s.CallTool(context.Background(), "delete_pod", map[string]any{"namespace": "demo", "name": "p"})
	if r.IsError {
		t.Fatalf("delete_pod: %s", text(r))
	}
	if _, err := s.clients.GetPod(context.Background(), "demo", "p"); err == nil {
		t.Fatal("pod still exists after delete_pod in readwrite mode")
	}
}

func crashLoopingPod() *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "memory-hog-1", Namespace: "demo"},
		Spec:       corev1.PodSpec{NodeName: "node-1"},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:         "worker",
				RestartCount: 4,
				State:        corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137},
				},
			}},
		},
	}
}

func TestListPodsAndGetPod(t *testing.T) {
	s := newTestServer(config.ModeReadOnly, crashLoopingPod())
	ctx := context.Background()

	out := text(s.CallTool(ctx, "list_pods", map[string]any{"namespace": "demo"}))
	if !strings.Contains(out, "PODS (1)") || !strings.Contains(out, "memory-hog-1") || !strings.Contains(out, "0/1") {
		t.Errorf("list_pods output:\n%s", out)
	}

	r := s.CallTool(ctx, "get_pod", map[string]any{"namespace": "demo", "name": "memory-hog-1"})
	out = text(r)
	if r.IsError {
		t.Fatalf("get_pod: %s", out)
	}
	for _, want := range []string{"Waiting(CrashLoopBackOff)", "restarts=4", "lastState=Terminated(OOMKilled)", "exitCode=137"} {
		if !strings.Contains(out, want) {
			t.Errorf("get_pod output missing %q:\n%s", want, out)
		}
	}

	if r := s.CallTool(ctx, "get_pod", map[string]any{"namespace": "demo", "name": "nope"}); !r.IsError {
		t.Errorf("get_pod for a missing pod should be an error, got %s", text(r))
	}
}

func TestDefaultNamespaceFromConfig(t *testing.T) {
	s := newTestServer(config.ModeReadOnly, crashLoopingPod(),
		&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "kube-system"}})
	s.cfg.Namespace = "demo"
	out := text(s.CallTool(context.Background(), "list_pods", nil))
	if !strings.Contains(out, "PODS (1)") || strings.Contains(out, "other") {
		t.Errorf("list_pods should default to the configured namespace:\n%s", out)
	}
}

func TestListHPAsWithoutMinReplicas(t *testing.T) {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "demo"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: "api"},
			MaxReplicas:    5, // MinReplicas deliberately nil
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{CurrentReplicas: 5, DesiredReplicas: 5},
	}
	s := newTestServer(config.ModeReadOnly, hpa)
	r := s.CallTool(context.Background(), "list_hpas", map[string]any{"namespace": "demo"})
	if r.IsError || !strings.Contains(text(r), "Deployment/api") {
		t.Fatalf("list_hpas: %+v", r)
	}
	sum := text(s.CallTool(context.Background(), "cluster_summary", map[string]any{"namespace": "demo"}))
	if !strings.Contains(sum, "AT MAX") {
		t.Errorf("cluster_summary should flag the saturated HPA:\n%s", sum)
	}
}

func TestClusterSummary(t *testing.T) {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "node-1"},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
		}},
	}
	s := newTestServer(config.ModeReadOnly, node, crashLoopingPod())
	out := text(s.CallTool(context.Background(), "cluster_summary", nil))
	for _, want := range []string{"NODES: 1 total, 1 Ready, 0 NotReady", "POD PHASES:", "Running", "WARNING EVENTS"} {
		if !strings.Contains(out, want) {
			t.Errorf("cluster_summary missing %q:\n%s", want, out)
		}
	}
}

func TestMetricsToolsWithoutMetricsServer(t *testing.T) {
	s := newTestServer(config.ModeReadOnly)
	for _, name := range []string{"list_pod_metrics", "list_node_metrics"} {
		r := s.CallTool(context.Background(), name, nil)
		if !r.IsError || !strings.Contains(text(r), "metrics-server not available") {
			t.Errorf("%s without metrics-server: %+v", name, r)
		}
	}
}

func TestUnknownTool(t *testing.T) {
	r := newTestServer(config.ModeReadWrite).CallTool(context.Background(), "rm_rf", nil)
	if !r.IsError || !strings.Contains(text(r), "unknown tool") {
		t.Fatalf("unknown tool: %+v", r)
	}
}

func TestToolsCallOverJSONRPC(t *testing.T) {
	s := newTestServer(config.ModeReadOnly, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "demo"}})
	resp := rpc(t, s, `{"jsonrpc":"2.0","id":"x","method":"tools/call","params":{"name":"list_namespaces","arguments":{}}}`)
	b, _ := json.Marshal(resp)
	if !strings.Contains(string(b), "NAMESPACES (1)") || !strings.Contains(string(b), `"id":"x"`) {
		t.Fatalf("tools/call response: %s", b)
	}
}

func TestArgHelpers(t *testing.T) {
	args := map[string]any{"s": "v", "empty": "", "f": float64(7), "n": json.Number("9"), "bad": "x"}
	if stringArg(args, "s", "d") != "v" || stringArg(args, "empty", "d") != "d" || stringArg(args, "missing", "d") != "d" {
		t.Error("stringArg")
	}
	if int64Arg(args, "f", 1) != 7 || int64Arg(args, "n", 1) != 9 || int64Arg(args, "bad", 1) != 1 {
		t.Error("int64Arg")
	}
}
