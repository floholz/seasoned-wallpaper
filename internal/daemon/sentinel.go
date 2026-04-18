package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/fsnotify/fsnotify"
)

// ControlDir returns the default control directory used for sentinel-file
// signalling. Windows always uses this mechanism; POSIX uses it only when
// the config enables sentinel_fallback.
//
//   - Linux/macOS: $XDG_RUNTIME_DIR/seasoned/control, fallback
//     /tmp/seasoned-<uid>/control
//   - Windows:     %LOCALAPPDATA%\seasoned\control
func ControlDir() (string, error) {
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "seasoned", "control"), nil
		}
		return "", errors.New("sentinel: %LOCALAPPDATA% not set")
	}
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		return filepath.Join(rt, "seasoned", "control"), nil
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("seasoned-%d", os.Getuid()), "control"), nil
}

// TouchControl creates an empty sentinel file named kind ("reload" or
// "kick") in the control directory. Used by the CLI subcommands to signal
// a running daemon on systems where signals aren't an option.
func TouchControl(dir, kind string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("sentinel: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, kind)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("sentinel: create %s: %w", path, err)
	}
	return f.Close()
}

// watchSentinel polls the control directory for sentinel files named
// "reload" or "kick", invokes the corresponding daemon action, and deletes
// the file. Runs until ctx is cancelled.
func (d *Daemon) watchSentinel(ctx context.Context, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("sentinel: mkdir %s: %w", dir, err)
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if err := w.Add(dir); err != nil {
		return err
	}
	slog.Debug("sentinel watch started", "dir", dir)

	// Sweep once at startup in case a file was dropped before we subscribed.
	d.sweepControl(dir)

	for {
		select {
		case <-ctx.Done():
			return nil

		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write) == 0 {
				continue
			}
			d.handleControl(ev.Name)

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			slog.Warn("sentinel watcher error", "error", err)
		}
	}
}

func (d *Daemon) sweepControl(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		d.handleControl(filepath.Join(dir, e.Name()))
	}
}

func (d *Daemon) handleControl(path string) {
	name := filepath.Base(path)
	switch name {
	case "reload":
		slog.Info("sentinel: reload")
		d.Reload()
	case "kick":
		slog.Info("sentinel: kick")
		d.Kick()
	default:
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("sentinel: remove failed", "path", path, "error", err)
	}
}
