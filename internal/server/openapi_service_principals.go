package server

func addServicePrincipalControlPlanePaths(paths map[string]any, workspaceID string) {
	collection := "/api/w/{workspace}/service-principals"
	item := collection + "/{service_principal_id}"
	itemParameters := []any{
		oapiWorkspaceParam(workspaceID),
		oapiPathParam("service_principal_id", "Service principal id."),
	}
	paths[collection] = map[string]any{
		"get": map[string]any{
			"operationId": "listServicePrincipals",
			"summary":     "List service principals",
			"parameters":  []any{oapiWorkspaceParam(workspaceID)},
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Workspace service principals.", map[string]any{
					"type": "array", "items": oapiSchemaRef("ServicePrincipal"),
				}),
			}, "401", "403"),
		},
		"post": map[string]any{
			"operationId": "createServicePrincipal",
			"summary":     "Create a service principal",
			"parameters":  []any{oapiWorkspaceParam(workspaceID)},
			"requestBody": oapiJSONBody(oapiSchemaRef("CreateServicePrincipalRequest"), true),
			"responses": withErrors(map[string]any{
				"201": oapiResponse("Service principal and its one-time API token.", oapiSchemaRef("ServicePrincipalTokenResult")),
			}, "400", "401", "403", "409"),
		},
	}
	paths[item] = map[string]any{
		"get": map[string]any{
			"operationId": "getServicePrincipal",
			"summary":     "Get a service principal",
			"parameters":  itemParameters,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Service principal.", oapiSchemaRef("ServicePrincipal")),
			}, "401", "403", "404"),
		},
		"patch": map[string]any{
			"operationId": "updateServicePrincipal",
			"summary":     "Update service principal scopes and target permissions",
			"parameters":  itemParameters,
			"requestBody": oapiJSONBody(oapiSchemaRef("UpdateServicePrincipalRequest"), true),
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Updated service principal.", oapiSchemaRef("ServicePrincipal")),
			}, "400", "401", "403", "404", "409"),
		},
		"delete": map[string]any{
			"operationId": "deleteServicePrincipal",
			"summary":     "Delete a service principal after token revocation",
			"parameters":  itemParameters,
			"responses": withErrors(map[string]any{
				"204": oapiResponse("Service principal deleted.", nil),
			}, "401", "403", "404", "409"),
		},
	}
	paths[item+"/token"] = map[string]any{
		"post": map[string]any{
			"operationId": "rotateServicePrincipalToken",
			"summary":     "Rotate a service principal token",
			"parameters":  itemParameters,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Service principal and its new one-time API token.", oapiSchemaRef("ServicePrincipalTokenResult")),
			}, "401", "403", "404", "409"),
		},
		"delete": map[string]any{
			"operationId": "revokeServicePrincipalToken",
			"summary":     "Revoke a service principal token",
			"parameters":  itemParameters,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Service principal with no active token.", oapiSchemaRef("ServicePrincipal")),
			}, "401", "403", "404"),
		},
	}
	paths[item+"/audit"] = map[string]any{
		"get": map[string]any{
			"operationId": "listServicePrincipalAudit",
			"summary":     "List service principal audit records",
			"parameters":  itemParameters,
			"responses": withErrors(map[string]any{
				"200": oapiResponse("Service principal audit trail.", map[string]any{
					"type": "array", "items": oapiSchemaRef("ServicePrincipalAudit"),
				}),
			}, "401", "403", "404"),
		},
	}
}

func addServicePrincipalControlPlaneSchemas(schemas map[string]any) {
	stringList := func() map[string]any {
		return map[string]any{"type": "array", "items": oapiStringSchema()}
	}
	scopeList := func() map[string]any {
		return map[string]any{
			"type": "array",
			"items": oapiStringEnumSchema(
				"runs:create",
				"runs:read:own",
				"runs:read:any",
				"runs:cancel:own",
				"runs:cancel:any",
				"apps:read",
			),
		}
	}
	schemas["ServicePrincipal"] = map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required": []any{
			"id", "workspace_id", "name", "has_token", "scopes", "allowed_targets",
			"created_by", "updated_by", "created_at", "updated_at",
		},
		"properties": map[string]any{
			"id":              oapiStringSchema(),
			"workspace_id":    oapiStringSchema(),
			"name":            oapiStringSchema(),
			"has_token":       oapiBooleanSchema(),
			"scopes":          scopeList(),
			"allowed_targets": stringList(),
			"created_by":      oapiStringSchema(),
			"updated_by":      oapiStringSchema(),
			"created_at":      oapiDateTimeSchema(),
			"updated_at":      oapiDateTimeSchema(),
		},
	}
	schemas["CreateServicePrincipalRequest"] = map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"name", "scopes"},
		"properties": map[string]any{
			"name":            oapiStringSchema(),
			"scopes":          scopeList(),
			"allowed_targets": stringList(),
		},
	}
	schemas["UpdateServicePrincipalRequest"] = map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"minProperties":        1,
		"properties": map[string]any{
			"name":            oapiStringSchema(),
			"scopes":          scopeList(),
			"allowed_targets": stringList(),
		},
	}
	schemas["ServicePrincipalTokenResult"] = map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []any{"service_principal", "api_token"},
		"properties": map[string]any{
			"service_principal": oapiSchemaRef("ServicePrincipal"),
			"api_token":         oapiStringSchema(),
		},
	}
	schemas["ServicePrincipalAudit"] = map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required": []any{
			"id", "workspace_id", "service_principal_id", "kind", "detail", "actor", "created_at",
		},
		"properties": map[string]any{
			"id":                   oapiStringSchema(),
			"workspace_id":         oapiStringSchema(),
			"service_principal_id": oapiStringSchema(),
			"kind":                 oapiStringSchema(),
			"detail":               oapiStringSchema(),
			"actor":                oapiStringSchema(),
			"created_at":           oapiDateTimeSchema(),
		},
	}
}
