package mcp

// toolDefs returns all tool definitions exposed to the LLM.
// Write-mode tools are only appended when mode == readwrite.
func toolDefs(mode string) []ToolDefinition {
	tools := []ToolDefinition{
		{
			Name:        "list_namespaces",
			Description: "List all Kubernetes namespaces in the cluster.",
			InputSchema: jsonSchema(map[string]any{}),
		},
		{
			Name:        "list_pods",
			Description: "List pods in a namespace, optionally filtered by label selector.",
			InputSchema: jsonSchema(map[string]any{
				"namespace":      schemaString("Kubernetes namespace. Leave empty for all namespaces."),
				"label_selector": schemaString("Optional label selector, e.g. app=payment-svc"),
			}),
		},
		{
			Name:        "get_pod",
			Description: "Get detailed status of a specific pod.",
			InputSchema: jsonSchemaRequired(
				map[string]any{
					"namespace": schemaString("Kubernetes namespace"),
					"name":      schemaString("Pod name"),
				},
				[]string{"namespace", "name"},
			),
		},
		{
			Name:        "get_pod_logs",
			Description: "Fetch recent logs from a pod container.",
			InputSchema: jsonSchemaRequired(
				map[string]any{
					"namespace":  schemaString("Kubernetes namespace"),
					"name":       schemaString("Pod name"),
					"container":  schemaString("Container name (optional, defaults to first container)"),
					"tail_lines": schemaInt("Number of log lines to return (default 100)"),
				},
				[]string{"namespace", "name"},
			),
		},
		{
			Name:        "list_nodes",
			Description: "List all nodes with their status, roles, and resource capacity.",
			InputSchema: jsonSchema(map[string]any{}),
		},
		{
			Name:        "list_events",
			Description: "List Kubernetes events, optionally filtered by namespace or field selector.",
			InputSchema: jsonSchema(map[string]any{
				"namespace":      schemaString("Namespace to query. Empty = all namespaces."),
				"field_selector": schemaString("Optional field selector, e.g. involvedObject.name=my-pod"),
			}),
		},
		{
			Name:        "list_deployments",
			Description: "List deployments with replica counts and rollout status.",
			InputSchema: jsonSchema(map[string]any{
				"namespace": schemaString("Namespace to query. Empty = all namespaces."),
			}),
		},
		{
			Name:        "list_hpas",
			Description: "List HorizontalPodAutoscalers with current/desired/min/max replica counts and metrics.",
			InputSchema: jsonSchema(map[string]any{
				"namespace": schemaString("Namespace to query. Empty = all namespaces."),
			}),
		},
		{
			Name:        "list_pod_metrics",
			Description: "List live CPU and memory usage for pods (requires metrics-server).",
			InputSchema: jsonSchema(map[string]any{
				"namespace": schemaString("Namespace to query. Empty = all namespaces."),
			}),
		},
		{
			Name:        "list_node_metrics",
			Description: "List live CPU and memory usage for nodes (requires metrics-server).",
			InputSchema: jsonSchema(map[string]any{}),
		},
		{
			Name:        "list_services",
			Description: "List Services with their type, cluster IP, and port mappings.",
			InputSchema: jsonSchema(map[string]any{
				"namespace": schemaString("Namespace to query. Empty = all namespaces."),
			}),
		},
		{
			Name:        "get_configmap",
			Description: "Read the data keys of a ConfigMap (values are shown).",
			InputSchema: jsonSchemaRequired(
				map[string]any{
					"namespace": schemaString("Kubernetes namespace"),
					"name":      schemaString("ConfigMap name"),
				},
				[]string{"namespace", "name"},
			),
		},
		{
			Name:        "cluster_summary",
			Description: "Return a concise health summary: node count/status, pod phase counts, recent Warning events, HPA saturation.",
			InputSchema: jsonSchema(map[string]any{
				"namespace": schemaString("Scope summary to a namespace. Empty = cluster-wide."),
			}),
		},
	}

	if mode == "readwrite" {
		tools = append(tools, ToolDefinition{
			Name:        "scale_deployment",
			Description: "Scale a Deployment to the specified replica count. WRITE operation — requires readwrite mode.",
			InputSchema: jsonSchemaRequired(
				map[string]any{
					"namespace": schemaString("Kubernetes namespace"),
					"name":      schemaString("Deployment name"),
					"replicas":  schemaInt("Desired replica count"),
				},
				[]string{"namespace", "name", "replicas"},
			),
		})
		tools = append(tools, ToolDefinition{
			Name:        "delete_pod",
			Description: "Delete a pod by name (triggers restart). WRITE operation — requires readwrite mode.",
			InputSchema: jsonSchemaRequired(
				map[string]any{
					"namespace": schemaString("Kubernetes namespace"),
					"name":      schemaString("Pod name"),
				},
				[]string{"namespace", "name"},
			),
		})
	}

	return tools
}

// ── Schema helpers ────────────────────────────────────────────────────────────

func schemaString(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

func schemaInt(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func jsonSchema(props map[string]any) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": props,
	}
}

func jsonSchemaRequired(props map[string]any, required []string) map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}
