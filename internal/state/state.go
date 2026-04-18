// Package state manages the JSON state file. Corruption is non-fatal —
// callers treat a zero value as a valid empty state.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
)

// State mirrors the JSON on disk. Keep struct tags stable — this is a
// user-visible format.
type State struct {
	LastAppliedDate   string              `json:"last_applied_date,omitempty"`
	LastAppliedPath   string              `json:"last_applied_path,omitempty"`
	LastAppliedSeason string              `json:"last_applied_season,omitempty"`
	RecentBySource    map[string][]string `json:"recent_by_source,omitempty"`
}

// Recent returns the recent-pick list for a given source folder.
func (s *State) Recent(source string) []string {
	if s == nil || s.RecentBySource == nil {
		return nil
	}
	return s.RecentBySource[source]
}

// SetRecent stores the recent-pick list for a source folder.
func (s *State) SetRecent(source string, recent []string) {
	if s.RecentBySource == nil {
		s.RecentBySource = map[string][]string{}
	}
	s.RecentBySource[source] = recent
}

// Load reads and parses the state file. A missing file yields an empty
// state with no error. A corrupt file is logged and also yields an empty
// state with no error, per SPEC.md.
func Load(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return &State{}, nil
		}
		return nil, fmt.Errorf("state: read %s: %w", path, err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		slog.Warn("state file corrupt; resetting", "path", path, "error", err)
		return &State{}, nil
	}
	return &s, nil
}

// Save writes the state file atomically (tempfile + rename).
func Save(path string, s *State) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("state: mkdir %s: %w", filepath.Dir(path), err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("state: marshal: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state.*.tmp")
	if err != nil {
		return fmt.Errorf("state: create tempfile: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op on success after rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("state: write tempfile: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("state: chmod tempfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("state: close tempfile: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("state: rename tempfile to %s: %w", path, err)
	}
	return nil
}

// DefaultPath returns the standard state file location for the current OS.
func DefaultPath() (string, error) {
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "seasoned", "state.json"), nil
		}
		return "", errors.New("state: %LOCALAPPDATA% not set")
	}
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "seasoned", "state.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("state: cannot resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "seasoned", "state.json"), nil
}
