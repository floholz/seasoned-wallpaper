// Package config loads, expands, and validates the YAML configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/floholz/seasoned-wallpaper/internal/season"
)

// DefaultExtensions is the fallback extension set when the user omits it.
var DefaultExtensions = []string{"jpg", "jpeg", "png", "webp"}

// DefaultRefreshInterval is the daemon's safety-net forced re-evaluation
// cadence when the user doesn't override it.
const DefaultRefreshInterval = 6 * time.Hour

// DefaultRotationAnchor is the default clock time at which a rotation
// fires: 03:00 local. Expressed as an offset from local midnight so the
// scheduler can do duration arithmetic without re-parsing.
const DefaultRotationAnchor = 3 * time.Hour

// MinRotationInterval is the smallest interval the daemon accepts.
// Sub-minute scheduling isn't meaningful for a wallpaper — clock
// resolution in configs is HH:MM, and picking a new image every few
// seconds would just thrash the display.
const MinRotationInterval = time.Minute

// Config is the parsed, validated, path-expanded configuration.
type Config struct {
	Source string // path the config was loaded from

	WallpaperDir string
	Recursive    bool
	Extensions   []string

	LinuxCommand string // optional; {{.Path}} is substituted at apply time

	Seasons []season.Spec

	Daemon DaemonConfig
}

// DaemonConfig holds the v2 daemon block. Zero value is safe: defaults are
// applied during Load.
type DaemonConfig struct {
	RefreshInterval  time.Duration
	WatchConfig      bool
	DBusSleepWake    bool
	SentinelFallback bool

	// RotationAt is the list of clock-time offsets from local midnight at
	// which a rotation fires. In list mode (RotationInterval == 0), each
	// entry fires independently. In interval mode, the first entry is the
	// anchor and all others are rejected at validation time. Always has
	// at least one entry after Load.
	RotationAt []time.Duration

	// RotationInterval is the spacing between rotations anchored at
	// RotationAt[0]. Zero means list mode. When non-zero, it is at least
	// MinRotationInterval.
	RotationInterval time.Duration
}

// rawConfig mirrors the YAML schema; kept internal so the public Config stays
// decoupled from YAML tags.
type rawConfig struct {
	WallpaperDir string   `yaml:"wallpaper_dir"`
	Recursive    bool     `yaml:"recursive"`
	Extensions   []string `yaml:"extensions"`

	Linux *struct {
		Command string `yaml:"command"`
	} `yaml:"linux"`

	Seasons []rawSeason `yaml:"seasons"`

	Daemon *rawDaemon `yaml:"daemon"`
}

type rawDaemon struct {
	RefreshInterval  string    `yaml:"refresh_interval"`
	RotationAt       yaml.Node `yaml:"rotation_at"` // scalar or sequence
	RotationInterval string    `yaml:"rotation_interval"`
	WatchConfig      *bool     `yaml:"watch_config"`
	DBusSleepWake    *bool     `yaml:"dbus_sleep_wake"`
	SentinelFallback *bool     `yaml:"sentinel_fallback"`
}

type rawSeason struct {
	Name      string `yaml:"name"`
	Date      string `yaml:"date"`
	DateRange string `yaml:"date_range"`
	Path      string `yaml:"path"`
}

// Load reads the YAML at path, expands paths, and validates the result.
// If path is empty, it falls back to the default search order.
func Load(path string) (*Config, error) {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	var raw rawConfig
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	cfg := &Config{Source: path}
	if err := cfg.populate(&raw); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) populate(raw *rawConfig) error {
	if strings.TrimSpace(raw.WallpaperDir) == "" {
		return errors.New("config: wallpaper_dir is required")
	}
	c.WallpaperDir = ExpandPath(raw.WallpaperDir)
	c.Recursive = raw.Recursive

	if len(raw.Extensions) == 0 {
		c.Extensions = append([]string(nil), DefaultExtensions...)
	} else {
		c.Extensions = make([]string, 0, len(raw.Extensions))
		for _, e := range raw.Extensions {
			e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
			if e == "" {
				return fmt.Errorf("config: extensions entry is empty")
			}
			c.Extensions = append(c.Extensions, e)
		}
	}

	if raw.Linux != nil {
		c.LinuxCommand = strings.TrimSpace(raw.Linux.Command)
	}

	specs := make([]season.Spec, 0, len(raw.Seasons))
	for i, rs := range raw.Seasons {
		rs.Path = ExpandPath(rs.Path)
		s, err := season.Parse(rs.Name, rs.Date, rs.DateRange, rs.Path)
		if err != nil {
			return fmt.Errorf("config: season #%d: %w", i+1, err)
		}
		specs = append(specs, s)
	}
	if err := season.CheckConflicts(specs); err != nil {
		return err
	}
	c.Seasons = specs

	c.Daemon = defaultDaemon()
	if raw.Daemon != nil {
		if s := strings.TrimSpace(raw.Daemon.RefreshInterval); s != "" {
			d, err := time.ParseDuration(s)
			if err != nil {
				return fmt.Errorf("config: daemon.refresh_interval %q: %w", s, err)
			}
			if d < time.Minute {
				return fmt.Errorf("config: daemon.refresh_interval must be at least 1m, got %s", d)
			}
			c.Daemon.RefreshInterval = d
		}

		userSetAt := false
		if raw.Daemon.RotationAt.Kind != 0 {
			at, err := parseRotationAt(&raw.Daemon.RotationAt)
			if err != nil {
				return err
			}
			c.Daemon.RotationAt = at
			userSetAt = true
		}

		if s := strings.TrimSpace(raw.Daemon.RotationInterval); s != "" {
			d, err := time.ParseDuration(s)
			if err != nil {
				return fmt.Errorf("config: daemon.rotation_interval %q: %w", s, err)
			}
			if d < MinRotationInterval {
				return fmt.Errorf("config: daemon.rotation_interval must be at least %s, got %s", MinRotationInterval, d)
			}
			if userSetAt && len(c.Daemon.RotationAt) > 1 {
				return errors.New("config: daemon.rotation_interval cannot be combined with a list of rotation_at values (provide a single anchor instead)")
			}
			c.Daemon.RotationInterval = d
		}

		if raw.Daemon.WatchConfig != nil {
			c.Daemon.WatchConfig = *raw.Daemon.WatchConfig
		}
		if raw.Daemon.DBusSleepWake != nil {
			c.Daemon.DBusSleepWake = *raw.Daemon.DBusSleepWake
		}
		if raw.Daemon.SentinelFallback != nil {
			c.Daemon.SentinelFallback = *raw.Daemon.SentinelFallback
		}
	}
	return nil
}

// parseRotationAt accepts a YAML scalar ("HH:MM") or sequence of scalars
// and returns a sorted, deduplicated list of offsets from local midnight.
func parseRotationAt(n *yaml.Node) ([]time.Duration, error) {
	var raw []string
	switch n.Kind {
	case yaml.ScalarNode:
		raw = []string{n.Value}
	case yaml.SequenceNode:
		if err := n.Decode(&raw); err != nil {
			return nil, fmt.Errorf("config: daemon.rotation_at: %w", err)
		}
	default:
		return nil, errors.New("config: daemon.rotation_at must be a HH:MM string or list of HH:MM strings")
	}
	if len(raw) == 0 {
		return nil, errors.New("config: daemon.rotation_at list is empty")
	}

	seen := make(map[time.Duration]struct{}, len(raw))
	out := make([]time.Duration, 0, len(raw))
	for _, s := range raw {
		d, err := parseClockTime(s)
		if err != nil {
			return nil, err
		}
		if _, dup := seen[d]; dup {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	// Sort ascending so scheduler can walk in order.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out, nil
}

// parseClockTime parses "HH:MM" (0-23 : 0-59) into an offset from midnight.
// Accepts "6:00" as well as "06:00".
func parseClockTime(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	colon := strings.IndexByte(s, ':')
	if colon <= 0 || colon == len(s)-1 {
		return 0, fmt.Errorf("config: rotation_at %q: expected HH:MM", s)
	}
	h, err := parseBoundedInt(s[:colon], 0, 23)
	if err != nil {
		return 0, fmt.Errorf("config: rotation_at %q: hour %w", s, err)
	}
	m, err := parseBoundedInt(s[colon+1:], 0, 59)
	if err != nil {
		return 0, fmt.Errorf("config: rotation_at %q: minute %w", s, err)
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, nil
}

func parseBoundedInt(s string, lo, hi int) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("missing value")
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("%q is not a number", s)
		}
		n = n*10 + int(c-'0')
		if n > hi {
			return 0, fmt.Errorf("%q out of range [%d, %d]", s, lo, hi)
		}
	}
	if n < lo {
		return 0, fmt.Errorf("%q out of range [%d, %d]", s, lo, hi)
	}
	return n, nil
}

func defaultDaemon() DaemonConfig {
	return DaemonConfig{
		RefreshInterval:  DefaultRefreshInterval,
		WatchConfig:      true,
		DBusSleepWake:    runtime.GOOS == "linux",
		SentinelFallback: false,
		RotationAt:       []time.Duration{DefaultRotationAnchor},
		RotationInterval: 0,
	}
}

// ExpandPath expands a leading `~` and any `$VAR` / `${VAR}` references.
func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.HasPrefix(p, "~/") || p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				p = home
			} else {
				p = filepath.Join(home, p[2:])
			}
		}
	}
	return os.ExpandEnv(p)
}

// DefaultPath returns the first standard config location for the current OS.
// It returns a path even if the file does not exist — callers decide what to
// do in that case.
func DefaultPath() (string, error) {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "seasoned", "config.yaml"), nil
		}
		return "", errors.New("config: %APPDATA% not set")
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "seasoned", "config.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: cannot resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "seasoned", "config.yaml"), nil
}
