package state

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	"github.com/imprun/windforce-core/internal/resourceconfig"
)

func (s *LocalStore) ListResourceTypes(ctx context.Context, workspaceID string) ([]ResourceType, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	items := make([]ResourceType, 0, len(snapshot.ResourceTypes[workspaceID]))
	for _, resourceType := range snapshot.ResourceTypes[workspaceID] {
		resourceType.Schema = cloneRaw(resourceType.Schema)
		items = append(items, resourceType)
	}
	sortResourceTypes(items)
	return items, nil
}

func (s *LocalStore) SetResourceType(ctx context.Context, workspaceID string, resourceType ResourceType) error {
	resourceType.Name = strings.TrimSpace(resourceType.Name)
	resourceType.Version = normalizeResourceTypeVersion(resourceType.Version)
	resourceType.Schema = canonicalJSONValue(resourceType.Schema, "{}")
	if resourceType.Name == "" || !json.Valid(resourceType.Schema) ||
		resourceconfig.ValidateSchema(resourceType.Schema) != nil {
		return ErrInvalidState
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	return s.update(ctx, func(snapshot *Snapshot, _ time.Time) error {
		if snapshot.ResourceTypes[workspaceID] == nil {
			snapshot.ResourceTypes[workspaceID] = map[string]ResourceType{}
		}
		resourceType.Schema = cloneRaw(resourceType.Schema)
		snapshot.ResourceTypes[workspaceID][resourceTypeKey(resourceType.Name, resourceType.Version)] = resourceType
		return nil
	})
}

func (s *LocalStore) GetResourceType(ctx context.Context, workspaceID string, name string, version string) (ResourceType, bool, error) {
	snapshot, err := s.Load(ctx)
	if err != nil {
		return ResourceType{}, false, err
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	resourceType, ok := snapshot.ResourceTypes[workspaceID][resourceTypeKey(name, normalizeResourceTypeVersion(version))]
	resourceType.Schema = cloneRaw(resourceType.Schema)
	return resourceType, ok, nil
}

func (s *LocalStore) DeleteResourceType(ctx context.Context, workspaceID string, name string, version string) error {
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	name = strings.TrimSpace(name)
	version = normalizeResourceTypeVersion(version)
	return s.update(ctx, func(snapshot *Snapshot, _ time.Time) error {
		key := resourceTypeKey(name, version)
		if _, found := snapshot.ResourceTypes[workspaceID][key]; !found {
			return ErrNotFound
		}
		for _, resource := range snapshot.Resources[workspaceID] {
			resourceName, resourceVersion, err := resourceconfig.ParseTypeReference(resource.ResourceType)
			if err != nil {
				return err
			}
			if resourceName == name && resourceVersion == version {
				return ErrConflict
			}
		}
		delete(snapshot.ResourceTypes[workspaceID], key)
		return nil
	})
}
