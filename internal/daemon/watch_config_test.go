package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatchConfig_FiresOnWrite(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("wallpaper_dir: /tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Daemon{
		cfgPath:  cfgPath,
		reloadCh: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.watchConfig(ctx)

	// Let fsnotify attach.
	time.Sleep(50 * time.Millisecond)

	if err := os.WriteFile(cfgPath, []byte("wallpaper_dir: /tmp2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Debounce is 250ms — give it 2s.
	select {
	case <-d.reloadCh:
	case <-time.After(2 * time.Second):
		t.Fatal("config write did not trigger reload")
	}
}

func TestWatchConfig_IgnoresSiblingFiles(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("x: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Daemon{
		cfgPath:  cfgPath,
		reloadCh: make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.watchConfig(ctx)

	time.Sleep(50 * time.Millisecond)

	// Write a sibling — must NOT trigger reload.
	if err := os.WriteFile(filepath.Join(dir, "other.yaml"), []byte("x: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-d.reloadCh:
		t.Fatal("sibling file change should not trigger reload")
	case <-time.After(500 * time.Millisecond):
		// Expected.
	}
}
