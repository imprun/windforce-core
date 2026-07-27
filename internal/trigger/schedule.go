package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/imprun/windforce-core/internal/state"
	"github.com/robfig/cron/v3"
)

type ScheduleConfig struct {
	Cron     string          `json:"cron"`
	Timezone string          `json:"timezone"`
	Input    json.RawMessage `json:"input,omitempty"`
}

type ScheduleTrigger struct {
	definition state.TriggerDefinition
	config     ScheduleConfig
	location   *time.Location
	schedule   cron.Schedule
	submitter  *Submitter

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewScheduleTrigger(definition state.TriggerDefinition) (Trigger, error) {
	config, location, schedule, err := parseScheduleDefinition(definition)
	if err != nil {
		return nil, err
	}
	return &ScheduleTrigger{
		definition: definition,
		config:     config,
		location:   location,
		schedule:   schedule,
	}, nil
}

func (t *ScheduleTrigger) Initialize(runtime Runtime) error {
	if runtime.Submitter == nil {
		return errors.New("schedule trigger submitter is required")
	}
	t.submitter = runtime.Submitter
	return nil
}

func (t *ScheduleTrigger) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		return nil
	}
	if t.submitter == nil {
		return errors.New("schedule trigger is not initialized")
	}
	runCtx, cancel := context.WithCancel(ctx)
	t.cancel = cancel
	t.done = make(chan struct{})
	go t.run(runCtx, t.done)
	return nil
}

func (t *ScheduleTrigger) Stop(ctx context.Context) error {
	t.mu.Lock()
	cancel := t.cancel
	done := t.done
	t.cancel = nil
	t.done = nil
	t.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *ScheduleTrigger) run(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		now := time.Now().In(t.location)
		scheduledFor := t.schedule.Next(now)
		timer := time.NewTimer(time.Until(scheduledFor))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}

		event := Event{
			TriggerID:     t.definition.ID,
			DeliveryID:    scheduleDeliveryID(t.definition.ID, scheduledFor),
			CorrelationID: scheduleDeliveryID(t.definition.ID, scheduledFor),
			Input:         append(json.RawMessage(nil), t.config.Input...),
			SafeMetadata: map[string]string{
				"scheduled_for": scheduledFor.UTC().Format(time.RFC3339Nano),
				"timezone":      t.config.Timezone,
			},
			ScheduledFor: scheduledFor.UTC(),
		}
		submission := t.submitter.Submit(ctx, t.definition, event)
		for attempt := 0; submission.State == DeliveryRetryable && attempt < 5; attempt++ {
			delay := time.Duration(1<<attempt) * time.Second
			retryTimer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				if !retryTimer.Stop() {
					<-retryTimer.C
				}
				return
			case <-retryTimer.C:
				submission = t.submitter.Submit(ctx, t.definition, event)
			}
		}
	}
}

func parseScheduleDefinition(definition state.TriggerDefinition) (ScheduleConfig, *time.Location, cron.Schedule, error) {
	var config ScheduleConfig
	if err := json.Unmarshal(definition.Config, &config); err != nil {
		return ScheduleConfig{}, nil, nil, fmt.Errorf("schedule config: %w", err)
	}
	config.Cron = strings.TrimSpace(config.Cron)
	config.Timezone = strings.TrimSpace(config.Timezone)
	if config.Cron == "" {
		return ScheduleConfig{}, nil, nil, errors.New("schedule cron is required")
	}
	if config.Timezone == "" {
		return ScheduleConfig{}, nil, nil, errors.New("schedule timezone is required")
	}
	location, err := time.LoadLocation(config.Timezone)
	if err != nil {
		return ScheduleConfig{}, nil, nil, fmt.Errorf("schedule timezone: %w", err)
	}
	schedule, err := cron.ParseStandard(config.Cron)
	if err != nil {
		return ScheduleConfig{}, nil, nil, fmt.Errorf("schedule cron: %w", err)
	}
	if len(config.Input) == 0 {
		config.Input = json.RawMessage("{}")
	}
	if !json.Valid(config.Input) {
		return ScheduleConfig{}, nil, nil, errors.New("schedule input must be valid JSON")
	}
	return config, location, schedule, nil
}

func scheduleDeliveryID(triggerID string, scheduledFor time.Time) string {
	return triggerID + ":" + scheduledFor.UTC().Format(time.RFC3339Nano)
}
