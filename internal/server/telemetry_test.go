package server

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/imprun/windforce-core/internal/state"
	"github.com/imprun/windforce-core/internal/telemetry"
)

func TestJobStatusExposesTraceIDWithoutRawCarrier(t *testing.T) {
	carrier, _ := telemetry.ParseCarrier(
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"vendor=private-routing-state",
		"http",
	)
	job := state.Job{ID: "job-a", TraceContext: carrier}
	run := state.Run{TraceContext: carrier}
	payload, err := json.Marshal(newJobStatus("ws-a", job, run))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(payload, []byte(`"trace_id":"4bf92f3577b34da6a3ce929d0e0e4736"`)) {
		t.Fatalf("job status missing trace_id: %s", payload)
	}
	if bytes.Contains(payload, []byte("traceparent")) || bytes.Contains(payload, []byte("tracestate")) ||
		bytes.Contains(payload, []byte("private-routing-state")) {
		t.Fatalf("job status leaked raw carrier: %s", payload)
	}
}
