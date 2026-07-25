package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/warblecloud/steadyhelm/internal/config"
	"github.com/warblecloud/steadyhelm/internal/mcp"
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
		log.Fatalf("[steadyhelm] failed to initialise server: %v", err)
	}

	switch *transport {
	case "stdio":
		log.Println("[steadyhelm] listening on stdio")
		if err := srv.ServeStdio(); err != nil {
			log.Fatalf("[steadyhelm] stdio error: %v", err)
		}
	case "http":
		log.Printf("[steadyhelm] listening on %s", *addr)
		if err := srv.ServeHTTP(*addr); err != nil {
			log.Fatalf("[steadyhelm] http error: %v", err)
		}
	default:
		log.Fatalf("[steadyhelm] unknown transport %q, use stdio or http", *transport)
	}
}
