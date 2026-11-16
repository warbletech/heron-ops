package main

import (
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testKubeconfig = `apiVersion: v1
kind: Config
clusters: [{name: t, cluster: {server: "http://127.0.0.1:1"}}]
contexts: [{name: t, context: {cluster: t, user: t}}]
current-context: t
users: [{name: t, user: {token: x}}]
`

func writeKubeconfig(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(p, []byte(testKubeconfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestMainHTTPTransport runs main in-process with --transport=http and checks
// the server answers on the requested address.
func TestMainHTTPTransport(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{"heron-ops", "-transport=http", "-addr=" + addr, "-kubeconfig=" + writeKubeconfig(t), "-mode=readwrite"}
	go main() // blocks in ServeHTTP for the rest of the test binary's life

	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get("http://" + addr + "/health")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if !strings.Contains(string(b), `"status":"ok"`) {
				t.Fatalf("/health = %q", b)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("main never started serving on %s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	resp, err := http.Post("http://"+addr+"/mcp", "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `"scale_deployment"`) {
		t.Fatalf("-mode=readwrite was not applied; tools/list = %s", b)
	}
}

// The remaining paths exit the process, so run them in a child copy of the
// test binary. Only exit status and output are checked; the child does not
// contribute to the coverage profile.
func TestMainExitPaths(t *testing.T) {
	if os.Getenv("HERON_OPS_MAIN_CHILD") == "1" {
		os.Args = append([]string{"heron-ops"}, strings.Fields(os.Getenv("HERON_OPS_MAIN_ARGS"))...)
		main()
		return
	}
	kc := writeKubeconfig(t)
	for _, tc := range []struct {
		name     string
		args     string
		env      []string
		wantCode int
		want     string
	}{
		{"version", "-version", nil, 0, "heron-ops " + version},
		{"unknown transport", "-transport=carrier-pigeon -kubeconfig=" + kc, nil, 1, `unknown transport "carrier-pigeon"`},
		{"no kubernetes config", "", []string{"KUBECONFIG=" + filepath.Join(t.TempDir(), "missing"), "HOME=" + t.TempDir(), "KUBERNETES_SERVICE_HOST="}, 1, "failed to initialise server"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestMainExitPaths$")
			cmd.Env = append(os.Environ(), "HERON_OPS_MAIN_CHILD=1", "HERON_OPS_MAIN_ARGS="+tc.args)
			cmd.Env = append(cmd.Env, tc.env...)
			out, err := cmd.CombinedOutput()
			code := 0
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			} else if err != nil {
				t.Fatal(err)
			}
			if code != tc.wantCode || !strings.Contains(string(out), tc.want) {
				t.Fatalf("exit %d, output:\n%s\nwant exit %d and %q", code, out, tc.wantCode, tc.want)
			}
		})
	}
}
