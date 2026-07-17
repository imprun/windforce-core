package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/state"
)

// Backend is exactly the store surface the Processor consumes. A state.Store
// satisfies it directly (local workers); the remote worker plane client
// (ADR 0010) implements it over /worker/v1 with prepared claims, in which
// case DecryptInput/ResolveInput are identities.
type Backend interface {
	RegisterWorker(ctx context.Context, record state.WorkerRecord) error
	HeartbeatWorker(ctx context.Context, workerID string) error
	DeregisterWorker(ctx context.Context, workerID string) error
	ClaimJobForWorker(ctx context.Context, workerID string, tags []string, labels []string, leaseTTL time.Duration) (state.Job, state.Lease, error)
	DecryptInput(ctx context.Context, workspaceID string, input json.RawMessage) (json.RawMessage, error)
	ResolveInput(ctx context.Context, workspaceID string, appKey string, actionKey string, clientID string, request json.RawMessage) (json.RawMessage, error)
	AppendLogs(ctx context.Context, jobID string, workspaceID string, chunk string) error
	HeartbeatJob(ctx context.Context, lease state.Lease, leaseTTL time.Duration) (state.HeartbeatResult, error)
	CompleteJobSucceeded(ctx context.Context, lease state.Lease, result contract.JobResult) error
	CompleteJobFailed(ctx context.Context, lease state.Lease, result contract.JobResult) error
	CompleteJobWaitingHuman(ctx context.Context, lease state.Lease, result contract.JobResult, task state.HumanTask) error
}

// JobTokenProvider is implemented by backends that receive pre-minted SDK
// callback tokens with their claims (remote workers); the signing secret
// never leaves the engine.
type JobTokenProvider interface {
	JobTokenFor(jobID string) string
}
