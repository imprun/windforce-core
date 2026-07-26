package server

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"
)

// invocationOpenAPISpec is the system-to-system Invocation API source of truth.
//
//go:embed invocation/v1/openapi.json
var invocationOpenAPISpec []byte

//go:embed invocation/v1/examples/*.json
var invocationExamples embed.FS

func (h *Handler) handleInvocationOpenAPI(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, buildInvocationOpenAPI(strings.TrimSuffix(requestBaseURL(r), "/")))
}

func buildInvocationOpenAPI(serverURL string) map[string]any {
	var document map[string]any
	if err := json.Unmarshal(invocationOpenAPISpec, &document); err != nil {
		panic("invalid embedded Invocation OpenAPI: " + err.Error())
	}
	document["servers"] = []any{map[string]any{"url": serverURL}}
	return document
}
