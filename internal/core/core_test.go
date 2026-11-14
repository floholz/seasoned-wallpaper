package core

import (
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/floholz/seasoned-wallpaper/internal/config"
	"github.com/floholz/seasoned-wallpaper/internal/season"
	"github.com/floholz/seasoned-wallpaper/internal/state"
)

func writeFiles(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(root, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func rng() *rand.Rand { return rand.New(rand.NewPCG(1, 2)) }

// ResolveForDate must not mutate state — daemon mode composes persistence separately.
func TestResolveForDate_NoSideEffectsOnState(t *testing.T) {
	pool := t.TempDir()
	writeFiles(t, pool, "a.jpg", "b.jpg", "c.jpg")

	cfg := &config.Config{WallpaperDir: pool, Extensions: []string{"jpg"}}
	st := &state.State{
		LastAppliedDate: "2000-01-01",
		RecentBySource:  map[string][]string{pool: {"a.jpg"}},
	}
	before := *st
	beforeRecent := append([]string(nil), st.RecentBySource[pool]...)

	_, err := ResolveForDate(cfg, st, time.Now(), Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if st.LastAppliedDate != before.LastAppliedDate {
		t.Errorf("state mutated: date %q → %q", before.LastAppliedDate, st.LastAppliedDate)
	}
	if got := st.RecentBySource[pool]; !equal(got, beforeRecent) {
		t.Errorf("state mutated: recent %v → %v", beforeRecent, got)
	}
}

func TestResolveForDate_ReusedTodayWhenStateMatches(t *testing.T) {
	pool := t.TempDir()
	writeFiles(t, pool, "a.jpg", "b.jpg")

	cfg := &config.Config{WallpaperDir: pool, Extensions: []string{"jpg"}}
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	st := &state.State{
		LastAppliedDate: "2026-04-18",
		LastAppliedPath: filepath.Join(pool, "a.jpg"),
	}

	d, err := ResolveForDate(cfg, st, now, Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if !d.ReusedToday {
		t.Fatal("expected ReusedToday=true")
	}
	if d.Path != st.LastAppliedPath {
		t.Errorf("path = %q, want %q", d.Path, st.LastAppliedPath)
	}
}

func TestResolveForDate_ForceBypassesReuse(t *testing.T) {
	pool := t.TempDir()
	writeFiles(t, pool, "a.jpg", "b.jpg", "c.jpg", "d.jpg")

	cfg := &config.Config{WallpaperDir: pool, Extensions: []string{"jpg"}}
	now := time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)
	st := &state.State{
		LastAppliedDate: "2026-04-18",
		LastAppliedPath: filepath.Join(pool, "a.jpg"),
	}

	d, err := ResolveForDate(cfg, st, now, Options{Force: true, Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if d.ReusedToday {
		t.Error("Force should not set ReusedToday")
	}
}

func TestResolveForDate_SeasonBeatsPool(t *testing.T) {
	pool := t.TempDir()
	writeFiles(t, pool, "pool1.jpg", "pool2.jpg")
	seasonDir := t.TempDir()
	writeFiles(t, seasonDir, "season1.jpg")

	s, _ := season.Parse("xmas", "12-25", "", "", seasonDir)
	cfg := &config.Config{
		WallpaperDir: pool,
		Extensions:   []string{"jpg"},
		Seasons:      []season.Spec{s},
	}

	dec25 := time.Date(2026, 12, 25, 12, 0, 0, 0, time.UTC)
	d, err := ResolveForDate(cfg, &state.State{}, dec25, Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if d.FromPool || d.SeasonName != "xmas" || d.Source != seasonDir {
		t.Errorf("bad decision: %+v", d)
	}

	jan1 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	d2, err := ResolveForDate(cfg, &state.State{}, jan1, Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if !d2.FromPool {
		t.Error("expected pool fallback")
	}
}

func TestResolveForDate_DeterministicForSameSeed(t *testing.T) {
	pool := t.TempDir()
	writeFiles(t, pool, "a.jpg", "b.jpg", "c.jpg", "d.jpg", "e.jpg")
	cfg := &config.Config{WallpaperDir: pool, Extensions: []string{"jpg"}}

	d1, _ := ResolveForDate(cfg, &state.State{}, time.Now(), Options{Rand: rand.New(rand.NewPCG(42, 42))})
	d2, _ := ResolveForDate(cfg, &state.State{}, time.Now(), Options{Rand: rand.New(rand.NewPCG(42, 42))})
	if d1.Path != d2.Path {
		t.Errorf("same seed should give same pick: %q vs %q", d1.Path, d2.Path)
	}
}

func TestRecord_DoesNotMutateInput(t *testing.T) {
	st := &state.State{RecentBySource: map[string][]string{"/src": {"old.jpg"}}}
	d := Decision{Path: "/src/new.jpg", Source: "/src", PoolSize: 10}
	next := Record(st, time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC), d)
	if st.LastAppliedDate != "" {
		t.Error("original state mutated")
	}
	if next.LastAppliedDate != "2026-04-18" {
		t.Errorf("new state date = %q", next.LastAppliedDate)
	}
	if next.RecentBySource["/src"][0] != "new.jpg" {
		t.Errorf("recent[0] = %q", next.RecentBySource["/src"][0])
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// timedSeasonCfg has a pool for the day and a June season that is only
// active 20:00..06:00, pointing at a separate "dark" folder.
func timedSeasonCfg(t *testing.T) (*config.Config, string, string) {
	t.Helper()
	pool := t.TempDir()
	writeFiles(t, pool, "bright1.jpg", "bright2.jpg")
	dark := t.TempDir()
	writeFiles(t, dark, "dark1.jpg", "dark2.jpg")
	night, err := season.Parse("june_night", "", "06-01..06-30", "20:00..06:00", dark)
	if err != nil {
		t.Fatal(err)
	}
	return &config.Config{
		WallpaperDir: pool,
		Extensions:   []string{"jpg"},
		Seasons:      []season.Spec{night},
	}, pool, dark
}

func TestResolveForDate_TimedSeasonOnlyInWindow(t *testing.T) {
	cfg, pool, dark := timedSeasonCfg(t)

	d, err := ResolveForDate(cfg, &state.State{}, time.Date(2026, 6, 15, 22, 0, 0, 0, time.UTC), Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if d.FromPool || d.Source != dark || d.SeasonName != "june_night" || d.Season == nil {
		t.Errorf("22:00 decision: %+v", d)
	}

	d, err = ResolveForDate(cfg, &state.State{}, time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC), Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if !d.FromPool || d.Source != pool || d.Season != nil {
		t.Errorf("12:00 decision: %+v", d)
	}
}

// Crossing into a timed season on the same day must re-pick; staying in the
// same source must keep reusing.
func TestResolveForDate_SourceChangeInvalidatesReuse(t *testing.T) {
	cfg, _, dark := timedSeasonCfg(t)
	noon := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	d, err := ResolveForDate(cfg, &state.State{}, noon, Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	st := Record(&state.State{}, noon, d)

	d, err = ResolveForDate(cfg, st, noon.Add(time.Hour), Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if !d.ReusedToday {
		t.Fatal("same day + same source should reuse")
	}

	evening := time.Date(2026, 6, 15, 20, 0, 0, 0, time.UTC)
	d, err = ResolveForDate(cfg, st, evening, Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if d.ReusedToday || d.Source != dark {
		t.Fatalf("expected fresh pick from timed season, got %+v", d)
	}
	st = Record(st, evening, d)

	d, err = ResolveForDate(cfg, st, evening.Add(2*time.Hour), Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if !d.ReusedToday {
		t.Error("staying inside the timed season should reuse")
	}
}

// State written before LastAppliedSource existed keeps reusing for the
// pool, but not once a timed season is active.
func TestResolveForDate_LegacyStateWithoutSource(t *testing.T) {
	cfg, pool, _ := timedSeasonCfg(t)
	st := &state.State{
		LastAppliedDate: "2026-06-15",
		LastAppliedPath: filepath.Join(pool, "bright1.jpg"),
	}
	d, err := ResolveForDate(cfg, st, time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC), Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if !d.ReusedToday {
		t.Error("legacy state in pool hours should reuse")
	}
	d, err = ResolveForDate(cfg, st, time.Date(2026, 6, 15, 21, 0, 0, 0, time.UTC), Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if d.ReusedToday {
		t.Error("legacy state should not suppress a timed-season pick")
	}
}

// A timed season takes over from its all-day sibling in its window, and
// crossing into it invalidates same-day reuse.
func TestResolveForDate_TimedSeason(t *testing.T) {
	pool := t.TempDir()
	writeFiles(t, pool, "p.jpg")
	winter := t.TempDir()
	writeFiles(t, winter, "snow1.jpg", "snow2.jpg")
	winterNight := t.TempDir()
	writeFiles(t, winterNight, "dark1.jpg", "dark2.jpg")

	day, _ := season.Parse("winter", "", "11-01..12-23", "", winter)
	night, err := season.Parse("winter_night", "", "11-01..12-23", "20:00..06:00", winterNight)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{WallpaperDir: pool, Extensions: []string{"jpg"}, Seasons: []season.Spec{day, night}}

	noon := time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)
	d, err := ResolveForDate(cfg, &state.State{}, noon, Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if d.SeasonName != "winter" || d.Source != winter {
		t.Fatalf("noon: %+v", d)
	}
	st := Record(&state.State{}, noon, d)

	evening := time.Date(2026, 12, 1, 20, 30, 0, 0, time.UTC)
	d, err = ResolveForDate(cfg, st, evening, Options{Rand: rng()})
	if err != nil {
		t.Fatal(err)
	}
	if d.ReusedToday || d.SeasonName != "winter_night" || d.Source != winterNight || d.Season == nil || d.Season.Time == nil {
		t.Fatalf("evening: %+v", d)
	}
}
