package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCanonicalReadme(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		path, markdown, available, err := readCanonicalReadme(t.TempDir())
		if err != nil || available || path != "" || markdown != "" {
			t.Fatalf("readme = %q %q %v %v", path, markdown, available, err)
		}
	})

	t.Run("uppercase extension", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "README.MD"), []byte("# Guide\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		path, markdown, available, err := readCanonicalReadme(root)
		if err != nil || !available || path != "README.md" || markdown != "# Guide\n" {
			t.Fatalf("readme = %q %q %v %v", path, markdown, available, err)
		}
	})

	t.Run("invalid UTF-8", func(t *testing.T) {
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "README.md"), []byte{0xff}, 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, _, err := readCanonicalReadme(root)
		if err == nil || !strings.Contains(err.Error(), "UTF-8") {
			t.Fatalf("error = %v", err)
		}
	})
}
