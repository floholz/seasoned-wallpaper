// Package daemon runs seasoned as a long-lived process. It reuses the v1
// core (ResolveForDate, setter) and adds scheduling, config hot-reload,
// signal/sentinel handling, and sleep-wake awareness on top.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/floholz/seasoned-wallpaper/internal/config"
	"github.com/floholz/seasoned-wallpaper/internal/core"
	"github.com/floholz/seasoned-wallpaper/internal/setter"
	"github.com/floholz/seasoned-wallpaper/internal/state"
)

// Options configure a Daemon.
type Options struct {
	Cfg       *config.Config // required
	CfgPath   string         // for fsnotify and SIGHUP reload
	StatePath string         // for persistence
	Clock     Clock          // defaults to realClock
	Rand      *rand.Rand     // defaults to time+pid seeded
	Setter    setter.Setter  // defaults to setter.New(Options{LinuxCommand: Cfg.LinuxCommand})
}

// Daemon is the long-running process. Construct with New, drive with Run.
type Daemon struct {
	cfgPath   string
	statePath string
	clock     Clock
	rng       *rand.Rand

	mu     sync.RWMutex
	cfg    *config.Config
	setter setter.Setter
	state  *state.State

	// Event channels fed by watchers (fsnotify, signal, sentinel, D-Bus).
	// Each is buffered(1) so a burst of events collapses to a single tick.
	reloadCh chan struct{}
	kickCh   chan struct{}
	wakeCh   chan struct{}
}

// New builds a Daemon. The initial config and setter are constructed so
// that Run can start applying immediately.
func New(opts Options) (*Daemon, error) {
	if opts.Cfg == nil {
		return nil, errors.New("daemon: Options.Cfg is required")
	}
	if opts.StatePath == "" {
		return nil, errors.New("daemon: Options.StatePath is required")
	}

	set := opts.Setter
	if set == nil {
		s, err := setter.New(setter.Options{LinuxCommand: opts.Cfg.LinuxCommand})
		if err != nil {
			return nil, fmt.Errorf("daemon: init setter: %w", err)
		}
		set = s
	}
	st, err := state.Load(opts.StatePath)
	if err != nil {
		return nil, fmt.Errorf("daemon: load state: %w", err)
	}

	clk := opts.Clock
	if clk == nil {
		clk = realClock{}
	}
	rng := opts.Rand
	if rng == nil {
		rng = rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(os.Getpid())))
	}

	return &Daemon{
		cfgPath:   opts.CfgPath,
		statePath: opts.StatePath,
		clock:     clk,
		rng:       rng,
		cfg:       opts.Cfg,
		setter:    set,
		state:     st,
		reloadCh:  make(chan struct{}, 1),
		kickCh:    make(chan struct{}, 1),
		wakeCh:    make(chan struct{}, 1),
	}, nil
}

// Reload signals the daemon to re-read its config.
func (d *Daemon) Reload() { nudge(d.reloadCh) }

// Kick signals the daemon to re-evaluate immediately, bypassing the
// ReusedToday short-circuit. Equivalent to `seasoned next` inside the
// daemon loop.
func (d *Daemon) Kick() { nudge(d.kickCh) }

// Wake signals the daemon that the machine just resumed from suspend.
func (d *Daemon) Wake() { nudge(d.wakeCh) }

// Run drives the main loop until ctx is cancelled. Returns ctx.Err() or
// the first fatal error; transient errors are logged and the loop continues.
func (d *Daemon) Run(ctx context.Context) error {
	slog.Info("daemon starting",
		"config", d.cfgPath,
		"backend", d.setter.Describe(),
		"refresh_interval", d.refreshInterval().String(),
	)

	// First pass: bring the wallpaper up-to-date immediately.
	d.evaluate(ctx, false)

	if d.cfg.Daemon.WatchConfig && d.cfgPath != "" {
		go func() {
			if err := d.watchConfig(ctx); err != nil {
				slog.Warn("config watcher stopped", "error", err)
			}
		}()
	}

	// The non-Linux build of watchSleepWake is a no-op, so this is safe
	// unconditionally. The default config already sets DBusSleepWake=false
	// off Linux so the goroutine typically isn't spawned there at all.
	if d.cfg.Daemon.DBusSleepWake {
		go func() {
			if err := d.watchSleepWake(ctx); err != nil {
				slog.Warn("dbus sleep/wake watcher stopped", "error", err)
			}
		}()
	}

	// Sentinel control files: mandatory on Windows (no POSIX signals),
	// opt-in on POSIX.
	if runtime.GOOS == "windows" || d.cfg.Daemon.SentinelFallback {
		if dir, err := ControlDir(); err == nil {
			go func() {
				if err := d.watchSentinel(ctx, dir); err != nil {
					slog.Warn("sentinel watcher stopped", "error", err)
				}
			}()
		} else {
			slog.Warn("sentinel watcher disabled", "error", err)
		}
	}

	for {
		now := d.clock.Now()
		rotation, at, interval := d.rotationParams()
		rot := NextRotation(now, at, interval)
		safety := now.Add(rotation) // safety-net ceiling on sleep length

		var wake time.Time
		var isRotation bool
		if rot.Before(safety) {
			wake, isRotation = rot, true
		} else {
			wake, isRotation = safety, false
		}
		sleep := max(wake.Sub(now), time.Second)
		slog.Debug("scheduling next wake",
			"at", wake.Format(time.RFC3339),
			"in", sleep.String(),
			"rotation", isRotation,
			"next_rotation", rot.Format(time.RFC3339),
		)

		select {
		case <-ctx.Done():
			slog.Info("daemon shutting down")
			return ctx.Err()

		case <-d.clock.After(sleep):
			actual := d.clock.Now()
			drifted := Drifted(wake, actual, sleep)
			if drifted {
				slog.Info("probable wake from suspend", "expected", wake.Format(time.RFC3339), "actual", actual.Format(time.RFC3339))
			}
			// Force a fresh pick on a scheduled rotation or after a probable
			// suspend; the safety-net wake stays a no-op when ReusedToday holds.
			d.evaluate(ctx, isRotation || drifted)

		case <-d.reloadCh:
			d.reload()
			d.evaluate(ctx, false)

		case <-d.kickCh:
			slog.Info("kick received, forcing re-roll")
			d.evaluate(ctx, true)

		case <-d.wakeCh:
			slog.Info("resume signal received")
			d.evaluate(ctx, true)
		}
	}
}

// rotationParams returns the daemon's current schedule params under the
// config lock: the safety-net refresh interval, the sorted rotation
// offsets, and the rotation interval (0 = list mode).
func (d *Daemon) rotationParams() (refresh time.Duration, at []time.Duration, interval time.Duration) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg.Daemon.RefreshInterval, d.cfg.Daemon.RotationAt, d.cfg.Daemon.RotationInterval
}

// evaluate runs one ResolveForDate + optional Apply + state save. Errors
// are logged; the loop continues.
func (d *Daemon) evaluate(ctx context.Context, force bool) {
	d.mu.RLock()
	cfg, st, set := d.cfg, d.state, d.setter
	d.mu.RUnlock()

	now := d.clock.Now()
	dec, err := core.ResolveForDate(cfg, st, now, core.Options{Force: force, Rand: d.rng})
	if err != nil {
		slog.Error("resolve failed", "event", "resolve", "error", err)
		return
	}
	if dec.ReusedToday {
		slog.Debug("already applied today", "event", "skip", "date", now.Format("2006-01-02"), "path", dec.Path)
		return
	}

	applyCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := set.Apply(applyCtx, dec.Path); err != nil {
		slog.Error("apply failed", "event", "apply", "path", dec.Path, "backend", set.Describe(), "error", err)
		return
	}

	next := core.Record(st, now, dec)
	if err := state.Save(d.statePath, next); err != nil {
		slog.Error("state save failed", "event", "state", "error", err)
		return
	}

	d.mu.Lock()
	d.state = next
	d.mu.Unlock()

	slog.Info("applied",
		"event", "apply",
		"date", now.Format("2006-01-02"),
		"path", dec.Path,
		"source", dec.Source,
		"season", dec.SeasonName,
		"backend", set.Describe(),
	)
}

// reload re-reads the config from disk. Invalid configs are logged and the
// previous config is kept.
func (d *Daemon) reload() {
	if d.cfgPath == "" {
		slog.Warn("reload requested but no config path known")
		return
	}
	newCfg, err := config.Load(d.cfgPath)
	if err != nil {
		slog.Error("reload: invalid config, keeping old", "event", "reload", "error", err)
		return
	}
	set, err := setter.New(setter.Options{LinuxCommand: newCfg.LinuxCommand})
	if err != nil {
		slog.Error("reload: setter init failed, keeping old config", "event", "reload", "error", err)
		return
	}
	d.mu.Lock()
	d.cfg = newCfg
	d.setter = set
	d.mu.Unlock()
	slog.Info("config reloaded", "event", "reload", "source", newCfg.Source, "backend", set.Describe())
}

func (d *Daemon) refreshInterval() time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg.Daemon.RefreshInterval
}

// Config returns the current config. For watchers that need to consult
// daemon.* fields (watch_config, dbus_sleep_wake, sentinel_fallback).
func (d *Daemon) Config() *config.Config {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cfg
}

// nudge sends on ch without blocking; buffering ensures we don't lose the
// first event and further events collapse into that one.
func nudge(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
