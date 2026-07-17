package state

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/catalog"
	"github.com/imprun/windforce-core/internal/contract"
	controlevent "github.com/imprun/windforce-core/internal/event"
	"github.com/imprun/windforce-core/internal/webhook"
)

type WebhookSubscriptionRecord struct {
	ID                     string          `json:"id"`
	WorkspaceID            string          `json:"workspaceId"`
	Name                   string          `json:"name"`
	EndpointEncrypted      json.RawMessage `json:"endpointEncrypted"`
	SigningSecretEncrypted json.RawMessage `json:"signingSecretEncrypted"`
	EventTypes             []string        `json:"eventTypes"`
	AppKeys                []string        `json:"appKeys"`
	Enabled                bool            `json:"enabled"`
	CreatedBy              string          `json:"createdBy"`
	UpdatedBy              string          `json:"updatedBy"`
	CreatedAt              time.Time       `json:"createdAt"`
	UpdatedAt              time.Time       `json:"updatedAt"`
	DeletedAt              *time.Time      `json:"deletedAt,omitempty"`
}

func prepareNewSubscription(subscription webhook.Subscription, now time.Time) (webhook.Subscription, error) {
	subscription.WorkspaceID = contract.NormalizeWorkspace(subscription.WorkspaceID)
	subscription.ID = strings.TrimSpace(subscription.ID)
	if subscription.ID == "" {
		subscription.ID = NewID("whs")
	}
	if !strings.HasPrefix(subscription.ID, "whs_") {
		return webhook.Subscription{}, fmt.Errorf("%w: subscription id must use the whs_ prefix", webhook.ErrInvalid)
	}
	webhook.NormalizeFilters(&subscription)
	if err := webhook.ValidateSubscription(subscription); err != nil {
		return webhook.Subscription{}, err
	}
	actor := strings.TrimSpace(subscription.CreatedBy)
	if actor == "" {
		actor = "system"
	}
	subscription.CreatedBy = actor
	subscription.UpdatedBy = actor
	subscription.CreatedAt = now
	subscription.UpdatedAt = now
	subscription.DeletedAt = nil
	return subscription, nil
}

func prepareUpdatedSubscription(existing webhook.Subscription, update webhook.Subscription, now time.Time) (webhook.Subscription, error) {
	if strings.TrimSpace(update.Endpoint) == "" {
		update.Endpoint = existing.Endpoint
	}
	if update.SigningSecret == "" {
		update.SigningSecret = existing.SigningSecret
	}
	update.ID = existing.ID
	update.WorkspaceID = existing.WorkspaceID
	update.CreatedBy = existing.CreatedBy
	update.CreatedAt = existing.CreatedAt
	update.DeletedAt = existing.DeletedAt
	webhook.NormalizeFilters(&update)
	if err := webhook.ValidateSubscription(update); err != nil {
		return webhook.Subscription{}, err
	}
	actor := strings.TrimSpace(update.UpdatedBy)
	if actor == "" {
		actor = existing.UpdatedBy
	}
	if actor == "" {
		actor = "system"
	}
	update.UpdatedBy = actor
	update.UpdatedAt = now
	return update, nil
}

func webhookSubscriptionKey(workspaceID string, subscriptionID string) string {
	return contract.NormalizeWorkspace(workspaceID) + "/" + subscriptionID
}

func subscriptionRecord(subscription webhook.Subscription, endpointEncrypted json.RawMessage, secretEncrypted json.RawMessage) WebhookSubscriptionRecord {
	return WebhookSubscriptionRecord{
		ID:                     subscription.ID,
		WorkspaceID:            subscription.WorkspaceID,
		Name:                   subscription.Name,
		EndpointEncrypted:      cloneRaw(endpointEncrypted),
		SigningSecretEncrypted: cloneRaw(secretEncrypted),
		EventTypes:             append([]string(nil), subscription.EventTypes...),
		AppKeys:                append([]string(nil), subscription.AppKeys...),
		Enabled:                subscription.Enabled,
		CreatedBy:              subscription.CreatedBy,
		UpdatedBy:              subscription.UpdatedBy,
		CreatedAt:              subscription.CreatedAt,
		UpdatedAt:              subscription.UpdatedAt,
		DeletedAt:              cloneTime(subscription.DeletedAt),
	}
}

func subscriptionFromRecord(record WebhookSubscriptionRecord, endpoint string, signingSecret string) webhook.Subscription {
	return webhook.Subscription{
		ID:            record.ID,
		WorkspaceID:   record.WorkspaceID,
		Name:          record.Name,
		Endpoint:      endpoint,
		SigningSecret: signingSecret,
		EventTypes:    append([]string(nil), record.EventTypes...),
		AppKeys:       append([]string(nil), record.AppKeys...),
		Enabled:       record.Enabled,
		CreatedBy:     record.CreatedBy,
		UpdatedBy:     record.UpdatedBy,
		CreatedAt:     record.CreatedAt,
		UpdatedAt:     record.UpdatedAt,
		DeletedAt:     cloneTime(record.DeletedAt),
	}
}

func encryptWebhookString(ctx context.Context, provider inputWorkspaceKeyProvider, config inputCryptoConfig, workspaceID string, value string, label string) (json.RawMessage, error) {
	if strings.TrimSpace(config.SecretKey) == "" {
		return nil, fmt.Errorf("%s requires SECRET_KEY", label)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return encryptJSONAtRest(ctx, provider, config, workspaceID, raw, `""`, label)
}

func decryptWebhookString(ctx context.Context, provider inputWorkspaceKeyProvider, config inputCryptoConfig, workspaceID string, value json.RawMessage, label string) (string, error) {
	raw, err := decryptJSONAtRest(ctx, provider, config, workspaceID, value, `""`, label)
	if err != nil {
		return "", err
	}
	var decoded string
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return "", fmt.Errorf("decode %s: %w", label, err)
	}
	return decoded, nil
}

func prepareReleaseEvent(history catalog.DeploymentHistory, previous *catalog.DeploymentHistory) (controlevent.Envelope, error) {
	data := controlevent.ReleasePublishedData{
		Workspace: history.Workspace,
		AppKey:    history.App,
		ReleaseID: history.ID,
		Commit:    history.Commit,
		Actor:     "system",
		Note:      cloneString(history.Message),
	}
	if history.CreatedBy != nil && strings.TrimSpace(*history.CreatedBy) != "" {
		data.Actor = strings.TrimSpace(*history.CreatedBy)
	}
	if previous != nil {
		if strings.TrimSpace(previous.ID) != "" {
			data.PreviousReleaseID = cloneString(&previous.ID)
		}
		if strings.TrimSpace(previous.Commit) != "" {
			data.PreviousCommit = cloneString(&previous.Commit)
		}
	}
	return controlevent.NewReleasePublished(NewID("evt"), history.CreatedAt, data)
}

func latestReleaseHistory(snapshot catalog.Snapshot, workspaceID string, appKey string) *catalog.DeploymentHistory {
	for index := len(snapshot.History) - 1; index >= 0; index-- {
		history := snapshot.History[index]
		if contract.NormalizeWorkspace(history.Workspace) == contract.NormalizeWorkspace(workspaceID) && history.App == appKey {
			return &history
		}
	}
	return nil
}

func matchingSubscriptions(records map[string]WebhookSubscriptionRecord, workspaceID string, eventType string, appKey string) []WebhookSubscriptionRecord {
	result := make([]WebhookSubscriptionRecord, 0)
	for _, record := range records {
		subscription := webhook.Subscription{
			EventTypes: record.EventTypes,
			AppKeys:    record.AppKeys,
			Enabled:    record.Enabled,
			DeletedAt:  record.DeletedAt,
		}
		if contract.NormalizeWorkspace(record.WorkspaceID) == contract.NormalizeWorkspace(workspaceID) && webhook.Matches(subscription, eventType, appKey) {
			result = append(result, record)
		}
	}
	return result
}

func newWebhookDelivery(event controlevent.Envelope, workspaceID string, subscriptionID string, now time.Time) webhook.Delivery {
	return webhook.Delivery{
		ID:             NewID("whd"),
		WorkspaceID:    contract.NormalizeWorkspace(workspaceID),
		EventID:        event.ID,
		SubscriptionID: subscriptionID,
		State:          webhook.DeliveryPending,
		NextAttemptAt:  now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

func prepareWebhookDeliveryQuery(query webhook.DeliveryListQuery) (webhook.DeliveryListQuery, error) {
	query.SubscriptionID = strings.TrimSpace(query.SubscriptionID)
	query.CursorID = strings.TrimSpace(query.CursorID)
	if query.State != "" && !webhook.ValidDeliveryState(query.State) {
		return webhook.DeliveryListQuery{}, fmt.Errorf("%w: invalid delivery state %q", webhook.ErrInvalid, query.State)
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}
	if query.Limit > 101 {
		query.Limit = 101
	}
	if query.CursorCreatedAt.IsZero() != (query.CursorID == "") {
		return webhook.DeliveryListQuery{}, fmt.Errorf("%w: delivery cursor requires created_at and id", webhook.ErrInvalid)
	}
	return query, nil
}

func webhookDeliveryMatches(delivery webhook.Delivery, workspaceID string, query webhook.DeliveryListQuery) bool {
	if contract.NormalizeWorkspace(delivery.WorkspaceID) != contract.NormalizeWorkspace(workspaceID) {
		return false
	}
	if query.SubscriptionID != "" && delivery.SubscriptionID != query.SubscriptionID {
		return false
	}
	if query.State != "" && delivery.State != query.State {
		return false
	}
	if !query.CursorCreatedAt.IsZero() {
		if delivery.CreatedAt.After(query.CursorCreatedAt) || (delivery.CreatedAt.Equal(query.CursorCreatedAt) && delivery.ID >= query.CursorID) {
			return false
		}
	}
	return true
}

func newWebhookAudit(workspaceID string, subscriptionID string, deliveryID string, kind string, detail string, actor string, now time.Time) webhook.Audit {
	return webhook.Audit{
		ID:             NewID("wha"),
		WorkspaceID:    contract.NormalizeWorkspace(workspaceID),
		SubscriptionID: strings.TrimSpace(subscriptionID),
		DeliveryID:     strings.TrimSpace(deliveryID),
		Kind:           strings.TrimSpace(kind),
		Detail:         strings.TrimSpace(detail),
		Actor:          firstNonEmpty(strings.TrimSpace(actor), "system"),
		CreatedAt:      now.UTC(),
	}
}

func webhookSubscriptionAuditDetail(subscription webhook.Subscription) string {
	return fmt.Sprintf("name=%s; enabled=%t; event_types=%d; app_keys=%d", subscription.Name, subscription.Enabled, len(subscription.EventTypes), len(subscription.AppKeys))
}

func webhookSubscriptionUpdateAuditDetail(before webhook.Subscription, after webhook.Subscription) string {
	return fmt.Sprintf(
		"name=%s; enabled=%t->%t; event_types=%d->%d; app_keys=%d->%d",
		after.Name,
		before.Enabled,
		after.Enabled,
		len(before.EventTypes),
		len(after.EventTypes),
		len(before.AppKeys),
		len(after.AppKeys),
	)
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func validateDeliveryLease(delivery webhook.Delivery, lease webhook.DeliveryLease) error {
	if delivery.State != webhook.DeliveryDelivering || delivery.LeaseOwner == nil || delivery.LeaseExpiresAt == nil {
		return webhook.ErrInvalidLease
	}
	if *delivery.LeaseOwner != lease.WorkerID || delivery.Attempt != lease.Attempt || !delivery.LeaseExpiresAt.Equal(lease.ExpiresAt) {
		return webhook.ErrInvalidLease
	}
	return nil
}

func deliveryEligible(delivery webhook.Delivery, subscription WebhookSubscriptionRecord, now time.Time) bool {
	if !subscription.Enabled || subscription.DeletedAt != nil {
		return false
	}
	switch delivery.State {
	case webhook.DeliveryPending, webhook.DeliveryRetrying:
		return !delivery.NextAttemptAt.After(now)
	case webhook.DeliveryDelivering:
		return delivery.LeaseExpiresAt != nil && !delivery.LeaseExpiresAt.After(now)
	default:
		return false
	}
}
