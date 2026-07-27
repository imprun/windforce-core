package server

func addTriggerControlPlanePaths(paths map[string]any, workspaceID string) {
	workspaceParameter := []any{oapiWorkspaceParam(workspaceID)}
	triggerParameters := []any{
		oapiWorkspaceParam(workspaceID),
		oapiPathParam("triggerId", "Trigger id."),
	}
	paths["/api/w/{workspace}/triggers"] = map[string]any{
		"get": map[string]any{
			"operationId": "listTriggers",
			"summary":     "List configured triggers",
			"parameters":  workspaceParameter,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Configured triggers.", oapiSchemaRef("TriggerListResponse")),
			}, "401", "403"),
		},
		"post": map[string]any{
			"operationId": "createTrigger",
			"summary":     "Create a webhook, schedule, or RabbitMQ trigger",
			"parameters":  workspaceParameter,
			"requestBody": oapiJSONBody(oapiSchemaRef("TriggerRequest"), true),
			"responses": withErrors(map[string]any{
				"201": oapiResponse("Created trigger.", oapiSchemaRef("Trigger")),
			}, "400", "401", "403", "409"),
		},
	}
	paths["/api/w/{workspace}/triggers/{triggerId}"] = map[string]any{
		"get": map[string]any{
			"operationId": "getTrigger",
			"summary":     "Get a configured trigger",
			"parameters":  triggerParameters,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Configured trigger.", oapiSchemaRef("Trigger")),
			}, "401", "403", "404"),
		},
		"put": map[string]any{
			"operationId": "updateTrigger",
			"summary":     "Replace a configured trigger",
			"parameters":  triggerParameters,
			"requestBody": oapiJSONBody(oapiSchemaRef("TriggerRequest"), true),
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Updated trigger.", oapiSchemaRef("Trigger")),
			}, "400", "401", "403", "404", "409"),
		},
		"delete": map[string]any{
			"operationId": "deleteTrigger",
			"summary":     "Delete a configured trigger",
			"parameters":  triggerParameters,
			"responses": withErrors(map[string]any{
				"204": map[string]any{"description": "Trigger deleted."},
			}, "401", "403", "404"),
		},
	}
	for _, operation := range []string{"enable", "disable"} {
		paths["/api/w/{workspace}/triggers/{triggerId}/"+operation] = map[string]any{
			"post": map[string]any{
				"operationId": operation + "Trigger",
				"summary":     operation + " a configured trigger",
				"parameters":  triggerParameters,
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Updated trigger.", oapiSchemaRef("Trigger")),
				}, "401", "403", "404", "409"),
			},
		}
	}
	paths["/api/w/{workspace}/triggers/{triggerId}/audit"] = map[string]any{
		"get": map[string]any{
			"operationId": "listTriggerAudit",
			"summary":     "List trigger configuration audit events",
			"parameters":  triggerParameters,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Trigger audit events.", oapiSchemaRef("TriggerAuditListResponse")),
			}, "401", "403", "404"),
		},
	}
	paths["/api/w/{workspace}/triggers/{triggerId}/deliveries"] = map[string]any{
		"get": map[string]any{
			"operationId": "listTriggerDeliveries",
			"summary":     "List recent trigger deliveries",
			"parameters":  triggerParameters,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Recent trigger deliveries.", oapiSchemaRef("TriggerDeliveryListResponse")),
			}, "401", "403", "404"),
		},
	}
	paths["/api/v1/workspaces/{workspace}/triggers/{triggerId}/events"] = map[string]any{
		"post": map[string]any{
			"operationId": "deliverWebhookTrigger",
			"summary":     "Deliver a signed event to a webhook trigger",
			"description": "Authentication is the configured HMAC-SHA256 signature header, not the control-plane bearer token.",
			"security":    []any{},
			"parameters":  triggerParameters,
			"requestBody": map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{"schema": map[string]any{}},
				},
			},
			"responses": withErrors(map[string]any{
				"202": oapiResponse("Event admitted.", oapiSchemaRef("TriggerAdmissionResponse")),
			}, "400", "401", "404", "413", "429", "503"),
		},
	}
}

func addTriggerControlPlaneSchemas(schemas map[string]any) {
	triggerProperties := map[string]any{
		"id":             oapiStringSchema(),
		"workspace_id":   oapiStringSchema(),
		"name":           oapiStringSchema(),
		"kind":           map[string]any{"type": "string", "enum": []any{"webhook", "schedule", "rabbitmq"}},
		"enabled":        oapiBooleanSchema(),
		"app":            oapiStringSchema(),
		"action":         oapiStringSchema(),
		"credential_ref": oapiStringSchema(),
		"config":         map[string]any{"type": "object", "additionalProperties": true},
		"has_secret":     oapiBooleanSchema(),
		"created_by":     oapiStringSchema(),
		"updated_by":     oapiStringSchema(),
		"created_at":     map[string]any{"type": "string", "format": "date-time"},
		"updated_at":     map[string]any{"type": "string", "format": "date-time"},
	}
	schemas["Trigger"] = map[string]any{
		"type":       "object",
		"required":   []any{"id", "workspace_id", "name", "kind", "enabled", "app", "action", "config", "has_secret", "created_at", "updated_at"},
		"properties": triggerProperties,
	}
	schemas["TriggerRequest"] = map[string]any{
		"type":     "object",
		"required": []any{"name", "kind", "app", "action", "config"},
		"properties": map[string]any{
			"name":           oapiStringSchema(),
			"kind":           map[string]any{"type": "string", "enum": []any{"webhook", "schedule", "rabbitmq"}},
			"enabled":        oapiBooleanSchema(),
			"app":            oapiStringSchema(),
			"action":         oapiStringSchema(),
			"credential_ref": oapiStringSchema(),
			"config":         map[string]any{"type": "object", "additionalProperties": true},
			"secret_config": map[string]any{
				"type":                 "object",
				"description":          "Write-only adapter secret configuration. Never returned or audited.",
				"writeOnly":            true,
				"additionalProperties": true,
			},
		},
	}
	schemas["TriggerListResponse"] = map[string]any{
		"type":       "object",
		"required":   []any{"items"},
		"properties": map[string]any{"items": map[string]any{"type": "array", "items": oapiSchemaRef("Trigger")}},
	}
	schemas["TriggerAudit"] = map[string]any{
		"type":     "object",
		"required": []any{"id", "workspace_id", "trigger_id", "kind", "actor", "created_at"},
		"properties": map[string]any{
			"id": oapiStringSchema(), "workspace_id": oapiStringSchema(), "trigger_id": oapiStringSchema(),
			"kind": oapiStringSchema(), "detail": oapiStringSchema(), "actor": oapiStringSchema(),
			"created_at": map[string]any{"type": "string", "format": "date-time"},
		},
	}
	schemas["TriggerAuditListResponse"] = map[string]any{
		"type":       "object",
		"required":   []any{"items"},
		"properties": map[string]any{"items": map[string]any{"type": "array", "items": oapiSchemaRef("TriggerAudit")}},
	}
	schemas["TriggerDelivery"] = map[string]any{
		"type":     "object",
		"required": []any{"id", "workspace_id", "trigger_id", "delivery_id", "state", "attempt", "created_at", "updated_at"},
		"properties": map[string]any{
			"id": oapiStringSchema(), "workspace_id": oapiStringSchema(), "trigger_id": oapiStringSchema(), "delivery_id": oapiStringSchema(),
			"correlation_id": oapiStringSchema(), "run_id": oapiStringSchema(), "state": oapiStringSchema(),
			"attempt": oapiIntegerSchema(), "error_summary": oapiStringSchema(),
			"scheduled_for": map[string]any{"type": []any{"string", "null"}, "format": "date-time"},
			"created_at":    map[string]any{"type": "string", "format": "date-time"},
			"updated_at":    map[string]any{"type": "string", "format": "date-time"},
		},
	}
	schemas["TriggerDeliveryListResponse"] = map[string]any{
		"type":       "object",
		"required":   []any{"items"},
		"properties": map[string]any{"items": map[string]any{"type": "array", "items": oapiSchemaRef("TriggerDelivery")}},
	}
	schemas["TriggerAdmissionResponse"] = map[string]any{
		"type":     "object",
		"required": []any{"run_id", "replayed"},
		"properties": map[string]any{
			"run_id": oapiStringSchema(), "replayed": oapiBooleanSchema(),
		},
	}
}
