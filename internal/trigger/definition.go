package trigger

import (
	"errors"
	"strings"

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
	switch definition.Kind {
	case KindWebhook:
		_, _, err := parseWebhookDefinition(definition)
		return err
	case KindSchedule:
		_, _, _, err := parseScheduleDefinition(definition)
		return err
	case KindRabbitMQ:
		_, _, err := parseRabbitMQDefinition(definition)
		return err
	default:
		return errors.New("unsupported trigger kind")
	}
}

func DefaultFactories() map[string]Factory {
	return map[string]Factory{
		KindWebhook:  NewWebhookTrigger,
		KindSchedule: NewScheduleTrigger,
		KindRabbitMQ: NewRabbitMQTrigger,
	}
}
