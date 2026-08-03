package server

func addHumanTaskControlPlanePaths(paths map[string]any, workspaceID string) {
	paths["/api/w/{workspace}/human-tasks"] = map[string]any{
		"get": map[string]any{
			"operationId": "listHumanTasks",
			"summary":     "List held HumanTasks",
			"description": "Lists generic workspace-scoped HumanTasks. Private context and decision values are never returned.",
			"parameters": []any{
				oapiWorkspaceParam(workspaceID),
				oapiQueryParam("state", "Optional pending, decided, expired, or canceled state.", oapiStringSchema(), false),
				oapiQueryParam("limit", "Maximum number of tasks, up to 500.", oapiIntegerSchema(), false),
			},
			"responses": withErrors(map[string]any{
				"200": oapiResponse("HumanTasks.", oapiSchemaRef("HumanTaskList")),
			}, "400", "401", "403"),
		},
	}
	paths["/api/w/{workspace}/human-tasks/{taskId}"] = map[string]any{
		"get": map[string]any{
			"operationId": "getHumanTask",
			"summary":     "Get a held HumanTask",
			"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("taskId", "HumanTask id.")},
			"responses": withErrors(map[string]any{
				"200": oapiResponse("HumanTask metadata.", oapiSchemaRef("HumanTask")),
			}, "401", "403", "404"),
		},
	}
	paths["/api/w/{workspace}/human-tasks/{taskId}/decision"] = map[string]any{
		"post": map[string]any{
			"operationId": "decideHumanTask",
			"summary":     "Submit one HumanTask decision",
			"description": "Requires an Idempotency-Key header. The decision value is encrypted at rest and omitted from the response.",
			"parameters": []any{
				oapiWorkspaceParam(workspaceID),
				oapiPathParam("taskId", "HumanTask id."),
				map[string]any{"name": "Idempotency-Key", "in": "header", "required": true, "schema": oapiStringSchema()},
			},
			"requestBody": oapiJSONBody(oapiSchemaRef("HumanTaskDecisionRequest"), true),
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Accepted or idempotently replayed decision metadata.", oapiSchemaRef("HumanTaskDecisionResult")),
				"408": oapiResponse("The HumanTask deadline won the race.", humanTaskErrorSchema()),
			}, "400", "401", "403", "404", "409"),
		},
	}
	paths["/api/w/{workspace}/human-tasks/wait"] = map[string]any{
		"post": map[string]any{
			"operationId": "waitForHumanTask",
			"summary":     "Create and hold a runtime HumanTask",
			"description": "Job-token-only runtime callback. The original Action process, Job lease, and call stack remain active until a decision or terminal interruption.",
			"parameters":  []any{oapiWorkspaceParam(workspaceID)},
			"requestBody": oapiJSONBody(oapiSchemaRef("HumanTaskWaitRequest"), true),
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Decision returned to the same waiting process.", oapiSchemaRef("HumanTaskRuntimeDecision")),
				"408": oapiResponse("HumanTask deadline reached.", humanTaskErrorSchema()),
			}, "400", "401", "403", "404", "409"),
		},
	}
}

func humanTaskErrorSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"error":   oapiStringSchema(),
			"code":    oapiStringSchema(),
			"task_id": oapiStringSchema(),
		},
		"required": []any{"error", "code", "task_id"},
	}
}

func addHumanTaskControlPlaneSchemas(schemas map[string]any) {
	jsonValue := map[string]any{}
	schemas["HumanTask"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":                  oapiStringSchema(),
			"workspace_id":        oapiStringSchema(),
			"run_id":              oapiStringSchema(),
			"job_id":              oapiStringSchema(),
			"attempt":             oapiIntegerSchema(),
			"app":                 oapiStringSchema(),
			"action":              oapiStringSchema(),
			"key":                 oapiStringSchema(),
			"mode":                map[string]any{"type": "string", "enum": []any{"hold"}},
			"kind":                map[string]any{"type": "string", "enum": []any{"form"}},
			"state":               map[string]any{"type": "string", "enum": []any{"pending", "decided", "expired", "canceled"}},
			"title":               oapiStringSchema(),
			"description":         oapiStringSchema(),
			"input_schema":        jsonValue,
			"presentation":        jsonValue,
			"has_private_context": oapiBooleanSchema(),
			"decision_outcome":    map[string]any{"type": "string", "enum": []any{"submit", "cancel"}},
			"decided_by":          oapiStringSchema(),
			"terminal_cause":      oapiStringSchema(),
			"created_at":          map[string]any{"type": "string", "format": "date-time"},
			"updated_at":          map[string]any{"type": "string", "format": "date-time"},
			"decided_at":          map[string]any{"type": "string", "format": "date-time"},
			"expires_at":          map[string]any{"type": "string", "format": "date-time"},
		},
		"required": []any{"id", "workspace_id", "run_id", "job_id", "attempt", "mode", "kind", "state", "title", "created_at", "updated_at"},
	}
	schemas["HumanTaskList"] = map[string]any{
		"type":       "object",
		"required":   []any{"items"},
		"properties": map[string]any{"items": map[string]any{"type": "array", "items": oapiSchemaRef("HumanTask")}},
	}
	schemas["HumanTaskWaitRequest"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"key":             oapiStringSchema(),
			"kind":            map[string]any{"type": "string", "enum": []any{"form"}},
			"title":           oapiStringSchema(),
			"description":     oapiStringSchema(),
			"input_schema":    jsonValue,
			"presentation":    jsonValue,
			"private_context": map[string]any{"writeOnly": true},
			"timeout_ms":      oapiIntegerSchema(),
		},
		"required": []any{"key", "kind", "title", "input_schema"},
	}
	schemas["HumanTaskDecisionRequest"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"outcome": map[string]any{"type": "string", "enum": []any{"submit", "cancel"}},
			"value":   map[string]any{"writeOnly": true},
		},
		"required": []any{"outcome"},
	}
	schemas["HumanTaskDecisionResult"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task":     oapiSchemaRef("HumanTask"),
			"replayed": oapiBooleanSchema(),
		},
		"required": []any{"task", "replayed"},
	}
	schemas["HumanTaskRuntimeDecision"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": oapiStringSchema(),
			"outcome": map[string]any{"type": "string", "enum": []any{"submit", "cancel"}},
			"value":   jsonValue,
		},
		"required": []any{"task_id", "outcome"},
	}
}
