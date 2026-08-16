package state

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

func (s *LocalStore) ListResources(ctx context.Context, workspaceID string) ([]Resource, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]Resource, 0, len(snapshot.Resources[contract.NormalizeWorkspace(workspaceID)]))
	for _, resource := range snapshot.Resources[contract.NormalizeWorkspace(workspaceID)] {
		resource.Value = cloneRaw(resource.Value)
		items = append(items, resource)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items, nil
}

func (s *LocalStore) DeleteResource(ctx context.Context, workspaceID string, path string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	path = strings.TrimSpace(path)
	return s.update(ctx, func(snapshot *Snapshot, _ time.Time) error {
		if _, found := snapshot.Resources[workspaceID][resourceKey("", path)]; !found {
			return ErrNotFound
		}
		delete(snapshot.Resources[workspaceID], resourceKey("", path))
		return nil
	})
}
