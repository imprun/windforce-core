package event

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

const (
	CloudEventsSpecVersion = "1.0"
	JSONContentType        = "application/json"
	ReleasePublishedType   = "windforce.release.published"
	ReleaseRolledBackType  = "windforce.release.rolled_back"
	WebhookTestType        = "windforce.webhook.test"
	HumanTaskCreatedType   = "windforce.human_task.created"
	HumanTaskDecidedType   = "windforce.human_task.decided"
	HumanTaskExpiredType   = "windforce.human_task.expired"
	HumanTaskCanceledType  = "windforce.human_task.canceled"
)

var ErrInvalidEvent = errors.New("invalid control plane event")

type Envelope struct {
	SpecVersion     string          `json:"specversion"`
	ID              string          `json:"id"`
	Type            string          `json:"type"`
	Source          string          `json:"source"`
	Subject         string          `json:"subject"`
	Time            time.Time       `json:"time"`
	DataContentType string          `json:"datacontenttype"`
	Data            json.RawMessage `json:"data"`
}

type ReleasePublishedData struct {
	Workspace         string  `json:"workspace"`
	AppKey            string  `json:"app_key"`
	ReleaseID         string  `json:"release_id"`
	Commit            string  `json:"commit"`
	PreviousReleaseID *string `json:"previous_release_id,omitempty"`
	PreviousCommit    *string `json:"previous_commit,omitempty"`
	Actor             string  `json:"actor"`
	Note              *string `json:"note,omitempty"`
}

type ReleaseRolledBackData struct {
	Workspace         string `json:"workspace"`
	AppKey            string `json:"app_key"`
	ReleaseID         string `json:"release_id"`
	Commit            string `json:"commit"`
	PreviousReleaseID string `json:"previous_release_id"`
	PreviousCommit    string `json:"previous_commit"`
	Actor             string `json:"actor"`
	Reason            string `json:"reason"`
}

type WebhookTestData struct {
	Workspace      string `json:"workspace"`
	SubscriptionID string `json:"subscription_id"`
	Actor          string `json:"actor"`
}

type HumanTaskLifecycleData struct {
	Workspace     string `json:"workspace"`
	TaskID        string `json:"task_id"`
	RunID         string `json:"run_id"`
	JobID         string `json:"job_id"`
	Attempt       int    `json:"attempt"`
	AppKey        string `json:"app_key"`
	ActionKey     string `json:"action_key"`
	CorrelationID string `json:"correlation_id,omitempty"`
	Mode          string `json:"mode"`
	Kind          string `json:"kind"`
	State         string `json:"state"`
	Outcome       string `json:"outcome,omitempty"`
	Actor         string `json:"actor"`
	TerminalCause string `json:"terminal_cause,omitempty"`
}

func NewReleasePublished(id string, occurredAt time.Time, data ReleasePublishedData) (Envelope, error) {
	data.Workspace = contract.NormalizeWorkspace(data.Workspace)
	data.Actor = strings.TrimSpace(data.Actor)
	if data.Actor == "" {
		data.Actor = "system"
	}
	if data.Note != nil {
		note := strings.TrimSpace(*data.Note)
		if note == "" {
			data.Note = nil
		} else {
			data.Note = &note
		}
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return Envelope{}, err
	}
	event := Envelope{
		SpecVersion:     CloudEventsSpecVersion,
		ID:              strings.TrimSpace(id),
		Type:            ReleasePublishedType,
		Source:          "/workspaces/" + data.Workspace + "/control-plane",
		Subject:         "apps/" + data.AppKey + "/releases/" + data.ReleaseID,
		Time:            occurredAt.UTC(),
		DataContentType: JSONContentType,
		Data:            raw,
	}
	if err := Validate(event); err != nil {
		return Envelope{}, err
	}
	return event, nil
}

func NewReleaseRolledBack(id string, occurredAt time.Time, data ReleaseRolledBackData) (Envelope, error) {
	data.Workspace = contract.NormalizeWorkspace(data.Workspace)
	data.Actor = strings.TrimSpace(data.Actor)
	data.Reason = strings.TrimSpace(data.Reason)
	raw, err := json.Marshal(data)
	if err != nil {
		return Envelope{}, err
	}
	event := Envelope{
		SpecVersion:     CloudEventsSpecVersion,
		ID:              strings.TrimSpace(id),
		Type:            ReleaseRolledBackType,
		Source:          "/workspaces/" + data.Workspace + "/control-plane",
		Subject:         "apps/" + data.AppKey + "/releases/" + data.ReleaseID,
		Time:            occurredAt.UTC(),
		DataContentType: JSONContentType,
		Data:            raw,
	}
	if err := Validate(event); err != nil {
		return Envelope{}, err
	}
	return event, nil
}

func NewWebhookTest(id string, occurredAt time.Time, data WebhookTestData) (Envelope, error) {
	data.Workspace = contract.NormalizeWorkspace(data.Workspace)
	data.SubscriptionID = strings.TrimSpace(data.SubscriptionID)
	data.Actor = strings.TrimSpace(data.Actor)
	if data.Actor == "" {
		data.Actor = "system"
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return Envelope{}, err
	}
	event := Envelope{
		SpecVersion:     CloudEventsSpecVersion,
		ID:              strings.TrimSpace(id),
		Type:            WebhookTestType,
		Source:          "/workspaces/" + data.Workspace + "/control-plane",
		Subject:         "webhooks/" + data.SubscriptionID + "/test",
		Time:            occurredAt.UTC(),
		DataContentType: JSONContentType,
		Data:            raw,
	}
	if err := Validate(event); err != nil {
		return Envelope{}, err
	}
	return event, nil
}

func NewHumanTaskLifecycle(id string, eventType string, occurredAt time.Time, data HumanTaskLifecycleData) (Envelope, error) {
	data.Workspace = contract.NormalizeWorkspace(data.Workspace)
	data.TaskID = strings.TrimSpace(data.TaskID)
	data.RunID = strings.TrimSpace(data.RunID)
	data.JobID = strings.TrimSpace(data.JobID)
	data.AppKey = strings.TrimSpace(data.AppKey)
	data.ActionKey = strings.TrimSpace(data.ActionKey)
	data.CorrelationID = strings.TrimSpace(data.CorrelationID)
	data.Mode = strings.TrimSpace(data.Mode)
	data.Kind = strings.TrimSpace(data.Kind)
	data.State = strings.TrimSpace(data.State)
	data.Outcome = strings.TrimSpace(data.Outcome)
	data.Actor = strings.TrimSpace(data.Actor)
	data.TerminalCause = strings.TrimSpace(data.TerminalCause)
	if data.Actor == "" {
		data.Actor = "system"
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return Envelope{}, err
	}
	event := Envelope{
		SpecVersion:     CloudEventsSpecVersion,
		ID:              strings.TrimSpace(id),
		Type:            strings.TrimSpace(eventType),
		Source:          "/workspaces/" + data.Workspace + "/execution",
		Subject:         "human-tasks/" + data.TaskID,
		Time:            occurredAt.UTC(),
		DataContentType: JSONContentType,
		Data:            raw,
	}
	if err := Validate(event); err != nil {
		return Envelope{}, err
	}
	return event, nil
}

func Validate(value Envelope) error {
	if value.SpecVersion != CloudEventsSpecVersion {
		return invalid("specversion must be %q", CloudEventsSpecVersion)
	}
	if !strings.HasPrefix(value.ID, "evt_") || len(value.ID) <= len("evt_") {
		return invalid("id must use the evt_ prefix")
	}
	if value.Time.IsZero() {
		return invalid("time is required")
	}
	if value.DataContentType != JSONContentType {
		return invalid("datacontenttype must be %q", JSONContentType)
	}
	if !json.Valid(value.Data) {
		return invalid("data must be valid JSON")
	}
	switch value.Type {
	case ReleasePublishedType:
		return validateReleasePublished(value)
	case ReleaseRolledBackType:
		return validateReleaseRolledBack(value)
	case WebhookTestType:
		return validateWebhookTest(value)
	case HumanTaskCreatedType, HumanTaskDecidedType, HumanTaskExpiredType, HumanTaskCanceledType:
		return validateHumanTaskLifecycle(value)
	default:
		return invalid("unsupported event type %q", value.Type)
	}
}

func ReleaseRolledBack(value Envelope) (ReleaseRolledBackData, error) {
	if value.Type != ReleaseRolledBackType {
		return ReleaseRolledBackData{}, invalid("event type is %q", value.Type)
	}
	var data ReleaseRolledBackData
	decoder := json.NewDecoder(bytes.NewReader(value.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return ReleaseRolledBackData{}, invalid("rollback data: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ReleaseRolledBackData{}, invalid("rollback data has trailing values")
	}
	return data, nil
}

func WebhookTest(value Envelope) (WebhookTestData, error) {
	if value.Type != WebhookTestType {
		return WebhookTestData{}, invalid("event type is %q", value.Type)
	}
	var data WebhookTestData
	decoder := json.NewDecoder(bytes.NewReader(value.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return WebhookTestData{}, invalid("webhook test data: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return WebhookTestData{}, invalid("webhook test data has trailing values")
	}
	return data, nil
}

func ReleasePublished(value Envelope) (ReleasePublishedData, error) {
	if value.Type != ReleasePublishedType {
		return ReleasePublishedData{}, invalid("event type is %q", value.Type)
	}
	var data ReleasePublishedData
	decoder := json.NewDecoder(bytes.NewReader(value.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return ReleasePublishedData{}, invalid("release data: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ReleasePublishedData{}, invalid("release data has trailing values")
	}
	return data, nil
}

func HumanTaskLifecycle(value Envelope) (HumanTaskLifecycleData, error) {
	switch value.Type {
	case HumanTaskCreatedType, HumanTaskDecidedType, HumanTaskExpiredType, HumanTaskCanceledType:
	default:
		return HumanTaskLifecycleData{}, invalid("event type is %q", value.Type)
	}
	var data HumanTaskLifecycleData
	decoder := json.NewDecoder(bytes.NewReader(value.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return HumanTaskLifecycleData{}, invalid("HumanTask lifecycle data: %v", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return HumanTaskLifecycleData{}, invalid("HumanTask lifecycle data has trailing values")
	}
	return data, nil
}

func validateReleasePublished(value Envelope) error {
	data, err := ReleasePublished(value)
	if err != nil {
		return err
	}
	data.Workspace = contract.NormalizeWorkspace(data.Workspace)
	if strings.TrimSpace(data.AppKey) == "" {
		return invalid("data.app_key is required")
	}
	if strings.TrimSpace(data.ReleaseID) == "" {
		return invalid("data.release_id is required")
	}
	if strings.TrimSpace(data.Commit) == "" {
		return invalid("data.commit is required")
	}
	if strings.TrimSpace(data.Actor) == "" {
		return invalid("data.actor is required")
	}
	wantSource := "/workspaces/" + data.Workspace + "/control-plane"
	if value.Source != wantSource {
		return invalid("source must be %q", wantSource)
	}
	wantSubject := "apps/" + data.AppKey + "/releases/" + data.ReleaseID
	if value.Subject != wantSubject {
		return invalid("subject must be %q", wantSubject)
	}
	return nil
}

func validateReleaseRolledBack(value Envelope) error {
	data, err := ReleaseRolledBack(value)
	if err != nil {
		return err
	}
	data.Workspace = contract.NormalizeWorkspace(data.Workspace)
	if strings.TrimSpace(data.AppKey) == "" {
		return invalid("data.app_key is required")
	}
	if strings.TrimSpace(data.ReleaseID) == "" {
		return invalid("data.release_id is required")
	}
	if strings.TrimSpace(data.Commit) == "" {
		return invalid("data.commit is required")
	}
	if strings.TrimSpace(data.PreviousReleaseID) == "" {
		return invalid("data.previous_release_id is required")
	}
	if strings.TrimSpace(data.PreviousCommit) == "" {
		return invalid("data.previous_commit is required")
	}
	if strings.TrimSpace(data.Actor) == "" {
		return invalid("data.actor is required")
	}
	if strings.TrimSpace(data.Reason) == "" {
		return invalid("data.reason is required")
	}
	wantSource := "/workspaces/" + data.Workspace + "/control-plane"
	if value.Source != wantSource {
		return invalid("source must be %q", wantSource)
	}
	wantSubject := "apps/" + data.AppKey + "/releases/" + data.ReleaseID
	if value.Subject != wantSubject {
		return invalid("subject must be %q", wantSubject)
	}
	return nil
}

func validateWebhookTest(value Envelope) error {
	data, err := WebhookTest(value)
	if err != nil {
		return err
	}
	data.Workspace = contract.NormalizeWorkspace(data.Workspace)
	if strings.TrimSpace(data.SubscriptionID) == "" {
		return invalid("data.subscription_id is required")
	}
	if strings.TrimSpace(data.Actor) == "" {
		return invalid("data.actor is required")
	}
	wantSource := "/workspaces/" + data.Workspace + "/control-plane"
	if value.Source != wantSource {
		return invalid("source must be %q", wantSource)
	}
	wantSubject := "webhooks/" + data.SubscriptionID + "/test"
	if value.Subject != wantSubject {
		return invalid("subject must be %q", wantSubject)
	}
	return nil
}

func validateHumanTaskLifecycle(value Envelope) error {
	data, err := HumanTaskLifecycle(value)
	if err != nil {
		return err
	}
	data.Workspace = contract.NormalizeWorkspace(data.Workspace)
	if strings.TrimSpace(data.TaskID) == "" || strings.TrimSpace(data.RunID) == "" || strings.TrimSpace(data.JobID) == "" {
		return invalid("data.task_id, data.run_id, and data.job_id are required")
	}
	if data.Attempt <= 0 || strings.TrimSpace(data.AppKey) == "" || strings.TrimSpace(data.ActionKey) == "" {
		return invalid("data.attempt, data.app_key, and data.action_key are required")
	}
	if data.Mode != "hold" || strings.TrimSpace(data.Kind) == "" || strings.TrimSpace(data.Actor) == "" {
		return invalid("data.mode must be hold and data.kind and data.actor are required")
	}
	wantState := map[string]string{
		HumanTaskCreatedType:  "pending",
		HumanTaskDecidedType:  "decided",
		HumanTaskExpiredType:  "expired",
		HumanTaskCanceledType: "canceled",
	}[value.Type]
	if data.State != wantState {
		return invalid("data.state must be %q for %s", wantState, value.Type)
	}
	if value.Type == HumanTaskDecidedType {
		if data.Outcome != "submit" && data.Outcome != "cancel" {
			return invalid("data.outcome must be submit or cancel for a decided HumanTask")
		}
	} else if data.Outcome != "" {
		return invalid("data.outcome is only allowed for a decided HumanTask")
	}
	if value.Type == HumanTaskExpiredType || value.Type == HumanTaskCanceledType {
		if data.TerminalCause == "" {
			return invalid("data.terminal_cause is required for a terminal HumanTask")
		}
	} else if data.TerminalCause != "" {
		return invalid("data.terminal_cause is not allowed for %s", value.Type)
	}
	wantSource := "/workspaces/" + data.Workspace + "/execution"
	if value.Source != wantSource {
		return invalid("source must be %q", wantSource)
	}
	wantSubject := "human-tasks/" + data.TaskID
	if value.Subject != wantSubject {
		return invalid("subject must be %q", wantSubject)
	}
	return nil
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidEvent, fmt.Sprintf(format, args...))
}
