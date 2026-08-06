package trigger

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/execution"
	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/telemetry"
	amqp "github.com/rabbitmq/amqp091-go"
)

type triggerTestStore struct {
	deliveries []state.TriggerDelivery
}

func (s *triggerTestStore) ListTriggers(context.Context, string) ([]state.TriggerDefinition, error) {
	return nil, nil
}

func (s *triggerTestStore) GetTrigger(context.Context, string, string) (state.TriggerDefinition, error) {
	return state.TriggerDefinition{}, state.ErrNotFound
}

func (s *triggerTestStore) GetWorkspace(context.Context, string) (state.Workspace, error) {
	return state.Workspace{ID: "ws"}, nil
}

func (s *triggerTestStore) UpsertTriggerDelivery(_ context.Context, delivery state.TriggerDelivery) (state.TriggerDelivery, error) {
	s.deliveries = append(s.deliveries, delivery)
	return delivery, nil
}

type triggerTestAdmission struct {
	request      execution.CreateRunRequest
	traceContext telemetry.TraceContextV1
	err          error
}

func (a *triggerTestAdmission) CreateRun(ctx context.Context, request execution.CreateRunRequest) (execution.Admission, error) {
	a.request = request
	a.traceContext = telemetry.CreationContext(ctx, "trigger")
	if a.err != nil {
		return execution.Admission{}, a.err
	}
	return execution.Admission{Run: state.Run{ID: "run-1"}}, nil
}

func TestSubmitterContinuesProtocolTraceContext(t *testing.T) {
	admission := &triggerTestAdmission{}
	submitter := &Submitter{Store: &triggerTestStore{}, Admission: admission}
	definition := state.TriggerDefinition{
		ID: "trg-rabbit", WorkspaceID: "ws", Kind: KindRabbitMQ, AppKey: "demo", ActionKey: "run",
	}
	submission := submitter.Submit(context.Background(), definition, Event{
		TriggerID:  definition.ID,
		DeliveryID: "delivery-1",
		Input:      json.RawMessage(`{}`),
		SafeMetadata: map[string]string{
			"TraceParent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			"TraceState":  "vendor=value",
		},
	})
	if submission.State != DeliveryAdmitted || telemetry.TraceID(admission.traceContext) != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("protocol submission = %#v trace=%#v", submission, admission.traceContext)
	}
}

func TestRabbitMQMetadataCarriesOnlyBoundedTraceHeaders(t *testing.T) {
	metadata := safeRabbitMQMetadata(amqp.Delivery{Headers: amqp.Table{
		"TraceParent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"TraceState":  "vendor=value",
		"Secret":      "must-not-cross",
	}})
	if metadata["traceparent"] == "" || metadata["tracestate"] != "vendor=value" {
		t.Fatalf("trace metadata = %#v", metadata)
	}
	if _, exists := metadata["Secret"]; exists {
		t.Fatalf("unapproved RabbitMQ header crossed metadata boundary: %#v", metadata)
	}
}

func TestWebhookTriggerVerifiesSignatureAndRedactsSecretHeader(t *testing.T) {
	store := &triggerTestStore{}
	admission := &triggerTestAdmission{}
	definition := state.TriggerDefinition{
		ID:           "trg-1",
		WorkspaceID:  "ws",
		Name:         "incoming",
		Kind:         KindWebhook,
		AppKey:       "demo",
		ActionKey:    "run",
		Config:       json.RawMessage(`{"signature_header":"X-Hook-Signature"}`),
		SecretConfig: json.RawMessage(`{"secret":"top-secret"}`),
	}
	adapter, err := NewWebhookTrigger(definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Initialize(Runtime{Submitter: &Submitter{Store: store, Admission: admission}}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"hello":"world"}`)
	signature := hmac.New(sha256.New, []byte("top-secret"))
	_, _ = signature.Write(body)
	submission := adapter.(*WebhookTrigger).Deliver(context.Background(), WebhookRequest{
		Headers: map[string][]string{
			"X-Hook-Signature": {"sha256=" + hex.EncodeToString(signature.Sum(nil))},
			"X-Safe":           {"visible"},
		},
		Body: body,
	})
	if submission.State != DeliveryAdmitted || submission.RunID != "run-1" {
		t.Fatalf("submission = %#v", submission)
	}
	if admission.request.IdempotencyKey == "" || admission.request.Adapter != "trigger:webhook" {
		t.Fatalf("admission request = %#v", admission.request)
	}
	var metadata map[string]string
	if err := json.Unmarshal(admission.request.TriggerHeaders, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata["x-safe"] != "visible" {
		t.Fatalf("safe metadata = %#v", metadata)
	}
	if _, leaked := metadata["x-hook-signature"]; leaked {
		t.Fatalf("signature header leaked: %#v", metadata)
	}
}

func TestWebhookTriggerRejectsInvalidSignature(t *testing.T) {
	store := &triggerTestStore{}
	admission := &triggerTestAdmission{}
	definition := state.TriggerDefinition{
		ID:           "trg-1",
		WorkspaceID:  "ws",
		Name:         "incoming",
		Kind:         KindWebhook,
		AppKey:       "demo",
		ActionKey:    "run",
		Config:       json.RawMessage(`{}`),
		SecretConfig: json.RawMessage(`{"secret":"top-secret"}`),
	}
	adapter, err := NewWebhookTrigger(definition)
	if err != nil {
		t.Fatal(err)
	}
	if err := adapter.Initialize(Runtime{Submitter: &Submitter{Store: store, Admission: admission}}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	submission := adapter.(*WebhookTrigger).Deliver(context.Background(), WebhookRequest{
		Headers: map[string][]string{"X-WF-Signature-256": {"wrong"}},
		Body:    []byte(`{}`),
	})
	if !errors.Is(submission.Err, ErrUnauthorized) {
		t.Fatalf("submission = %#v", submission)
	}
	if admission.request.Workspace != "" {
		t.Fatalf("unexpected admission: %#v", admission.request)
	}
}

func TestScheduleDefinitionAndOccurrenceID(t *testing.T) {
	definition := state.TriggerDefinition{
		ID:          "trg-schedule",
		WorkspaceID: "ws",
		Name:        "daily",
		Kind:        KindSchedule,
		AppKey:      "demo",
		ActionKey:   "run",
		Config:      json.RawMessage(`{"cron":"0 9 * * *","timezone":"Asia/Seoul","input":{"source":"timer"}}`),
	}
	config, location, schedule, err := parseScheduleDefinition(definition)
	if err != nil {
		t.Fatal(err)
	}
	if location.String() != "Asia/Seoul" || config.Timezone != "Asia/Seoul" {
		t.Fatalf("timezone = %q / %q", location, config.Timezone)
	}
	next := schedule.Next(time.Date(2026, 7, 27, 8, 59, 0, 0, location))
	if got := next.Hour(); got != 9 {
		t.Fatalf("next hour = %d", got)
	}
	if scheduleDeliveryID(definition.ID, next) != scheduleDeliveryID(definition.ID, next.UTC()) {
		t.Fatal("occurrence id is not timezone-stable")
	}
}

type notifyingAdmission struct {
	requests chan execution.CreateRunRequest
}

func (a *notifyingAdmission) CreateRun(_ context.Context, request execution.CreateRunRequest) (execution.Admission, error) {
	a.requests <- request
	return execution.Admission{Run: state.Run{ID: "run-schedule"}}, nil
}

type scheduleStub struct {
	mu    sync.Mutex
	first time.Time
	used  bool
}

func (s *scheduleStub) Next(now time.Time) time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.used {
		s.used = true
		return s.first
	}
	return now.Add(time.Hour)
}

func TestScheduleTriggerSubmitsScheduledForWithoutHTTPLoopback(t *testing.T) {
	scheduledFor := time.Now().UTC().Add(50 * time.Millisecond)
	admission := &notifyingAdmission{requests: make(chan execution.CreateRunRequest, 1)}
	adapter := &ScheduleTrigger{
		definition: state.TriggerDefinition{
			ID: "trg-schedule", WorkspaceID: "ws", Kind: KindSchedule, AppKey: "demo", ActionKey: "run",
		},
		config:   ScheduleConfig{Timezone: "UTC", Input: json.RawMessage(`{"source":"timer"}`)},
		location: time.UTC,
		schedule: &scheduleStub{first: scheduledFor},
	}
	if err := adapter.Initialize(Runtime{Submitter: &Submitter{
		Store:     &triggerTestStore{},
		Admission: admission,
	}}); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-admission.requests:
		if !request.ScheduledFor.Equal(scheduledFor) {
			t.Fatalf("scheduled_for = %s, want %s", request.ScheduledFor, scheduledFor)
		}
		if request.IdempotencyKey != scheduleDeliveryID("trg-schedule", scheduledFor) {
			t.Fatalf("idempotency key = %q", request.IdempotencyKey)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("schedule admission timed out")
	}
	stopCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := adapter.Stop(stopCtx); err != nil {
		t.Fatal(err)
	}
}

type testAcknowledger struct {
	acked    bool
	nacked   bool
	rejected bool
	requeue  bool
}

func (a *testAcknowledger) Ack(uint64, bool) error {
	a.acked = true
	return nil
}

func (a *testAcknowledger) Nack(_ uint64, _ bool, requeue bool) error {
	a.nacked = true
	a.requeue = requeue
	return nil
}

func (a *testAcknowledger) Reject(uint64, bool) error {
	a.rejected = true
	return nil
}

func TestRabbitMQDeliveryAcknowledgementPolicy(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		acked    bool
		nacked   bool
		rejected bool
		requeue  bool
	}{
		{name: "admitted", acked: true},
		{name: "retryable", err: &execution.Fault{Kind: execution.FaultUnavailable, Message: "retry"}, nacked: true, requeue: true},
		{name: "terminal", err: &execution.Fault{Kind: execution.FaultInvalidRequest, Message: "invalid"}, rejected: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			acknowledger := &testAcknowledger{}
			adapter := &RabbitMQTrigger{
				definition: state.TriggerDefinition{
					ID: "trg-rabbit", WorkspaceID: "ws", Kind: KindRabbitMQ, AppKey: "demo", ActionKey: "run",
				},
				config: RabbitMQConfig{InputMode: "json"},
				submitter: &Submitter{
					Store:     &triggerTestStore{},
					Admission: &triggerTestAdmission{err: test.err},
				},
			}
			adapter.handleDelivery(context.Background(), amqp.Delivery{
				Acknowledger: acknowledger,
				DeliveryTag:  1,
				MessageId:    "message-1",
				Body:         []byte(`{"ok":true}`),
			})
			if acknowledger.acked != test.acked ||
				acknowledger.nacked != test.nacked ||
				acknowledger.rejected != test.rejected ||
				acknowledger.requeue != test.requeue {
				t.Fatalf("ack policy = %#v", acknowledger)
			}
		})
	}
}

func TestTriggerMetricsExposeOnlyBoundedLabels(t *testing.T) {
	metrics := NewMetrics()
	metrics.ObserveAdmission(KindWebhook, DeliveryAdmitted)
	metrics.SetActive(map[string]int{KindWebhook: 2})
	response := httptest.NewRecorder()
	metrics.Handler(nil).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	for _, expected := range []string{
		`windforce_trigger_admissions_total{kind="webhook",state="admitted"} 1`,
		`windforce_trigger_active{kind="webhook"} 2`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics missing %q:\n%s", expected, body)
		}
	}
}
