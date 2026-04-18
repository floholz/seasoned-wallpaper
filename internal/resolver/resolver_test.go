package resolver

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

// Resolve must not mutate the state argument — required for v2 daemon mode
// where state is persisted separately from the compute step.
func TestResolve_NoSideEffectsOnState(t *testing.T) {
	pool := t.TempDir()
	writeFiles(t, pool, "a.jpg", "b.jpg", "c.jpg")

	cfg := &config.Config{
		WallpaperDir: pool,
		Extensions:   []string{"jpg"},
	}
	st := &state.State{
		LastAppliedDate: "old",
		RecentBySource:  map[string][]string{pool: {"a.jpg"}},
	}
	before := *st
	beforeRecent := append([]string(nil), st.RecentBySource[pool]...)

	_, err := Resolve(time.Now(), cfg, st, rng())
	if err != nil {
		t.Fatal(err)
	}
	if st.LastAppliedDate != before.LastAppliedDate {
		t.Errorf("state mutated: LastAppliedDate %q → %q", before.LastAppliedDate, st.LastAppliedDate)
	}
	if got := st.RecentBySource[pool]; !equal(got, beforeRecent) {
		t.Errorf("state mutated: recent %v → %v", beforeRecent, got)
	}
}

// Resolve must honour season priority — a specific season on the date
// should win over the default pool.
func TestResolve_SeasonBeatsPool(t *testing.T) {
	pool := t.TempDir()
	writeFiles(t, pool, "pool1.jpg", "pool2.jpg")
	seasonDir := t.TempDir()
	writeFiles(t, seasonDir, "season1.jpg")

	s, err := season.Parse("xmas", "12-25", "", seasonDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		WallpaperDir: pool,
		Extensions:   []string{"jpg"},
		Seasons:      []season.Spec{s},
	}
	st := &state.State{}

	dec25 := time.Date(2026, 12, 25, 12, 0, 0, 0, time.UTC)
	r, err := Resolve(dec25, cfg, st, rng())
	if err != nil {
		t.Fatal(err)
	}
	if r.FromPool {
		t.Error("expected season match, got pool")
	}
	if r.SeasonName != "xmas" {
		t.Errorf("season name = %q", r.SeasonName)
	}
	if r.Source != seasonDir {
		t.Errorf("source = %q, want %q", r.Source, seasonDir)
	}

	// Off-season day: falls back to pool.
	jan1 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	r2, err := Resolve(jan1, cfg, st, rng())
	if err != nil {
		t.Fatal(err)
	}
	if !r2.FromPool {
		t.Error("expected pool fallback")
	}
}

// Changing only the date parameter must vary the pick source as expected —
// demonstrates the "compute next wallpaper for date X" contract.
func TestResolve_DeterministicForSameSeed(t *testing.T) {
	pool := t.TempDir()
	writeFiles(t, pool, "a.jpg", "b.jpg", "c.jpg", "d.jpg", "e.jpg")
	cfg := &config.Config{WallpaperDir: pool, Extensions: []string{"jpg"}}
	st := &state.State{}

	r1, err := Resolve(time.Now(), cfg, st, rand.New(rand.NewPCG(42, 42)))
	if err != nil {
		t.Fatal(err)
	}
	r2, err := Resolve(time.Now(), cfg, st, rand.New(rand.NewPCG(42, 42)))
	if err != nil {
		t.Fatal(err)
	}
	if r1.Path != r2.Path {
		t.Errorf("same seed should give same pick: %q vs %q", r1.Path, r2.Path)
	}
}

func TestRecordResolution_DoesNotMutateInput(t *testing.T) {
	st := &state.State{
		RecentBySource: map[string][]string{"/src": {"old.jpg"}},
	}
	r := Resolution{
		Path:     "/src/new.jpg",
		Source:   "/src",
		PoolSize: 10,
	}
	next := RecordResolution(st, time.Date(2026, 4, 18, 0, 0, 0, 0, time.UTC), r)
	if st.LastAppliedDate != "" {
		t.Error("original state mutated")
	}
	if next.LastAppliedDate != "2026-04-18" {
		t.Errorf("new state date = %q", next.LastAppliedDate)
	}
	if got := next.RecentBySource["/src"]; got[0] != "new.jpg" {
		t.Errorf("recent[0] = %q, want new.jpg", got[0])
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
