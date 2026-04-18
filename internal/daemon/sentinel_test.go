package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestControlDir_ReturnsSomething(t *testing.T) {
	// Best-effort smoke test: should not error on a normal dev environment.
	got, err := ControlDir()
	if err != nil {
		t.Fatalf("ControlDir: %v", err)
	}
	if got == "" {
		t.Error("ControlDir returned empty path")
	}
}

func TestTouchControl_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	if err := TouchControl(dir, "reload"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reload")); err != nil {
		t.Fatalf("sentinel not created: %v", err)
	}
}

func TestSentinelWatcher_ReloadAndKick(t *testing.T) {
	dir := t.TempDir()
	d := &Daemon{
		reloadCh: make(chan struct{}, 1),
		kickCh:   make(chan struct{}, 1),
		wakeCh:   make(chan struct{}, 1),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stopped := make(chan struct{})
	go func() {
		_ = d.watchSentinel(ctx, dir)
		close(stopped)
	}()

	// Give the watcher a moment to attach.
	time.Sleep(50 * time.Millisecond)

	if err := TouchControl(dir, "reload"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.reloadCh:
	case <-time.After(time.Second):
		t.Fatal("reload sentinel did not fire")
	}
	// File should be deleted by the handler.
	if _, err := os.Stat(filepath.Join(dir, "reload")); !os.IsNotExist(err) {
		t.Errorf("sentinel file should be deleted after handling: %v", err)
	}

	if err := TouchControl(dir, "kick"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.kickCh:
	case <-time.After(time.Second):
		t.Fatal("kick sentinel did not fire")
	}

	// Unknown sentinel name is ignored.
	if err := TouchControl(dir, "unknown"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-d.reloadCh:
		t.Error("unknown sentinel should not trigger reload")
	case <-d.kickCh:
		t.Error("unknown sentinel should not trigger kick")
	case <-time.After(100 * time.Millisecond):
		// Expected: nothing fires.
	}

	cancel()
	<-stopped
}

func TestSentinelWatcher_SweepAtStartup(t *testing.T) {
	// File dropped before the watcher starts must still be picked up.
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "reload"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Daemon{
		reloadCh: make(chan struct{}, 1),
		kickCh:   make(chan struct{}, 1),
		wakeCh:   make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.watchSentinel(ctx, dir)

	select {
	case <-d.reloadCh:
	case <-time.After(time.Second):
		t.Fatal("pre-existing sentinel not swept")
	}
}
