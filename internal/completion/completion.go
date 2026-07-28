package completion

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/state"
)

const EnvelopeType = "windforce.trigger.completion"

type Envelope struct {
	Version       string          `json:"version"`
	Type          string          `json:"type"`
	WorkspaceID   string          `json:"workspace_id"`
	TriggerID     string          `json:"trigger_id"`
	DeliveryID    string          `json:"delivery_id"`
	CorrelationID string          `json:"correlation_id,omitempty"`
	RunID         string          `json:"run_id"`
	App           string          `json:"app"`
	Action        string          `json:"action"`
	State         state.RunState  `json:"state"`
	Output        json.RawMessage `json:"output,omitempty"`
	Error         json.RawMessage `json:"error,omitempty"`
	CompletedAt   time.Time       `json:"completed_at"`
}

type Secrets struct {
	SigningSecret string `json:"signing_secret"`
	RabbitMQURL   string `json:"rabbitmq_url"`
}

func NewEnvelope(claim *state.TriggerCompletionClaim) Envelope {
	run := claim.Run
	output := run.Output
	if len(output) == 0 && run.Result != nil {
		output = run.Result.Output
	}
	return Envelope{
		Version:       "1",
		Type:          EnvelopeType,
		WorkspaceID:   claim.Delivery.WorkspaceID,
		TriggerID:     claim.Delivery.TriggerID,
		DeliveryID:    claim.Delivery.DeliveryID,
		CorrelationID: claim.Delivery.CorrelationID,
		RunID:         run.ID,
		App:           run.App,
		Action:        run.Action,
		State:         run.State,
		Output:        validJSON(output),
		Error:         validJSON(run.Error),
		CompletedAt:   run.UpdatedAt.UTC(),
	}
}

func ParseSecrets(raw json.RawMessage) (Secrets, error) {
	var value struct {
		Completion Secrets `json:"completion"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return Secrets{}, err
	}
	value.Completion.SigningSecret = strings.TrimSpace(value.Completion.SigningSecret)
	value.Completion.RabbitMQURL = strings.TrimSpace(value.Completion.RabbitMQURL)
	return value.Completion, nil
}

func validJSON(value json.RawMessage) json.RawMessage {
	if len(value) == 0 || !json.Valid(value) {
		return nil
	}
	return value
}
