package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/imprun/windforce-core/internal/state"
)

const (
	maxQueueDemandBodyBytes      = 64 << 10
	maxQueueDemandSelectors      = 100
	maxQueueDemandSelectorValues = 32
	maxQueueDemandValueBytes     = 128
)

type queueDemandSnapshotRequest struct {
	Selectors []state.QueueDemandSelector `json:"selectors"`
}

func (h *Handler) handleQueueDemandSnapshot(w http.ResponseWriter, r *http.Request) {
	principal := workspacePrincipalFrom(r.Context())
	if principal == nil || !principal.Admin {
		writeError(w, http.StatusForbidden, "instance administrator required")
		return
	}
	if h.store == nil {
		writeError(w, http.StatusServiceUnavailable, "state store is not configured")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxQueueDemandBodyBytes)
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var request queueDemandSnapshotRequest
	if err := decoder.Decode(&request); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid queue demand snapshot request")
		}
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return
	}
	if message := validateQueueDemandSelectors(request.Selectors); message != "" {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	snapshot, err := h.store.QueueDemandSnapshot(r.Context(), request.Selectors)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func validateQueueDemandSelectors(selectors []state.QueueDemandSelector) string {
	if len(selectors) == 0 || len(selectors) > maxQueueDemandSelectors {
		return "selectors must contain between 1 and 100 items"
	}
	keys := map[string]struct{}{}
	for index, selector := range selectors {
		key := strings.TrimSpace(selector.Key)
		if key == "" || len(key) > maxQueueDemandValueBytes {
			return "selector key must be between 1 and 128 bytes"
		}
		if _, exists := keys[key]; exists {
			return "selector keys must be unique"
		}
		keys[key] = struct{}{}
		workspaceID := strings.TrimSpace(selector.WorkspaceID)
		if workspaceID == "" || len(workspaceID) > maxQueueDemandValueBytes {
			return "selector workspace_id must be between 1 and 128 bytes"
		}
		if message := validateQueueDemandValues(index, "tags", selector.Tags); message != "" {
			return message
		}
		if message := validateQueueDemandValues(index, "labels", selector.Labels); message != "" {
			return message
		}
	}
	return ""
}

func validateQueueDemandValues(_ int, name string, values []string) string {
	if len(values) > maxQueueDemandSelectorValues {
		return "selector " + name + " must contain at most 32 items"
	}
	seen := map[string]struct{}{}
	for _, raw := range values {
		value := strings.TrimSpace(raw)
		if value == "" || len(value) > maxQueueDemandValueBytes {
			return "selector " + name + " values must be between 1 and 128 bytes"
		}
		if _, exists := seen[value]; exists {
			return "selector " + name + " values must be unique"
		}
		seen[value] = struct{}{}
	}
	return ""
}
