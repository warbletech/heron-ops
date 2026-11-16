package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/warbletech/heron-ops/internal/config"
	"github.com/warbletech/heron-ops/internal/mcp"
)

const kubeconfig = `apiVersion: v1
kind: Config
clusters: [{name: t, cluster: {server: "http://127.0.0.1:1"}}]
contexts: [{name: t, context: {cluster: t, user: t}}]
current-context: t
users: [{name: t, user: {token: x}}]
`

func newServer(t *testing.T, mode string) *Server {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(p, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := New(Options{Kubeconfig: p, Mode: mode})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewDefaultsToReadOnly(t *testing.T) {
	if m := newServer(t, "").Mode(); m != ModeReadOnly {
		t.Fatalf("Mode() = %q, want readonly", m)
	}
	if m := newServer(t, "readwrite").Mode(); m != ModeReadWrite {
		t.Fatalf("Mode() = %q, want readwrite", m)
	}
}

func TestNewWithoutKubeconfigFails(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("HOME", t.TempDir())
	if _, err := New(Options{}); err == nil {
		t.Fatal("New without any Kubernetes config should fail")
	}
}

// IsClusterTool must stay in sync with the tools Heron Ops actually serves,
// otherwise Starling Lite routes calls to the wrong backend.
func TestIsClusterToolMatchesToolDefs(t *testing.T) {
	cfg := config.New(writeTmp(t), "", "readwrite")
	inner, err := mcp.NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	served := map[string]bool{}
	for _, d := range inner.ToolNames() {
		served[d.Name] = true
		if !IsClusterTool(d.Name) {
			t.Errorf("IsClusterTool(%q) = false, but Heron Ops serves it", d.Name)
		}
	}
	for _, name := range []string{"scan_manifest", "propose_remediation", ""} {
		if IsClusterTool(name) {
			t.Errorf("IsClusterTool(%q) = true, want false", name)
		}
	}
	if len(served) == 0 {
		t.Fatal("no tools served")
	}
}

func writeTmp(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(p, []byte(kubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
