package state

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func (s *LocalStore) ListTriggers(ctx context.Context, workspaceID string) ([]TriggerDefinition, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	items := make([]TriggerDefinition, 0)
	for _, record := range snapshot.Triggers {
		if record.WorkspaceID != workspaceID || record.DeletedAt != nil {
			continue
		}
		definition, err := s.triggerFromRecord(ctx, record)
		if err != nil {
			return nil, err
		}
		items = append(items, definition)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Name == items[j].Name {
			return items[i].ID < items[j].ID
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func (s *LocalStore) GetTrigger(ctx context.Context, workspaceID string, id string) (TriggerDefinition, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return TriggerDefinition{}, err
	}
	record, ok := snapshot.Triggers[triggerKey(workspaceID, id)]
	if !ok || record.DeletedAt != nil {
		return TriggerDefinition{}, fmt.Errorf("%w: trigger %q", ErrNotFound, id)
	}
	return s.triggerFromRecord(ctx, record)
}

func (s *LocalStore) CreateTrigger(ctx context.Context, definition TriggerDefinition, actor string) (TriggerDefinition, error) {
	definition, err := prepareTriggerDefinition(definition, actor, true)
	if err != nil {
		return TriggerDefinition{}, err
	}
	var created TriggerDefinition
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := triggerKey(definition.WorkspaceID, definition.ID)
		if _, exists := snapshot.Triggers[key]; exists {
			return fmt.Errorf("%w: trigger %q", ErrConflict, definition.ID)
		}
		if triggerNameExists(snapshot, definition.WorkspaceID, definition.Name, "") {
			return fmt.Errorf("%w: trigger name %q", ErrConflict, definition.Name)
		}
		definition.CreatedAt = now
		definition.UpdatedAt = now
		record, err := s.triggerRecord(ctx, definition)
		if err != nil {
			return err
		}
		snapshot.Triggers[key] = record
		appendTriggerAudit(snapshot, definition, "created", triggerAuditDetail(definition), actor, now)
		created = cloneTriggerDefinition(definition)
		return nil
	})
	return created, err
}

func (s *LocalStore) UpdateTrigger(ctx context.Context, definition TriggerDefinition, actor string) (TriggerDefinition, error) {
	definition, err := prepareTriggerDefinition(definition, actor, false)
	if err != nil {
		return TriggerDefinition{}, err
	}
	var updated TriggerDefinition
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := triggerKey(definition.WorkspaceID, definition.ID)
		existing, ok := snapshot.Triggers[key]
		if !ok || existing.DeletedAt != nil {
			return fmt.Errorf("%w: trigger %q", ErrNotFound, definition.ID)
		}
		if triggerNameExists(snapshot, definition.WorkspaceID, definition.Name, definition.ID) {
			return fmt.Errorf("%w: trigger name %q", ErrConflict, definition.Name)
		}
		definition.CreatedAt = existing.CreatedAt
		definition.CreatedBy = existing.CreatedBy
		definition.UpdatedAt = now
		if len(definition.SecretConfig) == 0 {
			definition.SecretConfig = nil
			record := TriggerRecord{TriggerDefinition: cloneTriggerDefinition(definition), SecretConfigEncrypted: cloneRaw(existing.SecretConfigEncrypted)}
			record.SecretConfig = nil
			snapshot.Triggers[key] = record
		} else {
			record, err := s.triggerRecord(ctx, definition)
			if err != nil {
				return err
			}
			snapshot.Triggers[key] = record
		}
		appendTriggerAudit(snapshot, definition, "updated", triggerAuditDetail(definition), actor, now)
		updated, err = s.triggerFromRecord(ctx, snapshot.Triggers[key])
		return err
	})
	return updated, err
}

func (s *LocalStore) SetTriggerEnabled(ctx context.Context, workspaceID string, id string, enabled bool, actor string) (TriggerDefinition, error) {
	var updated TriggerDefinition
	err := s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := triggerKey(workspaceID, id)
		record, ok := snapshot.Triggers[key]
		if !ok || record.DeletedAt != nil {
			return fmt.Errorf("%w: trigger %q", ErrNotFound, id)
		}
		record.Enabled = enabled
		record.UpdatedBy = normalizedActor(actor)
		record.UpdatedAt = now
		snapshot.Triggers[key] = record
		var err error
		updated, err = s.triggerFromRecord(ctx, record)
		if err != nil {
			return err
		}
		appendTriggerAudit(snapshot, updated, map[bool]string{true: "enabled", false: "disabled"}[enabled], "", actor, now)
		return nil
	})
	return updated, err
}

func (s *LocalStore) DeleteTrigger(ctx context.Context, workspaceID string, id string, actor string) error {
	return s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := triggerKey(workspaceID, id)
		record, ok := snapshot.Triggers[key]
		if !ok || record.DeletedAt != nil {
			return fmt.Errorf("%w: trigger %q", ErrNotFound, id)
		}
		record.Enabled = false
		record.UpdatedBy = normalizedActor(actor)
		record.UpdatedAt = now
		record.DeletedAt = cloneTime(&now)
		snapshot.Triggers[key] = record
		requestHTTPRouteBindingsForTrigger(snapshot, record.WorkspaceID, record.ID, actor, now)
		appendTriggerAudit(snapshot, record.TriggerDefinition, "deleted", "", actor, now)
		return nil
	})
}

func (s *LocalStore) ListTriggerAudit(ctx context.Context, workspaceID string, id string) ([]TriggerAudit, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	items := append([]TriggerAudit(nil), snapshot.TriggerAudits[triggerKey(workspaceID, id)]...)
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.Before(items[j].CreatedAt) })
	return items, nil
}

func (s *LocalStore) UpsertTriggerDelivery(ctx context.Context, delivery TriggerDelivery) (TriggerDelivery, error) {
	delivery.WorkspaceID = contract.NormalizeWorkspace(delivery.WorkspaceID)
	delivery.TriggerID = strings.TrimSpace(delivery.TriggerID)
	delivery.DeliveryID = strings.TrimSpace(delivery.DeliveryID)
	delivery.State = strings.TrimSpace(delivery.State)
	var err error
	delivery.Completion, err = NormalizeTriggerCompletionPolicy(delivery.Completion)
	if err != nil {
		return TriggerDelivery{}, err
	}
	if delivery.CompletionState == "" {
		delivery.CompletionState = InitialTriggerCompletionState(delivery.Completion)
	}
	if delivery.TriggerID == "" || delivery.DeliveryID == "" || delivery.State == "" {
		return TriggerDelivery{}, fmt.Errorf("%w: trigger_id, delivery_id, and state are required", ErrInvalidState)
	}
	var stored TriggerDelivery
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		key := triggerDeliveryKey(delivery.WorkspaceID, delivery.TriggerID, delivery.DeliveryID)
		existing, exists := snapshot.TriggerDeliveries[key]
		if exists {
			delivery.ID = existing.ID
			delivery.CreatedAt = existing.CreatedAt
			delivery.Completion = existing.Completion
			delivery.CompletionState = existing.CompletionState
			delivery.CompletionAttempt = existing.CompletionAttempt
			delivery.CompletionNextAttemptAt = existing.CompletionNextAttemptAt
			delivery.CompletionLeaseOwner = existing.CompletionLeaseOwner
			delivery.CompletionLeaseExpiresAt = cloneTime(existing.CompletionLeaseExpiresAt)
			delivery.CompletionResponseStatus = cloneInt(existing.CompletionResponseStatus)
			delivery.CompletionErrorSummary = existing.CompletionErrorSummary
			delivery.CompletionCompletedAt = cloneTime(existing.CompletionCompletedAt)
			if delivery.Attempt <= existing.Attempt {
				delivery.Attempt = existing.Attempt + 1
			}
		}
		if delivery.ID == "" {
			delivery.ID = NewID("trd")
		}
		if delivery.Attempt <= 0 {
			delivery.Attempt = 1
		}
		delivery.CreatedAt = nonZeroTime(delivery.CreatedAt, now)
		delivery.UpdatedAt = now
		delivery.ErrorSummary = truncateTriggerError(delivery.ErrorSummary)
		snapshot.TriggerDeliveries[key] = delivery
		stored = delivery
		return nil
	})
	return stored, err
}

func (s *LocalStore) ListTriggerDeliveries(ctx context.Context, workspaceID string, triggerID string, limit int) ([]TriggerDelivery, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	triggerID = strings.TrimSpace(triggerID)
	items := make([]TriggerDelivery, 0)
	for _, delivery := range snapshot.TriggerDeliveries {
		if delivery.WorkspaceID == workspaceID && delivery.TriggerID == triggerID {
			items = append(items, delivery)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID > items[j].ID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *LocalStore) triggerRecord(ctx context.Context, definition TriggerDefinition) (TriggerRecord, error) {
	encrypted, err := s.encryptInput(ctx, definition.WorkspaceID, definition.SecretConfig)
	if err != nil {
		return TriggerRecord{}, err
	}
	record := TriggerRecord{
		TriggerDefinition:     cloneTriggerDefinition(definition),
		SecretConfigEncrypted: encrypted,
	}
	record.SecretConfig = nil
	return record, nil
}

func (s *LocalStore) triggerFromRecord(ctx context.Context, record TriggerRecord) (TriggerDefinition, error) {
	definition := cloneTriggerDefinition(record.TriggerDefinition)
	secret, err := s.decryptInput(ctx, definition.WorkspaceID, record.SecretConfigEncrypted)
	if err != nil {
		return TriggerDefinition{}, err
	}
	definition.SecretConfig = secret
	return definition, nil
}

func prepareTriggerDefinition(definition TriggerDefinition, actor string, create bool) (TriggerDefinition, error) {
	definition.WorkspaceID = contract.NormalizeWorkspace(definition.WorkspaceID)
	definition.ID = strings.TrimSpace(definition.ID)
	if create && definition.ID == "" {
		definition.ID = NewID("trg")
	}
	definition.Name = strings.TrimSpace(definition.Name)
	definition.Kind = strings.ToLower(strings.TrimSpace(definition.Kind))
	definition.AppKey = strings.TrimSpace(definition.AppKey)
	definition.ActionKey = strings.TrimSpace(definition.ActionKey)
	definition.CredentialRef = strings.TrimSpace(definition.CredentialRef)
	definition.Config = canonicalJSONInput(definition.Config)
	completion, err := NormalizeTriggerCompletionPolicy(definition.Completion)
	if err != nil {
		return TriggerDefinition{}, err
	}
	definition.Completion = completion
	response, err := NormalizeTriggerResponsePolicy(definition.Kind, definition.Response)
	if err != nil {
		return TriggerDefinition{}, err
	}
	definition.Response = response
	if len(definition.SecretConfig) > 0 {
		definition.SecretConfig = canonicalJSONInput(definition.SecretConfig)
	}
	if definition.ID == "" || definition.Name == "" || definition.Kind == "" {
		return TriggerDefinition{}, fmt.Errorf("%w: trigger id, name, and kind are required", ErrInvalidState)
	}
	if !contract.ValidAppKey(definition.AppKey) || !contract.ValidActionKey(definition.ActionKey) {
		return TriggerDefinition{}, fmt.Errorf("%w: invalid trigger target", ErrInvalidState)
	}
	if !json.Valid(definition.Config) || (len(definition.SecretConfig) > 0 && !json.Valid(definition.SecretConfig)) {
		return TriggerDefinition{}, fmt.Errorf("%w: trigger config must be valid JSON", ErrInvalidState)
	}
	actor = normalizedActor(actor)
	definition.UpdatedBy = actor
	if create {
		definition.CreatedBy = actor
	}
	return definition, nil
}

func appendTriggerAudit(snapshot *Snapshot, definition TriggerDefinition, kind string, detail string, actor string, now time.Time) {
	key := triggerKey(definition.WorkspaceID, definition.ID)
	snapshot.TriggerAudits[key] = append(snapshot.TriggerAudits[key], TriggerAudit{
		ID:          NewID("tra"),
		WorkspaceID: definition.WorkspaceID,
		TriggerID:   definition.ID,
		Kind:        kind,
		Detail:      detail,
		Actor:       normalizedActor(actor),
		CreatedAt:   now,
	})
}

func triggerAuditDetail(definition TriggerDefinition) string {
	return fmt.Sprintf("kind=%s target=%s/%s enabled=%t credential_ref=%t", definition.Kind, definition.AppKey, definition.ActionKey, definition.Enabled, definition.CredentialRef != "")
}

func triggerKey(workspaceID string, id string) string {
	return contract.NormalizeWorkspace(workspaceID) + "\x00" + strings.TrimSpace(id)
}

func triggerDeliveryKey(workspaceID string, triggerID string, deliveryID string) string {
	return triggerKey(workspaceID, triggerID) + "\x00" + strings.TrimSpace(deliveryID)
}

func triggerNameExists(snapshot *Snapshot, workspaceID string, name string, exceptID string) bool {
	for _, record := range snapshot.Triggers {
		if record.WorkspaceID == workspaceID &&
			record.ID != exceptID &&
			record.DeletedAt == nil &&
			strings.EqualFold(record.Name, name) {
			return true
		}
	}
	return false
}

func cloneTriggerDefinition(definition TriggerDefinition) TriggerDefinition {
	definition.Config = cloneRaw(definition.Config)
	definition.SecretConfig = cloneRaw(definition.SecretConfig)
	if definition.Completion.Callback != nil {
		callback := *definition.Completion.Callback
		definition.Completion.Callback = &callback
	}
	if definition.Completion.Publish != nil {
		publish := *definition.Completion.Publish
		definition.Completion.Publish = &publish
	}
	definition.DeletedAt = cloneTime(definition.DeletedAt)
	return definition
}

func normalizedActor(actor string) string {
	actor = strings.TrimSpace(actor)
	if actor == "" {
		return defaultActorSubject
	}
	return actor
}

func truncateTriggerError(value string) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > 500 {
		return string(runes[:500])
	}
	return value
}
