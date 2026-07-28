package trigger

import (
	"errors"
	"strings"

	"github.com/imprun/windforce-core/internal/completion"
	"github.com/imprun/windforce-core/internal/state"
)

func ValidateDefinition(definition state.TriggerDefinition) error {
	if strings.TrimSpace(definition.WorkspaceID) == "" {
		return errors.New("trigger workspace is required")
	}
	if strings.TrimSpace(definition.Name) == "" {
		return errors.New("trigger name is required")
	}
	if strings.TrimSpace(definition.AppKey) == "" || strings.TrimSpace(definition.ActionKey) == "" {
		return errors.New("trigger target app and action are required")
	}
	var err error
	switch definition.Kind {
	case KindWebhook:
		_, _, err = parseWebhookDefinition(definition)
	case KindSchedule:
		_, _, _, err = parseScheduleDefinition(definition)
	case KindRabbitMQ:
		_, _, err = parseRabbitMQDefinition(definition)
	default:
		return errors.New("unsupported trigger kind")
	}
	if err != nil {
		return err
	}
	policy, err := state.NormalizeTriggerCompletionPolicy(definition.Completion)
	if err != nil {
		return err
	}
	switch policy.Mode {
	case state.TriggerCompletionModeCallback:
		secrets, err := completion.ParseSecrets(definition.SecretConfig)
		if err != nil {
			return errors.New("trigger completion secret config must be valid JSON")
		}
		if secrets.SigningSecret == "" {
			return errors.New("callback completion signing secret is required")
		}
	case state.TriggerCompletionModePublish:
		secrets, err := completion.ParseSecrets(definition.SecretConfig)
		if err != nil {
			return errors.New("trigger completion secret config must be valid JSON")
		}
		if secrets.RabbitMQURL == "" {
			return errors.New("publish completion RabbitMQ URL is required")
		}
	}
	_, err = state.NormalizeTriggerResponsePolicy(definition.Kind, definition.Response)
	return err
}

func DefaultFactories() map[string]Factory {
	return map[string]Factory{
		KindWebhook:  NewWebhookTrigger,
		KindSchedule: NewScheduleTrigger,
		KindRabbitMQ: NewRabbitMQTrigger,
	}
}
