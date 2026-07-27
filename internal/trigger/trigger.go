package trigger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
)

const (
	KindWebhook  = "webhook"
	KindSchedule = "schedule"
	KindRabbitMQ = "rabbitmq"

	DeliveryAdmitted  = "admitted"
	DeliveryRetryable = "retryable"
	DeliveryTerminal  = "terminal"
)

var (
	ErrNotFound     = errors.New("trigger not found")
	ErrUnauthorized = errors.New("trigger authentication failed")
	ErrInvalidEvent = errors.New("invalid trigger event")
)

type Store interface {
	ListTriggers(ctx context.Context, workspaceID string) ([]state.TriggerDefinition, error)
	GetTrigger(ctx context.Context, workspaceID string, id string) (state.TriggerDefinition, error)
	GetWorkspace(ctx context.Context, workspaceID string) (state.Workspace, error)
	UpsertTriggerDelivery(ctx context.Context, delivery state.TriggerDelivery) (state.TriggerDelivery, error)
}

type AdmissionService interface {
	CreateRun(ctx context.Context, request execution.CreateRunRequest) (execution.Admission, error)
}

type Trigger interface {
	Initialize(Runtime) error
	Start(context.Context) error
	Stop(context.Context) error
}

type Factory func(state.TriggerDefinition) (Trigger, error)

type Runtime struct {
	Submitter *Submitter
}

type Event struct {
	TriggerID     string
	DeliveryID    string
	CorrelationID string
	Input         json.RawMessage
	RawPayload    []byte
	SafeMetadata  map[string]string
	ScheduledFor  time.Time
}

type Submission struct {
	State    string
	RunID    string
	Replayed bool
	Err      error
}

type Submitter struct {
	Store     Store
	Admission AdmissionService
	Metrics   *Metrics
}

func (s *Submitter) Submit(ctx context.Context, definition state.TriggerDefinition, event Event) Submission {
	if s == nil || s.Store == nil || s.Admission == nil {
		return Submission{State: DeliveryRetryable, Err: errors.New("trigger submitter is not configured")}
	}
	event.TriggerID = strings.TrimSpace(event.TriggerID)
	event.DeliveryID = strings.TrimSpace(event.DeliveryID)
	if event.TriggerID == "" || event.TriggerID != definition.ID || event.DeliveryID == "" {
		return s.record(ctx, definition, event, Submission{State: DeliveryTerminal, Err: ErrInvalidEvent})
	}
	input := event.Input
	if len(input) == 0 {
		input = json.RawMessage("{}")
	}
	if !json.Valid(input) {
		return s.record(ctx, definition, event, Submission{State: DeliveryTerminal, Err: fmt.Errorf("%w: input must be valid JSON", ErrInvalidEvent)})
	}
	headers, err := json.Marshal(event.SafeMetadata)
	if err != nil {
		return s.record(ctx, definition, event, Submission{State: DeliveryTerminal, Err: fmt.Errorf("%w: metadata", ErrInvalidEvent)})
	}
	admission, err := s.Admission.CreateRun(ctx, execution.CreateRunRequest{
		Workspace:      definition.WorkspaceID,
		App:            definition.AppKey,
		Action:         definition.ActionKey,
		Input:          input,
		Adapter:        "trigger:" + definition.Kind,
		TriggerKind:    definition.Kind,
		TriggerHeaders: headers,
		CorrelationID:  event.CorrelationID,
		IdempotencyKey: event.DeliveryID,
		ScheduledFor:   event.ScheduledFor,
		Principal:      execution.TriggerPrincipal(definition.WorkspaceID, definition.ID, definition.AppKey, definition.ActionKey),
	})
	if err != nil {
		state := DeliveryTerminal
		switch execution.FaultKindOf(err) {
		case execution.FaultUnavailable, execution.FaultInternal:
			state = DeliveryRetryable
		}
		return s.record(ctx, definition, event, Submission{State: state, Err: err})
	}
	return s.record(ctx, definition, event, Submission{
		State:    DeliveryAdmitted,
		RunID:    admission.Run.ID,
		Replayed: admission.Replayed,
	})
}

func (s *Submitter) record(ctx context.Context, definition state.TriggerDefinition, event Event, result Submission) Submission {
	if s != nil && s.Metrics != nil {
		defer func() {
			s.Metrics.ObserveAdmission(definition.Kind, result.State)
		}()
	}
	if s == nil || s.Store == nil || definition.ID == "" || strings.TrimSpace(event.DeliveryID) == "" {
		return result
	}
	delivery := state.TriggerDelivery{
		WorkspaceID:   definition.WorkspaceID,
		TriggerID:     definition.ID,
		DeliveryID:    event.DeliveryID,
		CorrelationID: event.CorrelationID,
		State:         result.State,
		RunID:         result.RunID,
	}
	if !event.ScheduledFor.IsZero() {
		scheduledFor := event.ScheduledFor.UTC()
		delivery.ScheduledFor = &scheduledFor
	}
	if result.Err != nil {
		delivery.ErrorSummary = result.Err.Error()
	}
	if _, err := s.Store.UpsertTriggerDelivery(ctx, delivery); err != nil && result.Err == nil {
		result.State = DeliveryRetryable
		result.Err = fmt.Errorf("record trigger delivery: %w", err)
	}
	return result
}

type instance struct {
	definition state.TriggerDefinition
	trigger    Trigger
	cancel     context.CancelFunc
}

type Manager struct {
	Store             Store
	Admission         AdmissionService
	Factories         map[string]Factory
	ReconcileInterval time.Duration
	Metrics           *Metrics

	reconcileMu sync.Mutex
	mu          sync.RWMutex
	instances   map[string]*instance
}

func (m *Manager) Run(ctx context.Context) error {
	if m == nil || m.Store == nil || m.Admission == nil {
		return errors.New("trigger manager is not configured")
	}
	if m.ReconcileInterval <= 0 {
		m.ReconcileInterval = time.Second
	}
	m.mu.Lock()
	if m.instances == nil {
		m.instances = map[string]*instance{}
	}
	m.mu.Unlock()
	if err := m.Reconcile(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(m.ReconcileInterval)
	defer ticker.Stop()
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		m.stopAll(stopCtx)
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := m.Reconcile(ctx); err != nil {
				return err
			}
		}
	}
}

func (m *Manager) Reconcile(ctx context.Context) error {
	m.reconcileMu.Lock()
	defer m.reconcileMu.Unlock()
	workspaces, err := m.triggerWorkspaces(ctx)
	if err != nil {
		return err
	}
	desired := map[string]state.TriggerDefinition{}
	for _, workspaceID := range workspaces {
		workspace, err := m.Store.GetWorkspace(ctx, workspaceID)
		if err != nil {
			return err
		}
		if workspace.Status == "archived" {
			continue
		}
		definitions, err := m.Store.ListTriggers(ctx, workspaceID)
		if err != nil {
			return err
		}
		for _, definition := range definitions {
			if definition.Enabled {
				desired[managerKey(definition.WorkspaceID, definition.ID)] = definition
			}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.instances == nil {
		m.instances = map[string]*instance{}
	}
	for key, current := range m.instances {
		next, ok := desired[key]
		if ok && current.definition.UpdatedAt.Equal(next.UpdatedAt) {
			delete(desired, key)
			continue
		}
		current.cancel()
		_ = current.trigger.Stop(context.Background())
		delete(m.instances, key)
	}
	for key, definition := range desired {
		factory := m.Factories[definition.Kind]
		if factory == nil {
			continue
		}
		adapter, err := factory(definition)
		if err != nil {
			return err
		}
		if err := adapter.Initialize(Runtime{Submitter: &Submitter{Store: m.Store, Admission: m.Admission, Metrics: m.Metrics}}); err != nil {
			return err
		}
		instanceCtx, cancel := context.WithCancel(ctx)
		if err := adapter.Start(instanceCtx); err != nil {
			cancel()
			return err
		}
		m.instances[key] = &instance{definition: definition, trigger: adapter, cancel: cancel}
	}
	if m.Metrics != nil {
		counts := map[string]int{}
		for _, current := range m.instances {
			counts[current.definition.Kind]++
		}
		m.Metrics.SetActive(counts)
	}
	return nil
}

func (m *Manager) stopAll(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, current := range m.instances {
		current.cancel()
		_ = current.trigger.Stop(ctx)
		delete(m.instances, key)
	}
	if m.Metrics != nil {
		m.Metrics.SetActive(map[string]int{})
	}
}

func (m *Manager) triggerWorkspaces(ctx context.Context) ([]string, error) {
	type workspaceLister interface {
		ListWorkspaces(context.Context) ([]state.Workspace, error)
	}
	lister, ok := m.Store.(workspaceLister)
	if !ok {
		return []string{"default"}, nil
	}
	workspaces, err := lister.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(workspaces))
	for _, workspace := range workspaces {
		ids = append(ids, workspace.ID)
	}
	return ids, nil
}

func managerKey(workspaceID string, triggerID string) string {
	return strings.TrimSpace(workspaceID) + "\x00" + strings.TrimSpace(triggerID)
}
