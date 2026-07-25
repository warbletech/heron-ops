package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

// CallTool is the public entrypoint for embedding SteadyHelm in other MCP facades.
func (s *Server) CallTool(ctx context.Context, name string, args map[string]any) ToolCallResult {
	if args == nil {
		args = map[string]any{}
	}
	return s.dispatch(ctx, ToolCallParams{Name: name, Arguments: args})
}

// ToolNames returns tool definitions for the current mode (readonly | readwrite).
func (s *Server) ToolNames() []ToolDefinition {
	return toolDefs(string(s.cfg.Mode))
}

// dispatch routes a tools/call request to the correct handler.
func (s *Server) dispatch(ctx context.Context, params ToolCallParams) ToolCallResult {
	// Guard write tools in readonly mode.
	writeTools := map[string]bool{"scale_deployment": true, "delete_pod": true}
	if writeTools[params.Name] && s.cfg.Mode != "readwrite" {
		return errorResult(fmt.Sprintf("tool %q requires readwrite mode; restart with --mode=readwrite", params.Name))
	}

	ns := stringArg(params.Arguments, "namespace", s.cfg.Namespace)

	switch params.Name {

	// ── Read tools ────────────────────────────────────────────────────────────

	case "list_namespaces":
		items, err := s.clients.ListNamespaces(ctx)
		if err != nil {
			return errorResult(err.Error())
		}
		lines := make([]string, 0, len(items))
		for _, n := range items {
			phase := string(n.Status.Phase)
			if phase == "" {
				phase = "Active"
			}
			lines = append(lines, fmt.Sprintf("%-40s %s", n.Name, phase))
		}
		return textResult(fmt.Sprintf("NAMESPACES (%d)\n%s\n%s",
			len(items), header("NAME", "STATUS"), strings.Join(lines, "\n")))

	case "list_pods":
		sel := stringArg(params.Arguments, "label_selector", "")
		pods, err := s.clients.ListPods(ctx, ns, sel)
		if err != nil {
			return errorResult(err.Error())
		}
		return textResult(renderPods(pods))

	case "get_pod":
		name := stringArg(params.Arguments, "name", "")
		pod, err := s.clients.GetPod(ctx, ns, name)
		if err != nil {
			return errorResult(err.Error())
		}
		return textResult(renderPodDetail(pod))

	case "get_pod_logs":
		name := stringArg(params.Arguments, "name", "")
		container := stringArg(params.Arguments, "container", "")
		tail := int64Arg(params.Arguments, "tail_lines", 100)
		logs, err := s.clients.GetPodLogs(ctx, ns, name, container, tail)
		if err != nil {
			return errorResult(err.Error())
		}
		return textResult(logs)

	case "list_nodes":
		nodes, err := s.clients.ListNodes(ctx)
		if err != nil {
			return errorResult(err.Error())
		}
		return textResult(renderNodes(nodes))

	case "list_events":
		fieldSel := stringArg(params.Arguments, "field_selector", "")
		events, err := s.clients.ListEvents(ctx, ns, fieldSel)
		if err != nil {
			return errorResult(err.Error())
		}
		return textResult(renderEvents(events))

	case "list_deployments":
		deploys, err := s.clients.ListDeployments(ctx, ns)
		if err != nil {
			return errorResult(err.Error())
		}
		out, _ := json.MarshalIndent(deploys, "", "  ")
		return textResult(string(out))

	case "list_hpas":
		hpas, err := s.clients.ListHPAs(ctx, ns)
		if err != nil {
			return errorResult(err.Error())
		}
		lines := []string{header("NAMESPACE", "NAME", "REFERENCE", "MINPODS", "MAXPODS", "CURRENT", "DESIRED")}
		for _, h := range hpas {
			cur := int32(0)
			if h.Status.CurrentReplicas != 0 {
				cur = h.Status.CurrentReplicas
			}
			lines = append(lines, fmt.Sprintf("%-20s %-30s %-30s %-8d %-8d %-8d %-8d",
				h.Namespace, h.Name,
				fmt.Sprintf("%s/%s", h.Spec.ScaleTargetRef.Kind, h.Spec.ScaleTargetRef.Name),
				*h.Spec.MinReplicas, h.Spec.MaxReplicas,
				cur, h.Status.DesiredReplicas,
			))
		}
		return textResult(fmt.Sprintf("HPAs (%d)\n%s", len(hpas), strings.Join(lines, "\n")))

	case "list_pod_metrics":
		metrics, err := s.clients.ListPodMetrics(ctx, ns)
		if err != nil {
			return errorResult(err.Error())
		}
		lines := []string{header("NAMESPACE", "POD", "CPU", "MEMORY")}
		for _, m := range metrics {
			for _, c := range m.Containers {
				lines = append(lines, fmt.Sprintf("%-20s %-40s %-10s %-10s",
					m.Namespace, m.Name,
					c.Usage.Cpu().String(),
					c.Usage.Memory().String(),
				))
			}
		}
		return textResult(strings.Join(lines, "\n"))

	case "list_node_metrics":
		metrics, err := s.clients.ListNodeMetrics(ctx)
		if err != nil {
			return errorResult(err.Error())
		}
		lines := []string{header("NODE", "CPU", "MEMORY")}
		for _, m := range metrics {
			lines = append(lines, fmt.Sprintf("%-40s %-10s %-10s",
				m.Name,
				m.Usage.Cpu().String(),
				m.Usage.Memory().String(),
			))
		}
		return textResult(strings.Join(lines, "\n"))

	case "list_services":
		svcs, err := s.clients.ListServices(ctx, ns)
		if err != nil {
			return errorResult(err.Error())
		}
		lines := []string{header("NAMESPACE", "NAME", "TYPE", "CLUSTER-IP", "PORTS")}
		for _, svc := range svcs {
			ports := make([]string, 0)
			for _, p := range svc.Spec.Ports {
				ports = append(ports, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
			}
			lines = append(lines, fmt.Sprintf("%-20s %-30s %-12s %-16s %s",
				svc.Namespace, svc.Name, svc.Spec.Type, svc.Spec.ClusterIP, strings.Join(ports, ",")))
		}
		return textResult(strings.Join(lines, "\n"))

	case "get_configmap":
		name := stringArg(params.Arguments, "name", "")
		cm, err := s.clients.GetConfigMap(ctx, ns, name)
		if err != nil {
			return errorResult(err.Error())
		}
		lines := []string{fmt.Sprintf("ConfigMap: %s/%s", cm.Namespace, cm.Name), ""}
		for k, v := range cm.Data {
			lines = append(lines, fmt.Sprintf("=== %s ===\n%s", k, v))
		}
		return textResult(strings.Join(lines, "\n"))

	case "cluster_summary":
		return s.clusterSummary(ctx, ns)

	// ── Write tools ───────────────────────────────────────────────────────────

	case "scale_deployment":
		name := stringArg(params.Arguments, "name", "")
		replicas := int32(int64Arg(params.Arguments, "replicas", 1))
		if err := s.clients.ScaleDeployment(ctx, ns, name, replicas); err != nil {
			return errorResult(err.Error())
		}
		return textResult(fmt.Sprintf("✓ Scaled %s/%s to %d replicas", ns, name, replicas))

	case "delete_pod":
		name := stringArg(params.Arguments, "name", "")
		if err := s.clients.DeletePod(ctx, ns, name); err != nil {
			return errorResult(err.Error())
		}
		return textResult(fmt.Sprintf("✓ Deleted pod %s/%s", ns, name))

	default:
		return errorResult(fmt.Sprintf("unknown tool: %q", params.Name))
	}
}

// clusterSummary builds a concise cluster health snapshot.
func (s *Server) clusterSummary(ctx context.Context, ns string) ToolCallResult {
	var sb strings.Builder

	nodes, err := s.clients.ListNodes(ctx)
	if err == nil {
		ready, notReady := 0, 0
		for _, n := range nodes {
			for _, c := range n.Status.Conditions {
				if c.Type == corev1.NodeReady {
					if c.Status == corev1.ConditionTrue {
						ready++
					} else {
						notReady++
					}
				}
			}
		}
		sb.WriteString(fmt.Sprintf("NODES: %d total, %d Ready, %d NotReady\n\n", len(nodes), ready, notReady))
	}

	pods, err := s.clients.ListPods(ctx, ns, "")
	if err == nil {
		phases := map[string]int{}
		for _, p := range pods {
			phases[string(p.Status.Phase)]++
		}
		sb.WriteString("POD PHASES:\n")
		for phase, count := range phases {
			sb.WriteString(fmt.Sprintf("  %-12s %d\n", phase, count))
		}
		sb.WriteString("\n")
	}

	events, err := s.clients.ListEvents(ctx, ns, "type=Warning")
	if err == nil {
		sb.WriteString(fmt.Sprintf("WARNING EVENTS (%d):\n", len(events)))
		shown := 0
		for _, e := range events {
			if shown >= 10 {
				sb.WriteString(fmt.Sprintf("  ... and %d more\n", len(events)-shown))
				break
			}
			sb.WriteString(fmt.Sprintf("  [%s] %s/%s: %s\n",
				e.Reason, e.InvolvedObject.Namespace, e.InvolvedObject.Name, e.Message))
			shown++
		}
		sb.WriteString("\n")
	}

	hpas, err := s.clients.ListHPAs(ctx, ns)
	if err == nil && len(hpas) > 0 {
		sb.WriteString("HPA SATURATION:\n")
		for _, h := range hpas {
			saturated := h.Status.CurrentReplicas >= h.Spec.MaxReplicas
			flag := ""
			if saturated {
				flag = " ⚠ AT MAX"
			}
			sb.WriteString(fmt.Sprintf("  %s/%s current=%d max=%d%s\n",
				h.Namespace, h.Name, h.Status.CurrentReplicas, h.Spec.MaxReplicas, flag))
		}
	}

	return textResult(sb.String())
}

// ── Render helpers ────────────────────────────────────────────────────────────

func renderPods(pods []corev1.Pod) string {
	lines := []string{header("NAMESPACE", "NAME", "READY", "STATUS", "RESTARTS", "NODE")}
	for _, p := range pods {
		ready, total := 0, len(p.Status.ContainerStatuses)
		restarts := int32(0)
		for _, cs := range p.Status.ContainerStatuses {
			if cs.Ready {
				ready++
			}
			restarts += cs.RestartCount
		}
		lines = append(lines, fmt.Sprintf("%-20s %-45s %d/%-4d %-12s %-8d %s",
			p.Namespace, p.Name, ready, total,
			string(p.Status.Phase), restarts, p.Spec.NodeName))
	}
	return fmt.Sprintf("PODS (%d)\n%s", len(pods), strings.Join(lines, "\n"))
}

func renderPodDetail(p *corev1.Pod) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Pod: %s/%s\n", p.Namespace, p.Name))
	sb.WriteString(fmt.Sprintf("Phase:  %s\n", p.Status.Phase))
	sb.WriteString(fmt.Sprintf("Node:   %s\n", p.Spec.NodeName))
	sb.WriteString(fmt.Sprintf("IP:     %s\n", p.Status.PodIP))
	sb.WriteString("\nConditions:\n")
	for _, c := range p.Status.Conditions {
		sb.WriteString(fmt.Sprintf("  %-24s %s\n", c.Type, c.Status))
	}
	sb.WriteString("\nContainers:\n")
	for _, cs := range p.Status.ContainerStatuses {
		state := "Unknown"
		if cs.State.Running != nil {
			state = "Running"
		} else if cs.State.Waiting != nil {
			state = fmt.Sprintf("Waiting(%s)", cs.State.Waiting.Reason)
		} else if cs.State.Terminated != nil {
			state = fmt.Sprintf("Terminated(%s)", cs.State.Terminated.Reason)
		}
		sb.WriteString(fmt.Sprintf("  %-30s ready=%-5v restarts=%-4d state=%s\n",
			cs.Name, cs.Ready, cs.RestartCount, state))
	}
	return sb.String()
}

func renderNodes(nodes []corev1.Node) string {
	lines := []string{header("NAME", "STATUS", "ROLES", "VERSION", "OS")}
	for _, n := range nodes {
		status := "Unknown"
		for _, c := range n.Status.Conditions {
			if c.Type == corev1.NodeReady {
				if c.Status == corev1.ConditionTrue {
					status = "Ready"
				} else {
					status = "NotReady"
				}
			}
		}
		roles := []string{}
		for label := range n.Labels {
			if strings.HasPrefix(label, "node-role.kubernetes.io/") {
				roles = append(roles, strings.TrimPrefix(label, "node-role.kubernetes.io/"))
			}
		}
		if len(roles) == 0 {
			roles = []string{"<none>"}
		}
		lines = append(lines, fmt.Sprintf("%-40s %-10s %-15s %-12s %s",
			n.Name, status, strings.Join(roles, ","),
			n.Status.NodeInfo.KubeletVersion,
			n.Status.NodeInfo.OSImage))
	}
	return fmt.Sprintf("NODES (%d)\n%s", len(nodes), strings.Join(lines, "\n"))
}

func renderEvents(events []corev1.Event) string {
	lines := []string{header("NAMESPACE", "TYPE", "REASON", "OBJECT", "MESSAGE")}
	for _, e := range events {
		msg := e.Message
		if len(msg) > 80 {
			msg = msg[:77] + "..."
		}
		lines = append(lines, fmt.Sprintf("%-20s %-8s %-20s %-35s %s",
			e.Namespace, e.Type, e.Reason,
			fmt.Sprintf("%s/%s", e.InvolvedObject.Kind, e.InvolvedObject.Name),
			msg))
	}
	return fmt.Sprintf("EVENTS (%d)\n%s", len(events), strings.Join(lines, "\n"))
}

func header(cols ...string) string {
	return strings.Join(cols, "  ")
}

// ── Argument helpers ──────────────────────────────────────────────────────────

func stringArg(args map[string]any, key, fallback string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

func int64Arg(args map[string]any, key string, fallback int64) int64 {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int64(n)
		case int64:
			return n
		case json.Number:
			if i, err := n.Int64(); err == nil {
				return i
			}
		}
	}
	return fallback
}
