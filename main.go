package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/warbletech/heron-ops/internal/config"
	"github.com/warbletech/heron-ops/internal/mcp"
	"github.com/warbletech/heron-ops/internal/metrics"
)

// version is overridden at release time by goreleaser via -ldflags "-X main.version=...".
var version = "0.1.0"

func main() {
	var (
		mode        = flag.String("mode", "readonly", "Server mode: readonly | readwrite")
		transport   = flag.String("transport", "stdio", "MCP transport: stdio | http")
		addr        = flag.String("addr", ":8811", "HTTP listen address (only used when transport=http)")
		kubeconfig  = flag.String("kubeconfig", "", "Path to kubeconfig file (defaults to in-cluster config)")
		namespace   = flag.String("namespace", "", "Restrict to a single namespace (empty = all namespaces)")
		metricsAddr = flag.String("metrics-addr", os.Getenv("HERON_OPS_METRICS_ADDR"), "Optional separate listen address for GET /metrics, e.g. 127.0.0.1:9090 (env HERON_OPS_METRICS_ADDR; empty = off). With transport=http, /metrics is also served on --addr")
		ver         = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	if *ver {
		fmt.Printf("heron-ops %s (binary alias: steadyhelm)\n", version)
		os.Exit(0)
	}

	log.Printf("[heron-ops] (legacy binary name: steadyhelm) version=%s mode=%s transport=%s", version, *mode, *transport)

	cfg := config.New(*kubeconfig, *namespace, *mode)

	m, err := metrics.New(metrics.NewRegistry())
	if err != nil {
		log.Fatalf("[heron-ops] failed to initialise metrics: %v", err)
	}
	m.SetBuildInfo(version, string(cfg.Mode))

	srv, err := mcp.NewServer(cfg, mcp.WithMetrics(m))
	if err != nil {
		log.Fatalf("[heron-ops] failed to initialise server: %v", err)
	}

	if *metricsAddr != "" {
		// Logs go to stderr; stdout stays reserved for the stdio transport.
		log.Printf("[heron-ops] serving metrics on %s/metrics", *metricsAddr)
		go func() {
			if err := metrics.ListenAndServe(*metricsAddr, m); err != nil {
				log.Printf("[heron-ops] metrics listener error: %v", err)
			}
		}()
	}

	switch *transport {
	case "stdio":
		log.Println("[heron-ops] listening on stdio")
		if err := srv.ServeStdio(); err != nil {
			log.Fatalf("[heron-ops] stdio error: %v", err)
		}
	case "http":
		log.Printf("[heron-ops] listening on %s", *addr)
		if err := srv.ServeHTTP(*addr); err != nil {
			log.Fatalf("[heron-ops] http error: %v", err)
		}
	default:
		log.Fatalf("[heron-ops] unknown transport %q, use stdio or http", *transport)
	}
}
