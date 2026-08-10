package server

import "testing"

func TestPresentationModesDefaultAndValidate(t *testing.T) {
	if got, err := ParseUIMode(""); err != nil || got != UIModeEmbedded {
		t.Fatalf("empty UI mode = %q, %v", got, err)
	}
	if got, err := ParseUIMode(" DISABLED "); err != nil || got != UIModeDisabled {
		t.Fatalf("disabled UI mode = %q, %v", got, err)
	}
	if _, err := ParseUIMode("hosted"); err == nil {
		t.Fatal("invalid UI mode accepted")
	}

	if got, err := ParseWorkerGroupOperator(""); err != nil || got != WorkerGroupOperatorSelfManaged {
		t.Fatalf("empty Worker Group operator = %q, %v", got, err)
	}
	if got, err := ParseWorkerGroupOperator(" EXTERNAL "); err != nil || got != WorkerGroupOperatorExternal {
		t.Fatalf("external Worker Group operator = %q, %v", got, err)
	}
	if _, err := ParseWorkerGroupOperator("core"); err == nil {
		t.Fatal("invalid Worker Group operator accepted")
	}
}
