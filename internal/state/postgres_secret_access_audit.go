package state

import (
	"context"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func (s *PostgresStore) AppendSecretAccessAudit(ctx context.Context, record SecretAccessAudit) error {
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `
INSERT INTO secret_access_audit (
    workspace_id, job_id, attempt, app_key, action_key, path, source, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
`, contract.NormalizeWorkspace(record.WorkspaceID), record.JobID, record.Attempt, record.AppKey,
		record.ActionKey, record.Path, record.Source, record.CreatedAt)
	return err
}

func (s *PostgresStore) ListSecretAccessAudits(ctx context.Context, workspaceID string, jobID string) ([]SecretAccessAudit, error) {
	rows, err := s.pool.Query(ctx, `
SELECT id::text, workspace_id, job_id, attempt, app_key, action_key, path, source, created_at
FROM secret_access_audit
WHERE workspace_id=$1 AND ($2='' OR job_id=$2)
ORDER BY created_at DESC, id DESC
`, contract.NormalizeWorkspace(workspaceID), jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []SecretAccessAudit{}
	for rows.Next() {
		var record SecretAccessAudit
		if err := rows.Scan(
			&record.ID,
			&record.WorkspaceID,
			&record.JobID,
			&record.Attempt,
			&record.AppKey,
			&record.ActionKey,
			&record.Path,
			&record.Source,
			&record.CreatedAt,
		); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
