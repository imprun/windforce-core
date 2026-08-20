package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestReplaceFileRetriesPermissionErrors(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "snapshot.tmp")
	destination := filepath.Join(directory, "snapshot.json")
	if err := os.WriteFile(source, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}

	attempts := 0
	var delays []time.Duration
	err := replaceFileWith(source, destination, func(source, destination string) error {
		attempts++
		if attempts < 3 {
			return &os.PathError{Op: "rename", Path: source, Err: fs.ErrPermission}
		}
		return os.Rename(source, destination)
	}, func(delay time.Duration) {
		delays = append(delays, delay)
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("rename attempts = %d, want 3", attempts)
	}
	wantDelays := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}
	if len(delays) != len(wantDelays) {
		t.Fatalf("retry delays = %v, want %v", delays, wantDelays)
	}
	for index := range wantDelays {
		if delays[index] != wantDelays[index] {
			t.Fatalf("retry delays = %v, want %v", delays, wantDelays)
		}
	}
	contents, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "current" {
		t.Fatalf("destination = %q, want %q", contents, "current")
	}
}

func TestReplaceFileReportsNonPermissionErrorWithoutRetrying(t *testing.T) {
	attempts := 0
	err := replaceFileWith("absent", "snapshot.json", func(_, _ string) error {
		attempts++
		return fs.ErrNotExist
	}, func(time.Duration) {
		t.Fatal("non-permission error was retried")
	})
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("replace error = %v, want not-exist", err)
	}
	if attempts != 1 {
		t.Fatalf("rename attempts = %d, want 1", attempts)
	}
}

func TestReplaceFileStopsAfterPermissionRetryBudget(t *testing.T) {
	attempts := 0
	var delays []time.Duration
	err := replaceFileWith("snapshot.tmp", "snapshot.json", func(_, _ string) error {
		attempts++
		return fs.ErrPermission
	}, func(delay time.Duration) {
		delays = append(delays, delay)
	})
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("replace error = %v, want permission", err)
	}
	if attempts != 10 {
		t.Fatalf("rename attempts = %d, want 10", attempts)
	}
	if len(delays) != 9 {
		t.Fatalf("retry delays = %v, want 9 delays", delays)
	}
	if delays[len(delays)-1] != 90*time.Millisecond {
		t.Fatalf("last retry delay = %v, want %v", delays[len(delays)-1], 90*time.Millisecond)
	}
}
