package state

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

const (
	WorkerCredentialActive  = "active"
	WorkerCredentialRevoked = "revoked"

	WorkerGroupRunning  = "running"
	WorkerGroupDraining = "draining"
)

// WorkerCredential is a hash-only credential record for a remotely managed
// worker group. TokenHash is persisted but must never be projected by an API.
type WorkerCredential struct {
	ID                 string     `json:"id"`
	Group              string     `json:"group"`
	Generation         int64      `json:"generation"`
	WorkspaceIDs       []string   `json:"workspace_ids"`
	Labels             []string   `json:"labels"`
	Status             string     `json:"status"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	RevokedAt          *time.Time `json:"revoked_at,omitempty"`
	DrainDeadlineAt    *time.Time `json:"drain_deadline_at,omitempty"`
	CreatedBy          string     `json:"created_by"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
	TokenHash          string     `json:"tokenHash,omitempty"`
	OperationID        string     `json:"operationId,omitempty"`
	RequestFingerprint string     `json:"requestFingerprint,omitempty"`
	RevokeOperationID  string     `json:"revokeOperationId,omitempty"`
	RevokeFingerprint  string     `json:"revokeFingerprint,omitempty"`
}

type CreateWorkerCredentialRequest struct {
	ID                 string
	Group              string
	ExpectedGeneration int64
	WorkspaceIDs       []string
	Labels             []string
	ExpiresAt          *time.Time
	TokenHash          string
	OperationID        string
	RequestFingerprint string
	Actor              string
}

type RevokeWorkerCredentialRequest struct {
	Group              string
	CredentialID       string
	OperationID        string
	RequestFingerprint string
	DrainDeadlineAt    time.Time
	Actor              string
}

// WorkerGroupRunState fences new managed claims without invalidating leases
// already held by workers in the group.
type WorkerGroupRunState struct {
	Group              string     `json:"group"`
	State              string     `json:"state"`
	OperationID        string     `json:"operation_id,omitempty"`
	Revision           int64      `json:"revision"`
	DeadlineAt         *time.Time `json:"deadline_at,omitempty"`
	UpdatedBy          string     `json:"updated_by,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at,omitempty"`
	RequestFingerprint string     `json:"requestFingerprint,omitempty"`
}

type PutWorkerGroupRunStateRequest struct {
	Group              string
	State              string
	OperationID        string
	ExpectedRevision   int64
	DeadlineAt         *time.Time
	RequestFingerprint string
	Actor              string
}

// WorkerControlStore is the optional public worker-management persistence
// capability used by the server. Keeping it separate preserves compatibility
// for narrow Store test doubles and third-party Store wrappers.
type WorkerControlStore interface {
	CreateWorkerCredential(context.Context, CreateWorkerCredentialRequest) (WorkerCredential, bool, error)
	ListWorkerCredentials(context.Context, string) ([]WorkerCredential, error)
	GetWorkerCredential(context.Context, string, string) (WorkerCredential, error)
	GetWorkerCredentialByTokenHash(context.Context, string) (WorkerCredential, error)
	RevokeWorkerCredential(context.Context, RevokeWorkerCredentialRequest) (WorkerCredential, bool, error)
	GetWorkerGroupRunState(context.Context, string) (WorkerGroupRunState, error)
	PutWorkerGroupRunState(context.Context, PutWorkerGroupRunStateRequest) (WorkerGroupRunState, bool, error)
	GetWorkerGroupObservation(context.Context, string) (WorkerGroupObservation, error)
	GetWorker(context.Context, string) (WorkerRecord, error)
	ClaimJobForWorkerScope(context.Context, string, []string, []string, []string, time.Duration) (Job, Lease, error)
}

func NormalizeWorkerGroup(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("worker group is required")
	}
	if len(value) > 128 || CleanID(value) != value {
		return "", fmt.Errorf("worker group must use at most 128 letters, digits, '.', '-', '_', or ':'")
	}
	return value, nil
}

func NormalizeWorkerGroupRunState(value string) (string, error) {
	switch strings.TrimSpace(value) {
	case WorkerGroupRunning:
		return WorkerGroupRunning, nil
	case WorkerGroupDraining:
		return WorkerGroupDraining, nil
	default:
		return "", fmt.Errorf("state must be running or draining")
	}
}

func NormalizeWorkerScope(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			seen[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func NormalizeWorkerCredentialScope(workspaceIDs []string, labels []string) ([]string, []string, error) {
	workspaces := NormalizeWorkerScope(workspaceIDs)
	if len(workspaces) == 0 {
		return nil, nil, fmt.Errorf("%w: worker credential requires at least one workspace", ErrInvalidState)
	}
	normalizedLabels, err := contract.NormalizeLabels(labels, true)
	if err != nil {
		return nil, nil, err
	}
	if normalizedLabels == nil {
		normalizedLabels = []string{}
	}
	return workspaces, normalizedLabels, nil
}

func (credential WorkerCredential) AllowsNewWork(now time.Time) bool {
	return credential.Status == WorkerCredentialActive &&
		(credential.ExpiresAt == nil || now.Before(*credential.ExpiresAt))
}

func (credential WorkerCredential) AllowsLeaseContinuation(now time.Time) bool {
	if credential.AllowsNewWork(now) {
		return true
	}
	return credential.Status == WorkerCredentialRevoked && credential.DrainDeadlineAt != nil &&
		!now.After(*credential.DrainDeadlineAt)
}

func (state WorkerGroupRunState) Draining() bool {
	return state.State == WorkerGroupDraining
}

func DefaultWorkerGroupRunState(group string) WorkerGroupRunState {
	return WorkerGroupRunState{Group: group, State: WorkerGroupRunning}
}

func SameWorkerScope(left []string, right []string) bool {
	left = NormalizeWorkerScope(left)
	right = NormalizeWorkerScope(right)
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func WorkspaceAllowed(workspaceID string, allowed []string) bool {
	workspaceID = strings.TrimSpace(workspaceID)
	for _, candidate := range allowed {
		if candidate == workspaceID {
			return true
		}
	}
	return false
}

func workerJobWorkspace(job Job) string {
	workspaceID := strings.TrimSpace(job.Payload.Workspace)
	if workspaceID == "" {
		workspaceID = job.Payload.PinnedDeployment().Workspace
	}
	return contract.NormalizeWorkspace(workspaceID)
}
