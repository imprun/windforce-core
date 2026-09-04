package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

// The execution attestation is host-private execution metadata. It travels in
// the Job payload to the worker plane and must never reach the public job
// status, which is what an ordinary API caller reads.
func TestPublicJobStatusOmitsTheExecutionAttestation(t *testing.T) {
	t.Parallel()

	now := time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC)
	job := state.Job{
		ID:        "job_synthetic",
		RunID:     "run_synthetic",
		Kind:      "action",
		State:     state.JobQueued,
		CreatedAt: now,
		Payload: state.JobPayload{
			Workspace: "synthetic",
			App:       "synthetic_app",
			Action:    "verify",
			Commit:    "0123456789abcdef",
			ExecutionAttestation: &contract.ExecutionAttestation{
				Kind: contract.ExecutionAttestationKindV1,
				Binding: contract.ExecutionAttestationBinding{
					Kind:            contract.ExecutionAttestationBindingKindV1,
					Audience:        "synthetic-capability-service",
					IssuerKeyID:     "synthetic-issuer-key-1",
					ExpiresAt:       now.Add(time.Minute).Format(time.RFC3339),
					RunRef:          "run_synthetic",
					Workspace:       "synthetic",
					App:             "synthetic_app",
					Action:          "verify",
					PublicationRef:  "synthetic-publication",
					RouteGeneration: 7,
					OperationRef:    "operations/synthetic/v1",
					CredentialRef:   contract.ImmutableReference{ID: "credential/synthetic", Version: "sha256:" + strings.Repeat("a", 64)},
					Release: contract.ExecutionReleasePin{
						Commit:       "0123456789abcdef",
						BundleDigest: "sha256:" + strings.Repeat("b", 64),
					},
				},
				BindingDigest: "sha256:" + strings.Repeat("c", 64),
				Algorithm:     contract.ExecutionAttestationAlgorithm,
				Signature:     "c3ludGhldGljLXNpZ25hdHVyZQ",
			},
		},
	}
	run := state.Run{ID: "run_synthetic", App: "synthetic_app", Action: "verify", CreatedAt: now, UpdatedAt: now}

	encoded, err := json.Marshal(newJobStatus("synthetic", job, run))
	if err != nil {
		t.Fatalf("encode job status: %v", err)
	}
	for _, forbidden := range []string{"executionAttestation", "bindingDigest", "signature", "issuerKeyId", "c3ludGhldGljLXNpZ25hdHVyZQ"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public job status exposes %q: %s", forbidden, encoded)
		}
	}
}
