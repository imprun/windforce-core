package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imprun/windforce-lite/internal/catalog"
	"github.com/imprun/windforce-lite/internal/contract"
	"github.com/imprun/windforce-lite/internal/state"
)

type canonicalAuditChanges struct {
	Added    []string `json:"added,omitempty"`
	Updated  []string `json:"updated,omitempty"`
	Removed  []string `json:"removed,omitempty"`
	Locked   []string `json:"locked,omitempty"`
	Unlocked []string `json:"unlocked,omitempty"`
}

type canonicalAuditEvent struct {
	ID          string                 `json:"id"`
	Category    string                 `json:"category"`
	Kind        string                 `json:"kind"`
	Summary     string                 `json:"summary"`
	Detail      string                 `json:"detail,omitempty"`
	AppKey      string                 `json:"app_key,omitempty"`
	ActionKey   string                 `json:"action_key,omitempty"`
	ClientID    string                 `json:"client_id,omitempty"`
	ClientName  string                 `json:"client_name,omitempty"`
	GitSourceID int64                  `json:"git_source_id,omitempty"`
	Actor       string                 `json:"actor"`
	Changes     *canonicalAuditChanges `json:"changes,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
}

type canonicalAuditQuery struct {
	AppKey      string
	ClientID    string
	Category    string
	Actor       string
	GitSourceID int64
	Since       *time.Time
	Until       *time.Time
	Limit       int
}

func (h *Handler) handleCanonicalAuditEvents(w http.ResponseWriter, r *http.Request, workspaceID string) {
	query, err := parseCanonicalAuditQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	snapshot, ok := h.loadCatalogSnapshot(w, r)
	if !ok {
		return
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	events := catalogAuditEvents(snapshot, workspaceID)

	if h.store != nil {
		clients, err := h.store.ListClients(r.Context(), workspaceID)
		if err != nil {
			writeStateError(w, err)
			return
		}
		clientNames := make(map[string]string, len(clients))
		for _, client := range clients {
			clientNames[client.ID] = client.Name
		}
		clientAudit, err := h.store.ListClientAudit(r.Context(), workspaceID, "")
		if err != nil {
			writeStateError(w, err)
			return
		}
		for _, record := range clientAudit {
			events = append(events, newClientAuditEvent(record, clientNames[record.ClientID]))
		}
		inputAudit, err := h.store.ListInputConfigAudit(r.Context(), workspaceID, "", "")
		if err != nil {
			writeStateError(w, err)
			return
		}
		sourceByApp := activeSourceByApp(snapshot, workspaceID)
		for _, record := range inputAudit {
			event := newInputConfigAuditEvent(record, clientNames[record.ClientID])
			event.GitSourceID = sourceByApp[record.AppKey]
			events = append(events, event)
		}
	}

	filtered := make([]canonicalAuditEvent, 0, len(events))
	for _, event := range events {
		if canonicalAuditEventMatches(event, query) {
			filtered = append(filtered, event)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].CreatedAt.Equal(filtered[j].CreatedAt) {
			return filtered[i].ID > filtered[j].ID
		}
		return filtered[i].CreatedAt.After(filtered[j].CreatedAt)
	})
	if len(filtered) > query.Limit {
		filtered = filtered[:query.Limit]
	}
	writeJSON(w, http.StatusOK, filtered)
}

func catalogAuditEvents(snapshot catalog.Snapshot, workspaceID string) []canonicalAuditEvent {
	events := make([]canonicalAuditEvent, 0, len(snapshot.Audit)+len(snapshot.History))
	for _, record := range snapshot.Audit {
		if contract.NormalizeWorkspace(record.Workspace) != workspaceID {
			continue
		}
		sourceID, _ := strconv.ParseInt(record.GitSourceID, 10, 64)
		events = append(events, canonicalAuditEvent{
			ID:          "repository:" + record.ID,
			Category:    "repository",
			Kind:        record.Kind,
			Summary:     canonicalAuditSummary("repository", record.Kind),
			Detail:      record.Detail,
			AppKey:      record.App,
			GitSourceID: sourceID,
			Actor:       firstNonEmpty(record.Actor, "system"),
			CreatedAt:   record.CreatedAt,
		})
	}
	for _, record := range snapshot.History {
		if contract.NormalizeWorkspace(record.Workspace) != workspaceID {
			continue
		}
		sourceID, _ := strconv.ParseInt(record.GitSourceID, 10, 64)
		detail := "commit " + shortAuditCommit(record.Commit)
		if record.Message != nil && strings.TrimSpace(*record.Message) != "" {
			detail = strings.TrimSpace(*record.Message)
		}
		actor := "system"
		if record.CreatedBy != nil {
			actor = firstNonEmpty(strings.TrimSpace(*record.CreatedBy), actor)
		}
		events = append(events, canonicalAuditEvent{
			ID:          "release:" + record.ID,
			Category:    "release",
			Kind:        "release_published",
			Summary:     canonicalAuditSummary("release", "release_published"),
			Detail:      detail,
			AppKey:      record.App,
			GitSourceID: sourceID,
			Actor:       actor,
			CreatedAt:   record.CreatedAt,
		})
	}
	return events
}

func newClientAuditEvent(record state.ClientAudit, clientName string) canonicalAuditEvent {
	return canonicalAuditEvent{
		ID:         "client:" + record.ID,
		Category:   "client",
		Kind:       "client_" + record.Kind,
		Summary:    canonicalAuditSummary("client", record.Kind),
		Detail:     record.Detail,
		ClientID:   record.ClientID,
		ClientName: clientName,
		Actor:      firstNonEmpty(record.Actor, "system"),
		CreatedAt:  record.CreatedAt,
	}
}

func newInputConfigAuditEvent(record state.InputConfigAudit, clientName string) canonicalAuditEvent {
	event := canonicalAuditEvent{
		ID:         "input_settings:" + record.ID,
		Category:   "input_settings",
		Kind:       "input_settings_" + record.Kind,
		Summary:    canonicalAuditSummary("input_settings", record.Kind),
		AppKey:     record.AppKey,
		ActionKey:  record.ActionKey,
		ClientID:   record.ClientID,
		ClientName: clientName,
		Actor:      firstNonEmpty(record.Actor, "system"),
		CreatedAt:  record.CreatedAt,
	}
	var changes canonicalAuditChanges
	if json.Unmarshal([]byte(record.Detail), &changes) == nil {
		event.Changes = &changes
	} else {
		event.Detail = record.Detail
	}
	return event
}

func activeSourceByApp(snapshot catalog.Snapshot, workspaceID string) map[string]int64 {
	sources := map[string]int64{}
	for _, deployment := range snapshot.Deployments {
		if contract.NormalizeWorkspace(deployment.SourceWorkspace()) != workspaceID {
			continue
		}
		sourceID, _ := strconv.ParseInt(deployment.SourceGitSourceID(), 10, 64)
		if sourceID != 0 {
			sources[deployment.App] = sourceID
		}
	}
	return sources
}

func parseCanonicalAuditQuery(r *http.Request) (canonicalAuditQuery, error) {
	values := r.URL.Query()
	query := canonicalAuditQuery{
		AppKey:   strings.TrimSpace(values.Get("app_key")),
		ClientID: strings.TrimSpace(values.Get("client_id")),
		Category: strings.TrimSpace(values.Get("category")),
		Actor:    strings.TrimSpace(values.Get("actor")),
		Limit:    100,
	}
	if query.Category != "" {
		validCategories := map[string]bool{"repository": true, "release": true, "client": true, "input_settings": true}
		if !validCategories[query.Category] {
			return canonicalAuditQuery{}, fmt.Errorf("invalid audit category")
		}
	}
	if raw := strings.TrimSpace(values.Get("git_source_id")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value <= 0 {
			return canonicalAuditQuery{}, fmt.Errorf("invalid git_source_id")
		}
		query.GitSourceID = value
	}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 500 {
			return canonicalAuditQuery{}, fmt.Errorf("limit must be between 1 and 500")
		}
		query.Limit = value
	}
	parseTime := func(label string, target **time.Time) error {
		raw := values.Get(label)
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return fmt.Errorf("invalid %s timestamp", label)
		}
		*target = &parsed
		return nil
	}
	if err := parseTime("since", &query.Since); err != nil {
		return canonicalAuditQuery{}, err
	}
	if err := parseTime("until", &query.Until); err != nil {
		return canonicalAuditQuery{}, err
	}
	if query.Since != nil && query.Until != nil && query.Since.After(*query.Until) {
		return canonicalAuditQuery{}, fmt.Errorf("since must not be after until")
	}
	return query, nil
}

func canonicalAuditEventMatches(event canonicalAuditEvent, query canonicalAuditQuery) bool {
	if query.AppKey != "" {
		matchesApp := event.AppKey == query.AppKey
		matchesSource := query.GitSourceID != 0 && event.GitSourceID == query.GitSourceID
		if !matchesApp && !matchesSource {
			return false
		}
	} else if query.GitSourceID != 0 && event.GitSourceID != query.GitSourceID {
		return false
	}
	if query.ClientID != "" && event.ClientID != query.ClientID {
		return false
	}
	if query.Category != "" && event.Category != query.Category {
		return false
	}
	if query.Actor != "" && !strings.Contains(strings.ToLower(event.Actor), strings.ToLower(query.Actor)) {
		return false
	}
	if query.Since != nil && event.CreatedAt.Before(*query.Since) {
		return false
	}
	if query.Until != nil && event.CreatedAt.After(*query.Until) {
		return false
	}
	return true
}

func canonicalAuditSummary(category string, kind string) string {
	labels := map[string]string{
		"source_registered":      "Repository source registered",
		"settings_changed":       "Repository settings changed",
		"source_deleted":         "Repository source removed",
		"route_tag_override":     "Route tag changed",
		"release_published":      "Release published",
		"created":                "Client registered",
		"updated":                "Client updated",
		"deleted":                "Client removed",
		"input_settings_set":     "Input settings updated",
		"input_settings_deleted": "Input settings removed",
	}
	lookup := kind
	if category == "input_settings" {
		lookup = "input_settings_" + kind
	}
	if label := labels[lookup]; label != "" {
		return label
	}
	return strings.ReplaceAll(kind, "_", " ")
}

func shortAuditCommit(commit string) string {
	commit = strings.TrimSpace(commit)
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
