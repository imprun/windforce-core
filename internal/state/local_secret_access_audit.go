package state

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func (s *LocalStore) AppendSecretAccessAudit(ctx context.Context, record SecretAccessAudit) error {
	record.WorkspaceID = contract.NormalizeWorkspace(record.WorkspaceID)
	record.JobID = strings.TrimSpace(record.JobID)
	record.Path = strings.TrimSpace(record.Path)
	if record.ID == "" {
		record.ID = NewID("secret-access")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	return s.update(ctx, func(snapshot *Snapshot, _ time.Time) error {
		for _, existing := range snapshot.SecretAccessAudits[record.WorkspaceID] {
			if existing.ID != record.ID {
				continue
			}
			if existing == record {
				return errSkipLocalStateWrite
			}
			return fmt.Errorf("%w: secret access audit %q", ErrConflict, record.ID)
		}
		snapshot.SecretAccessAudits[record.WorkspaceID] = append(
			snapshot.SecretAccessAudits[record.WorkspaceID],
			record,
		)
		return nil
	})
}

func (s *LocalStore) ListSecretAccessAudits(ctx context.Context, workspaceID string, jobID string) ([]SecretAccessAudit, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	records := []SecretAccessAudit{}
	for _, record := range snapshot.SecretAccessAudits[contract.NormalizeWorkspace(workspaceID)] {
		if jobID == "" || record.JobID == jobID {
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].CreatedAt.After(records[j].CreatedAt)
	})
	return records, nil
}
