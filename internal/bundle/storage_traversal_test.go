package bundle

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeSegmentNeutralizesTraversalTokens(t *testing.T) {
	for _, value := range []string{".", "..", "..."} {
		if got := safeSegment(value); got != "_" {
			t.Fatalf("safeSegment(%q) = %q, want %q", value, got, "_")
		}
	}
}

func TestBundleDirStaysUnderRoot(t *testing.T) {
	root := filepath.Clean(filepath.Join(t.TempDir(), "bundles"))
	store := NewLocalStore(root)
	dir := filepath.Clean(store.bundleDir("..", "..", "deadbeef"))

	if dir != root && !strings.HasPrefix(dir, root+string(filepath.Separator)) {
		t.Fatalf("bundleDir escaped root: %q is outside %q", dir, root)
	}
}
