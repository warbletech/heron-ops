package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeletePod deletes a pod by name, triggering a restart via the controller.
func (c *Clients) DeletePod(ctx context.Context, namespace, name string) error {
	err := c.Core.CoreV1().Pods(namespace).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("delete pod %s/%s: %w", namespace, name, err)
	}
	return nil
}
