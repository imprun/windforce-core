package server

func addTriggerControlPlanePaths(paths map[string]any, workspaceID string) {
	workspaceParameter := []any{oapiWorkspaceParam(workspaceID)}
	triggerParameters := []any{
		oapiWorkspaceParam(workspaceID),
		oapiPathParam("triggerId", "Trigger id."),
	}
	routeBindingParameters := []any{
		oapiWorkspaceParam(workspaceID),
		oapiPathParam("triggerId", "Webhook Trigger id."),
		oapiPathParam("bindingId", "HTTP Route Binding id."),
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
	paths["/api/w/{workspace}/triggers/{triggerId}/routes"] = map[string]any{
		"get": map[string]any{
			"operationId": "listHTTPRouteBindings",
			"summary":     "List external HTTP routes for a webhook Trigger",
			"parameters":  triggerParameters,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("HTTP Route Bindings.", oapiSchemaRef("HTTPRouteBindingListResponse")),
			}, "401", "403", "404"),
		},
		"post": map[string]any{
			"operationId": "createHTTPRouteBinding",
			"summary":     "Request an external HTTP route for a webhook Trigger",
			"parameters":  triggerParameters,
			"requestBody": oapiJSONBody(oapiSchemaRef("HTTPRouteBindingRequest"), true),
			"responses": withErrors(map[string]any{
				"201": oapiResponse("Created pending HTTP Route Binding.", oapiSchemaRef("HTTPRouteBinding")),
			}, "400", "401", "403", "404", "409"),
		},
	}
	paths["/api/w/{workspace}/triggers/{triggerId}/routes/{bindingId}"] = map[string]any{
		"get": map[string]any{
			"operationId": "getHTTPRouteBinding",
			"summary":     "Get an external HTTP Route Binding",
			"parameters":  routeBindingParameters,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("HTTP Route Binding.", oapiSchemaRef("HTTPRouteBinding")),
			}, "401", "403", "404"),
		},
		"put": map[string]any{
			"operationId": "updateHTTPRouteBinding",
			"summary":     "Replace desired HTTP route fields",
			"parameters":  routeBindingParameters,
			"requestBody": oapiJSONBody(oapiSchemaRef("HTTPRouteBindingRequest"), true),
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Updated pending HTTP Route Binding.", oapiSchemaRef("HTTPRouteBinding")),
			}, "400", "401", "403", "404", "409"),
		},
		"delete": map[string]any{
			"operationId": "deleteHTTPRouteBinding",
			"summary":     "Request deletion of an external HTTP route",
			"description": "Returns a deleting tombstone. A Router Provider completes deletion asynchronously.",
			"parameters":  routeBindingParameters,
			"responses": withErrors(map[string]any{
				"202": oapiResponse("Deleting HTTP Route Binding.", oapiSchemaRef("HTTPRouteBinding")),
			}, "401", "403", "404", "409"),
		},
	}
	paths["/api/w/{workspace}/triggers/{triggerId}/routes/{bindingId}/audit"] = map[string]any{
		"get": map[string]any{
			"operationId": "listHTTPRouteBindingAudit",
			"summary":     "List desired and observed HTTP route transitions",
			"parameters":  routeBindingParameters,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("HTTP Route Binding audit events.", oapiSchemaRef("HTTPRouteBindingAuditListResponse")),
			}, "401", "403", "404"),
		},
	}
	paths["/api/w/{workspace}/http-route-bindings"] = map[string]any{
		"get": map[string]any{
			"operationId": "listProviderHTTPRouteBindings",
			"summary":     "List desired HTTP routes for Router Provider reconciliation",
			"parameters": []any{
				oapiWorkspaceParam(workspaceID),
				oapiQueryParam("include_deleted", "Include deleted tombstones.", oapiBooleanSchema(), false),
				oapiQueryParam("provider", "Provider name. Bindings using auto also match.", oapiStringSchema(), false),
				oapiQueryParam("state", "Observed state filter.", oapiStringSchema(), false),
			},
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Provider reconciliation view.", oapiSchemaRef("HTTPRouteBindingProviderListResponse")),
			}, "400", "401", "403"),
		},
	}
	paths["/api/w/{workspace}/http-route-bindings/{bindingId}/status"] = map[string]any{
		"put": map[string]any{
			"operationId": "updateHTTPRouteBindingStatus",
			"summary":     "Report Router Provider observed state",
			"parameters": []any{
				oapiWorkspaceParam(workspaceID),
				oapiPathParam("bindingId", "HTTP Route Binding id."),
			},
			"requestBody": oapiJSONBody(oapiSchemaRef("HTTPRouteBindingStatusRequest"), true),
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Updated observed state.", oapiSchemaRef("HTTPRouteBinding")),
			}, "400", "401", "403", "404", "409"),
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
	schemas["HTTPRouteBinding"] = map[string]any{
		"type":        "object",
		"description": "Provider-neutral desired and observed state for exposing a webhook Trigger. Provider-specific Kubernetes or hosted router fields are intentionally absent.",
		"required": []any{
			"id", "workspace_id", "trigger_id", "path", "visibility", "provider", "state",
			"generation", "observed_generation", "created_by", "updated_by", "created_at", "updated_at",
		},
		"properties": map[string]any{
			"id":                  oapiStringSchema(),
			"workspace_id":        oapiStringSchema(),
			"trigger_id":          oapiStringSchema(),
			"hostname":            oapiStringSchema(),
			"path":                oapiStringSchema(),
			"visibility":          map[string]any{"type": "string", "enum": []any{"public"}},
			"provider":            oapiStringSchema(),
			"state":               map[string]any{"type": "string", "enum": []any{"pending", "ready", "error", "deleting", "deleted"}},
			"public_url":          oapiStringSchema(),
			"error_summary":       oapiStringSchema(),
			"generation":          oapiIntegerSchema(),
			"observed_generation": oapiIntegerSchema(),
			"created_by":          oapiStringSchema(),
			"updated_by":          oapiStringSchema(),
			"created_at":          map[string]any{"type": "string", "format": "date-time"},
			"updated_at":          map[string]any{"type": "string", "format": "date-time"},
			"delete_requested_at": map[string]any{"type": []any{"string", "null"}, "format": "date-time"},
			"deleted_at":          map[string]any{"type": []any{"string", "null"}, "format": "date-time"},
		},
	}
	schemas["HTTPRouteBindingRequest"] = map[string]any{
		"type":     "object",
		"required": []any{"path"},
		"properties": map[string]any{
			"hostname":   oapiStringSchema(),
			"path":       oapiStringSchema(),
			"visibility": map[string]any{"type": "string", "enum": []any{"public"}, "default": "public"},
			"provider":   map[string]any{"type": "string", "default": "auto"},
		},
	}
	schemas["HTTPRouteBindingStatusRequest"] = map[string]any{
		"type":     "object",
		"required": []any{"state", "observed_generation"},
		"properties": map[string]any{
			"state":               map[string]any{"type": "string", "enum": []any{"pending", "ready", "error", "deleted"}},
			"public_url":          oapiStringSchema(),
			"error_summary":       oapiStringSchema(),
			"observed_generation": oapiIntegerSchema(),
		},
	}
	schemas["HTTPRouteBindingListResponse"] = map[string]any{
		"type":       "object",
		"required":   []any{"items"},
		"properties": map[string]any{"items": map[string]any{"type": "array", "items": oapiSchemaRef("HTTPRouteBinding")}},
	}
	schemas["HTTPRouteBindingProviderListResponse"] = map[string]any{
		"type":     "object",
		"required": []any{"items", "configured_provider"},
		"properties": map[string]any{
			"items":               map[string]any{"type": "array", "items": oapiSchemaRef("HTTPRouteBinding")},
			"configured_provider": oapiStringSchema(),
		},
	}
	schemas["HTTPRouteBindingAudit"] = map[string]any{
		"type":     "object",
		"required": []any{"id", "workspace_id", "trigger_id", "binding_id", "kind", "actor", "created_at"},
		"properties": map[string]any{
			"id":           oapiStringSchema(),
			"workspace_id": oapiStringSchema(),
			"trigger_id":   oapiStringSchema(),
			"binding_id":   oapiStringSchema(),
			"kind":         oapiStringSchema(),
			"detail":       oapiStringSchema(),
			"actor":        oapiStringSchema(),
			"created_at":   map[string]any{"type": "string", "format": "date-time"},
		},
	}
	schemas["HTTPRouteBindingAuditListResponse"] = map[string]any{
		"type":       "object",
		"required":   []any{"items"},
		"properties": map[string]any{"items": map[string]any{"type": "array", "items": oapiSchemaRef("HTTPRouteBindingAudit")}},
	}
}
