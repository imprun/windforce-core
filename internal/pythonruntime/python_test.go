package pythonruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindWindowsExecutableSkipsWindowsAppsAlias(t *testing.T) {
	root := t.TempDir()
	aliasDir := filepath.Join(root, "Microsoft", "WindowsApps")
	realDir := filepath.Join(root, "Python", "bin")
	for _, dir := range []string{aliasDir, realDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "python.exe"), []byte("test"), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	got := FindWindowsExecutable(strings.Join([]string{aliasDir, realDir}, string(os.PathListSeparator)))
	want := filepath.Join(realDir, "python.exe")
	if got != want {
		t.Fatalf("FindWindowsExecutable() = %q, want %q", got, want)
	}
}

func TestFindWindowsExecutableSkipsEmptyAndQuotedEntries(t *testing.T) {
	realDir := filepath.Join(t.TempDir(), "Python", "bin")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(realDir, "python3.exe"), []byte("test"), 0o755); err != nil {
		t.Fatal(err)
	}

	pathValue := strings.Join([]string{"", `"` + realDir + `"`}, string(os.PathListSeparator))
	got := FindWindowsExecutable(pathValue)
	want := filepath.Join(realDir, "python3.exe")
	if got != want {
		t.Fatalf("FindWindowsExecutable() = %q, want %q", got, want)
	}
}

func TestIsWindowsAppsAliasUsesPathBoundaries(t *testing.T) {
	if !IsWindowsAppsAlias(filepath.Join("C:\\", "Users", "test", "Microsoft", "WindowsApps", "python.exe")) {
		t.Fatal("IsWindowsAppsAlias() = false, want true")
	}
	if IsWindowsAppsAlias(filepath.Join("C:\\", "tools", "WindowsAppsBackup", "python.exe")) {
		t.Fatal("IsWindowsAppsAlias() = true for an unrelated directory")
	}
}
