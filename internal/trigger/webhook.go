package trigger

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/imprun/windforce-core/internal/state"
)

type WebhookConfig struct {
	SignatureHeader   string `json:"signature_header,omitempty"`
	DeliveryIDHeader  string `json:"delivery_id_header,omitempty"`
	CorrelationHeader string `json:"correlation_header,omitempty"`
	InputMode         string `json:"input_mode,omitempty"`
}

type webhookSecret struct {
	Secret string `json:"secret"`
}

type WebhookRequest struct {
	Headers map[string][]string
	Body    []byte
}

type WebhookTrigger struct {
	definition state.TriggerDefinition
	config     WebhookConfig
	secret     string
	submitter  *Submitter
	mu         sync.RWMutex
	started    bool
}

func NewWebhookTrigger(definition state.TriggerDefinition) (Trigger, error) {
	config, secret, err := parseWebhookDefinition(definition)
	if err != nil {
		return nil, err
	}
	return &WebhookTrigger{definition: definition, config: config, secret: secret}, nil
}

func (t *WebhookTrigger) Initialize(runtime Runtime) error {
	if runtime.Submitter == nil {
		return errors.New("webhook trigger submitter is required")
	}
	t.submitter = runtime.Submitter
	return nil
}

func (t *WebhookTrigger) Start(context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.started = true
	return nil
}

func (t *WebhookTrigger) Stop(context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.started = false
	return nil
}

func (t *WebhookTrigger) Deliver(ctx context.Context, request WebhookRequest) Submission {
	t.mu.RLock()
	started := t.started
	t.mu.RUnlock()
	if !started {
		return Submission{State: DeliveryRetryable, Err: errors.New("webhook trigger is not running")}
	}
	signature := firstHeader(request.Headers, t.config.SignatureHeader)
	if !validWebhookSignature(t.secret, request.Body, signature) {
		return Submission{State: DeliveryTerminal, Err: ErrUnauthorized}
	}
	deliveryID := strings.TrimSpace(firstHeader(request.Headers, t.config.DeliveryIDHeader))
	if deliveryID == "" {
		sum := sha256.Sum256(request.Body)
		deliveryID = "body-" + hex.EncodeToString(sum[:])
	}
	input := json.RawMessage(request.Body)
	if t.config.InputMode == "raw" {
		input, _ = json.Marshal(map[string]string{
			"raw_base64":   base64.StdEncoding.EncodeToString(request.Body),
			"content_type": firstHeader(request.Headers, "Content-Type"),
		})
	} else if !json.Valid(input) {
		return t.submitter.record(ctx, t.definition, Event{TriggerID: t.definition.ID, DeliveryID: deliveryID}, Submission{
			State: DeliveryTerminal,
			Err:   fmt.Errorf("%w: webhook body must be JSON", ErrInvalidEvent),
		})
	}
	return t.submitter.Submit(ctx, t.definition, Event{
		TriggerID:     t.definition.ID,
		DeliveryID:    deliveryID,
		CorrelationID: firstHeader(request.Headers, t.config.CorrelationHeader),
		Input:         input,
		RawPayload:    append([]byte(nil), request.Body...),
		SafeMetadata:  safeWebhookHeaders(request.Headers, t.config.SignatureHeader),
	})
}

func (m *Manager) DeliverWebhook(ctx context.Context, workspaceID string, triggerID string, request WebhookRequest) Submission {
	m.mu.RLock()
	current := m.instances[managerKey(workspaceID, triggerID)]
	m.mu.RUnlock()
	if current == nil {
		return Submission{State: DeliveryTerminal, Err: ErrNotFound}
	}
	receiver, ok := current.trigger.(*WebhookTrigger)
	if !ok {
		return Submission{State: DeliveryTerminal, Err: ErrNotFound}
	}
	return receiver.Deliver(ctx, request)
}

func parseWebhookDefinition(definition state.TriggerDefinition) (WebhookConfig, string, error) {
	var config WebhookConfig
	if err := json.Unmarshal(definition.Config, &config); err != nil {
		return WebhookConfig{}, "", fmt.Errorf("webhook config: %w", err)
	}
	if config.SignatureHeader == "" {
		config.SignatureHeader = "X-WF-Signature-256"
	}
	if config.DeliveryIDHeader == "" {
		config.DeliveryIDHeader = "X-WF-Delivery-Id"
	}
	if config.CorrelationHeader == "" {
		config.CorrelationHeader = "X-WF-Correlation-Id"
	}
	if config.InputMode == "" {
		config.InputMode = "json"
	}
	if config.InputMode != "json" && config.InputMode != "raw" {
		return WebhookConfig{}, "", errors.New("webhook input_mode must be json or raw")
	}
	var secret webhookSecret
	if err := json.Unmarshal(definition.SecretConfig, &secret); err != nil {
		return WebhookConfig{}, "", fmt.Errorf("webhook secret config: %w", err)
	}
	secret.Secret = strings.TrimSpace(secret.Secret)
	if secret.Secret == "" {
		return WebhookConfig{}, "", errors.New("webhook secret is required")
	}
	return config, secret.Secret, nil
}

func validWebhookSignature(secret string, body []byte, signature string) bool {
	signature = strings.TrimSpace(signature)
	signature = strings.TrimPrefix(signature, "sha256=")
	provided, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(provided, mac.Sum(nil))
}

func safeWebhookHeaders(headers map[string][]string, signatureHeader string) map[string]string {
	deny := map[string]struct{}{
		"authorization": {}, "cookie": {}, "set-cookie": {},
		"x-wf-signature-256": {}, "x-wf-webhook-secret": {},
	}
	deny[strings.ToLower(strings.TrimSpace(signatureHeader))] = struct{}{}
	result := map[string]string{}
	total := 0
	for name, values := range headers {
		key := strings.ToLower(strings.TrimSpace(name))
		if _, denied := deny[key]; denied || key == "" {
			continue
		}
		value := strings.Join(values, ",")
		if len(value) > 1024 {
			value = value[:1024]
		}
		if total+len(key)+len(value) > 8192 {
			break
		}
		total += len(key) + len(value)
		result[key] = value
	}
	return result
}

func firstHeader(headers map[string][]string, name string) string {
	for candidate, values := range headers {
		if strings.EqualFold(candidate, name) && len(values) > 0 {
			return values[0]
		}
	}
	return ""
}
