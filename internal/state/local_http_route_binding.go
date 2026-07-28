package state

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/imprun/windforce-core/internal/contract"
)

func (s *LocalStore) ListHTTPRouteBindings(ctx context.Context, workspaceID string, triggerID string, includeDeleted bool) ([]HTTPRouteBinding, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	triggerID = strings.TrimSpace(triggerID)
	items := make([]HTTPRouteBinding, 0)
	for _, binding := range snapshot.HTTPRouteBindings {
		if binding.WorkspaceID != workspaceID ||
			(triggerID != "" && binding.TriggerID != triggerID) ||
			(!includeDeleted && binding.DeletedAt != nil) {
			continue
		}
		items = append(items, cloneHTTPRouteBinding(binding))
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].TriggerID != items[j].TriggerID {
			return items[i].TriggerID < items[j].TriggerID
		}
		if items[i].Hostname != items[j].Hostname {
			return items[i].Hostname < items[j].Hostname
		}
		if items[i].Path != items[j].Path {
			return items[i].Path < items[j].Path
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}

func (s *LocalStore) GetHTTPRouteBinding(ctx context.Context, workspaceID string, triggerID string, id string) (HTTPRouteBinding, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return HTTPRouteBinding{}, err
	}
	binding, ok := snapshot.HTTPRouteBindings[httpRouteBindingKey(workspaceID, triggerID, id)]
	if !ok || binding.DeletedAt != nil {
		return HTTPRouteBinding{}, fmt.Errorf("%w: HTTP route binding %q", ErrNotFound, id)
	}
	return cloneHTTPRouteBinding(binding), nil
}

func (s *LocalStore) CreateHTTPRouteBinding(ctx context.Context, binding HTTPRouteBinding, actor string) (HTTPRouteBinding, error) {
	binding, err := prepareHTTPRouteBinding(binding, actor, true)
	if err != nil {
		return HTTPRouteBinding{}, err
	}
	var created HTTPRouteBinding
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		trigger, ok := snapshot.Triggers[triggerKey(binding.WorkspaceID, binding.TriggerID)]
		if !ok || trigger.DeletedAt != nil {
			return fmt.Errorf("%w: trigger %q", ErrNotFound, binding.TriggerID)
		}
		if trigger.Kind != "webhook" {
			return fmt.Errorf("%w: HTTP route bindings require a webhook trigger", ErrInvalidState)
		}
		key := httpRouteBindingKey(binding.WorkspaceID, binding.TriggerID, binding.ID)
		if _, exists := snapshot.HTTPRouteBindings[key]; exists {
			return fmt.Errorf("%w: HTTP route binding %q", ErrConflict, binding.ID)
		}
		if httpRouteBindingAddressExists(snapshot, binding, "") {
			return fmt.Errorf("%w: HTTP route binding address already exists", ErrConflict)
		}
		binding.State = HTTPRouteBindingPending
		binding.Generation = 1
		binding.ObservedGeneration = 0
		binding.CreatedAt = now
		binding.UpdatedAt = now
		snapshot.HTTPRouteBindings[key] = cloneHTTPRouteBinding(binding)
		appendHTTPRouteBindingAudit(snapshot, binding, "created", httpRouteBindingAuditDetail(binding), actor, now)
		created = cloneHTTPRouteBinding(binding)
		return nil
	})
	return created, err
}

func (s *LocalStore) UpdateHTTPRouteBinding(ctx context.Context, binding HTTPRouteBinding, actor string) (HTTPRouteBinding, error) {
	binding, err := prepareHTTPRouteBinding(binding, actor, false)
	if err != nil {
		return HTTPRouteBinding{}, err
	}
	var updated HTTPRouteBinding
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := httpRouteBindingKey(binding.WorkspaceID, binding.TriggerID, binding.ID)
		existing, ok := snapshot.HTTPRouteBindings[key]
		if !ok || existing.DeletedAt != nil {
			return fmt.Errorf("%w: HTTP route binding %q", ErrNotFound, binding.ID)
		}
		if existing.DeleteRequestedAt != nil {
			return fmt.Errorf("%w: HTTP route binding %q is deleting", ErrInvalidState, binding.ID)
		}
		if httpRouteBindingAddressExists(snapshot, binding, binding.ID) {
			return fmt.Errorf("%w: HTTP route binding address already exists", ErrConflict)
		}
		if sameHTTPRouteBindingDesired(existing, binding) {
			updated = cloneHTTPRouteBinding(existing)
			return nil
		}
		binding.State = HTTPRouteBindingPending
		binding.PublicURL = ""
		binding.ErrorSummary = ""
		binding.Generation = existing.Generation + 1
		binding.ObservedGeneration = existing.ObservedGeneration
		binding.CreatedBy = existing.CreatedBy
		binding.CreatedAt = existing.CreatedAt
		binding.UpdatedAt = now
		snapshot.HTTPRouteBindings[key] = cloneHTTPRouteBinding(binding)
		appendHTTPRouteBindingAudit(snapshot, binding, "updated", httpRouteBindingAuditDetail(binding), actor, now)
		updated = cloneHTTPRouteBinding(binding)
		return nil
	})
	return updated, err
}

func (s *LocalStore) RequestDeleteHTTPRouteBinding(ctx context.Context, workspaceID string, triggerID string, id string, actor string) (HTTPRouteBinding, error) {
	var updated HTTPRouteBinding
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := httpRouteBindingKey(workspaceID, triggerID, id)
		binding, ok := snapshot.HTTPRouteBindings[key]
		if !ok || binding.DeletedAt != nil {
			return fmt.Errorf("%w: HTTP route binding %q", ErrNotFound, id)
		}
		if binding.DeleteRequestedAt != nil {
			updated = cloneHTTPRouteBinding(binding)
			return nil
		}
		binding.State = HTTPRouteBindingDeleting
		binding.ErrorSummary = ""
		binding.Generation++
		binding.UpdatedBy = normalizedActor(actor)
		binding.UpdatedAt = now
		binding.DeleteRequestedAt = cloneTime(&now)
		snapshot.HTTPRouteBindings[key] = cloneHTTPRouteBinding(binding)
		appendHTTPRouteBindingAudit(snapshot, binding, "delete_requested", "", actor, now)
		updated = cloneHTTPRouteBinding(binding)
		return nil
	})
	return updated, err
}

func (s *LocalStore) UpdateHTTPRouteBindingStatus(ctx context.Context, workspaceID string, id string, status HTTPRouteBindingStatus, actor string) (HTTPRouteBinding, error) {
	status, err := prepareHTTPRouteBindingStatus(status)
	if err != nil {
		return HTTPRouteBinding{}, err
	}
	var updated HTTPRouteBinding
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key, binding, ok := findHTTPRouteBinding(snapshot, workspaceID, id)
		if !ok || binding.DeletedAt != nil {
			return fmt.Errorf("%w: HTTP route binding %q", ErrNotFound, id)
		}
		if status.ObservedGeneration < binding.ObservedGeneration {
			return fmt.Errorf("%w: observed generation moved backwards", ErrConflict)
		}
		if status.ObservedGeneration > binding.Generation {
			return fmt.Errorf("%w: observed generation exceeds desired generation", ErrConflict)
		}
		if status.State == HTTPRouteBindingReady && status.ObservedGeneration != binding.Generation {
			return fmt.Errorf("%w: stale generation cannot be ready", ErrConflict)
		}
		if status.State == HTTPRouteBindingDeleted && binding.DeleteRequestedAt == nil {
			return fmt.Errorf("%w: binding is not deleting", ErrInvalidState)
		}
		previousState := binding.State
		binding.State = status.State
		binding.PublicURL = status.PublicURL
		binding.ErrorSummary = truncateHTTPRouteBindingError(status.ErrorSummary)
		binding.ObservedGeneration = status.ObservedGeneration
		binding.UpdatedBy = normalizedActor(actor)
		binding.UpdatedAt = now
		if status.State == HTTPRouteBindingDeleted {
			binding.PublicURL = ""
			binding.ErrorSummary = ""
			binding.DeletedAt = cloneTime(&now)
		}
		snapshot.HTTPRouteBindings[key] = cloneHTTPRouteBinding(binding)
		detail := fmt.Sprintf("state=%s previous=%s observed_generation=%d", binding.State, previousState, binding.ObservedGeneration)
		appendHTTPRouteBindingAudit(snapshot, binding, "status_changed", detail, actor, now)
		updated = cloneHTTPRouteBinding(binding)
		return nil
	})
	return updated, err
}

func (s *LocalStore) ListHTTPRouteBindingAudit(ctx context.Context, workspaceID string, triggerID string, id string) ([]HTTPRouteBindingAudit, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	items := append([]HTTPRouteBindingAudit(nil), snapshot.HTTPRouteBindingAudits[httpRouteBindingKey(workspaceID, triggerID, id)]...)
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items, nil
}

func prepareHTTPRouteBinding(binding HTTPRouteBinding, actor string, create bool) (HTTPRouteBinding, error) {
	binding.WorkspaceID = contract.NormalizeWorkspace(binding.WorkspaceID)
	binding.ID = strings.TrimSpace(binding.ID)
	if create && binding.ID == "" {
		binding.ID = NewID("hrb")
	}
	binding.TriggerID = strings.TrimSpace(binding.TriggerID)
	binding.Hostname = strings.ToLower(strings.TrimSpace(binding.Hostname))
	binding.Path = strings.TrimSpace(binding.Path)
	binding.Visibility = strings.ToLower(strings.TrimSpace(binding.Visibility))
	binding.Provider = strings.ToLower(strings.TrimSpace(binding.Provider))
	if binding.Visibility == "" {
		binding.Visibility = "public"
	}
	if binding.Provider == "" {
		binding.Provider = "auto"
	}
	if binding.ID == "" || binding.TriggerID == "" {
		return HTTPRouteBinding{}, fmt.Errorf("%w: binding id and trigger_id are required", ErrInvalidState)
	}
	if err := validateHTTPRouteBindingHostname(binding.Hostname); err != nil {
		return HTTPRouteBinding{}, err
	}
	if err := validateHTTPRouteBindingPath(binding.Path); err != nil {
		return HTTPRouteBinding{}, err
	}
	if binding.Visibility != "public" {
		return HTTPRouteBinding{}, fmt.Errorf("%w: visibility must be public", ErrInvalidState)
	}
	if !validHTTPRouteBindingProvider(binding.Provider) {
		return HTTPRouteBinding{}, fmt.Errorf("%w: invalid HTTP route provider", ErrInvalidState)
	}
	binding.UpdatedBy = normalizedActor(actor)
	if create {
		binding.CreatedBy = binding.UpdatedBy
	}
	return binding, nil
}

func prepareHTTPRouteBindingStatus(status HTTPRouteBindingStatus) (HTTPRouteBindingStatus, error) {
	status.State = strings.ToLower(strings.TrimSpace(status.State))
	status.PublicURL = strings.TrimSpace(status.PublicURL)
	status.ErrorSummary = strings.TrimSpace(status.ErrorSummary)
	switch status.State {
	case HTTPRouteBindingPending, HTTPRouteBindingReady, HTTPRouteBindingError, HTTPRouteBindingDeleted:
	default:
		return HTTPRouteBindingStatus{}, fmt.Errorf("%w: invalid HTTP route binding state", ErrInvalidState)
	}
	if status.ObservedGeneration <= 0 {
		return HTTPRouteBindingStatus{}, fmt.Errorf("%w: observed_generation must be greater than zero", ErrInvalidState)
	}
	if status.State == HTTPRouteBindingReady && status.PublicURL == "" {
		return HTTPRouteBindingStatus{}, fmt.Errorf("%w: ready binding requires public_url", ErrInvalidState)
	}
	if status.State == HTTPRouteBindingError && status.ErrorSummary == "" {
		return HTTPRouteBindingStatus{}, fmt.Errorf("%w: error binding requires error_summary", ErrInvalidState)
	}
	if status.PublicURL != "" {
		parsed, err := url.Parse(status.PublicURL)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return HTTPRouteBindingStatus{}, fmt.Errorf("%w: public_url must be an absolute HTTP URL", ErrInvalidState)
		}
	}
	return status, nil
}

func validateHTTPRouteBindingHostname(hostname string) error {
	if hostname == "" {
		return nil
	}
	if strings.ContainsAny(hostname, "/\\?#:@") {
		return fmt.Errorf("%w: hostname must not include a scheme, port, path, query, or fragment", ErrInvalidState)
	}
	for _, r := range hostname {
		if unicode.IsSpace(r) {
			return fmt.Errorf("%w: hostname must not contain whitespace", ErrInvalidState)
		}
	}
	labels := strings.Split(hostname, ".")
	for _, label := range labels {
		if label == "" || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("%w: invalid hostname", ErrInvalidState)
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return fmt.Errorf("%w: invalid hostname", ErrInvalidState)
			}
		}
	}
	return nil
}

func validateHTTPRouteBindingPath(routePath string) error {
	if routePath == "" || !strings.HasPrefix(routePath, "/") {
		return fmt.Errorf("%w: path must start with /", ErrInvalidState)
	}
	if strings.ContainsAny(routePath, "\\?#") {
		return fmt.Errorf("%w: path must not contain a backslash, query, or fragment", ErrInvalidState)
	}
	decoded, err := url.PathUnescape(routePath)
	if err != nil {
		return fmt.Errorf("%w: invalid escaped path", ErrInvalidState)
	}
	for _, segment := range strings.Split(decoded, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("%w: path must not contain dot segments", ErrInvalidState)
		}
	}
	return nil
}

func validHTTPRouteBindingProvider(provider string) bool {
	for i, r := range provider {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			continue
		}
		if i > 0 && (r == '-' || r == '_' || r == '.') {
			continue
		}
		return false
	}
	return provider != ""
}

func httpRouteBindingKey(workspaceID string, triggerID string, id string) string {
	return triggerKey(workspaceID, triggerID) + "\x00" + strings.TrimSpace(id)
}

func findHTTPRouteBinding(snapshot *Snapshot, workspaceID string, id string) (string, HTTPRouteBinding, bool) {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	id = strings.TrimSpace(id)
	for key, binding := range snapshot.HTTPRouteBindings {
		if binding.WorkspaceID == workspaceID && binding.ID == id {
			return key, binding, true
		}
	}
	return "", HTTPRouteBinding{}, false
}

func httpRouteBindingAddressExists(snapshot *Snapshot, candidate HTTPRouteBinding, exceptID string) bool {
	for _, binding := range snapshot.HTTPRouteBindings {
		if binding.WorkspaceID == candidate.WorkspaceID &&
			binding.ID != exceptID &&
			binding.DeletedAt == nil &&
			strings.EqualFold(binding.Hostname, candidate.Hostname) &&
			binding.Path == candidate.Path {
			return true
		}
	}
	return false
}

func sameHTTPRouteBindingDesired(left HTTPRouteBinding, right HTTPRouteBinding) bool {
	return left.Hostname == right.Hostname &&
		left.Path == right.Path &&
		left.Visibility == right.Visibility &&
		left.Provider == right.Provider
}

func cloneHTTPRouteBinding(binding HTTPRouteBinding) HTTPRouteBinding {
	binding.DeleteRequestedAt = cloneTime(binding.DeleteRequestedAt)
	binding.DeletedAt = cloneTime(binding.DeletedAt)
	return binding
}

func appendHTTPRouteBindingAudit(snapshot *Snapshot, binding HTTPRouteBinding, kind string, detail string, actor string, now time.Time) {
	key := httpRouteBindingKey(binding.WorkspaceID, binding.TriggerID, binding.ID)
	snapshot.HTTPRouteBindingAudits[key] = append(snapshot.HTTPRouteBindingAudits[key], HTTPRouteBindingAudit{
		ID:          NewID("hra"),
		WorkspaceID: binding.WorkspaceID,
		TriggerID:   binding.TriggerID,
		BindingID:   binding.ID,
		Kind:        kind,
		Detail:      detail,
		Actor:       normalizedActor(actor),
		CreatedAt:   now,
	})
}

func httpRouteBindingAuditDetail(binding HTTPRouteBinding) string {
	return fmt.Sprintf("hostname=%s path=%s visibility=%s provider=%s generation=%d",
		binding.Hostname, binding.Path, binding.Visibility, binding.Provider, binding.Generation)
}

func truncateHTTPRouteBindingError(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return value
}

func requestHTTPRouteBindingsForTrigger(snapshot *Snapshot, workspaceID string, triggerID string, actor string, now time.Time) {
	for key, binding := range snapshot.HTTPRouteBindings {
		if binding.WorkspaceID != contract.NormalizeWorkspace(workspaceID) ||
			binding.TriggerID != strings.TrimSpace(triggerID) ||
			binding.DeletedAt != nil ||
			binding.DeleteRequestedAt != nil {
			continue
		}
		binding.State = HTTPRouteBindingDeleting
		binding.ErrorSummary = ""
		binding.Generation++
		binding.UpdatedBy = normalizedActor(actor)
		binding.UpdatedAt = now
		binding.DeleteRequestedAt = cloneTime(&now)
		snapshot.HTTPRouteBindings[key] = cloneHTTPRouteBinding(binding)
		appendHTTPRouteBindingAudit(snapshot, binding, "delete_requested", "trigger deleted", actor, now)
	}
}
