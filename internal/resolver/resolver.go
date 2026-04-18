// Package resolver computes which wallpaper should be applied for a given
// date. It is intentionally side-effect free: no wallpaper change, no state
// writes. The only IO is reading the wallpaper directory to enumerate
// candidates.
//
// A daemon (v2) and the v1 one-shot CLI subcommands (run, next, preview)
// all share this same code path. Callers layer their own concerns on top:
//
//   - "run" checks State.LastAppliedDate first for idempotence.
//   - "next" always calls Resolve.
//   - "preview" calls Resolve with a user-provided date and does not apply.
//   - A future daemon would call Resolve on a schedule, then apply.
package resolver

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/floholz/seasoned-wallpaper/internal/config"
	"github.com/floholz/seasoned-wallpaper/internal/picker"
	"github.com/floholz/seasoned-wallpaper/internal/season"
	"github.com/floholz/seasoned-wallpaper/internal/state"
)

// Resolution is the output of a resolution step.
type Resolution struct {
	// Path is the absolute path of the wallpaper to apply.
	Path string
	// Source is the folder (or file) this pick originated from. Used as the
	// key for no-repeat tracking in State.
	Source string
	// SeasonName is the name of the matched season, or empty when the pool
	// was used.
	SeasonName string
	// FromPool is true when no season matched and the default pool was used.
	FromPool bool
	// PoolSize is the number of candidate files considered.
	PoolSize int
}

// Resolve returns what seasoned would pick for date `on`, given cfg and a
// read-only view of st. It does not mutate st or touch the OS beyond
// reading the source directory.
//
// rng drives the random pick; callers should seed deterministically for
// tests and non-deterministically for normal use.
func Resolve(on time.Time, cfg *config.Config, st *state.State, rng *rand.Rand) (Resolution, error) {
	if cfg == nil {
		return Resolution{}, fmt.Errorf("resolver: config is nil")
	}

	src, name, fromPool := resolveSource(on, cfg)
	if src == "" {
		return Resolution{}, fmt.Errorf("resolver: no source for %s (pool empty and no matching season)", on.Format("2006-01-02"))
	}

	result, err := picker.Pick(src, cfg.Recursive, cfg.Extensions, st.Recent(src), rng)
	if err != nil {
		return Resolution{}, err
	}

	return Resolution{
		Path:       result.Path,
		Source:     result.Source,
		SeasonName: name,
		FromPool:   fromPool,
		PoolSize:   result.Pool,
	}, nil
}

// resolveSource picks the directory (or file) to pick from, given the date.
// Returns the source path, season name (empty for pool), and whether it came
// from the default pool.
func resolveSource(on time.Time, cfg *config.Config) (src, name string, fromPool bool) {
	if s := season.Match(on, cfg.Seasons); s != nil {
		return s.Path, s.Name, false
	}
	return cfg.WallpaperDir, "", true
}

// SourceFor is a lightweight, IO-free version of resolveSource exposed for
// CLI subcommands (seasons, preview) that want to describe the pick without
// enumerating files.
func SourceFor(on time.Time, cfg *config.Config) (src, seasonName string, fromPool bool) {
	return resolveSource(on, cfg)
}

// RecordResolution returns a new state value reflecting that r was applied
// on date `on`. The original state is not mutated.
func RecordResolution(st *state.State, on time.Time, r Resolution) *state.State {
	next := cloneState(st)
	next.LastAppliedDate = on.Format("2006-01-02")
	next.LastAppliedPath = r.Path
	next.LastAppliedSeason = r.SeasonName

	// Only track recent picks when the source is a real folder (>1 option).
	if r.PoolSize > 1 {
		recent := picker.Trim(next.Recent(r.Source), picker.RelKey(r.Source, r.Path), r.PoolSize)
		next.SetRecent(r.Source, recent)
	}
	return next
}

func cloneState(st *state.State) *state.State {
	if st == nil {
		return &state.State{}
	}
	out := *st
	if st.RecentBySource != nil {
		out.RecentBySource = make(map[string][]string, len(st.RecentBySource))
		for k, v := range st.RecentBySource {
			cp := make([]string, len(v))
			copy(cp, v)
			out.RecentBySource[k] = cp
		}
	}
	return &out
}
