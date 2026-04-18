package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad_Good(t *testing.T) {
	p := writeConfig(t, `
wallpaper_dir: /tmp/wallpapers
recursive: true
extensions: [jpg, png]
linux:
  command: "swww img {{.Path}}"
seasons:
  - name: xmas
    date: "12-25"
    path: /tmp/xmas
  - date: "2026-04-05"
    path: /tmp/specific.jpg
  - name: december
    date_range: "12-01..12-24"
    path: /tmp/december
  - date_range: "2026-03-28..2026-03-30"
    path: /tmp/spring-2026
  - name: year-turn
    date_range: "12-30..01-02"
    path: /tmp/new-year
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.WallpaperDir != "/tmp/wallpapers" {
		t.Errorf("wallpaper_dir = %q", cfg.WallpaperDir)
	}
	if !cfg.Recursive {
		t.Errorf("recursive = %v", cfg.Recursive)
	}
	if len(cfg.Extensions) != 2 {
		t.Errorf("extensions len = %d", len(cfg.Extensions))
	}
	if cfg.LinuxCommand != "swww img {{.Path}}" {
		t.Errorf("linux command = %q", cfg.LinuxCommand)
	}
	if len(cfg.Seasons) != 5 {
		t.Errorf("seasons len = %d", len(cfg.Seasons))
	}
}

func TestLoad_DefaultExtensions(t *testing.T) {
	p := writeConfig(t, `wallpaper_dir: /tmp/w`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.Extensions) != len(DefaultExtensions) {
		t.Errorf("default extensions not applied: %v", cfg.Extensions)
	}
}

func TestLoad_MissingWallpaperDir(t *testing.T) {
	p := writeConfig(t, `recursive: true`)
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "wallpaper_dir") {
		t.Fatalf("expected wallpaper_dir error, got %v", err)
	}
}

func TestLoad_UnknownField(t *testing.T) {
	p := writeConfig(t, `
wallpaper_dir: /tmp/w
unknown_field: oops
`)
	if _, err := Load(p); err == nil {
		t.Fatal("expected strict-mode error for unknown field")
	}
}

func TestLoad_ConflictingSeasons(t *testing.T) {
	p := writeConfig(t, `
wallpaper_dir: /tmp/w
seasons:
  - name: a
    date: "12-25"
    path: /tmp/a
  - name: b
    date: "12-25"
    path: /tmp/b
`)
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "both pin") {
		t.Fatalf("expected conflict error, got %v", err)
	}
}

func TestLoad_InvalidSeason(t *testing.T) {
	p := writeConfig(t, `
wallpaper_dir: /tmp/w
seasons:
  - name: bad
    date: "13-40"
    path: /tmp/bad
`)
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected invalid date error, got %v", err)
	}
}

func TestLoad_SeasonBothDateAndRange(t *testing.T) {
	p := writeConfig(t, `
wallpaper_dir: /tmp/w
seasons:
  - date: "12-25"
    date_range: "12-01..12-24"
    path: /tmp/x
`)
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected mutual-exclusion error, got %v", err)
	}
}

func TestLoad_DaemonDefaults(t *testing.T) {
	p := writeConfig(t, `wallpaper_dir: /tmp/w`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Daemon.RefreshInterval != DefaultRefreshInterval {
		t.Errorf("refresh_interval = %v, want %v", cfg.Daemon.RefreshInterval, DefaultRefreshInterval)
	}
	if !cfg.Daemon.WatchConfig {
		t.Error("watch_config default should be true")
	}
}

func TestLoad_DaemonOverrides(t *testing.T) {
	p := writeConfig(t, `
wallpaper_dir: /tmp/w
daemon:
  refresh_interval: 30m
  watch_config: false
  dbus_sleep_wake: false
  sentinel_fallback: true
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Daemon.RefreshInterval != 30*60*1e9 { // 30m
		t.Errorf("refresh_interval = %v", cfg.Daemon.RefreshInterval)
	}
	if cfg.Daemon.WatchConfig {
		t.Error("watch_config should be false")
	}
	if cfg.Daemon.DBusSleepWake {
		t.Error("dbus_sleep_wake should be false")
	}
	if !cfg.Daemon.SentinelFallback {
		t.Error("sentinel_fallback should be true")
	}
}

func TestLoad_DaemonRefreshTooShort(t *testing.T) {
	p := writeConfig(t, `
wallpaper_dir: /tmp/w
daemon:
  refresh_interval: 10s
`)
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "at least 1m") {
		t.Fatalf("expected min-duration error, got %v", err)
	}
}

func TestExpandPath_Tilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	got := ExpandPath("~/foo/bar")
	want := filepath.Join(home, "foo/bar")
	if got != want {
		t.Errorf("ExpandPath ~ = %q, want %q", got, want)
	}
}

func TestExpandPath_EnvVar(t *testing.T) {
	t.Setenv("MYDIR", "/opt/foo")
	got := ExpandPath("$MYDIR/bar")
	if got != "/opt/foo/bar" {
		t.Errorf("ExpandPath $VAR = %q", got)
	}
}
