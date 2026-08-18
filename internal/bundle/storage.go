package bundle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
)

const markerFile = ".windforce_clone_complete"

// Store keeps source-only bundles addressed by workspace, git source, and commit.
type Store interface {
	Exists(ctx context.Context, workspace string, gitSourceID string, commit string) (bool, error)
	Materialize(ctx context.Context, workspace string, gitSourceID string, commit string, sourceDir string) error
	FetchTo(ctx context.Context, destinationDir string, workspace string, gitSourceID string, commit string) error
}

// Verifier is the optional read-only source marker capability used by
// operational diagnostics. It does not materialize or copy source data.
type Verifier interface {
	Verify(ctx context.Context, workspace string, gitSourceID string, commit string) error
}

// LocalStore is the development and test implementation of Store.
type LocalStore struct {
	Root string
}

func NewLocalStore(root string) *LocalStore {
	return &LocalStore{Root: root}
}

func (s *LocalStore) bundleDir(workspace string, gitSourceID string, commit string) string {
	return filepath.Join(s.Root, "gitrepos", safeSegment(contract.NormalizeWorkspace(workspace)), safeSegment(contract.NormalizeGitSourceID(gitSourceID, "")), safeSegment(commit))
}

func (s *LocalStore) Exists(_ context.Context, workspace string, gitSourceID string, commit string) (bool, error) {
	_, err := os.Stat(filepath.Join(s.bundleDir(workspace, gitSourceID, commit), markerFile))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (s *LocalStore) Verify(ctx context.Context, workspace string, gitSourceID string, commit string) error {
	dir := s.bundleDir(workspace, gitSourceID, commit)
	markerPath := filepath.Join(dir, markerFile)
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return fmt.Errorf("read source marker: %w", err)
	}
	var stored marker
	if err := json.Unmarshal(data, &stored); err != nil {
		return errors.New("source marker is not valid JSON")
	}
	expectedWorkspace := contract.NormalizeWorkspace(workspace)
	expectedSource := contract.NormalizeGitSourceID(gitSourceID, "")
	if stored.CompletedAt.IsZero() || stored.Workspace != expectedWorkspace || stored.GitSourceID != expectedSource || stored.Commit != commit {
		return errors.New("source marker identity does not match its address")
	}
	files := 0
	if err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() && filepath.Clean(path) != filepath.Clean(markerPath) {
			files++
		}
		return nil
	}); err != nil {
		return fmt.Errorf("inspect source snapshot: %w", err)
	}
	if stored.FileCount != files {
		return fmt.Errorf("source marker file count mismatch: got %d, want %d", files, stored.FileCount)
	}
	return nil
}

func (s *LocalStore) Materialize(ctx context.Context, workspace string, gitSourceID string, commit string, sourceDir string) error {
	workspace = contract.NormalizeWorkspace(workspace)
	gitSourceID = contract.NormalizeGitSourceID(gitSourceID, "")
	exists, err := s.Exists(ctx, workspace, gitSourceID, commit)
	if err != nil || exists {
		return err
	}

	targetDir := s.bundleDir(workspace, gitSourceID, commit)
	if err := os.RemoveAll(targetDir); err != nil {
		return err
	}
	if err := copyTree(ctx, sourceDir, targetDir); err != nil {
		_ = os.RemoveAll(targetDir)
		return err
	}
	return writeMarker(filepath.Join(targetDir, markerFile), markerPayload(workspace, gitSourceID, commit, targetDir))
}

func (s *LocalStore) FetchTo(ctx context.Context, destinationDir string, workspace string, gitSourceID string, commit string) error {
	exists, err := s.Exists(ctx, workspace, gitSourceID, commit)
	if err != nil {
		return err
	}
	if !exists {
		return os.ErrNotExist
	}
	if err := os.RemoveAll(destinationDir); err != nil {
		return err
	}
	return copyTree(ctx, s.bundleDir(workspace, gitSourceID, commit), destinationDir)
}

func copyTree(ctx context.Context, src string, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}

		base := filepath.Base(rel)
		if entry.IsDir() {
			if skipSourceDir(base) {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		if base == markerFile {
			return nil
		}
		return copyFile(path, filepath.Join(dst, rel))
	})
}

func skipSourceDir(base string) bool {
	switch base {
	case ".git", "node_modules":
		return true
	default:
		return false
	}
}

func copyFile(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func markerPayload(workspace string, gitSourceID string, commit string, dir string) []byte {
	payload, _ := json.Marshal(marker{
		CompletedAt: time.Now().UTC(),
		Commit:      commit,
		FileCount:   countFiles(dir),
		GitSourceID: gitSourceID,
		Workspace:   workspace,
	})
	return payload
}

func writeMarker(path string, payload []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func countFiles(dir string) int {
	count := 0
	_ = filepath.WalkDir(dir, func(_ string, entry os.DirEntry, err error) error {
		if err == nil && !entry.IsDir() {
			count++
		}
		return nil
	})
	return count
}

func safeSegment(value string) string {
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.', r == '-', r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	if builder.Len() == 0 {
		return "_"
	}
	out := builder.String()
	if strings.Trim(out, ".") == "" {
		return "_"
	}
	return out
}
