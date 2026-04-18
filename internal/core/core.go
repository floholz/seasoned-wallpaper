// Package core is the single side-effect-free entry point that turns
// (config, state, date) into a wallpaper decision. CLI subcommands
// (run, next, preview) and the daemon all go through ResolveForDate —
// no logic is duplicated between one-shot and long-running modes.
//
// Apply and state persistence live in the caller. Core never touches the
// OS beyond reading the candidate directory.
package core

import (
	"fmt"
	"math/rand/v2"
	"os"
	"time"

	"github.com/floholz/seasoned-wallpaper/internal/config"
	"github.com/floholz/seasoned-wallpaper/internal/picker"
	"github.com/floholz/seasoned-wallpaper/internal/season"
	"github.com/floholz/seasoned-wallpaper/internal/state"
)

// Decision is what ResolveForDate returns to the caller.
type Decision struct {
	// Path is the absolute path of the wallpaper to apply.
	Path string
	// Source is the folder (or file) this pick came from — the key for
	// no-repeat tracking.
	Source string
	// SeasonName is the matched season; empty when the pool was used.
	SeasonName string
	// FromPool is true when no season matched and the default pool was used.
	FromPool bool
	// PoolSize is the number of candidate files considered.
	PoolSize int
	// ReusedToday is true when state already holds a pick for the same
	// calendar date — callers can skip applying. Never set when Force=true.
	ReusedToday bool
}

// Options tunes resolution. Zero value is fine for normal use.
type Options struct {
	// Force bypasses the ReusedToday short-circuit. Used by `next` and the
	// daemon's SIGUSR1 / kick signal.
	Force bool
	// Rand is the RNG used for random selection. Nil seeds a fresh
	// time+pid-based RNG — fine for the one-shot CLI; the daemon should
	// pass a stable one so its seed lineage is test-visible.
	Rand *rand.Rand
}

// ResolveForDate returns the decision for time t given cfg and st. It does
// not mutate st, does not apply anything, and does not write to disk beyond
// reading the pool directory.
func ResolveForDate(cfg *config.Config, st *state.State, t time.Time, opts Options) (Decision, error) {
	if cfg == nil {
		return Decision{}, fmt.Errorf("core: config is nil")
	}
	if st == nil {
		st = &state.State{}
	}

	// Idempotence short-circuit: same date, state has a recorded path.
	if !opts.Force && st.LastAppliedDate == t.Format(dateFmt) && st.LastAppliedPath != "" {
		return Decision{
			Path:        st.LastAppliedPath,
			Source:      "", // unknown from state alone; caller doesn't need it in the reuse path
			SeasonName:  st.LastAppliedSeason,
			FromPool:    st.LastAppliedSeason == "",
			ReusedToday: true,
		}, nil
	}

	src, name, fromPool := resolveSource(t, cfg)
	if src == "" {
		return Decision{}, fmt.Errorf("core: no source for %s (pool empty and no matching season)", t.Format(dateFmt))
	}

	rng := opts.Rand
	if rng == nil {
		rng = newRNG()
	}

	result, err := picker.Pick(src, cfg.Recursive, cfg.Extensions, st.Recent(src), rng)
	if err != nil {
		return Decision{}, err
	}

	return Decision{
		Path:       result.Path,
		Source:     result.Source,
		SeasonName: name,
		FromPool:   fromPool,
		PoolSize:   result.Pool,
	}, nil
}

// SourceFor reports which source (pool or season path) applies on date t,
// without enumerating files. Used by `seasons` output and preview hints.
func SourceFor(t time.Time, cfg *config.Config) (src, seasonName string, fromPool bool) {
	return resolveSource(t, cfg)
}

// Record returns a new state reflecting that d was applied on date t. The
// input state is not mutated; callers persist the returned value.
func Record(st *state.State, t time.Time, d Decision) *state.State {
	next := cloneState(st)
	next.LastAppliedDate = t.Format(dateFmt)
	next.LastAppliedPath = d.Path
	next.LastAppliedSeason = d.SeasonName
	if d.PoolSize > 1 && d.Source != "" {
		recent := picker.Trim(next.Recent(d.Source), picker.RelKey(d.Source, d.Path), d.PoolSize)
		next.SetRecent(d.Source, recent)
	}
	return next
}

const dateFmt = "2006-01-02"

func resolveSource(t time.Time, cfg *config.Config) (src, name string, fromPool bool) {
	if s := season.Match(t, cfg.Seasons); s != nil {
		return s.Path, s.Name, false
	}
	return cfg.WallpaperDir, "", true
}

func newRNG() *rand.Rand {
	return rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(os.Getpid())))
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
