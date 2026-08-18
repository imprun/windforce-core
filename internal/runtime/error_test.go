package runtime

import (
	"encoding/json"
	"testing"
)

func TestErrorResultWithMetadataPreservesLegacyFields(t *testing.T) {
	var result struct {
		Name      string `json:"name"`
		Message   string `json:"message"`
		Phase     string `json:"phase"`
		Reason    string `json:"reason"`
		Retryable bool   `json:"retryable"`
	}
	if err := json.Unmarshal(ErrorResultWithMetadata(
		"RuntimeBindingError",
		"could not apply runtime bindings",
		FailureMetadata{Phase: "capability_run_open", Reason: "capacity_unavailable", Retryable: true},
	), &result); err != nil {
		t.Fatal(err)
	}
	if result.Name != "RuntimeBindingError" || result.Message != "could not apply runtime bindings" ||
		result.Phase != "capability_run_open" || result.Reason != "capacity_unavailable" || !result.Retryable {
		t.Fatalf("error result = %#v", result)
	}
}
