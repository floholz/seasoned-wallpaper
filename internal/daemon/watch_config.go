package daemon

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchConfig runs a goroutine that tails the config file and nudges
// d.Reload() on every meaningful change. It handles editor atomic-save
// patterns (write to tmp + rename) by re-establishing the watch on the
// parent directory and filtering by basename.
//
// Returns once ctx is cancelled, closing the watcher.
func (d *Daemon) watchConfig(ctx context.Context) error {
	if d.cfgPath == "" {
		slog.Warn("config watch disabled: no config path")
		return nil
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()

	// Watch the directory and filter by basename. A bare file-watch breaks
	// when editors rename-into-place because the inode is replaced.
	dir := filepath.Dir(d.cfgPath)
	base := filepath.Base(d.cfgPath)
	if err := w.Add(dir); err != nil {
		return err
	}
	slog.Debug("config watch started", "dir", dir, "file", base)

	// Coalesce: editors often emit a burst (write, chmod, rename). Wait a
	// short quiet period after the last event before firing.
	const debounce = 250 * time.Millisecond
	var timer *time.Timer
	fire := func() {
		if timer != nil {
			timer.Stop()
		}
		timer = time.AfterFunc(debounce, d.Reload)
	}

	for {
		select {
		case <-ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return nil

		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if filepath.Base(ev.Name) != base {
				continue
			}
			// Write, Create, Rename, Remove all mean "contents may have changed".
			// Chmod alone is not interesting.
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}
			slog.Debug("config event", "op", ev.Op.String(), "path", ev.Name)
			fire()

		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			slog.Warn("config watcher error", "error", err)
		}
	}
}
