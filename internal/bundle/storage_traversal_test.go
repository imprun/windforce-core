package bundle

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestSafeSegmentNeutralizesDotDot locks the fix for a 2nd-pass security finding
// (N-1): safeSegment allowlisted '.', so ".." passed through and bundleDir escaped
// its Root — and Materialize does os.RemoveAll(targetDir), turning a ".."
// workspace/gitSourceID into arbitrary directory deletion. safeSegment now
// neutralizes all-dots segments; RED before the fix, GREEN after.
func TestSafeSegmentNeutralizesDotDot(t *testing.T) {
	if got := safeSegment(".."); got == ".." {
		t.Fatalf("safeSegment(%q) = %q; expected traversal token to be neutralized", "..", got)
	}
}

// TestBundleDirStaysUnderRoot proves the traversal is reachable end-to-end: with a
// ".." workspace and ".." gitSourceID the computed bundle path escapes Root.
func TestBundleDirStaysUnderRoot(t *testing.T) {
	root := "/srv/windforce/bundles"
	s := NewLocalStore(root)
	dir := s.bundleDir("..", "..", "deadbeef")
	clean := filepath.Clean(dir)
	if !strings.HasPrefix(clean, filepath.Clean(root)+string(filepath.Separator)) && clean != filepath.Clean(root) {
		t.Fatalf("bundleDir escaped Root: %q is outside %q", clean, root)
	}
}
