package server

import (
	"fmt"
	"sort"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
)

func buildAppOpenAPI(baseURL string, workspaceID string, deployment contract.Deployment, actions []openAPIAction) map[string]any {
	sorted := append([]openAPIAction(nil), actions...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ActionKey < sorted[j].ActionKey })
	safeFailureCodeSchema := map[string]any{
		"type":      "string",
		"pattern":   "^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$",
		"maxLength": 64,
	}

	createVariants := make([]any, 0, len(sorted))
	resultVariants := make([]any, 0, len(sorted))
	for _, action := range sorted {
		createVariants = append(createVariants, map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"properties": map[string]any{
				"app":            map[string]any{"type": "string", "const": deployment.App},
				"action":         map[string]any{"type": "string", "const": action.ActionKey},
				"input":          schemaOrAny(action.InputSchema),
				"correlation_id": oapiStringSchema(),
			},
			"required": []any{"app", "action", "input"},
		})
		resultVariants = append(resultVariants, schemaOrAny(action.OutputSchema))
	}
	resultVariants = append(resultVariants, map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":      oapiStringSchema(),
			"message":   oapiStringSchema(),
			"phase":     safeFailureCodeSchema,
			"reason":    safeFailureCodeSchema,
			"retryable": oapiBooleanSchema(),
		},
		"required": []any{"name", "message"},
	})
	createSchema := map[string]any{"oneOf": createVariants}
	if len(createVariants) == 0 {
		createSchema = map[string]any{"type": "object", "additionalProperties": false}
	}
	resultSchema := map[string]any{"anyOf": resultVariants}
	runStateSchema := oapiInvocationRunStateSchema()

	runSchema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"run_id":         oapiStringSchema(),
			"state":          runStateSchema,
			"app":            map[string]any{"type": "string", "const": deployment.App},
			"action":         map[string]any{"type": "string"},
			"correlation_id": oapiStringSchema(),
			"replayed":       oapiBooleanSchema(),
			"created_at":     map[string]any{"type": "string", "format": "date-time"},
			"updated_at":     map[string]any{"type": "string", "format": "date-time"},
		},
		"required": []any{"run_id", "state", "app", "action", "created_at", "updated_at"},
	}
	runResponses := withErrors(map[string]any{
		"200": oapiResponse("Run state.", runSchema),
		"201": oapiResponse("Run admitted.", runSchema),
	}, "400", "401", "403", "404", "409", "422")
	admissionRunResponses := withErrors(map[string]any{
		"200": oapiInvocationAdmissionResponse("Existing Run reused.", runSchema),
		"201": oapiInvocationAdmissionResponse("Run admitted.", runSchema),
	}, "400", "401", "403", "404", "409", "422")
	runIDParameter := oapiPathParam("run_id", "Caller-visible Run identifier.")
	workspaceParameter := oapiPathParam("workspace", fmt.Sprintf("Workspace id. This document was generated for %q.", workspaceID))

	paths := map[string]any{
		"/api/v1/workspaces/{workspace}/runs": map[string]any{
			"post": map[string]any{
				"operationId": "createRun",
				"summary":     fmt.Sprintf("Admit a %s Run", deployment.App),
				"description": "Creates the caller-visible Run lifecycle resource. Supply Idempotency-Key as an HTTP header when retries must replay the same Run.",
				"parameters": []any{
					workspaceParameter,
					oapiHeaderParam("Idempotency-Key", "Principal-scoped idempotency key.", oapiStringSchema(), false),
				},
				"requestBody": oapiJSONBody(createSchema, true),
				"responses":   admissionRunResponses,
			},
		},
		"/api/v1/workspaces/{workspace}/runs/wait": map[string]any{
			"post": map[string]any{
				"operationId": "createRunAndWait",
				"summary":     fmt.Sprintf("Admit a %s Run and wait", deployment.App),
				"description": "Returns the action result when the Run settles before the timeout. Response headers identify the Run, its observed state, and whether admission reused an existing idempotent Run.",
				"parameters": []any{
					workspaceParameter,
					oapiHeaderParam("Idempotency-Key", "Principal-scoped idempotency key.", oapiStringSchema(), false),
					oapiQueryParam("timeout", "Wait duration such as 30s.", oapiStringSchema(), false),
				},
				"requestBody": oapiJSONBody(createSchema, true),
				"responses": withErrors(map[string]any{
					"200": oapiInvocationAdmissionResponse("Terminal action result.", resultSchema),
					"202": oapiInvocationAdmissionResponse("Run is still active.", runSchema),
				}, "400", "401", "403", "404", "409", "422"),
			},
		},
		"/api/v1/workspaces/{workspace}/runs/{run_id}": map[string]any{
			"get": map[string]any{
				"operationId": "getRun",
				"summary":     "Get Run state",
				"parameters":  []any{workspaceParameter, runIDParameter},
				"responses":   runResponses,
			},
		},
		"/api/v1/workspaces/{workspace}/runs/{run_id}/result": map[string]any{
			"get": map[string]any{
				"operationId": "getRunResult",
				"summary":     "Get Run result",
				"parameters":  []any{workspaceParameter, runIDParameter},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Terminal action result.", resultSchema),
					"202": oapiResponse("Run is still active.", runSchema),
				}, "401", "403", "404"),
			},
		},
		"/api/v1/workspaces/{workspace}/runs/{run_id}/cancel": map[string]any{
			"post": map[string]any{
				"operationId": "cancelRun",
				"summary":     "Cancel a Run",
				"parameters":  []any{workspaceParameter, runIDParameter},
				"requestBody": oapiJSONBody(map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties":           map[string]any{"reason": oapiStringSchema()},
				}, false),
				"responses": runResponses,
			},
		},
	}

	version := "current"
	if commit := strings.TrimSpace(deployment.Commit); commit != "" {
		if len(commit) > 12 {
			commit = commit[:12]
		}
		version = commit
	}

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       deployment.App + " Invocation API",
			"version":     version,
			"description": "App-specific view of the canonical Run-based Invocation API, generated from the active release action schemas.",
		},
		"servers":  []any{map[string]any{"url": baseURL}},
		"security": []any{map[string]any{"bearerAuth": []any{}}},
		"components": map[string]any{
			"schemas": map[string]any{
				"Error": map[string]any{
					"type":       "object",
					"properties": map[string]any{"error": oapiStringSchema()},
					"required":   []any{"error"},
				},
			},
			"responses": appOpenAPIErrorResponses(),
			"securitySchemes": map[string]any{
				"bearerAuth": map[string]any{
					"type":        "http",
					"scheme":      "bearer",
					"description": "Operator, workspace, client, or service-principal bearer accepted by the Invocation API.",
				},
			},
		},
		"paths": paths,
	}
}

func oapiInvocationRunStateSchema() map[string]any {
	return oapiStringEnumSchema("queued", "running", "waiting_human", "resuming", "succeeded", "failed", "canceled", "expired")
}

func oapiInvocationAdmissionResponse(description string, schema any) map[string]any {
	response := oapiResponse(description, schema)
	response["headers"] = map[string]any{
		"Location": map[string]any{
			"description": "Canonical URL of the admitted Run.",
			"schema":      oapiStringSchema(),
		},
		invocationRunIDHeader: map[string]any{
			"description": "Caller-visible Run identifier.",
			"schema":      oapiStringSchema(),
		},
		invocationRunStateHeader: map[string]any{
			"description": "Run state observed for this response.",
			"schema":      oapiInvocationRunStateSchema(),
		},
		invocationIdempotencyReusedHeader: map[string]any{
			"description": "True when this admission reused the existing Run for the same Idempotency-Key and request.",
			"schema":      oapiBooleanSchema(),
		},
	}
	return response
}
