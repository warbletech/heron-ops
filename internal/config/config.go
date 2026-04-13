package config

import (
	"os"
	"path/filepath"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Mode controls what tools are exposed to the LLM.
type Mode string

const (
	ModeReadOnly  Mode = "readonly"
	ModeReadWrite Mode = "readwrite"
)

// Config holds all runtime configuration for SteadyHelm.
type Config struct {
	Mode       Mode
	Namespace  string // empty = all namespaces
	RESTConfig *rest.Config
}

// New builds a Config, resolving kubeconfig in order:
//  1. explicit --kubeconfig flag
//  2. KUBECONFIG env var
//  3. ~/.kube/config
//  4. in-cluster service-account token
func New(kubeconfigPath, namespace, mode string) *Config {
	if kubeconfigPath == "" {
		kubeconfigPath = os.Getenv("KUBECONFIG")
	}
	if kubeconfigPath == "" {
		if home, err := os.UserHomeDir(); err == nil {
			candidate := filepath.Join(home, ".kube", "config")
			if _, err := os.Stat(candidate); err == nil {
				kubeconfigPath = candidate
			}
		}
	}

	var restCfg *rest.Config
	var err error
	if kubeconfigPath != "" {
		restCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	} else {
		restCfg, err = rest.InClusterConfig()
	}
	if err != nil {
		// Return a config without a REST client; the server will surface
		// a descriptive error on the first tool call instead of crashing.
		restCfg = nil
	}

	m := ModeReadOnly
	if mode == string(ModeReadWrite) {
		m = ModeReadWrite
	}

	return &Config{
		Mode:       m,
		Namespace:  namespace,
		RESTConfig: restCfg,
	}
}
