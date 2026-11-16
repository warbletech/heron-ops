package mcp

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/warbletech/heron-ops/internal/config"
)

// runStdio feeds input to ServeStdio through os.Stdin/os.Stdout pipes and
// returns everything it wrote plus its return value.
func runStdio(t *testing.T, s *Server, input string) ([]string, error) {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	t.Cleanup(func() { os.Stdin, os.Stdout = oldIn, oldOut })

	done := make(chan error, 1)
	go func() { done <- s.ServeStdio() }()

	// Read concurrently so a large response cannot block on a full pipe.
	outc := make(chan []byte, 1)
	go func() { b, _ := io.ReadAll(outR); outc <- b }()

	if _, err := io.WriteString(inW, input); err != nil {
		t.Fatal(err)
	}
	inW.Close()

	var serveErr error
	select {
	case serveErr = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("ServeStdio did not return after stdin was closed")
	}
	outW.Close()
	inR.Close()
	out := <-outc
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, serveErr
}

func TestServeStdio(t *testing.T) {
	s := newTestServer(config.ModeReadOnly, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "demo"}})
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		``, // blank lines are skipped
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_namespaces","arguments":{}}}`,
		`{oops`,
		`{"jsonrpc":"2.0","id":4,"method":"ping"}`,
	}, "\n") + "\n"
	lines, err := runStdio(t, s, input)
	if err != nil {
		t.Fatalf("ServeStdio: %v", err)
	}

	type wire struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *RPCError       `json:"error"`
	}
	var got []wire
	for _, l := range lines {
		var w wire
		if err := json.Unmarshal([]byte(l), &w); err != nil {
			t.Fatalf("stdout line is not JSON: %q", l)
		}
		if w.JSONRPC != "2.0" {
			t.Errorf("stdout line is not a JSON-RPC 2.0 message: %q", l)
		}
		got = append(got, w)
	}
	// One response per request; the notification gets none.
	wantIDs := []string{"1", "2", "3", "", "4"}
	if len(got) != len(wantIDs) {
		t.Fatalf("got %d responses, want %d:\n%s", len(got), len(wantIDs), strings.Join(lines, "\n"))
	}
	for i, id := range wantIDs {
		if string(got[i].ID) != id {
			t.Errorf("response %d id = %s, want %q", i, got[i].ID, id)
		}
	}
	if !strings.Contains(string(got[0].Result), `"heron-ops"`) {
		t.Errorf("initialize result = %s", got[0].Result)
	}
	if !strings.Contains(string(got[1].Result), `"list_pods"`) {
		t.Errorf("tools/list result = %s", got[1].Result)
	}
	if !strings.Contains(string(got[2].Result), "NAMESPACES (1)") {
		t.Errorf("tools/call result = %s", got[2].Result)
	}
	if got[3].Error == nil || got[3].Error.Code != -32700 {
		t.Errorf("malformed line: error = %+v, want -32700", got[3].Error)
	}
}

func TestServeStdioLineTooLong(t *testing.T) {
	s := newTestServer(config.ModeReadOnly)
	_, err := runStdio(t, s, strings.Repeat("a", 4*1024*1024+1)+"\n")
	if !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("ServeStdio err = %v, want bufio.ErrTooLong", err)
	}
}

func TestServeStdioEmptyInput(t *testing.T) {
	lines, err := runStdio(t, newTestServer(config.ModeReadOnly), "")
	if err != nil || len(lines) != 0 {
		t.Fatalf("empty stdin: lines=%q err=%v", lines, err)
	}
}

// startHTTP runs ServeHTTP on a free loopback port and waits until it is up.
func startHTTP(t *testing.T, s *Server) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	go func() { _ = s.ServeHTTP(addr) }() // ServeHTTP has no shutdown hook; it ends with the test binary
	base := "http://" + addr
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Get(base + "/health")
		if err == nil {
			resp.Body.Close()
			return base
		}
		if time.Now().After(deadline) {
			t.Fatalf("ServeHTTP never came up on %s: %v", addr, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestServeHTTP(t *testing.T) {
	s := newTestServer(config.ModeReadOnly, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "demo"}})
	base := startHTTP(t, s)

	for _, tc := range []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
		wantCT     string
		wantBody   []string
	}{
		{"health", http.MethodGet, "/health", "", 200, "", []string{`"status":"ok"`, `"server":"heron-ops"`}},
		{"GET /mcp rejected", http.MethodGet, "/mcp", "", 405, "", []string{"POST only"}},
		{"bad JSON", http.MethodPost, "/mcp", "{nope", 400, "", []string{"bad JSON"}},
		{"initialize", http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":1,"method":"initialize"}`, 200, "application/json", []string{`"protocolVersion":"2024-11-05"`, `"id":1`}},
		{"tools/call", http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":"a","method":"tools/call","params":{"name":"list_namespaces"}}`, 200, "application/json", []string{"NAMESPACES (1)", `"id":"a"`}},
		{"write tool blocked", http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"delete_pod","arguments":{"name":"x"}}}`, 200, "application/json", []string{"requires readwrite mode", `"isError":true`}},
		{"unknown method", http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":3,"method":"nope"}`, 200, "application/json", []string{`"code":-32601`}},
		{"unknown path", http.MethodGet, "/nope", "", 404, "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, base+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", resp.StatusCode, tc.wantStatus, b)
			}
			if tc.wantCT != "" && resp.Header.Get("Content-Type") != tc.wantCT {
				t.Errorf("Content-Type = %q, want %q", resp.Header.Get("Content-Type"), tc.wantCT)
			}
			for _, w := range tc.wantBody {
				if !strings.Contains(string(b), w) {
					t.Errorf("body missing %q: %s", w, b)
				}
			}
		})
	}
}

func TestServeHTTPNotificationHasNoBody(t *testing.T) {
	base := startHTTP(t, newTestServer(config.ModeReadOnly))
	resp, err := http.Post(base+"/mcp", "application/json", strings.NewReader(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusAccepted || len(b) != 0 {
		t.Fatalf("notification: status=%d body=%q, want 202 with no body", resp.StatusCode, b)
	}
}

func TestServeHTTPListenError(t *testing.T) {
	if err := newTestServer(config.ModeReadOnly).ServeHTTP("127.0.0.1:-1"); err == nil {
		t.Fatal("ServeHTTP on an invalid address should return an error")
	}
}
