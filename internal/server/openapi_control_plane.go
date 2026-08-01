package server

func buildControlPlaneOpenAPI(baseURL string, workspaceID string) map[string]any {
	paths := map[string]any{
		"/api/queue-demand-snapshots": map[string]any{
			"post": map[string]any{
				"operationId": "createQueueDemandSnapshot",
				"summary":     "Read a fenced bulk queue-demand snapshot",
				"description": "Instance-admin operation. Every selector result is evaluated against one authoritative store epoch, revision, and observed time.",
				"requestBody": oapiJSONBody(oapiSchemaRef("QueueDemandSnapshotRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Fenced queue-demand snapshot.", oapiSchemaRef("QueueDemandSnapshot")),
				}, "400", "401", "403", "413", "500", "503"),
			},
		},
		"/api/workspaces": map[string]any{
			"get": map[string]any{
				"operationId": "listWorkspaces",
				"summary":     "List managed workspaces",
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Managed workspace registry.", oapiSchemaRef("WorkspaceListResponse")),
				}, "401", "403"),
			},
			"post": map[string]any{
				"operationId": "createWorkspace",
				"summary":     "Create a managed workspace",
				"requestBody": oapiJSONBody(oapiSchemaRef("CreateWorkspaceRequest"), true),
				"responses": withErrors(map[string]any{
					"201": oapiResponse("Created workspace.", oapiSchemaRef("Workspace")),
				}, "400", "401", "403", "409"),
			},
		},
		"/api/workspaces/{workspace_id}": map[string]any{
			"get": map[string]any{
				"operationId": "getWorkspace",
				"summary":     "Get a managed workspace",
				"parameters":  []any{oapiPathParam("workspace_id", "Immutable workspace id.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Managed workspace.", oapiSchemaRef("Workspace")),
				}, "401", "403", "404"),
			},
			"patch": map[string]any{
				"operationId": "updateWorkspace",
				"summary":     "Update a workspace display name",
				"parameters":  []any{oapiPathParam("workspace_id", "Immutable workspace id.")},
				"requestBody": oapiJSONBody(oapiSchemaRef("UpdateWorkspaceRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Updated workspace.", oapiSchemaRef("Workspace")),
				}, "400", "401", "403", "404", "409"),
			},
		},
		"/api/workspaces/{workspace_id}/archive": map[string]any{
			"post": map[string]any{
				"operationId": "archiveWorkspace",
				"summary":     "Archive a managed workspace",
				"parameters":  []any{oapiPathParam("workspace_id", "Immutable workspace id.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Archived workspace.", oapiSchemaRef("Workspace")),
				}, "400", "401", "403", "404", "409"),
			},
		},
		"/api/workspaces/{workspace_id}/tokens": map[string]any{
			"get": map[string]any{
				"operationId": "listWorkspaceTokens",
				"summary":     "List named workspace API tokens",
				"parameters":  []any{oapiPathParam("workspace_id", "Immutable workspace id.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Workspace token registry.", oapiSchemaRef("WorkspaceTokenListResponse")),
				}, "401", "403", "404"),
			},
			"post": map[string]any{
				"operationId": "createWorkspaceToken",
				"summary":     "Create a named workspace API token",
				"parameters":  []any{oapiPathParam("workspace_id", "Immutable workspace id.")},
				"requestBody": oapiJSONBody(oapiSchemaRef("CreateWorkspaceTokenRequest"), true),
				"responses": withErrors(map[string]any{
					"201": oapiResponse("Named token and its one-time secret.", oapiSchemaRef("WorkspaceTokenResult")),
				}, "400", "401", "403", "404", "409"),
			},
		},
		"/api/workspaces/{workspace_id}/tokens/{token_id}": map[string]any{
			"delete": map[string]any{
				"operationId": "revokeWorkspaceToken",
				"summary":     "Revoke a named workspace API token",
				"parameters": []any{
					oapiPathParam("workspace_id", "Immutable workspace id."),
					oapiPathParam("token_id", "Workspace token id."),
				},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Revoked workspace token.", oapiSchemaRef("WorkspaceToken")),
				}, "401", "403", "404", "409"),
			},
		},
		"/api/workspaces/{workspace_id}/tokens/{token_id}/rotate": map[string]any{
			"post": map[string]any{
				"operationId": "rotateWorkspaceToken",
				"summary":     "Rotate a named workspace API token",
				"parameters": []any{
					oapiPathParam("workspace_id", "Immutable workspace id."),
					oapiPathParam("token_id", "Workspace token id."),
				},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Rotated token and its new one-time secret.", oapiSchemaRef("WorkspaceTokenResult")),
				}, "401", "403", "404", "409"),
			},
		},
		"/api/workspaces/{workspace_id}/audit": map[string]any{
			"get": map[string]any{
				"operationId": "listWorkspaceAudit",
				"summary":     "List workspace lifecycle audit records",
				"parameters":  []any{oapiPathParam("workspace_id", "Immutable workspace id.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Workspace lifecycle audit trail.", map[string]any{"type": "array", "items": oapiSchemaRef("WorkspaceAudit")}),
				}, "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/openapi.json": map[string]any{
			"get": map[string]any{
				"operationId": "getControlPlaneOpenAPI",
				"summary":     "Get the control-plane OpenAPI document",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"responses": map[string]any{
					"200": oapiResponse("Control-plane OpenAPI document.", map[string]any{"type": "object", "additionalProperties": true}),
				},
			},
		},
		"/api/w/{workspace}/provisioning/import": map[string]any{
			"post": map[string]any{
				"operationId": "importProvisioningResources",
				"summary":     "Import provisioning resources",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiQueryParam("dry_run", "Validate resources without writing state.", oapiBooleanSchema(), false),
				},
				"requestBody": oapiJSONBody(oapiSchemaRef("ProvisioningImportRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Provisioning import result.", oapiSchemaRef("ProvisioningResult")),
				}, "400", "401", "403", "422"),
			},
		},
		"/api/w/{workspace}/provisioning/export": map[string]any{
			"get": map[string]any{
				"operationId": "exportProvisioningResources",
				"summary":     "Export provisioning resources",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiQueryParam("format", "Response format.", map[string]any{"type": "string", "enum": []any{"json", "yaml"}}, false),
					oapiQueryParam("include_values", "Include non-secret values where export permits it.", oapiBooleanSchema(), false),
				},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Provisioning resources.", oapiSchemaRef("ProvisioningExportResponse")),
				}, "401", "403"),
			},
		},
		"/api/w/{workspace}/audit-events": map[string]any{
			"get": map[string]any{
				"operationId": "listAuditEvents",
				"summary":     "List canonical workspace audit events",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiQueryParam("app_key", "Optional app key filter.", oapiStringSchema(), false),
					oapiQueryParam("client_id", "Optional client id filter.", oapiStringSchema(), false),
					oapiQueryParam("category", "Optional event category filter.", map[string]any{"type": "string", "enum": []any{"workspace", "repository", "release", "client", "input_settings", "runtime_configuration", "webhook"}}, false),
					oapiQueryParam("actor", "Optional case-insensitive actor filter.", oapiStringSchema(), false),
					oapiQueryParam("git_source_id", "Optional numeric git source id filter.", oapiIntegerSchema(), false),
					oapiQueryParam("since", "RFC3339 lower bound for created_at.", oapiStringSchema(), false),
					oapiQueryParam("until", "RFC3339 upper bound for created_at.", oapiStringSchema(), false),
					oapiQueryParam("limit", "Maximum number of events from 1 to 500. Defaults to 100.", oapiIntegerSchema(), false),
				},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Canonical workspace audit events, newest first.", map[string]any{
						"type": "array", "items": oapiSchemaRef("AuditEvent"),
					}),
				}, "400", "401", "403"),
			},
		},
		"/api/w/{workspace}/clients": map[string]any{
			"get": map[string]any{
				"operationId": "listClients",
				"summary":     "List registered external clients",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Workspace client registry.", map[string]any{
						"type": "array", "items": oapiSchemaRef("Client"),
					}),
				}, "401", "403"),
			},
			"post": map[string]any{
				"operationId": "createClient",
				"summary":     "Register an external client",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"requestBody": oapiJSONBody(oapiSchemaRef("CreateClientRequest"), true),
				"responses": withErrors(map[string]any{
					"201": oapiResponse("Registered client and its one-time API token.", oapiSchemaRef("ClientTokenResult")),
				}, "400", "401", "403", "409"),
			},
		},
		"/api/w/{workspace}/clients/{client_id}": map[string]any{
			"get": map[string]any{
				"operationId": "getClient",
				"summary":     "Get a registered client",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("client_id", "Client id.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Registered client.", oapiSchemaRef("Client")),
				}, "401", "403", "404"),
			},
			"patch": map[string]any{
				"operationId": "updateClient",
				"summary":     "Update a registered client",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("client_id", "Client id.")},
				"requestBody": oapiJSONBody(oapiSchemaRef("UpdateClientRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Updated client.", oapiSchemaRef("Client")),
				}, "400", "401", "403", "404", "409"),
			},
			"delete": map[string]any{
				"operationId": "deleteClient",
				"summary":     "Delete a registered client",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("client_id", "Client id.")},
				"responses": withErrors(map[string]any{
					"204": oapiResponse("Client deleted.", nil),
				}, "401", "403", "404", "409"),
			},
		},
		"/api/w/{workspace}/clients/{client_id}/token": map[string]any{
			"post": map[string]any{
				"operationId": "rotateClientToken",
				"summary":     "Rotate a client API token",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("client_id", "Client id.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Client and its new one-time API token.", oapiSchemaRef("ClientTokenResult")),
				}, "401", "403", "404", "409"),
			},
			"delete": map[string]any{
				"operationId": "revokeClientToken",
				"summary":     "Revoke a client API token",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("client_id", "Client id.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Client with no active API token.", oapiSchemaRef("Client")),
				}, "401", "403", "404", "409"),
			},
		},
		"/api/w/{workspace}/clients/{client_id}/audit": map[string]any{
			"get": map[string]any{
				"operationId": "listClientAudit",
				"summary":     "List client registry audit records",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("client_id", "Client id.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Client registry audit trail.", map[string]any{
						"type": "array", "items": oapiSchemaRef("ClientAudit"),
					}),
				}, "401", "403"),
			},
		},
		"/api/w/{workspace}/clients/{client_id}/input-configs": map[string]any{
			"get": map[string]any{
				"operationId": "listClientInputConfigs",
				"summary":     "List input settings for an external client",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("client_id", "Client id.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Client input settings.", map[string]any{"type": "array", "items": oapiSchemaRef("InputConfig")}),
				}, "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/clients/{client_id}/input-config-audit": map[string]any{
			"get": map[string]any{
				"operationId": "listClientInputConfigAudit",
				"summary":     "List input-setting audit records for an external client",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("client_id", "Client id.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Client input-setting audit trail.", map[string]any{"type": "array", "items": oapiSchemaRef("InputConfigAudit")}),
				}, "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/git_sources": map[string]any{
			"get": map[string]any{
				"operationId": "listGitSources",
				"summary":     "List git sources",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Registered git sources.", map[string]any{
						"type":  "array",
						"items": oapiSchemaRef("GitSource"),
					}),
				}, "401", "403"),
			},
			"post": map[string]any{
				"operationId": "registerGitSource",
				"summary":     "Validate and register a git source",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"requestBody": oapiJSONBody(oapiSchemaRef("RegisterGitSourceRequest"), true),
				"responses": withErrors(map[string]any{
					"201": oapiResponse("Registered git source.", oapiSchemaRef("GitSource")),
				}, "400", "401", "403", "422"),
			},
		},
		"/api/w/{workspace}/git_sources/probe": map[string]any{
			"post": map[string]any{
				"operationId": "probeGitSource",
				"summary":     "Probe a git source without registering it",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"requestBody": oapiJSONBody(oapiSchemaRef("ProbeGitSourceRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Probe result.", oapiSchemaRef("GitSourceProbeResult")),
				}, "400", "401", "403"),
			},
		},
		"/api/w/{workspace}/git_sources/sample": map[string]any{
			"post": map[string]any{
				"operationId": "createSampleGitSource",
				"summary":     "Create, synchronize, and publish a managed sample git source",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"requestBody": oapiJSONBody(oapiSchemaRef("SampleGitSourceRequest"), false),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Existing sample source synchronized and published.", oapiSchemaRef("SampleSyncResponse")),
					"201": oapiResponse("Sample source created, synchronized, and published.", oapiSchemaRef("SampleSyncResponse")),
				}, "400", "401", "403"),
			},
		},
		"/api/w/{workspace}/git_sources/{gitSourceId}": map[string]any{
			"patch": map[string]any{
				"operationId": "patchGitSource",
				"summary":     "Patch a git source",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("gitSourceId", "Numeric git source id returned by register/list.")},
				"requestBody": oapiJSONBody(oapiSchemaRef("PatchGitSourceRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Updated git source.", oapiSchemaRef("GitSource")),
				}, "400", "401", "403", "404", "422"),
			},
			"delete": map[string]any{
				"operationId": "deleteGitSource",
				"summary":     "Delete a git source",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("gitSourceId", "Numeric git source id returned by register/list.")},
				"responses": withErrors(map[string]any{
					"204": map[string]any{"description": "Deleted."},
				}, "400", "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/git_sources/{gitSourceId}/sync": map[string]any{
			"post": map[string]any{
				"operationId": "syncGitSource",
				"summary":     "Synchronize an immutable source revision",
				"description": "Resolves the remote branch, optionally requires it to match expected_commit, validates the manifest, schemas, and lockfile, then stores the exact source revision. Runtime dependencies are not installed and the active release is not changed.",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("gitSourceId", "Numeric git source id returned by register/list.")},
				"requestBody": oapiJSONBody(oapiSchemaRef("SyncGitSourceRequest"), false),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Synchronized source revision and discovered actions.", oapiSchemaRef("GitSourceSyncResult")),
				}, "400", "401", "403", "404", "409", "422"),
			},
		},
		"/api/w/{workspace}/git_sources/{gitSourceId}/deploy": map[string]any{
			"post": map[string]any{
				"operationId": "deployGitSource",
				"summary":     "Prepare and publish the latest synchronized revision",
				"description": "Pins the latest synchronized source at operation start, optionally requires it to match expected_commit, prepares and validates its execution bundle, then atomically makes it visible to new jobs and records release history, audit, and Control Plane events.",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("gitSourceId", "Numeric git source id returned by register/list.")},
				"requestBody": oapiJSONBody(oapiSchemaRef("DeployGitSourceRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Published release and discovered actions.", oapiSchemaRef("GitSourceDeployResult")),
				}, "400", "401", "403", "404", "409", "422"),
			},
		},
		"/api/w/{workspace}/apps": map[string]any{
			"get": map[string]any{
				"operationId": "listApps",
				"summary":     "List apps",
				"description": "The bare response is an array of app keys. Use view=summary for catalog rows.",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiQueryParam("view", "Set to summary to return app summary rows.", map[string]any{"type": "string", "enum": []any{"summary"}}, false),
				},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("App keys or summary rows.", map[string]any{
						"oneOf": []any{
							map[string]any{"type": "array", "items": oapiStringSchema()},
							oapiSchemaRef("AppsSummaryResponse"),
						},
					}),
				}, "401", "403"),
			},
		},
		"/api/w/{workspace}/apps/{app}": map[string]any{
			"get": map[string]any{
				"operationId": "getApp",
				"summary":     "Get app detail and action contracts",
				"description": "Returns app metadata and actions. Each action includes Windforce catalog input_schema and output_schema fields as base64-encoded materialized JSON Schema bytes. This is the bulk schema discovery API.",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("App detail including action schemas.", oapiSchemaRef("AppDetailResponse")),
				}, "400", "401", "403", "404"),
			},
			"patch": map[string]any{
				"operationId": "patchApp",
				"summary":     "Set or clear the app route tag override",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key.")},
				"requestBody": oapiJSONBody(oapiSchemaRef("TagOverrideRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Updated app.", oapiSchemaRef("App")),
				}, "400", "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/apps/{app}/source": map[string]any{
			"get": map[string]any{
				"operationId": "getAppSource",
				"summary":     "Get materialized app source files",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Materialized source files.", oapiSchemaRef("AppSourceResponse")),
				}, "400", "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/apps/{app}/documentation": map[string]any{
			"get": map[string]any{
				"operationId": "getAppDocumentation",
				"summary":     "Get active release documentation",
				"description": "Returns README.md from the source snapshot pinned by the active release. It never reads a mutable repository branch.",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Active release README, when present.", oapiSchemaRef("AppDocumentationResponse")),
				}, "400", "401", "403", "404", "422"),
			},
		},
		"/api/w/{workspace}/apps/{app}/history": map[string]any{
			"get": map[string]any{
				"operationId": "getAppHistory",
				"summary":     "Get app deployment history",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Deployment history.", map[string]any{"type": "array", "items": oapiSchemaRef("AppHistoryItem")}),
				}, "400", "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/apps/{app}/releases/{releaseId}/rollback": map[string]any{
			"post": map[string]any{
				"operationId": "rollbackAppRelease",
				"summary":     "Activate a historical app release",
				"description": "Validates the stored execution bundle and atomically moves the active release pointer. It does not synchronize Git or rebuild the execution bundle.",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiPathParam("app", "App key."),
					oapiPathParam("releaseId", "Historical release ID."),
				},
				"requestBody": oapiJSONBody(oapiSchemaRef("ReleaseRollbackRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("The historical release is active.", oapiSchemaRef("ReleaseRollbackResponse")),
				}, "400", "401", "403", "404", "409", "422", "503"),
			},
		},
		"/api/w/{workspace}/apps/{app}/openapi.json": map[string]any{
			"get": map[string]any{
				"operationId": "getAppInvocationOpenAPI",
				"summary":     "Get app invocation OpenAPI",
				"description": "Returns an app-specific OpenAPI generated from the materialized action input/output schemas.",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("App invocation OpenAPI.", map[string]any{"type": "object", "additionalProperties": true}),
				}, "400", "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/apps/{app}/input-configs": map[string]any{
			"get": map[string]any{
				"operationId": "listAppInputConfigs",
				"summary":     "List default and client-specific input settings",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("App input settings.", map[string]any{"type": "array", "items": oapiSchemaRef("InputConfig")}),
				}, "401", "403", "404"),
			},
			"put": map[string]any{
				"operationId": "setAppInputConfig",
				"summary":     "Set one app, action, and client input-setting layer",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key.")},
				"requestBody": oapiJSONBody(oapiSchemaRef("SetInputConfigRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Saved input-setting layer.", oapiSchemaRef("InputConfig")),
				}, "400", "401", "403", "404"),
			},
			"delete": map[string]any{
				"operationId": "deleteAppInputConfig",
				"summary":     "Delete one input-setting layer",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key."),
					oapiQueryParam("action_key", "Empty selects the app-level layer.", oapiStringSchema(), false),
					oapiQueryParam("client_id", "Empty selects the client-independent default layer.", oapiStringSchema(), false),
				},
				"responses": withErrors(map[string]any{
					"204": oapiResponse("Input-setting layer deleted.", nil),
				}, "400", "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/apps/{app}/input-config-audit": map[string]any{
			"get": map[string]any{
				"operationId": "listAppInputConfigAudit",
				"summary":     "List input-setting audit records for an app",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("App input-setting audit trail.", map[string]any{"type": "array", "items": oapiSchemaRef("InputConfigAudit")}),
				}, "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/apps/{app}/actions/{action}": map[string]any{
			"get": map[string]any{
				"operationId": "getAction",
				"summary":     "Get action detail and schemas",
				"description": "Returns canonical action metadata. input_schema and output_schema use Windforce catalog encoding: base64-encoded materialized JSON Schema bytes from windforce.json/source. Use the sibling /schema endpoint when a control-plane client needs raw JSON Schema documents.",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key."), oapiPathParam("action", "Action key.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Action detail including materialized schemas.", oapiSchemaRef("Action")),
				}, "400", "401", "403", "404"),
			},
			"patch": map[string]any{
				"operationId": "patchAction",
				"summary":     "Set or clear the action route tag override",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key."), oapiPathParam("action", "Action key.")},
				"requestBody": oapiJSONBody(oapiSchemaRef("TagOverrideRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Updated action.", oapiSchemaRef("Action")),
				}, "400", "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/apps/{app}/actions/{action}/schema": map[string]any{
			"get": map[string]any{
				"operationId": "getActionSchema",
				"summary":     "Get action JSON Schemas",
				"description": "Schema discovery endpoint for protocol adapters and UI forms. Returns request, result, and operator-settings JSON Schema documents pinned by the active release, while GET /actions/{action} keeps Windforce's canonical base64 catalog encoding.",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key."), oapiPathParam("action", "Action key.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Action request, result, and operator-settings JSON Schemas.", oapiSchemaRef("ActionSchema")),
				}, "400", "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/apps/{app}/requeue": map[string]any{
			"post": map[string]any{
				"operationId": "requeueApp",
				"summary":     "Requeue queued jobs for an app",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("app", "App key.")},
				"requestBody": oapiJSONBody(map[string]any{"type": "object", "additionalProperties": false}, false),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Requeue count.", oapiSchemaRef("RequeueResponse")),
				}, "400", "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/worker-tags": map[string]any{
			"get": map[string]any{
				"operationId": "listWorkerTags",
				"summary":     "List worker tag liveness",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Worker tag liveness.", oapiSchemaRef("WorkerTagsResponse")),
				}, "401", "403"),
			},
		},
		"/api/w/{workspace}/state": map[string]any{
			"get": map[string]any{
				"operationId": "getState",
				"summary":     "Get a ctx.state value",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiQueryParam("path", "State path.", oapiStringSchema(), true),
				},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Stored JSON value or null.", oapiSchemaRef("JSONValue")),
				}, "400", "401", "403"),
			},
			"post": map[string]any{
				"operationId": "setState",
				"summary":     "Set a ctx.state value",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiQueryParam("path", "State path.", oapiStringSchema(), true),
				},
				"requestBody": oapiJSONBody(oapiSchemaRef("JSONValue"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Stored path.", oapiSchemaRef("PathResponse")),
				}, "400", "401", "403"),
			},
		},
		"/api/w/{workspace}/variables": map[string]any{
			"get": map[string]any{
				"operationId": "listVariables",
				"summary":     "List workspace variables",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Variables. Secret values are redacted in list responses.", map[string]any{"type": "array", "items": oapiSchemaRef("Variable")}),
				}, "401", "403"),
			},
			"post": map[string]any{
				"operationId": "setVariable",
				"summary":     "Set a workspace or app-scoped variable",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"requestBody": oapiJSONBody(oapiSchemaRef("SetVariableRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Stored variable key.", oapiSchemaRef("VariableSetResponse")),
				}, "400", "401", "403"),
			},
		},
		"/api/w/{workspace}/variables/get/p/{path}": map[string]any{
			"get": map[string]any{
				"operationId": "getVariable",
				"summary":     "Get a variable by path",
				"description": "The {path} segment represents the remaining path after /variables/get/p/ and may contain slashes.",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiPathParam("path", "Variable path."),
					oapiQueryParam("app", "Optional exact app key scope for console lookup.", oapiStringSchema(), false),
				},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Variable metadata. Secret values are omitted outside runtime callbacks.", oapiSchemaRef("VariableValueResponse")),
				}, "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/variables/p/{path}": map[string]any{
			"delete": map[string]any{
				"operationId": "deleteVariable",
				"summary":     "Delete a variable by path",
				"description": "The {path} segment represents the remaining path after /variables/p/ and may contain slashes.",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiPathParam("path", "Variable path."),
					oapiQueryParam("app", "Optional app key for app-scoped deletion.", oapiStringSchema(), false),
				},
				"responses": withErrors(map[string]any{
					"204": map[string]any{"description": "Deleted."},
				}, "401", "403"),
			},
		},
		"/api/w/{workspace}/resource-types": map[string]any{
			"get": map[string]any{
				"operationId": "listResourceTypes",
				"summary":     "List resource types",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Registered JSON Schema resource types.", map[string]any{"type": "array", "items": oapiSchemaRef("ResourceType")}),
				}, "401", "403"),
			},
			"post": map[string]any{
				"operationId": "setResourceType",
				"summary":     "Create or replace a resource type version",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"requestBody": oapiJSONBody(oapiSchemaRef("ResourceType"), true),
				"responses": withErrors(map[string]any{
					"201": oapiResponse("Stored resource type.", oapiSchemaRef("ResourceType")),
				}, "400", "401", "403"),
			},
		},
		"/api/w/{workspace}/resource-types/{name}/{version}": map[string]any{
			"get": map[string]any{
				"operationId": "getResourceType",
				"summary":     "Get a resource type version",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiPathParam("name", "Resource type name."),
					oapiPathParam("version", "Resource type version."),
				},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Resource type.", oapiSchemaRef("ResourceType")),
				}, "401", "403", "404"),
			},
			"delete": map[string]any{
				"operationId": "deleteResourceType",
				"summary":     "Delete an unused resource type version",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiPathParam("name", "Resource type name."),
					oapiPathParam("version", "Resource type version."),
				},
				"responses": withErrors(map[string]any{
					"204": map[string]any{"description": "Deleted."},
				}, "401", "403", "404", "409"),
			},
		},
		"/api/w/{workspace}/resources": map[string]any{
			"get": map[string]any{
				"operationId": "listResources",
				"summary":     "List JSON resources",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Resources with unresolved references.", map[string]any{"type": "array", "items": oapiSchemaRef("Resource")}),
				}, "401", "403"),
			},
			"post": map[string]any{
				"operationId": "setResource",
				"summary":     "Set a JSON resource",
				"parameters":  []any{oapiWorkspaceParam(workspaceID)},
				"requestBody": oapiJSONBody(oapiSchemaRef("SetResourceRequest"), true),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Stored resource path.", oapiSchemaRef("PathResponse")),
				}, "400", "401", "403"),
			},
		},
		"/api/w/{workspace}/resources/p/{path}": map[string]any{
			"delete": map[string]any{
				"operationId": "deleteResource",
				"summary":     "Delete a resource by path",
				"description": "The {path} segment represents the remaining path after /resources/p/ and may contain slashes.",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("path", "Resource path.")},
				"responses": withErrors(map[string]any{
					"204": map[string]any{"description": "Deleted."},
				}, "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/resources/get/p/{path}": map[string]any{
			"get": map[string]any{
				"operationId": "getResource",
				"summary":     "Get a JSON resource by path",
				"description": "The {path} segment represents the remaining path after /resources/get/p/ and may contain slashes.",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("path", "Resource path.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Stored JSON with unresolved references for operators; Job callbacks receive the Admission-allowed resolved value.", oapiSchemaRef("JSONValue")),
				}, "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/jobs": map[string]any{
			"get": map[string]any{
				"operationId": "listJobs",
				"summary":     "List jobs",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiQueryParam("status", "Filter by queued, running, success, failure, canceled, completed, or all.", oapiStringSchema(), false),
					oapiQueryParam("limit", "Page size from 1 to 500.", oapiIntegerSchema(), false),
					oapiQueryParam("cursor", "Opaque cursor returned by the previous page.", oapiStringSchema(), false),
					oapiQueryParam("app", "Optional app key filter.", oapiStringSchema(), false),
					oapiQueryParam("action", "Optional action key filter.", oapiStringSchema(), false),
					oapiQueryParam("trigger_kind", "Optional trigger kind filter.", oapiStringSchema(), false),
					oapiQueryParam("since", "RFC3339 lower bound for created_at.", oapiStringSchema(), false),
					oapiQueryParam("until", "RFC3339 upper bound for created_at.", oapiStringSchema(), false),
				},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Job page.", oapiSchemaRef("JobListResponse")),
				}, "400", "401", "403"),
			},
		},
		"/api/w/{workspace}/jobs/summary": map[string]any{
			"get": map[string]any{
				"operationId": "getJobSummary",
				"summary":     "Get job queue summary",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiQueryParam("recent_seconds", "Recent completion window from 1 to 604800 seconds.", oapiIntegerSchema(), false),
				},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Queue summary.", oapiSchemaRef("JobSummary")),
				}, "400", "401", "403"),
			},
		},
		"/api/w/{workspace}/jobs/{jobId}": map[string]any{
			"get": map[string]any{
				"operationId": "getJob",
				"summary":     "Get job status",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("jobId", "Job id.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Job status.", oapiSchemaRef("JobStatus")),
				}, "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/jobs/{jobId}/result": map[string]any{
			"get": map[string]any{
				"operationId": "getJobResult",
				"summary":     "Get or poll a job result",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("jobId", "Job id.")},
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Finished job result.", oapiSchemaRef("JobResultResponse")),
					"202": oapiResponse("Job is still pending.", oapiSchemaRef("JobPendingResponse")),
				}, "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/jobs/{jobId}/logs": map[string]any{
			"get": map[string]any{
				"operationId": "getJobLogs",
				"summary":     "Get job logs",
				"parameters": []any{
					oapiWorkspaceParam(workspaceID),
					oapiPathParam("jobId", "Job id."),
					oapiQueryParam("tail_bytes", "Optional non-negative byte count; capped by the server.", oapiIntegerSchema(), false),
				},
				"responses": withErrors(map[string]any{
					"200": oapiTextResponse("Plaintext job logs."),
				}, "400", "401", "403", "404"),
			},
		},
		"/api/w/{workspace}/jobs/{jobId}/cancel": map[string]any{
			"post": map[string]any{
				"operationId": "cancelJob",
				"summary":     "Cancel a job",
				"parameters":  []any{oapiWorkspaceParam(workspaceID), oapiPathParam("jobId", "Job id.")},
				"requestBody": oapiJSONBody(oapiSchemaRef("CancelJobRequest"), false),
				"responses": withErrors(map[string]any{
					"200": oapiResponse("Cancel result.", oapiSchemaRef("CancelResult")),
				}, "400", "401", "403", "404"),
			},
		},
	}
	addWebhookControlPlanePaths(paths, workspaceID)
	addServicePrincipalControlPlanePaths(paths, workspaceID)
	addTriggerControlPlanePaths(paths, workspaceID)
	schemas := controlPlaneSchemas()
	addWebhookControlPlaneSchemas(schemas)
	addServicePrincipalControlPlaneSchemas(schemas)
	addTriggerControlPlaneSchemas(schemas)

	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "Windforce Core Control Plane API",
			"version":     "current",
			"description": "Workspace control-plane API for registering and synchronizing git sources, preparing and publishing active app releases, inspecting app/action metadata, and discovering action input/output schemas.",
		},
		"servers":  []any{map[string]any{"url": baseURL}},
		"security": []any{map[string]any{"bearerAuth": []any{}}},
		"components": map[string]any{
			"schemas":         schemas,
			"responses":       openAPIErrorResponses(),
			"securitySchemes": openAPISecuritySchemes(),
		},
		"paths": paths,
	}
}

func controlPlaneSchemas() map[string]any {
	jsonSchema := oapiSchemaRef("JSONSchema")
	catalogSchema := oapiSchemaRef("Base64JSONSchema")
	stringArray := map[string]any{"type": "array", "items": oapiStringSchema()}
	nullableString := map[string]any{"type": []any{"string", "null"}}
	nullableInteger := map[string]any{"type": []any{"integer", "null"}}
	nullableDateTime := map[string]any{"type": []any{"string", "null"}, "format": "date-time"}
	appProperties := map[string]any{
		"id":                    oapiStringSchema(),
		"workspace_id":          oapiStringSchema(),
		"app_key":               oapiStringSchema(),
		"git_source_id":         oapiIntegerSchema(),
		"commit_sha":            oapiStringSchema(),
		"entrypoint":            oapiStringSchema(),
		"tag":                   oapiStringSchema(),
		"tag_override":          nullableString,
		"timeout_s":             oapiIntegerSchema(),
		"script_lang":           oapiStringSchema(),
		"bundle_status":         map[string]any{"type": "string", "enum": []any{"ready", "missing"}},
		"bundle_digest":         oapiStringSchema(),
		"bundle_uri":            oapiStringSchema(),
		"required_capabilities": stringArray,
		"max_concurrent":        nullableInteger,
		"updated_at":            oapiDateTimeSchema(),
	}
	appViewProperties := cloneSchemaProperties(appProperties)
	appViewProperties["effective_route_tag"] = oapiStringSchema()
	actionProperties := map[string]any{
		"id":                    oapiStringSchema(),
		"workspace_id":          oapiStringSchema(),
		"app_key":               oapiStringSchema(),
		"action_key":            oapiStringSchema(),
		"display_name":          map[string]any{"type": "string", "description": "Human-readable label derived from a materialized JSON Schema title, preferring the input schema."},
		"input_schema":          catalogSchema,
		"output_schema":         catalogSchema,
		"tag":                   nullableString,
		"tag_override":          nullableString,
		"timeout_s":             nullableInteger,
		"required_capabilities": stringArray,
		"runtime_access":        oapiSchemaRef("RuntimeAccess"),
		"updated_at":            oapiDateTimeSchema(),
	}
	appActionProperties := cloneSchemaProperties(actionProperties)
	appActionProperties["effective_capabilities"] = stringArray
	appActionProperties["effective_route_tag"] = oapiStringSchema()

	return map[string]any{
		"RuntimeAccess": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"variables": stringArray,
				"resources": stringArray,
			},
			"required": []any{"variables", "resources"},
		},
		"Workspace": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":         oapiStringSchema(),
				"name":       oapiStringSchema(),
				"status":     map[string]any{"type": "string", "enum": []any{"active", "archived"}},
				"created_by": oapiStringSchema(),
				"created_at": oapiDateTimeSchema(),
				"updated_by": oapiStringSchema(),
				"updated_at": oapiDateTimeSchema(),
			},
			"required": []any{"id", "name", "status", "created_by", "created_at", "updated_by", "updated_at"},
		},
		"CreateWorkspaceRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":   map[string]any{"type": "string", "pattern": "^[a-z][a-z0-9-]{0,46}[a-z0-9]$"},
				"name": oapiStringSchema(),
			},
			"required": []any{"id", "name"},
		},
		"WorkspaceListResponse": map[string]any{
			"type":       "object",
			"properties": map[string]any{"items": map[string]any{"type": "array", "items": oapiSchemaRef("Workspace")}},
			"required":   []any{"items"},
		},
		"UpdateWorkspaceRequest": map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": oapiStringSchema()},
			"required":   []any{"name"},
		},
		"CreateWorkspaceTokenRequest": map[string]any{
			"type":       "object",
			"properties": map[string]any{"name": oapiStringSchema()},
			"required":   []any{"name"},
		},
		"WorkspaceToken": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":           oapiStringSchema(),
				"workspace_id": oapiStringSchema(),
				"name":         oapiStringSchema(),
				"status":       map[string]any{"type": "string", "enum": []any{"active", "revoked"}},
				"created_by":   oapiStringSchema(),
				"created_at":   oapiDateTimeSchema(),
				"updated_by":   oapiStringSchema(),
				"updated_at":   oapiDateTimeSchema(),
				"revoked_at":   map[string]any{"type": []any{"string", "null"}, "format": "date-time"},
			},
			"required": []any{"id", "workspace_id", "name", "status", "created_by", "created_at", "updated_by", "updated_at"},
		},
		"WorkspaceTokenListResponse": map[string]any{
			"type":       "object",
			"properties": map[string]any{"items": map[string]any{"type": "array", "items": oapiSchemaRef("WorkspaceToken")}},
			"required":   []any{"items"},
		},
		"WorkspaceTokenResult": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"token":     oapiSchemaRef("WorkspaceToken"),
				"api_token": map[string]any{"type": "string", "writeOnly": true, "description": "One-time workspace API token."},
			},
			"required": []any{"token", "api_token"},
		},
		"WorkspaceAudit": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":           oapiStringSchema(),
				"workspace_id": oapiStringSchema(),
				"kind":         oapiStringSchema(),
				"actor":        oapiStringSchema(),
				"detail":       oapiStringSchema(),
				"created_at":   oapiDateTimeSchema(),
			},
			"required": []any{"id", "workspace_id", "kind", "actor", "created_at"},
		},
		"JSONSchema": map[string]any{
			"type":                 "object",
			"description":          "Materialized action input/output JSON Schema document. An empty object means unconstrained JSON.",
			"additionalProperties": true,
		},
		"Base64JSONSchema": map[string]any{
			"type":        "string",
			"format":      "byte",
			"description": "Base64-encoded materialized JSON Schema bytes, matching canonical Windforce catalog action JSON encoding.",
		},
		"Error": map[string]any{
			"type":        "object",
			"description": "windforce's uniform error envelope.",
			"properties":  map[string]any{"error": oapiStringSchema()},
			"required":    []any{"error"},
		},
		"ProvisioningValueSource": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"value":    map[string]any{"description": "Inline value. Avoid using this for secrets.", "nullable": true},
				"redacted": oapiBooleanSchema(),
				"valueFrom": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"env":  oapiStringSchema(),
						"file": oapiStringSchema(),
					},
				},
			},
		},
		"ProvisioningResource": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"apiVersion": oapiStringSchema(),
				"kind":       map[string]any{"type": "string", "enum": []any{"GitCredential", "AppSource", "Client", "Variable", "InputSettings"}},
				"metadata": map[string]any{
					"type":       "object",
					"properties": map[string]any{"name": oapiStringSchema()},
					"required":   []any{"name"},
				},
				"spec": map[string]any{
					"type":                 "object",
					"additionalProperties": true,
					"properties": map[string]any{
						"name":        oapiStringSchema(),
						"appKey":      oapiStringSchema(),
						"actionKey":   oapiStringSchema(),
						"clientRef":   oapiStringSchema(),
						"method":      oapiStringEnumSchema("none", "pat", "basic"),
						"storageRef":  oapiStringSchema(),
						"username":    oapiSchemaRef("ProvisioningValueSource"),
						"password":    oapiSchemaRef("ProvisioningValueSource"),
						"token":       oapiSchemaRef("ProvisioningValueSource"),
						"path":        oapiStringSchema(),
						"appScope":    oapiStringSchema(),
						"value":       oapiSchemaRef("ProvisioningValueSource"),
						"secret":      oapiBooleanSchema(),
						"description": oapiStringSchema(),
						"repository": map[string]any{
							"type": "object",
							"properties": map[string]any{
								"url":           oapiStringSchema(),
								"branch":        oapiStringSchema(),
								"subpath":       oapiStringSchema(),
								"authRef":       oapiStringSchema(),
								"credentialRef": oapiStringSchema(),
							},
						},
						"config":     map[string]any{"type": "object", "additionalProperties": true},
						"lockedKeys": stringArray,
					},
				},
			},
			"required": []any{"kind", "metadata", "spec"},
		},
		"ProvisioningImportRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"resources": map[string]any{"type": "array", "items": oapiSchemaRef("ProvisioningResource")},
				"dry_run":   oapiBooleanSchema(),
			},
			"required": []any{"resources"},
		},
		"ProvisioningAppliedResource": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"kind":   oapiStringSchema(),
				"name":   oapiStringSchema(),
				"action": oapiStringSchema(),
				"detail": oapiStringSchema(),
			},
			"required": []any{"kind", "name", "action"},
		},
		"ProvisioningResult": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"applied": map[string]any{"type": "array", "items": oapiSchemaRef("ProvisioningAppliedResource")},
			},
			"required": []any{"applied"},
		},
		"ProvisioningExportResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"resources": map[string]any{"type": "array", "items": oapiSchemaRef("ProvisioningResource")},
			},
			"required": []any{"resources"},
		},
		"Client": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":           oapiStringSchema(),
				"workspace_id": oapiStringSchema(),
				"name":         oapiStringSchema(),
				"has_token":    map[string]any{"type": "boolean"},
				"created_by":   oapiStringSchema(),
				"updated_by":   oapiStringSchema(),
				"created_at":   oapiDateTimeSchema(),
				"updated_at":   oapiDateTimeSchema(),
			},
			"required": []any{"id", "workspace_id", "name", "has_token", "created_by", "updated_by", "created_at", "updated_at"},
		},
		"CreateClientRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": oapiStringSchema(),
			},
			"required": []any{"name"},
		},
		"UpdateClientRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": oapiStringSchema(),
			},
			"required": []any{"name"},
		},
		"ClientTokenResult": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"client":    oapiSchemaRef("Client"),
				"api_token": oapiStringSchema(),
			},
			"required": []any{"client", "api_token"},
		},
		"ClientAudit": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":           oapiStringSchema(),
				"workspace_id": oapiStringSchema(),
				"client_id":    oapiStringSchema(),
				"kind":         oapiStringSchema(),
				"detail":       oapiStringSchema(),
				"actor":        oapiStringSchema(),
				"created_at":   oapiDateTimeSchema(),
			},
			"required": []any{"id", "workspace_id", "client_id", "kind", "actor", "created_at"},
		},
		"AuditChanges": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"added":    stringArray,
				"updated":  stringArray,
				"removed":  stringArray,
				"locked":   stringArray,
				"unlocked": stringArray,
			},
		},
		"AuditEvent": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":                  oapiStringSchema(),
				"category":            map[string]any{"type": "string", "enum": []any{"workspace", "repository", "release", "client", "input_settings", "runtime_configuration", "webhook"}},
				"kind":                oapiStringSchema(),
				"summary":             oapiStringSchema(),
				"detail":              oapiStringSchema(),
				"app_key":             oapiStringSchema(),
				"action_key":          oapiStringSchema(),
				"client_id":           oapiStringSchema(),
				"client_name":         oapiStringSchema(),
				"git_source_id":       oapiIntegerSchema(),
				"job_id":              oapiStringSchema(),
				"attempt":             oapiIntegerSchema(),
				"runtime_config_path": oapiStringSchema(),
				"source":              oapiStringSchema(),
				"actor":               oapiStringSchema(),
				"changes":             oapiSchemaRef("AuditChanges"),
				"created_at":          oapiDateTimeSchema(),
			},
			"required": []any{"id", "category", "kind", "summary", "actor", "created_at"},
		},
		"InputConfig": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"workspace_id": oapiStringSchema(),
				"app_key":      oapiStringSchema(),
				"action_key":   oapiStringSchema(),
				"client_id":    oapiStringSchema(),
				"config":       map[string]any{"type": "object", "additionalProperties": true},
				"locked_keys":  stringArray,
				"updated_by":   oapiStringSchema(),
				"updated_at":   oapiDateTimeSchema(),
			},
			"required": []any{"workspace_id", "app_key", "action_key", "config", "locked_keys", "updated_by", "updated_at"},
		},
		"SetInputConfigRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action_key":  oapiStringSchema(),
				"client_id":   oapiStringSchema(),
				"config":      map[string]any{"type": "object", "additionalProperties": true},
				"locked_keys": stringArray,
			},
			"required": []any{"config", "locked_keys"},
		},
		"InputConfigAudit": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":           oapiStringSchema(),
				"workspace_id": oapiStringSchema(),
				"app_key":      oapiStringSchema(),
				"action_key":   oapiStringSchema(),
				"client_id":    oapiStringSchema(),
				"kind":         oapiStringSchema(),
				"detail":       oapiStringSchema(),
				"actor":        oapiStringSchema(),
				"created_at":   oapiDateTimeSchema(),
			},
			"required": []any{"id", "workspace_id", "app_key", "action_key", "kind", "actor", "created_at"},
		},
		"GitSource": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":                 oapiIntegerSchema(),
				"name":               oapiStringSchema(),
				"workspace_id":       oapiStringSchema(),
				"repo_url":           oapiStringSchema(),
				"branch":             oapiStringSchema(),
				"subpath":            oapiStringSchema(),
				"creds_ref":          oapiStringSchema(),
				"kind":               oapiStringSchema(),
				"last_synced_commit": nullableString,
				"last_synced_at":     nullableDateTime,
				"created_at":         oapiDateTimeSchema(),
			},
			"required": []any{"id", "name", "workspace_id", "repo_url", "branch", "subpath", "creds_ref", "kind", "last_synced_commit", "last_synced_at", "created_at"},
		},
		"RegisterGitSourceRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":      oapiStringSchema(),
				"repo_url":  oapiStringSchema(),
				"branch":    oapiStringSchema(),
				"subpath":   oapiStringSchema(),
				"creds_ref": oapiStringSchema(),
			},
			"required": []any{"name", "repo_url"},
		},
		"ProbeGitSourceRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"repo_url":     oapiStringSchema(),
				"branch":       oapiStringSchema(),
				"auth_method":  oapiStringEnumSchema("none", "pat", "basic"),
				"access_token": oapiStringSchema(),
				"username":     oapiStringSchema(),
				"password":     oapiStringSchema(),
				"creds_ref":    oapiStringSchema(),
			},
			"required": []any{"repo_url"},
		},
		"DeployGitSourceRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"confirm":         oapiBooleanSchema(),
				"message":         nullableString,
				"expected_commit": oapiStringSchema(),
			},
			"required": []any{"confirm"},
		},
		"SyncGitSourceRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"expected_commit": oapiStringSchema(),
			},
		},
		"PatchGitSourceRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":      oapiStringSchema(),
				"repo_url":  oapiStringSchema(),
				"branch":    oapiStringSchema(),
				"subpath":   oapiStringSchema(),
				"creds_ref": nullableString,
			},
		},
		"SampleGitSourceRequest": map[string]any{
			"type":       "object",
			"properties": map[string]any{"app_key": oapiStringSchema()},
		},
		"GitSourceProbeResult": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"reachable":     oapiBooleanSchema(),
				"branch":        oapiStringSchema(),
				"branch_exists": oapiBooleanSchema(),
				"branches":      stringArray,
				"error":         oapiStringSchema(),
			},
			"required": []any{"reachable", "branches"},
		},
		"GitSourceSyncResult": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"commit":            oapiStringSchema(),
				"app":               oapiStringSchema(),
				"actions":           stringArray,
				"runtime":           oapiStringSchema(),
				"sync_status":       map[string]any{"type": "string", "enum": []any{"synced"}},
				"synced_at":         oapiDateTimeSchema(),
				"validation_checks": stringArray,
			},
			"required": []any{"commit", "app", "actions", "runtime", "sync_status", "synced_at", "validation_checks"},
		},
		"GitSourceDeployResult": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"commit":            oapiStringSchema(),
				"app":               oapiStringSchema(),
				"actions":           stringArray,
				"source":            oapiStringSchema(),
				"release_id":        oapiStringSchema(),
				"deployment_id":     nullableString,
				"created_by":        nullableString,
				"message":           nullableString,
				"bundle_status":     map[string]any{"type": "string", "enum": []any{"ready"}},
				"bundle_digest":     oapiStringSchema(),
				"bundle_uri":        oapiStringSchema(),
				"runtime":           oapiStringSchema(),
				"validation_checks": stringArray,
			},
			"required": []any{"commit", "app", "actions", "release_id", "bundle_status", "bundle_digest", "runtime", "validation_checks"},
		},
		"SampleSyncResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"source":      oapiSchemaRef("GitSource"),
				"sync_result": oapiSchemaRef("GitSourceDeployResult"),
			},
			"required": []any{"source", "sync_result"},
		},
		"App": map[string]any{
			"type":       "object",
			"properties": appProperties,
			"required":   []any{"id", "workspace_id", "app_key", "git_source_id", "commit_sha", "entrypoint", "timeout_s", "updated_at"},
		},
		"AppView": map[string]any{
			"type":        "object",
			"description": "App detail view returned by GET /apps/{app}, including server-computed routing fields.",
			"properties":  appViewProperties,
			"required":    []any{"id", "workspace_id", "app_key", "git_source_id", "commit_sha", "entrypoint", "timeout_s", "updated_at", "effective_route_tag"},
		},
		"AppSummary": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":                    oapiStringSchema(),
				"workspace_id":          oapiStringSchema(),
				"app_key":               oapiStringSchema(),
				"git_source_id":         oapiIntegerSchema(),
				"commit_sha":            oapiStringSchema(),
				"entrypoint":            oapiStringSchema(),
				"tag":                   oapiStringSchema(),
				"tag_override":          nullableString,
				"timeout_s":             oapiIntegerSchema(),
				"script_lang":           oapiStringSchema(),
				"bundle_status":         map[string]any{"type": "string", "enum": []any{"ready", "missing"}},
				"bundle_digest":         oapiStringSchema(),
				"bundle_uri":            oapiStringSchema(),
				"required_capabilities": stringArray,
				"max_concurrent":        nullableInteger,
				"updated_at":            oapiDateTimeSchema(),
				"effective_route_tag":   oapiStringSchema(),
				"actions_count":         oapiIntegerSchema(),
				"schedules_count":       oapiIntegerSchema(),
				"flows_count":           oapiIntegerSchema(),
			},
		},
		"AppsSummaryResponse": map[string]any{
			"type":       "object",
			"properties": map[string]any{"apps": map[string]any{"type": "array", "items": oapiSchemaRef("AppSummary")}},
			"required":   []any{"apps"},
		},
		"Action": map[string]any{
			"type":        "object",
			"description": "Canonical action detail. input_schema and output_schema use the canonical catalog encoding: base64-encoded materialized JSON Schema bytes.",
			"properties":  actionProperties,
			"required":    []any{"id", "workspace_id", "app_key", "action_key", "input_schema", "output_schema", "updated_at"},
		},
		"ActionSchema": map[string]any{
			"type":        "object",
			"description": "Raw materialized JSON Schema documents for one action. operator_settings_schema describes release-owned settings that are not public request fields.",
			"properties": map[string]any{
				"workspace_id":             oapiStringSchema(),
				"app_key":                  oapiStringSchema(),
				"action_key":               oapiStringSchema(),
				"input_schema":             jsonSchema,
				"output_schema":            jsonSchema,
				"operator_settings_schema": jsonSchema,
			},
			"required": []any{"workspace_id", "app_key", "action_key", "input_schema", "output_schema", "operator_settings_schema"},
		},
		"AppAction": map[string]any{
			"type":        "object",
			"description": "Action view returned inside app detail, including server-computed routing fields.",
			"properties":  appActionProperties,
			"required": []any{
				"id", "workspace_id", "app_key", "action_key", "input_schema", "output_schema", "updated_at",
				"effective_capabilities", "effective_route_tag",
			},
		},
		"AppDetailResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"app":     oapiSchemaRef("AppView"),
				"actions": map[string]any{"type": "array", "items": oapiSchemaRef("AppAction")},
			},
			"required": []any{"app", "actions"},
		},
		"TagOverrideRequest": map[string]any{
			"type":       "object",
			"properties": map[string]any{"tag_override": nullableString},
			"required":   []any{"tag_override"},
		},
		"AppSourceResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"app_key":       oapiStringSchema(),
				"git_source_id": oapiIntegerSchema(),
				"commit_sha":    oapiStringSchema(),
				"files":         map[string]any{"type": "object", "additionalProperties": oapiStringSchema()},
				"skipped":       stringArray,
			},
		},
		"AppDocumentationResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"app_key":    oapiStringSchema(),
				"commit_sha": oapiStringSchema(),
				"available":  map[string]any{"type": "boolean"},
				"path":       nullableString,
				"markdown":   nullableString,
			},
			"required": []any{"app_key", "commit_sha", "available"},
		},
		"AppHistoryItem": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":            oapiStringSchema(),
				"commit_sha":    oapiStringSchema(),
				"entrypoint":    oapiStringSchema(),
				"source":        oapiStringSchema(),
				"active":        map[string]any{"type": "boolean"},
				"bundle_status": map[string]any{"type": "string", "enum": []any{"ready", "missing"}},
				"deployment_id": nullableString,
				"message":       nullableString,
				"created_by":    nullableString,
				"created_at":    oapiDateTimeSchema(),
			},
		},
		"ReleaseRollbackRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"confirm": map[string]any{"type": "boolean"},
				"reason":  oapiStringSchema(),
			},
			"required": []any{"confirm", "reason"},
		},
		"ReleaseRollbackResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"app":                 oapiStringSchema(),
				"active_release_id":   oapiStringSchema(),
				"previous_release_id": oapiStringSchema(),
				"commit":              oapiStringSchema(),
				"bundle_digest":       oapiStringSchema(),
				"actor":               oapiStringSchema(),
				"reason":              oapiStringSchema(),
				"rolled_back_at":      oapiDateTimeSchema(),
			},
			"required": []any{"app", "active_release_id", "previous_release_id", "commit", "bundle_digest", "actor", "reason", "rolled_back_at"},
		},
		"RequeueResponse": map[string]any{
			"type":       "object",
			"properties": map[string]any{"requeued": oapiIntegerSchema()},
		},
		"WorkerTagsResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tags": map[string]any{"type": "array", "items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"tag":          oapiStringSchema(),
						"live_workers": oapiIntegerSchema(),
						"capabilities": stringArray,
						"workers":      map[string]any{"type": "array", "items": map[string]any{}},
					},
				}},
				"dedicated_tag": nullableString,
			},
		},
		"JSONValue": map[string]any{
			"description": "Any JSON value.",
		},
		"PathResponse": map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": oapiStringSchema()},
			"required":   []any{"path"},
		},
		"Variable": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"app_key":     oapiStringSchema(),
				"path":        oapiStringSchema(),
				"value":       oapiStringSchema(),
				"is_secret":   oapiBooleanSchema(),
				"description": oapiStringSchema(),
			},
			"required": []any{"app_key", "path", "value", "is_secret", "description"},
		},
		"SetVariableRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":        oapiStringSchema(),
				"value":       oapiStringSchema(),
				"description": oapiStringSchema(),
				"is_secret":   oapiBooleanSchema(),
				"app_key":     oapiStringSchema(),
			},
			"required": []any{"path"},
		},
		"VariableSetResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    oapiStringSchema(),
				"app_key": oapiStringSchema(),
			},
			"required": []any{"path", "app_key"},
		},
		"VariableValueResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       oapiStringSchema(),
				"value":      oapiStringSchema(),
				"is_secret":  oapiBooleanSchema(),
				"configured": oapiBooleanSchema(),
			},
			"required": []any{"path", "is_secret"},
		},
		"ResourceType": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":        oapiStringSchema(),
				"version":     oapiStringSchema(),
				"schema":      oapiSchemaRef("JSONValue"),
				"description": oapiStringSchema(),
			},
			"required": []any{"name", "version", "schema"},
		},
		"Resource": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":          oapiStringSchema(),
				"value":         oapiSchemaRef("JSONValue"),
				"resource_type": oapiStringSchema(),
				"description":   oapiStringSchema(),
			},
			"required": []any{"path", "value", "resource_type", "description"},
		},
		"SetResourceRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":          oapiStringSchema(),
				"value":         oapiSchemaRef("JSONValue"),
				"resource_type": oapiStringSchema(),
				"description":   oapiStringSchema(),
			},
			"required": []any{"path"},
		},
		"JobPendingResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"job_id": oapiStringSchema(),
				"status": oapiStringSchema(),
			},
			"required": []any{"status"},
		},
		"JobResultResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status": oapiStringSchema(),
				"result": oapiSchemaRef("JSONValue"),
			},
			"required": []any{"status", "result"},
		},
		"JobStatus": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":              oapiStringSchema(),
				"workspace_id":    oapiStringSchema(),
				"state":           oapiStringSchema(),
				"status":          nullableString,
				"worker":          nullableString,
				"app_key":         nullableString,
				"action_key":      nullableString,
				"trigger_kind":    nullableString,
				"kind":            nullableString,
				"git_source_id":   nullableInteger,
				"commit_sha":      nullableString,
				"entrypoint":      nullableString,
				"input_schema":    jsonSchema,
				"output_schema":   jsonSchema,
				"input":           oapiSchemaRef("JSONValue"),
				"tag":             oapiStringSchema(),
				"timeout_s":       oapiIntegerSchema(),
				"created_by":      oapiStringSchema(),
				"permissioned_as": oapiStringSchema(),
				"created_at":      nullableDateTime,
				"started_at":      nullableDateTime,
				"completed_at":    nullableDateTime,
				"duration_ms":     oapiIntegerSchema(),
				"canceled_by":     nullableString,
				"canceled_reason": nullableString,
				"flow_run_id":     nullableString,
				"flow_key":        nullableString,
				"flow_step_key":   nullableString,
			},
			"required": []any{"id", "workspace_id", "state"},
		},
		"JobListItem": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":              oapiStringSchema(),
				"workspace_id":    oapiStringSchema(),
				"app_key":         oapiStringSchema(),
				"action_key":      oapiStringSchema(),
				"trigger_kind":    oapiStringSchema(),
				"status":          oapiStringSchema(),
				"queued":          oapiBooleanSchema(),
				"running":         oapiBooleanSchema(),
				"completed":       oapiBooleanSchema(),
				"created_at":      oapiDateTimeSchema(),
				"started_at":      nullableDateTime,
				"completed_at":    nullableDateTime,
				"duration_ms":     oapiIntegerSchema(),
				"worker":          nullableString,
				"git_source_id":   nullableInteger,
				"commit_sha":      nullableString,
				"entrypoint":      oapiStringSchema(),
				"tag":             oapiStringSchema(),
				"created_by":      oapiStringSchema(),
				"permissioned_as": oapiStringSchema(),
				"canceled_by":     nullableString,
				"canceled_reason": nullableString,
				"flow_run_id":     nullableString,
				"flow_step_id":    nullableString,
				"error_snippet":   nullableString,
			},
			"required": []any{"id", "workspace_id", "app_key", "action_key", "trigger_kind", "status", "queued", "running", "completed", "created_at"},
		},
		"JobListResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"items": map[string]any{"type": "array", "items": oapiSchemaRef("JobListItem")},
				"pagination": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit":       oapiIntegerSchema(),
						"count":       oapiIntegerSchema(),
						"has_more":    oapiBooleanSchema(),
						"next_cursor": oapiStringSchema(),
					},
					"required": []any{"limit", "count", "has_more"},
				},
			},
			"required": []any{"items", "pagination"},
		},
		"JobSummaryCounts": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"queued_count":           oapiIntegerSchema(),
				"running_count":          oapiIntegerSchema(),
				"completed_count_recent": oapiIntegerSchema(),
				"failed_count_recent":    oapiIntegerSchema(),
				"canceled_count_recent":  oapiIntegerSchema(),
			},
		},
		"JobSummary": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"queued_count":           oapiIntegerSchema(),
				"running_count":          oapiIntegerSchema(),
				"completed_count_recent": oapiIntegerSchema(),
				"failed_count_recent":    oapiIntegerSchema(),
				"canceled_count_recent":  oapiIntegerSchema(),
				"oldest_queued_at":       nullableDateTime,
				"by_tag": map[string]any{"type": "array", "items": map[string]any{
					"allOf": []any{
						oapiSchemaRef("JobSummaryCounts"),
						map[string]any{"type": "object", "properties": map[string]any{"tag": oapiStringSchema()}},
					},
				}},
				"by_app": map[string]any{"type": "array", "items": map[string]any{
					"allOf": []any{
						oapiSchemaRef("JobSummaryCounts"),
						map[string]any{"type": "object", "properties": map[string]any{"app_key": oapiStringSchema()}},
					},
				}},
			},
		},
		"QueueDemandSelector": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"key":          oapiStringSchema(),
				"workspace_id": oapiStringSchema(),
				"tags":         stringArray,
				"labels":       stringArray,
			},
			"required": []any{"key", "workspace_id"},
		},
		"QueueDemandSnapshotRequest": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"selectors": map[string]any{
					"type": "array", "minItems": 1, "maxItems": maxQueueDemandSelectors,
					"items": oapiSchemaRef("QueueDemandSelector"),
				},
			},
			"required": []any{"selectors"},
		},
		"QueueDemand": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"selector":             oapiSchemaRef("QueueDemandSelector"),
				"eligible":             oapiIntegerSchema(),
				"queued":               oapiIntegerSchema(),
				"expired_reacquirable": oapiIntegerSchema(),
				"claimed":              oapiIntegerSchema(),
				"busy_workers":         oapiIntegerSchema(),
				"oldest_eligible_at":   nullableDateTime,
			},
			"required": []any{"selector", "eligible", "queued", "expired_reacquirable", "claimed", "busy_workers"},
		},
		"QueueDemandSnapshot": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"store_epoch":       oapiStringSchema(),
				"snapshot_revision": oapiIntegerSchema(),
				"observed_at":       oapiDateTimeSchema(),
				"items": map[string]any{
					"type": "array", "items": oapiSchemaRef("QueueDemand"),
				},
			},
			"required": []any{"store_epoch", "snapshot_revision", "observed_at", "items"},
		},
		"CancelJobRequest": map[string]any{
			"type":       "object",
			"properties": map[string]any{"reason": oapiStringSchema()},
		},
		"CancelResult": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"found":             oapiBooleanSchema(),
				"completed_now":     oapiBooleanSchema(),
				"soft_canceled":     oapiBooleanSchema(),
				"already_completed": oapiBooleanSchema(),
			},
			"required": []any{"found", "completed_now", "soft_canceled", "already_completed"},
		},
	}
}
