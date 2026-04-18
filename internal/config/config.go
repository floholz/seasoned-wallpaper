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
	RefreshInterval  string `yaml:"refresh_interval"`
	WatchConfig      *bool  `yaml:"watch_config"`
	DBusSleepWake    *bool  `yaml:"dbus_sleep_wake"`
	SentinelFallback *bool  `yaml:"sentinel_fallback"`
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

func defaultDaemon() DaemonConfig {
	return DaemonConfig{
		RefreshInterval:  DefaultRefreshInterval,
		WatchConfig:      true,
		DBusSleepWake:    runtime.GOOS == "linux",
		SentinelFallback: false,
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
