package pidfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAcquire_Release(t *testing.T) {
	p := filepath.Join(t.TempDir(), "test.pid")
	f, err := Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := ReadPID(p)
	if err != nil {
		t.Fatal(err)
	}
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
	if err := f.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("pidfile should be gone: %v", err)
	}
}

func TestAcquire_DoubleLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("windows locking semantics differ; covered in integration")
	}
	p := filepath.Join(t.TempDir(), "test.pid")
	f1, err := Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f1.Release()

	_, err = Acquire(p)
	if !errors.Is(err, ErrLocked) {
		t.Fatalf("second Acquire: err = %v, want ErrLocked", err)
	}
}

func TestAcquire_StalePidfile(t *testing.T) {
	// Stale file with an old PID should be reclaimable.
	p := filepath.Join(t.TempDir(), "test.pid")
	if err := os.WriteFile(p, []byte("99999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Acquire(p)
	if err != nil {
		t.Fatalf("expected to reclaim stale pidfile, got %v", err)
	}
	defer f.Release()
	pid, _ := ReadPID(p)
	if pid != os.Getpid() {
		t.Errorf("pid = %d, want %d", pid, os.Getpid())
	}
}
