package k8s

import (
	"context"
	"fmt"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
)

// ── Namespaces ────────────────────────────────────────────────────────────────

func (c *Clients) ListNamespaces(ctx context.Context) ([]corev1.Namespace, error) {
	list, err := c.Core.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	return list.Items, nil
}

// ── Pods ──────────────────────────────────────────────────────────────────────

func (c *Clients) ListPods(ctx context.Context, namespace, labelSelector string) ([]corev1.Pod, error) {
	list, err := c.Core.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	return list.Items, nil
}

func (c *Clients) GetPod(ctx context.Context, namespace, name string) (*corev1.Pod, error) {
	pod, err := c.Core.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get pod %s/%s: %w", namespace, name, err)
	}
	return pod, nil
}

func (c *Clients) GetPodLogs(ctx context.Context, namespace, name, container string, tailLines int64) (string, error) {
	opts := &corev1.PodLogOptions{TailLines: &tailLines}
	if container != "" {
		opts.Container = container
	}
	req := c.Core.CoreV1().Pods(namespace).GetLogs(name, opts)
	raw, err := req.DoRaw(ctx)
	if err != nil {
		return "", fmt.Errorf("pod logs %s/%s: %w", namespace, name, err)
	}
	return string(raw), nil
}

// ── Nodes ─────────────────────────────────────────────────────────────────────

func (c *Clients) ListNodes(ctx context.Context) ([]corev1.Node, error) {
	list, err := c.Core.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	return list.Items, nil
}

// ── Events ────────────────────────────────────────────────────────────────────

func (c *Clients) ListEvents(ctx context.Context, namespace, fieldSelector string) ([]corev1.Event, error) {
	list, err := c.Core.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	return list.Items, nil
}

// ── Deployments ───────────────────────────────────────────────────────────────

func (c *Clients) ListDeployments(ctx context.Context, namespace string) ([]interface{}, error) {
	list, err := c.Core.AppsV1().Deployments(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list deployments: %w", err)
	}
	result := make([]interface{}, len(list.Items))
	for i := range list.Items {
		result[i] = list.Items[i]
	}
	return result, nil
}

func (c *Clients) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	scale, err := c.Core.AppsV1().Deployments(namespace).GetScale(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get scale %s/%s: %w", namespace, name, err)
	}
	scale.Spec.Replicas = replicas
	_, err = c.Core.AppsV1().Deployments(namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update scale %s/%s: %w", namespace, name, err)
	}
	return nil
}

// ── HPA ───────────────────────────────────────────────────────────────────────

func (c *Clients) ListHPAs(ctx context.Context, namespace string) ([]autoscalingv2.HorizontalPodAutoscaler, error) {
	list, err := c.Core.AutoscalingV2().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list HPAs: %w", err)
	}
	return list.Items, nil
}

// ── Pod Metrics ───────────────────────────────────────────────────────────────

func (c *Clients) ListPodMetrics(ctx context.Context, namespace string) ([]metricsv1beta1.PodMetrics, error) {
	if c.Metrics == nil {
		return nil, fmt.Errorf("metrics-server not available in this cluster")
	}
	list, err := c.Metrics.MetricsV1beta1().PodMetricses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list pod metrics: %w", err)
	}
	return list.Items, nil
}

func (c *Clients) ListNodeMetrics(ctx context.Context) ([]metricsv1beta1.NodeMetrics, error) {
	if c.Metrics == nil {
		return nil, fmt.Errorf("metrics-server not available in this cluster")
	}
	list, err := c.Metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list node metrics: %w", err)
	}
	return list.Items, nil
}

// ── Services ──────────────────────────────────────────────────────────────────

func (c *Clients) ListServices(ctx context.Context, namespace string) ([]corev1.Service, error) {
	list, err := c.Core.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	return list.Items, nil
}

// ── ConfigMaps ────────────────────────────────────────────────────────────────

func (c *Clients) GetConfigMap(ctx context.Context, namespace, name string) (*corev1.ConfigMap, error) {
	cm, err := c.Core.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get configmap %s/%s: %w", namespace, name, err)
	}
	return cm, nil
}
