package state

import (
	"context"
	"sort"
	"strings"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/secretbackend"
)

func (s *LocalStore) ListLiveRuntimeSecretCandidateReferences(ctx context.Context) ([]secretbackend.Reference, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	live := map[secretbackend.Reference]struct{}{}
	for workspaceID, variables := range snapshot.Variables {
		for _, variable := range variables {
			if variable.OwnerScope != contract.RuntimeConfigScopeApp || !variable.IsSecret {
				continue
			}
			if err := addLiveRuntimeSecretCandidate(live, workspaceID, variable); err != nil {
				return nil, err
			}
		}
	}
	return sortedRuntimeSecretCandidateReferences(live), nil
}

func (s *PostgresStore) ListLiveRuntimeSecretCandidateReferences(ctx context.Context) ([]secretbackend.Reference, error) {
	rows, err := s.pool.Query(ctx, `
SELECT workspace_id, app_key, path, value
FROM runtime_variable
WHERE owner_scope='app' AND is_secret=true
ORDER BY workspace_id, app_key, path
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	live := map[secretbackend.Reference]struct{}{}
	for rows.Next() {
		var workspaceID string
		var variable Variable
		variable.OwnerScope = contract.RuntimeConfigScopeApp
		variable.IsSecret = true
		if err := rows.Scan(&workspaceID, &variable.AppKey, &variable.Path, &variable.Value); err != nil {
			return nil, err
		}
		if err := addLiveRuntimeSecretCandidate(live, workspaceID, variable); err != nil {
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return sortedRuntimeSecretCandidateReferences(live), nil
}

func addLiveRuntimeSecretCandidate(live map[secretbackend.Reference]struct{}, workspaceID string, variable Variable) error {
	base := secretbackend.Reference{
		WorkspaceID: contract.NormalizeWorkspace(workspaceID),
		Kind:        "variable-app",
		Path:        strings.Trim(variable.AppKey, "/") + "/" + strings.Trim(variable.Path, "/"),
	}
	reference, candidate, err := secretbackend.RuntimeCandidateReference(base, variable.Value)
	if err != nil {
		return err
	}
	if candidate {
		live[reference] = struct{}{}
	}
	return nil
}

func sortedRuntimeSecretCandidateReferences(live map[secretbackend.Reference]struct{}) []secretbackend.Reference {
	references := make([]secretbackend.Reference, 0, len(live))
	for reference := range live {
		references = append(references, reference)
	}
	sort.Slice(references, func(i, j int) bool {
		if references[i].WorkspaceID != references[j].WorkspaceID {
			return references[i].WorkspaceID < references[j].WorkspaceID
		}
		if references[i].Kind != references[j].Kind {
			return references[i].Kind < references[j].Kind
		}
		return references[i].Path < references[j].Path
	})
	return references
}

var _ secretbackend.RuntimeCandidateLiveReferenceSource = (*LocalStore)(nil)
var _ secretbackend.RuntimeCandidateLiveReferenceSource = (*PostgresStore)(nil)
