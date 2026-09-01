package server

import "github.com/imprun/windforce-core/internal/state"

func addOpaqueHTTPProjectionControlPlanePaths(paths map[string]any, workspaceID string) {
	workspace := []any{oapiWorkspaceParam(workspaceID)}
	paths["/api/w/{workspace}/opaque-http-projections/credential-snapshots"] = map[string]any{
		"post": map[string]any{
			"operationId": "createOpaqueHTTPProjectionCredentialSnapshot",
			"summary":     "Persist an immutable opaque ingress credential-reference snapshot",
			"description": "Administrator-only. The request contains immutable references and freshness bounds, never raw credential material.",
			"parameters":  workspace,
			"requestBody": oapiJSONBody(oapiSchemaRef("OpaqueIngressCredentialSnapshotRequest"), true),
			"responses": opaqueHTTPProjectionMutationResponses(map[string]any{
				"200": oapiResponse("Exact operation replay.", oapiSchemaRef("OpaqueIngressCredentialSnapshot")),
				"201": oapiResponse("Immutable credential-reference snapshot created.", oapiSchemaRef("OpaqueIngressCredentialSnapshot")),
			}),
		},
	}
	paths["/api/w/{workspace}/opaque-http-projections/credential-revocations"] = map[string]any{
		"post": map[string]any{
			"operationId": "revokeOpaqueHTTPProjectionCredentialSnapshot",
			"summary":     "Revoke an immutable opaque ingress credential-reference snapshot",
			"description": "Administrator-only. Revocation is append-only and does not accept a caller-supplied reason.",
			"parameters":  workspace,
			"requestBody": oapiJSONBody(oapiSchemaRef("OpaqueIngressCredentialRevocationRequest"), true),
			"responses": opaqueHTTPProjectionMutationResponses(map[string]any{
				"200": oapiResponse("Exact operation replay.", oapiSchemaRef("OpaqueIngressCredentialRevocation")),
				"201": oapiResponse("Credential-reference snapshot revoked.", oapiSchemaRef("OpaqueIngressCredentialRevocation")),
			}),
		},
	}
	paths["/api/w/{workspace}/opaque-http-projections/publication-revisions"] = map[string]any{
		"post": map[string]any{
			"operationId": "createOpaqueHTTPProjectionPublicationRevision",
			"summary":     "Persist an immutable opaque ingress publication revision",
			"description": "A Service Principal must have exactly one allowed target equal to the revision's app/action.",
			"parameters":  workspace,
			"requestBody": oapiJSONBody(oapiSchemaRef("OpaqueIngressPublicationRevisionRequest"), true),
			"responses": opaqueHTTPProjectionMutationResponses(map[string]any{
				"200": oapiResponse("Exact operation replay.", oapiSchemaRef("OpaqueIngressPublicationRevision")),
				"201": oapiResponse("Immutable publication revision created.", oapiSchemaRef("OpaqueIngressPublicationRevision")),
			}),
		},
	}
	paths["/api/w/{workspace}/opaque-http-projections/activations"] = map[string]any{
		"post": map[string]any{
			"operationId": "activateOpaqueHTTPProjectionPublication",
			"summary":     "Activate, roll back, or revoke a publication through monotonic compare-and-swap",
			"description": "The expected generation must equal the current head. Every successful operation appends one greater generation.",
			"parameters":  workspace,
			"requestBody": oapiJSONBody(oapiSchemaRef("OpaqueIngressActivationRequest"), true),
			"responses": opaqueHTTPProjectionMutationResponses(map[string]any{
				"200": oapiResponse("Exact operation replay.", oapiSchemaRef("OpaqueIngressActivation")),
				"201": oapiResponse("New monotonic activation generation appended.", oapiSchemaRef("OpaqueIngressActivation")),
			}),
		},
	}
	paths["/api/w/{workspace}/opaque-http-projections/audit"] = map[string]any{
		"get": map[string]any{
			"operationId": "listOpaqueHTTPProjectionAudit",
			"summary":     "List sanitized opaque ingress projection audit records",
			"parameters": []any{
				oapiWorkspaceParam(workspaceID),
				oapiQueryParam("publicationRef", "Optional exact publication reference.", opaqueIngressPublicationRefSchema(), false),
				oapiQueryParam("limit", "Maximum records from 1 to 1000.", map[string]any{"type": "integer", "minimum": 1, "maximum": 1000}, false),
			},
			"responses": opaqueHTTPProjectionReadResponses(map[string]any{
				"200": oapiResponse("Sanitized audit records.", map[string]any{"type": "array", "items": oapiSchemaRef("OpaqueIngressAudit")}),
			}),
		},
	}
	paths["/api/w/{workspace}/opaque-http-projections/retention/prune"] = map[string]any{
		"post": map[string]any{
			"operationId": "pruneOpaqueHTTPProjectionHistory",
			"summary":     "Prune expired unreferenced opaque ingress projection snapshots",
			"description": "Administrator-only. Activated history, revocations, audit, and retained dependencies are preserved.",
			"parameters":  workspace,
			"requestBody": oapiJSONBody(oapiSchemaRef("OpaqueIngressRetentionRequest"), true),
			"responses": opaqueHTTPProjectionMutationResponses(map[string]any{
				"200": oapiResponse("Exact operation replay.", oapiSchemaRef("OpaqueIngressRetentionResult")),
				"201": oapiResponse("Retention operation completed.", oapiSchemaRef("OpaqueIngressRetentionResult")),
			}),
		},
	}
}

func opaqueHTTPProjectionMutationResponses(responses map[string]any) map[string]any {
	responses = withErrors(responses, "400", "401", "403", "404", "409")
	responses["415"] = oapiResponse("Content-Type must be application/json.", oapiSchemaRef("Error"))
	responses["422"] = oapiResponse("Projection input was rejected without exposing internal state.", oapiSchemaRef("Error"))
	responses["500"] = oapiResponse("Projection operation failed.", oapiSchemaRef("Error"))
	return responses
}

func opaqueHTTPProjectionReadResponses(responses map[string]any) map[string]any {
	responses = withErrors(responses, "400", "401", "403")
	responses["500"] = oapiResponse("Projection operation failed.", oapiSchemaRef("Error"))
	return responses
}

func addOpaqueHTTPProjectionControlPlaneSchemas(schemas map[string]any) {
	operationID := map[string]any{"type": "string", "minLength": 1, "maxLength": 128, "pattern": `^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`}
	issuerAudience := map[string]any{"type": "string", "minLength": 1, "maxLength": 160, "pattern": `^[ -~]+$`}
	sha256Digest := map[string]any{"type": "string", "pattern": `^sha256:[0-9a-f]{64}$`}
	operationRef := map[string]any{"type": "string", "minLength": 3, "maxLength": 200, "pattern": `^[a-z][a-z0-9.-]*(?:/[a-z][a-z0-9.-]*)+$`}
	immutableRef := strictObjectSchema([]string{"id", "version"}, map[string]any{
		"id":      map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "pattern": `^[ -~]+$`},
		"version": map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "pattern": `^[ -~]+$`},
	})
	credentialRef := strictObjectSchema([]string{"id", "revision", "digest"}, map[string]any{
		"id":       map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "pattern": `^[ -~]+$`},
		"revision": sha256Digest,
		"digest":   sha256Digest,
	})
	namedRef := strictObjectSchema([]string{"name", "reference"}, map[string]any{
		"name":      map[string]any{"type": "string", "minLength": 1, "maxLength": 64, "pattern": `^[a-z][A-Za-z0-9.-]*$`},
		"reference": immutableRef,
	})
	references := map[string]any{"type": "array", "maxItems": 31, "items": namedRef}
	credentialRefs := map[string]any{"type": "array", "minItems": 1, "maxItems": 64, "items": credentialRef}
	freshness := map[string]any{
		"projectedAt":         oapiDateTimeSchema(),
		"notAfter":            oapiDateTimeSchema(),
		"maxStalenessSeconds": map[string]any{"type": "integer", "format": "int64", "minimum": 1, "maximum": 2592000},
	}
	release := strictObjectSchema([]string{"deploymentId", "commit", "bundleDigest"}, map[string]any{
		"deploymentId": map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "pattern": `^[ -~]+$`},
		"commit":       map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "pattern": `^[ -~]+$`},
		"bundleDigest": sha256Digest,
	})
	responsePolicy := strictObjectSchema([]string{"contentTypes", "maxBodyBytes"}, map[string]any{
		"contentTypes":            map[string]any{"type": "array", "minItems": 1, "maxItems": 16, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 160, "pattern": `^[ -~]+$`}},
		"maxBodyBytes":            map[string]any{"type": "integer", "format": "int64", "minimum": 1, "maximum": 7340032},
		"allowMissingContentType": oapiBooleanSchema(),
	})
	httpContract := strictObjectSchema([]string{"method", "exactEscapedPath", "contentType", "maxRequestBodyBytes", "responsePolicy"}, map[string]any{
		"method":              map[string]any{"type": "string", "minLength": 1, "maxLength": 32, "pattern": `^[A-Z]+$`},
		"exactEscapedPath":    map[string]any{"type": "string", "minLength": 1, "maxLength": 1024, "pattern": `^/[ -~]*$`},
		"contentType":         map[string]any{"type": "string", "minLength": 1, "maxLength": 160, "pattern": `^[ -~]+$`},
		"maxRequestBodyBytes": map[string]any{"type": "integer", "format": "int64", "minimum": 1, "maximum": 16777216},
		"responsePolicy":      responsePolicy,
	})

	credentialRequestProperties := map[string]any{
		"issuer": issuerAudience, "audience": issuerAudience, "reference": credentialRef,
		"operationRef": operationRef, "references": references,
		"projectedAt": freshness["projectedAt"], "notAfter": freshness["notAfter"], "maxStalenessSeconds": freshness["maxStalenessSeconds"],
		"operationId": operationID,
	}
	credentialRequestSchema := strictObjectSchema(
		[]string{"issuer", "audience", "reference", "operationRef", "projectedAt", "notAfter", "maxStalenessSeconds", "operationId"},
		credentialRequestProperties,
	)
	credentialRequestSchema["x-windforce-content-digest"] = map[string]any{
		"field": "reference.digest", "canonicalization": state.OpaqueIngressProjectionDigestCanonicalization,
		"materialSchema": "windforce-core.opaque-ingress-credential-snapshot/v1", "workspaceSource": "path",
	}
	schemas["OpaqueIngressCredentialSnapshotRequest"] = credentialRequestSchema
	schemas["OpaqueIngressCredentialRevocationRequest"] = strictObjectSchema(
		[]string{"issuer", "audience", "reference", "operationId"},
		map[string]any{"issuer": issuerAudience, "audience": issuerAudience, "reference": credentialRef, "operationId": operationID},
	)

	publicationProperties := map[string]any{
		"issuer": issuerAudience, "audience": issuerAudience,
		"publicationRef": opaqueIngressPublicationRefSchema(), "revision": map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "pattern": `^[ -~]+$`},
		"digest": sha256Digest, "app": map[string]any{"type": "string", "minLength": 2, "maxLength": 64, "pattern": `^[A-Za-z0-9_]+$`},
		"action":  map[string]any{"type": "string", "minLength": 1, "maxLength": 128, "pattern": `^[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+){0,7}$`},
		"release": release, "http": httpContract, "operationRef": operationRef,
		"credentialRefs": credentialRefs, "references": references,
		"projectedAt": freshness["projectedAt"], "notAfter": freshness["notAfter"], "maxStalenessSeconds": freshness["maxStalenessSeconds"],
		"retainUntil": oapiDateTimeSchema(), "operationId": operationID,
	}
	publicationRequired := []string{"issuer", "audience", "publicationRef", "revision", "digest", "app", "action", "release", "http", "operationRef", "credentialRefs", "projectedAt", "notAfter", "maxStalenessSeconds", "retainUntil", "operationId"}
	publicationRequestSchema := strictObjectSchema(publicationRequired, publicationProperties)
	publicationRequestSchema["x-windforce-content-digest"] = map[string]any{
		"field": "digest", "canonicalization": state.OpaqueIngressProjectionDigestCanonicalization,
		"materialSchema": "windforce-core.opaque-ingress-publication-revision/v1", "workspaceSource": "path",
	}
	schemas["OpaqueIngressPublicationRevisionRequest"] = publicationRequestSchema
	schemas["OpaqueIngressActivationRequest"] = strictObjectSchema(
		[]string{"issuer", "audience", "publicationRef", "expectedGeneration", "kind", "operationId"},
		map[string]any{
			"issuer": issuerAudience, "audience": issuerAudience, "publicationRef": opaqueIngressPublicationRefSchema(),
			"expectedGeneration": map[string]any{"type": "integer", "format": "int64", "minimum": 0},
			"targetRevision":     map[string]any{"type": "string", "minLength": 1, "maxLength": 200, "pattern": `^[ -~]+$`},
			"kind":               oapiStringEnumSchema("activate", "rollback", "revoke"), "operationId": operationID,
		},
	)
	schemas["OpaqueIngressRetentionRequest"] = strictObjectSchema(
		[]string{"before", "limit", "operationId"},
		map[string]any{"before": oapiDateTimeSchema(), "limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 1000}, "operationId": operationID},
	)

	viewMetadata := map[string]any{
		"workspaceId": map[string]any{"type": "string"}, "operationId": operationID,
		"actor": map[string]any{"type": "string"}, "createdAt": oapiDateTimeSchema(),
	}
	credentialView := copySchemaProperties(credentialRequestProperties)
	delete(credentialView, "operationId")
	for key, value := range viewMetadata {
		credentialView[key] = value
	}
	schemas["OpaqueIngressCredentialSnapshot"] = strictObjectSchema(
		[]string{"workspaceId", "issuer", "audience", "reference", "operationRef", "projectedAt", "notAfter", "maxStalenessSeconds", "operationId", "actor", "createdAt"},
		credentialView,
	)
	schemas["OpaqueIngressCredentialRevocation"] = strictObjectSchema(
		[]string{"id", "workspaceId", "issuer", "audience", "reference", "operationId", "actor", "createdAt"},
		map[string]any{
			"id": map[string]any{"type": "string"}, "workspaceId": viewMetadata["workspaceId"],
			"issuer": issuerAudience, "audience": issuerAudience, "reference": credentialRef,
			"operationId": operationID, "actor": viewMetadata["actor"], "createdAt": viewMetadata["createdAt"],
		},
	)
	publicationView := copySchemaProperties(publicationProperties)
	delete(publicationView, "operationId")
	for key, value := range viewMetadata {
		publicationView[key] = value
	}
	schemas["OpaqueIngressPublicationRevision"] = strictObjectSchema(
		append([]string{"workspaceId"}, append(publicationRequired, "actor", "createdAt")...),
		publicationView,
	)
	schemas["OpaqueIngressActivation"] = strictObjectSchema(
		[]string{"workspaceId", "issuer", "audience", "publicationRef", "generation", "revision", "publicationDigest", "state", "kind", "operationId", "actor", "createdAt"},
		map[string]any{
			"workspaceId": viewMetadata["workspaceId"], "issuer": issuerAudience, "audience": issuerAudience,
			"publicationRef": opaqueIngressPublicationRefSchema(), "generation": map[string]any{"type": "integer", "format": "int64", "minimum": 1},
			"revision": map[string]any{"type": "string"}, "publicationDigest": sha256Digest,
			"state": oapiStringEnumSchema("active", "revoked"), "kind": oapiStringEnumSchema("activate", "rollback", "revoke"),
			"operationId": operationID, "actor": viewMetadata["actor"], "createdAt": viewMetadata["createdAt"],
		},
	)
	schemas["OpaqueIngressAudit"] = strictObjectSchema(
		[]string{"id", "workspaceId", "issuer", "audience", "subjectKind", "subjectId", "kind", "operationId", "actor", "createdAt"},
		map[string]any{
			"id": map[string]any{"type": "string"}, "workspaceId": viewMetadata["workspaceId"],
			"issuer": oapiStringSchema(), "audience": oapiStringSchema(), "publicationRef": opaqueIngressPublicationRefSchema(),
			"generation":  map[string]any{"type": "integer", "format": "int64", "minimum": 1},
			"subjectKind": map[string]any{"type": "string"}, "subjectId": map[string]any{"type": "string"}, "kind": map[string]any{"type": "string"},
			"operationId": operationID, "actor": viewMetadata["actor"], "createdAt": viewMetadata["createdAt"],
		},
	)
	schemas["OpaqueIngressRetentionResult"] = strictObjectSchema(
		[]string{"publicationRevisions", "credentialSnapshots"},
		map[string]any{
			"publicationRevisions": map[string]any{"type": "integer", "format": "int64", "minimum": 0},
			"credentialSnapshots":  map[string]any{"type": "integer", "format": "int64", "minimum": 0},
		},
	)
}

func opaqueIngressPublicationRefSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": 100, "pattern": `^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`}
}

func strictObjectSchema(required []string, properties map[string]any) map[string]any {
	schema := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
	if len(required) > 0 {
		items := make([]any, 0, len(required))
		for _, name := range required {
			items = append(items, name)
		}
		schema["required"] = items
	}
	return schema
}

func copySchemaProperties(input map[string]any) map[string]any {
	output := make(map[string]any, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
