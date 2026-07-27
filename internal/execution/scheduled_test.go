package execution

import (
	"encoding/json"
	"testing"
	"time"
)

func TestInvocationFingerprintIncludesScheduledOccurrence(t *testing.T) {
	first, err := invocationRequestFingerprint(
		"demo",
		"run",
		json.RawMessage(`{"ok":true}`),
		"correlation",
		time.Date(2026, 7, 27, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	second, err := invocationRequestFingerprint(
		"demo",
		"run",
		json.RawMessage(`{"ok":true}`),
		"correlation",
		time.Date(2026, 7, 28, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("scheduled occurrences produced the same request fingerprint")
	}
}
