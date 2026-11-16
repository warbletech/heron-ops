package mcpserver

import (
	"context"
	"strings"
	"testing"
)

// These cases are answered before any Kubernetes API call, so the
// unreachable test cluster is never dialled.
func TestCallTool(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    string
		tool    string
		args    map[string]any
		wantErr bool
		want    string
	}{
		{"unknown tool", "readwrite", "scan_manifest", nil, true, `unknown tool: "scan_manifest"`},
		{"write tool in readonly", "readonly", "scale_deployment", map[string]any{"name": "web", "replicas": float64(3)}, true, "requires readwrite mode"},
		{"delete in default mode", "", "delete_pod", map[string]any{"name": "web-1"}, true, "requires readwrite mode"},
		{"metrics tool without metrics-server reachable", "readonly", "list_node_metrics", nil, true, "node metrics"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // anything that does reach the network fails fast
			text, isErr := newServer(t, tc.mode).CallTool(ctx, tc.tool, tc.args)
			if isErr != tc.wantErr {
				t.Fatalf("isError = %v, want %v (text %q)", isErr, tc.wantErr, text)
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("text = %q, want it to contain %q", text, tc.want)
			}
		})
	}
}

func TestStringsBuilder(t *testing.T) {
	var b stringsBuilder
	if b.String() != "" {
		t.Fatal("zero stringsBuilder is not empty")
	}
	b.WriteString("a")
	b.WriteString("")
	b.WriteString("bc")
	if got := b.String(); got != "abc" {
		t.Fatalf("String() = %q, want abc", got)
	}
}
