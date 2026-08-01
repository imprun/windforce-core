package state

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

const localStoreLockHelper = "WINDFORCE_LOCAL_STORE_LOCK_HELPER"

func TestLocalStoreWithLockAcceptsLegacySentinel(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	lockPath := statePath + ".lock"
	if err := os.WriteFile(lockPath, []byte("123\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-3 * time.Minute)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	called := false
	if err := NewLocalStore(statePath).withLock(context.Background(), func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("critical section was not called")
	}
}

func TestLocalStoreWithLockTimesOutWhileOwned(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "state.json")
	owner := flock.New(statePath+".lock", flock.SetPermissions(0o600))
	locked, err := owner.TryLock()
	if err != nil {
		t.Fatal(err)
	}
	if !locked {
		t.Fatal("owner did not acquire lock")
	}
	t.Cleanup(func() { _ = owner.Unlock() })

	err = NewLocalStore(statePath).withLockTimeout(context.Background(), 100*time.Millisecond, func() error {
		t.Fatal("critical section ran while another owner held the lock")
		return nil
	})
	if !errors.Is(err, ErrLockTimeout) {
		t.Fatalf("withLockTimeout() error = %v, want %v", err, ErrLockTimeout)
	}
}

func TestLocalStoreWithLockRecoversAfterOwnerProcessExit(t *testing.T) {
	if os.Getenv(localStoreLockHelper) == "1" {
		statePath := os.Getenv(localStoreLockHelper + "_STATE")
		readyPath := os.Getenv(localStoreLockHelper + "_READY")
		err := NewLocalStore(statePath).withLock(context.Background(), func() error {
			if err := os.WriteFile(readyPath, []byte("ready\n"), 0o600); err != nil {
				return err
			}
			for {
				time.Sleep(time.Hour)
			}
		})
		if err != nil {
			t.Fatal(err)
		}
		return
	}

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	readyPath := filepath.Join(dir, "ready")
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(testBinary, "-test.run=^TestLocalStoreWithLockRecoversAfterOwnerProcessExit$")
	command.Env = append(os.Environ(),
		localStoreLockHelper+"=1",
		localStoreLockHelper+"_STATE="+statePath,
		localStoreLockHelper+"_READY="+readyPath,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if time.Now().After(deadline) {
			t.Fatal("helper process did not acquire lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err == nil {
		t.Fatal("killed helper process exited successfully")
	}

	acquired := false
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := NewLocalStore(statePath).withLock(ctx, func() error {
		acquired = true
		return nil
	}); err != nil {
		t.Fatalf("acquire after owner exit: %v", err)
	}
	if !acquired {
		t.Fatal("critical section was not called after owner exit")
	}
}
