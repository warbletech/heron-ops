package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewResolutionOrder covers each step of the kubeconfig lookup:
// flag > KUBECONFIG > ~/.kube/config > in-cluster.
func TestNewResolutionOrder(t *testing.T) {
	homeWithConfig := func(t *testing.T) string {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".kube"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, ".kube", "config"), []byte(testKubeconfig), 0o600); err != nil {
			t.Fatal(err)
		}
		return home
	}
	// os.UserHomeDir reads HOME on Unix and USERPROFILE on Windows.
	setUserHome := func(t *testing.T, home string) {
		t.Helper()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
	}
	for _, tc := range []struct {
		name     string
		flag     func(t *testing.T) string
		env      func(t *testing.T) string
		home     func(t *testing.T) string
		wantREST bool
	}{
		{
			name:     "flag wins over a broken KUBECONFIG",
			flag:     writeKubeconfig,
			env:      func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") },
			home:     func(t *testing.T) string { return t.TempDir() },
			wantREST: true,
		},
		{
			name:     "KUBECONFIG wins over home",
			env:      func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") },
			home:     homeWithConfig,
			wantREST: false,
		},
		{
			name:     "falls back to ~/.kube/config",
			home:     homeWithConfig,
			wantREST: true,
		},
		{
			name:     "falls back to in-cluster, which fails outside a pod",
			home:     func(t *testing.T) string { return t.TempDir() },
			wantREST: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KUBERNETES_SERVICE_HOST", "")
			t.Setenv("KUBERNETES_SERVICE_PORT", "")
			flag, env := "", ""
			if tc.flag != nil {
				flag = tc.flag(t)
			}
			if tc.env != nil {
				env = tc.env(t)
			}
			t.Setenv("KUBECONFIG", env)
			setUserHome(t, tc.home(t))
			cfg := New(flag, "", "")
			if got := cfg.RESTConfig != nil; got != tc.wantREST {
				t.Fatalf("RESTConfig != nil is %v, want %v", got, tc.wantREST)
			}
			if cfg.Mode != ModeReadOnly {
				t.Fatalf("Mode = %q, want readonly", cfg.Mode)
			}
		})
	}
}
