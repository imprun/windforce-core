package event

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReleasePublishedGolden(t *testing.T) {
	previousRelease := "rel_previous"
	previousCommit := "commit-previous"
	note := "Publish checkout update"
	value, err := NewReleasePublished("evt_0123456789abcdef", time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC), ReleasePublishedData{
		Workspace:         "default",
		AppKey:            "checkout",
		ReleaseID:         "rel_current",
		Commit:            "commit-current",
		PreviousReleaseID: &previousRelease,
		PreviousCommit:    &previousCommit,
		Actor:             "operator@example.test",
		Note:              &note,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	want, err := os.ReadFile(filepath.Join("..", "..", "contracts", "webhooks", "v1", "release-published.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("release event mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
	for _, protected := range [][]byte{[]byte("credential"), []byte("signing_secret"), []byte("external_key"), []byte("input")} {
		if bytes.Contains(bytes.ToLower(got), protected) {
			t.Fatalf("release event contains protected field %q: %s", protected, got)
		}
	}
}

func TestPublicWebhookTestFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "contracts", "webhooks", "v1", "webhook-test.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	var value Envelope
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	if err := Validate(value); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseRolledBackGolden(t *testing.T) {
	value, err := NewReleaseRolledBack("evt_rollbackexample", time.Date(2026, 7, 18, 4, 0, 0, 0, time.UTC), ReleaseRolledBackData{
		Workspace:         "default",
		AppKey:            "checkout",
		ReleaseID:         "rel_stable",
		Commit:            "commit-stable",
		PreviousReleaseID: "rel_current",
		PreviousCommit:    "commit-current",
		Actor:             "operator@example.test",
		Reason:            "Restore the stable release",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got = append(got, '\n')
	want, err := os.ReadFile(filepath.Join("..", "..", "contracts", "webhooks", "v1", "release-rolled-back.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("rollback event mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestValidateRejectsUnknownTypeAndFields(t *testing.T) {
	base := Envelope{
		SpecVersion:     CloudEventsSpecVersion,
		ID:              "evt_test",
		Type:            "windforce.unknown",
		Source:          "/workspaces/default/control-plane",
		Subject:         "unknown",
		Time:            time.Now().UTC(),
		DataContentType: JSONContentType,
		Data:            json.RawMessage(`{}`),
	}
	if err := Validate(base); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("unknown type error = %v", err)
	}
	base.Type = ReleasePublishedType
	base.Subject = "apps/echo/releases/release-a"
	base.Data = json.RawMessage(`{"workspace":"default","app_key":"echo","release_id":"release-a","commit":"commit-a","actor":"system","password":"secret"}`)
	if err := Validate(base); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("unknown field error = %v", err)
	}
}

func TestWebhookTestEvent(t *testing.T) {
	event, err := NewWebhookTest("evt_test", time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC), WebhookTestData{
		Workspace: "workspace-a", SubscriptionID: "whs_example", Actor: "operator@example.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != WebhookTestType || event.Subject != "webhooks/whs_example/test" {
		t.Fatalf("event = %#v", event)
	}
	data, err := WebhookTest(event)
	if err != nil {
		t.Fatal(err)
	}
	if data.Workspace != "workspace-a" || data.SubscriptionID != "whs_example" || data.Actor != "operator@example.test" {
		t.Fatalf("data = %#v", data)
	}
}

func TestHumanTaskLifecycleEvent(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	event, err := NewHumanTaskLifecycle("evt_human_created", HumanTaskCreatedType, now, HumanTaskLifecycleData{
		Workspace: "ws-a", TaskID: "human_1", RunID: "run_1", JobID: "job_1", Attempt: 1,
		AppKey: "scraper", ActionKey: "login", CorrelationID: "corr-1", Mode: "hold", Kind: "form",
		State: "pending", Actor: "app:scraper",
	})
	if err != nil {
		t.Fatal(err)
	}
	if event.Source != "/workspaces/ws-a/execution" || event.Subject != "human-tasks/human_1" {
		t.Fatalf("event routing = %#v", event)
	}
	data, err := HumanTaskLifecycle(event)
	if err != nil || data.AppKey != "scraper" || data.CorrelationID != "corr-1" {
		t.Fatalf("HumanTaskLifecycle = %#v, err=%v", data, err)
	}

	if _, err := NewHumanTaskLifecycle("evt_human_invalid", HumanTaskDecidedType, now, HumanTaskLifecycleData{
		Workspace: "ws-a", TaskID: "human_1", RunID: "run_1", JobID: "job_1", Attempt: 1,
		AppKey: "scraper", ActionKey: "login", Mode: "hold", Kind: "form", State: "decided", Actor: "operator:test",
	}); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("missing decision outcome error = %v", err)
	}
}
