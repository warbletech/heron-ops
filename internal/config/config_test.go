package config

import (
	"os"
	"path/filepath"
	"testing"
)

const testKubeconfig = `apiVersion: v1
kind: Config
clusters:
- name: test
  cluster:
    server: https://127.0.0.1:6443
contexts:
- name: test
  context:
    cluster: test
    user: test
current-context: test
users:
- name: test
  user:
    token: not-a-real-token
`

func writeKubeconfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(p, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewModeDefaultsToReadOnly(t *testing.T) {
	kc := writeKubeconfig(t)
	for _, tc := range []struct {
		in   string
		want Mode
	}{
		{"", ModeReadOnly},
		{"readonly", ModeReadOnly},
		{"bogus", ModeReadOnly},
		{"READWRITE", ModeReadOnly}, // exact match only: never widen on a typo
		{"readwrite", ModeReadWrite},
	} {
		if got := New(kc, "", tc.in).Mode; got != tc.want {
			t.Errorf("New(mode=%q).Mode = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNewExplicitKubeconfig(t *testing.T) {
	cfg := New(writeKubeconfig(t), "demo", "readonly")
	if cfg.RESTConfig == nil {
		t.Fatal("RESTConfig is nil for a valid kubeconfig")
	}
	if cfg.RESTConfig.Host != "https://127.0.0.1:6443" {
		t.Errorf("Host = %q", cfg.RESTConfig.Host)
	}
	if cfg.Namespace != "demo" {
		t.Errorf("Namespace = %q, want demo", cfg.Namespace)
	}
}

func TestNewKubeconfigFromEnv(t *testing.T) {
	t.Setenv("KUBECONFIG", writeKubeconfig(t))
	if cfg := New("", "", ""); cfg.RESTConfig == nil {
		t.Fatal("RESTConfig is nil when KUBECONFIG points at a valid file")
	}
}

func TestNewBadKubeconfigDoesNotPanic(t *testing.T) {
	cfg := New(filepath.Join(t.TempDir(), "missing"), "", "readonly")
	if cfg.RESTConfig != nil {
		t.Fatal("expected nil RESTConfig for a missing kubeconfig")
	}
}
