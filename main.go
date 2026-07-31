package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/warble-tech/heron-ops/internal/config"
	"github.com/warble-tech/heron-ops/internal/mcp"
)

const version = "0.1.0"

func main() {
	var (
		mode       = flag.String("mode", "readonly", "Server mode: readonly | readwrite")
		transport  = flag.String("transport", "stdio", "MCP transport: stdio | http")
		addr       = flag.String("addr", ":8811", "HTTP listen address (only used when transport=http)")
		kubeconfig = flag.String("kubeconfig", "", "Path to kubeconfig file (defaults to in-cluster config)")
		namespace  = flag.String("namespace", "", "Restrict to a single namespace (empty = all namespaces)")
		ver        = flag.Bool("version", false, "Print version and exit")
	)
	flag.Parse()

	if *ver {
		fmt.Printf("heron-ops %s (binary alias: steadyhelm)\n", version)
		os.Exit(0)
	}

	log.Printf("[heron-ops] (legacy binary name: steadyhelm) version=%s mode=%s transport=%s", version, *mode, *transport)

	cfg := config.New(*kubeconfig, *namespace, *mode)

	srv, err := mcp.NewServer(cfg)
	if err != nil {
		log.Fatalf("[heron-ops] failed to initialise server: %v", err)
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
