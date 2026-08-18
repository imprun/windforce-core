package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReplaceFileOverwritesAnExistingDestination(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "snapshot.tmp")
	destination := filepath.Join(directory, "snapshot.json")
	if err := os.WriteFile(destination, []byte("previous"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := replaceFile(source, destination); err != nil {
		t.Fatal(err)
	}

	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "current" {
		t.Fatalf("destination = %q, want %q", contents, "current")
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatal("the staged file was left behind")
	}
}

func TestReplaceFileReportsAMissingSourceWithoutRetrying(t *testing.T) {
	directory := t.TempDir()
	err := replaceFile(filepath.Join(directory, "absent"), filepath.Join(directory, "snapshot.json"))
	if err == nil {
		t.Fatal("a missing staged file was accepted")
	}
}
