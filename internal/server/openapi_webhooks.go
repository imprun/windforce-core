package server

func addWebhookControlPlanePaths(paths map[string]any, workspaceID string) {
	webhookParams := []any{oapiWorkspaceParam(workspaceID), oapiPathParam("webhookId", "Webhook subscription id.")}
	deliveryParams := []any{oapiWorkspaceParam(workspaceID), oapiPathParam("deliveryId", "Webhook delivery id.")}
	paths["/api/w/{workspace}/webhooks"] = map[string]any{
		"get": map[string]any{
			"operationId": "listWebhookSubscriptions",
			"summary":     "List webhook subscriptions",
			"parameters": []any{
				oapiWorkspaceParam(workspaceID),
				oapiQueryParam("include_deleted", "Include deleted subscription history.", oapiBooleanSchema(), false),
			},
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Webhook subscriptions with masked endpoints.", map[string]any{"type": "array", "items": oapiSchemaRef("WebhookSubscription")}),
			}, "401", "403"),
		},
		"post": map[string]any{
			"operationId": "createWebhookSubscription",
			"summary":     "Create a webhook subscription",
			"parameters":  []any{oapiWorkspaceParam(workspaceID)},
			"requestBody": oapiJSONBody(oapiSchemaRef("WebhookSubscriptionCreate"), true),
			"responses": withErrors(map[string]any{
				"201": oapiResponse("Created subscription. A generated signing secret is returned once.", oapiSchemaRef("WebhookSubscriptionMutation")),
			}, "400", "401", "403", "409"),
		},
	}
	paths["/api/w/{workspace}/webhooks/{webhookId}"] = map[string]any{
		"get": map[string]any{
			"operationId": "getWebhookSubscription",
			"summary":     "Get a webhook subscription",
			"parameters":  webhookParams,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Webhook subscription with a masked endpoint.", oapiSchemaRef("WebhookSubscription")),
			}, "401", "403", "404"),
		},
		"patch": map[string]any{
			"operationId": "updateWebhookSubscription",
			"summary":     "Update or rotate a webhook subscription",
			"parameters":  webhookParams,
			"requestBody": oapiJSONBody(oapiSchemaRef("WebhookSubscriptionUpdate"), true),
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Updated subscription. A rotated signing secret is returned once.", oapiSchemaRef("WebhookSubscriptionMutation")),
			}, "400", "401", "403", "404", "409"),
		},
		"delete": map[string]any{
			"operationId": "deleteWebhookSubscription",
			"summary":     "Delete a webhook subscription",
			"parameters":  webhookParams,
			"responses": withErrors(map[string]any{
				"204": map[string]any{"description": "Subscription deleted and queued deliveries canceled."},
			}, "401", "403", "404"),
		},
	}
	paths["/api/w/{workspace}/webhooks/{webhookId}/test"] = map[string]any{
		"post": map[string]any{
			"operationId": "testWebhookSubscription",
			"summary":     "Queue a test webhook delivery",
			"parameters":  webhookParams,
			"responses": withErrors(map[string]any{
				"202": oapiResponse("Test delivery queued.", oapiSchemaRef("WebhookDeliveryDetail")),
			}, "401", "403", "404", "409"),
		},
	}
	paths["/api/w/{workspace}/webhooks/{webhookId}/deliveries"] = map[string]any{
		"get": map[string]any{
			"operationId": "listWebhookDeliveries",
			"summary":     "List webhook delivery history",
			"parameters": append(append([]any{}, webhookParams...),
				oapiQueryParam("state", "Optional delivery state filter.", map[string]any{"type": "string", "enum": []any{"pending", "delivering", "retrying", "succeeded", "failed", "canceled"}}, false),
				oapiQueryParam("limit", "Page size from 1 to 100. Defaults to 50.", oapiIntegerSchema(), false),
				oapiQueryParam("cursor", "Opaque keyset cursor from the previous page.", oapiStringSchema(), false),
			),
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Webhook delivery page.", oapiSchemaRef("WebhookDeliveryPage")),
			}, "400", "401", "403"),
		},
	}
	paths["/api/w/{workspace}/webhook-deliveries/{deliveryId}"] = map[string]any{
		"get": map[string]any{
			"operationId": "getWebhookDelivery",
			"summary":     "Get webhook delivery detail",
			"parameters":  deliveryParams,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Webhook delivery and immutable event envelope.", oapiSchemaRef("WebhookDeliveryDetail")),
			}, "401", "403", "404"),
		},
	}
	paths["/api/w/{workspace}/webhook-deliveries/{deliveryId}/retry"] = map[string]any{
		"post": map[string]any{
			"operationId": "retryWebhookDelivery",
			"summary":     "Retry a failed webhook delivery",
			"parameters":  deliveryParams,
			"responses": withErrors(map[string]any{
				"202": oapiResponse("Failed delivery queued for retry.", oapiSchemaRef("WebhookDeliveryDetail")),
			}, "401", "403", "404", "409"),
		},
	}
}

func addWebhookControlPlaneSchemas(schemas map[string]any) {
	stringArray := map[string]any{"type": "array", "items": oapiStringSchema()}
	nullableString := map[string]any{"type": []any{"string", "null"}}
	nullableInteger := map[string]any{"type": []any{"integer", "null"}}
	nullableDateTime := map[string]any{"type": []any{"string", "null"}, "format": "date-time"}
	schemas["WebhookSubscription"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": oapiStringSchema(), "workspace_id": oapiStringSchema(), "name": oapiStringSchema(),
			"endpoint_summary":   map[string]any{"type": "string", "description": "Endpoint scheme and host only; paths and query values are never returned."},
			"has_signing_secret": oapiBooleanSchema(), "event_types": stringArray, "app_keys": stringArray,
			"enabled": oapiBooleanSchema(), "created_by": oapiStringSchema(), "updated_by": oapiStringSchema(),
			"created_at": oapiDateTimeSchema(), "updated_at": oapiDateTimeSchema(), "deleted_at": nullableDateTime,
		},
		"required": []any{"id", "workspace_id", "name", "endpoint_summary", "has_signing_secret", "event_types", "app_keys", "enabled", "created_by", "updated_by", "created_at", "updated_at"},
	}
	schemas["WebhookSubscriptionCreate"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": oapiStringSchema(), "endpoint": map[string]any{"type": "string", "format": "uri"},
			"signing_secret": map[string]any{"type": "string", "minLength": 16, "writeOnly": true},
			"event_types":    stringArray, "app_keys": stringArray, "enabled": oapiBooleanSchema(),
		},
		"required": []any{"name", "endpoint"},
	}
	schemas["WebhookSubscriptionUpdate"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": oapiStringSchema(), "endpoint": map[string]any{"type": "string", "format": "uri"},
			"signing_secret":        map[string]any{"type": "string", "minLength": 16, "writeOnly": true},
			"rotate_signing_secret": oapiBooleanSchema(), "event_types": stringArray, "app_keys": stringArray, "enabled": oapiBooleanSchema(),
		},
		"minProperties": 1,
	}
	schemas["WebhookSubscriptionMutation"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subscription":   oapiSchemaRef("WebhookSubscription"),
			"signing_secret": map[string]any{"type": "string", "description": "Generated or rotated secret returned only in this response.", "readOnly": true},
		},
		"required": []any{"subscription"},
	}
	schemas["ControlPlaneEvent"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"specversion": oapiStringSchema(), "id": oapiStringSchema(), "type": oapiStringSchema(),
			"source": oapiStringSchema(), "subject": oapiStringSchema(), "time": oapiDateTimeSchema(),
			"datacontenttype": oapiStringSchema(), "data": map[string]any{"type": "object", "additionalProperties": true},
		},
		"required": []any{"specversion", "id", "type", "source", "subject", "time", "datacontenttype", "data"},
	}
	schemas["WebhookDelivery"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": oapiStringSchema(), "workspace_id": oapiStringSchema(), "event_id": oapiStringSchema(), "subscription_id": oapiStringSchema(),
			"state":   map[string]any{"type": "string", "enum": []any{"pending", "delivering", "retrying", "succeeded", "failed", "canceled"}},
			"attempt": oapiIntegerSchema(), "next_attempt_at": oapiDateTimeSchema(), "lease_owner": nullableString, "lease_expires_at": nullableDateTime,
			"response_status": nullableInteger, "latency_ms": nullableInteger, "error_summary": nullableString,
			"created_at": oapiDateTimeSchema(), "updated_at": oapiDateTimeSchema(), "completed_at": nullableDateTime,
		},
		"required": []any{"id", "workspace_id", "event_id", "subscription_id", "state", "attempt", "next_attempt_at", "created_at", "updated_at"},
	}
	schemas["WebhookDeliveryDetail"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"delivery": oapiSchemaRef("WebhookDelivery"), "event": oapiSchemaRef("ControlPlaneEvent"), "subscription_name": oapiStringSchema(),
		},
		"required": []any{"delivery", "event", "subscription_name"},
	}
	schemas["WebhookDeliveryPage"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items":       map[string]any{"type": "array", "items": oapiSchemaRef("WebhookDeliveryDetail")},
			"next_cursor": oapiStringSchema(),
		},
		"required": []any{"items"},
	}
}
