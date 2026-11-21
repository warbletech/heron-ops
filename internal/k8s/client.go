package k8s

import (
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsclient "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Recorder receives one observation per Kubernetes API call. Verb and resource
// are fixed strings chosen by this package (for example "list" and "pods"), so
// they are safe to use as low-cardinality metric labels.
type Recorder interface {
	ObserveK8s(verb, resource string, err error, d time.Duration)
}

// Clients bundles the core and metrics clientsets.
type Clients struct {
	Core    kubernetes.Interface
	Metrics metricsclient.Interface
	// Recorder, when non-nil, is told about every Kubernetes API call.
	Recorder Recorder
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

// observe reports one API call to the Recorder, if any.
func (c *Clients) observe(verb, resource string, start time.Time, err error) {
	if c.Recorder == nil {
		return
	}
	c.Recorder.ObserveK8s(verb, resource, err, time.Since(start))
}
