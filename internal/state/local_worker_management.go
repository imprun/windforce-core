package state

import (
	"context"
	"sort"
	"strings"
	"time"
)

func (s *LocalStore) CreateWorkerCredential(ctx context.Context, request CreateWorkerCredentialRequest) (WorkerCredential, bool, error) {
	group, err := NormalizeWorkerGroup(request.Group)
	if err != nil {
		return WorkerCredential{}, false, err
	}
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	request.TokenHash = strings.TrimSpace(request.TokenHash)
	workspaces, labels, err := NormalizeWorkerCredentialScope(request.WorkspaceIDs, request.Labels)
	if err != nil {
		return WorkerCredential{}, false, err
	}
	if request.ExpectedGeneration < 0 || request.OperationID == "" || request.RequestFingerprint == "" || request.TokenHash == "" {
		return WorkerCredential{}, false, ErrInvalidState
	}
	var result WorkerCredential
	var replayed bool
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		for _, existing := range snapshot.WorkerCredentials {
			if existing.Group != group || existing.OperationID != request.OperationID {
				continue
			}
			if existing.RequestFingerprint != request.RequestFingerprint {
				return ErrConflict
			}
			result = existing
			replayed = true
			return nil
		}
		var currentGeneration int64
		for _, existing := range snapshot.WorkerCredentials {
			if existing.Group == group && existing.Generation > currentGeneration {
				currentGeneration = existing.Generation
			}
			if existing.TokenHash == request.TokenHash {
				return ErrConflict
			}
		}
		if request.ExpectedGeneration != currentGeneration {
			return ErrConflict
		}
		id := strings.TrimSpace(request.ID)
		if id == "" {
			id = NewID("worker_credential")
		}
		if _, exists := snapshot.WorkerCredentials[id]; exists {
			return ErrConflict
		}
		result = WorkerCredential{
			ID: id, Group: group, Generation: currentGeneration + 1,
			WorkspaceIDs: workspaces, Labels: labels,
			Status: WorkerCredentialActive, ExpiresAt: request.ExpiresAt,
			CreatedBy: strings.TrimSpace(request.Actor), CreatedAt: now, UpdatedAt: now,
			TokenHash: request.TokenHash, OperationID: request.OperationID,
			RequestFingerprint: request.RequestFingerprint,
		}
		snapshot.WorkerCredentials[id] = result
		return nil
	})
	return result, replayed, err
}

func (s *LocalStore) ListWorkerCredentials(ctx context.Context, group string) ([]WorkerCredential, error) {
	group, err := NormalizeWorkerGroup(group)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]WorkerCredential, 0)
	for _, credential := range snapshot.WorkerCredentials {
		if credential.Group == group {
			out = append(out, credential)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Generation > out[j].Generation })
	return out, nil
}

func (s *LocalStore) GetWorkerCredential(ctx context.Context, group string, id string) (WorkerCredential, error) {
	group, err := NormalizeWorkerGroup(group)
	if err != nil {
		return WorkerCredential{}, err
	}
	snapshot, err := s.Load(ctx)
	if err != nil {
		return WorkerCredential{}, err
	}
	credential, ok := snapshot.WorkerCredentials[strings.TrimSpace(id)]
	if !ok || credential.Group != group {
		return WorkerCredential{}, ErrNotFound
	}
	return credential, nil
}

func (s *LocalStore) GetWorkerCredentialByTokenHash(ctx context.Context, tokenHash string) (WorkerCredential, error) {
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return WorkerCredential{}, ErrNotFound
	}
	snapshot, err := s.Load(ctx)
	if err != nil {
		return WorkerCredential{}, err
	}
	for _, credential := range snapshot.WorkerCredentials {
		if credential.TokenHash == tokenHash {
			return credential, nil
		}
	}
	return WorkerCredential{}, ErrNotFound
}

func (s *LocalStore) RevokeWorkerCredential(ctx context.Context, request RevokeWorkerCredentialRequest) (WorkerCredential, bool, error) {
	group, err := NormalizeWorkerGroup(request.Group)
	if err != nil {
		return WorkerCredential{}, false, err
	}
	request.CredentialID = strings.TrimSpace(request.CredentialID)
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	if request.CredentialID == "" || request.OperationID == "" || request.RequestFingerprint == "" || request.DrainDeadlineAt.IsZero() {
		return WorkerCredential{}, false, ErrInvalidState
	}
	var result WorkerCredential
	var replayed bool
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		credential, ok := snapshot.WorkerCredentials[request.CredentialID]
		if !ok || credential.Group != group {
			return ErrNotFound
		}
		if credential.RevokeOperationID != "" {
			if credential.RevokeOperationID != request.OperationID || credential.RevokeFingerprint != request.RequestFingerprint {
				return ErrConflict
			}
			result = credential
			replayed = true
			return nil
		}
		deadline := request.DrainDeadlineAt.UTC()
		credential.Status = WorkerCredentialRevoked
		credential.RevokedAt = &now
		credential.DrainDeadlineAt = &deadline
		credential.RevokeOperationID = request.OperationID
		credential.RevokeFingerprint = request.RequestFingerprint
		credential.UpdatedAt = now
		snapshot.WorkerCredentials[credential.ID] = credential
		result = credential
		return nil
	})
	return result, replayed, err
}

func (s *LocalStore) GetWorkerGroupRunState(ctx context.Context, group string) (WorkerGroupRunState, error) {
	group, err := NormalizeWorkerGroup(group)
	if err != nil {
		return WorkerGroupRunState{}, err
	}
	snapshot, err := s.Load(ctx)
	if err != nil {
		return WorkerGroupRunState{}, err
	}
	if current, ok := snapshot.WorkerGroupRunStates[group]; ok {
		return current, nil
	}
	return DefaultWorkerGroupRunState(group), nil
}

func (s *LocalStore) PutWorkerGroupRunState(ctx context.Context, request PutWorkerGroupRunStateRequest) (WorkerGroupRunState, bool, error) {
	group, err := NormalizeWorkerGroup(request.Group)
	if err != nil {
		return WorkerGroupRunState{}, false, err
	}
	stateValue, err := NormalizeWorkerGroupRunState(request.State)
	if err != nil {
		return WorkerGroupRunState{}, false, err
	}
	request.OperationID = strings.TrimSpace(request.OperationID)
	request.RequestFingerprint = strings.TrimSpace(request.RequestFingerprint)
	if request.ExpectedRevision < 0 || request.OperationID == "" || request.RequestFingerprint == "" ||
		(stateValue == WorkerGroupDraining && request.DeadlineAt == nil) {
		return WorkerGroupRunState{}, false, ErrInvalidState
	}
	if stateValue == WorkerGroupRunning {
		request.DeadlineAt = nil
	} else if request.DeadlineAt != nil {
		deadline := request.DeadlineAt.UTC()
		request.DeadlineAt = &deadline
	}
	var result WorkerGroupRunState
	var replayed bool
	err = s.update(ctx, func(snapshot *Snapshot, now time.Time) error {
		current, ok := snapshot.WorkerGroupRunStates[group]
		if !ok {
			current = DefaultWorkerGroupRunState(group)
		}
		if current.OperationID == request.OperationID {
			if current.RequestFingerprint != request.RequestFingerprint {
				return ErrConflict
			}
			result = current
			replayed = true
			return nil
		}
		if current.Revision != request.ExpectedRevision {
			return ErrConflict
		}
		result = WorkerGroupRunState{
			Group: group, State: stateValue, OperationID: request.OperationID,
			Revision: current.Revision + 1, DeadlineAt: request.DeadlineAt,
			UpdatedBy: strings.TrimSpace(request.Actor), UpdatedAt: now,
			RequestFingerprint: request.RequestFingerprint,
		}
		snapshot.WorkerGroupRunStates[group] = result
		return nil
	})
	return result, replayed, err
}

func (s *LocalStore) GetWorker(ctx context.Context, workerID string) (WorkerRecord, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return WorkerRecord{}, err
	}
	record, ok := snapshot.Workers[strings.TrimSpace(workerID)]
	if !ok {
		return WorkerRecord{}, ErrNotFound
	}
	return record, nil
}
