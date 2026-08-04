package bundle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

// Reference identifies one immutable source snapshot.
type Reference struct {
	Workspace   string
	GitSourceID string
	Commit      string
}

// PruneOptions controls one source snapshot retention sweep.
type PruneOptions struct {
	Referenced []Reference
	Before     time.Time
	DryRun     bool
}

// PruneResult reports what one retention sweep observed and changed.
type PruneResult struct {
	Discovered int
	Referenced int
	Recent     int
	Eligible   int
	Removed    int
	Invalid    int
}

type marker struct {
	CompletedAt time.Time `json:"completedAt"`
	Commit      string    `json:"commit"`
	FileCount   int       `json:"fileCount"`
	GitSourceID string    `json:"gitSourceId"`
	Workspace   string    `json:"workspace"`
}

// PruneUnreferenced removes completed source snapshots that are not referenced
// by release state and that were materialized before the supplied cutoff.
// Invalid or incomplete snapshots are retained for operator inspection.
func (s *LocalStore) PruneUnreferenced(ctx context.Context, options PruneOptions) (PruneResult, error) {
	var result PruneResult
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if options.Before.IsZero() {
		return result, errors.New("source bundle prune cutoff is required")
	}

	referenced := make(map[string]struct{}, len(options.Referenced))
	for _, reference := range options.Referenced {
		if key, ok := referenceKey(reference); ok {
			referenced[key] = struct{}{}
		}
	}

	root := filepath.Join(s.Root, "gitrepos")
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) && path == root {
				return nil
			}
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != markerFile {
			return nil
		}

		result.Discovered++
		metadata, err := readMarker(path)
		if err != nil {
			result.Invalid++
			return nil
		}
		reference := Reference{
			Workspace:   metadata.Workspace,
			GitSourceID: metadata.GitSourceID,
			Commit:      metadata.Commit,
		}
		key, ok := referenceKey(reference)
		if !ok || filepath.Clean(filepath.Dir(path)) != filepath.Clean(s.bundleDir(reference.Workspace, reference.GitSourceID, reference.Commit)) {
			result.Invalid++
			return nil
		}
		if _, ok := referenced[key]; ok {
			result.Referenced++
			return nil
		}
		if !metadata.CompletedAt.Before(options.Before) {
			result.Recent++
			return nil
		}

		result.Eligible++
		if options.DryRun {
			return nil
		}
		if err := os.RemoveAll(filepath.Dir(path)); err != nil {
			return fmt.Errorf("remove source bundle %s: %w", key, err)
		}
		result.Removed++
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	return result, err
}

func referenceKey(reference Reference) (string, bool) {
	workspace := contract.NormalizeWorkspace(reference.Workspace)
	gitSourceID := contract.NormalizeGitSourceID(reference.GitSourceID, "")
	commit := strings.TrimSpace(reference.Commit)
	if workspace == "" || gitSourceID == "" || commit == "" {
		return "", false
	}
	return workspace + "\x00" + gitSourceID + "\x00" + commit, true
}

func readMarker(path string) (marker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return marker{}, err
	}
	var metadata marker
	if err := json.Unmarshal(data, &metadata); err != nil {
		return marker{}, err
	}
	if metadata.CompletedAt.IsZero() {
		return marker{}, errors.New("source bundle marker is missing completedAt")
	}
	return metadata, nil
}
