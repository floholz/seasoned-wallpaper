// Package pidfile creates, locks, and releases a daemon pidfile. Locking
// is advisory via flock (POSIX) / LockFileEx (Windows) on the pidfile
// itself — presence alone is unreliable because a crashed daemon leaves
// its pidfile behind.
package pidfile

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// ErrLocked is returned when the pidfile is held by another live process.
var ErrLocked = errors.New("pidfile: already locked by a live process")

// File represents a held pidfile lock. Release must be called on shutdown;
// crashes leave the file on disk, and the next startup retries the lock.
type File struct {
	path string
	f    *os.File
}

// Path returns the default pidfile path for the current OS.
//
//   - Linux/macOS: $XDG_RUNTIME_DIR/seasoned.pid, falling back to
//     /tmp/seasoned-<uid>.pid
//   - Windows:     %LOCALAPPDATA%\seasoned\seasoned.pid
func Path() (string, error) {
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "seasoned", "seasoned.pid"), nil
		}
		return "", errors.New("pidfile: %LOCALAPPDATA% not set")
	}
	if rt := os.Getenv("XDG_RUNTIME_DIR"); rt != "" {
		return filepath.Join(rt, "seasoned.pid"), nil
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("seasoned-%d.pid", os.Getuid())), nil
}

// Acquire opens the pidfile, flocks it, and writes the current PID. If
// another live process already holds the lock, Acquire returns ErrLocked.
// Stale pidfiles (dead PID) are reclaimed.
func Acquire(path string) (*File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("pidfile: mkdir %s: %w", filepath.Dir(path), err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("pidfile: open %s: %w", path, err)
	}
	if err := lockExclusive(f); err != nil {
		f.Close()
		if errors.Is(err, ErrLocked) {
			return nil, ErrLocked
		}
		return nil, fmt.Errorf("pidfile: lock %s: %w", path, err)
	}
	if err := f.Truncate(0); err != nil {
		unlock(f)
		f.Close()
		return nil, fmt.Errorf("pidfile: truncate: %w", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		unlock(f)
		f.Close()
		return nil, fmt.Errorf("pidfile: seek: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%d\n", os.Getpid()); err != nil {
		unlock(f)
		f.Close()
		return nil, fmt.Errorf("pidfile: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		unlock(f)
		f.Close()
		return nil, fmt.Errorf("pidfile: sync: %w", err)
	}
	return &File{path: path, f: f}, nil
}

// Release removes the pidfile and unlocks it.
func (p *File) Release() error {
	if p == nil || p.f == nil {
		return nil
	}
	_ = unlock(p.f)
	_ = p.f.Close()
	p.f = nil
	if err := os.Remove(p.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("pidfile: remove %s: %w", p.path, err)
	}
	return nil
}

// ReadPID reads the PID recorded in the pidfile at path. Returns (0, err)
// when the file is missing or malformed.
func ReadPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(data))
	if s == "" {
		return 0, fmt.Errorf("pidfile: empty pidfile at %s", path)
	}
	pid, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("pidfile: parse %s: %w", path, err)
	}
	return pid, nil
}
