package k8s

import (
	"fmt"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Clients bundles the core and metrics clientsets.
type Clients struct {
	Core    kubernetes.Interface
	Metrics metricsclient.Interface
}

// NewClients builds both clientsets from a REST config.
func NewClients(cfg *rest.Config) (*Clients, error) {
	if cfg == nil {
		return nil, fmt.Errorf("no Kubernetes config: provide --kubeconfig or run inside a cluster")
	}
	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("core client: %w", err)
	}
	metrics, err := metricsclient.NewForConfig(cfg)
	if err != nil {
		// Metrics API may not be installed; non-fatal.
		metrics = nil
	}
	return &Clients{Core: core, Metrics: metrics}, nil
}
