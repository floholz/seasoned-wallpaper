package daemon

import (
	"context"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/floholz/seasoned-wallpaper/internal/config"
)

// fakeClock is a deterministic Clock for daemon tests. After(d) registers a
// pending firing; test code advances virtual time with Advance(d), which
// delivers any pending firings whose deadline has elapsed.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	pending []pending
}

type pending struct {
	deadline time.Time
	ch       chan time.Time
}

func newFakeClock(start time.Time) *fakeClock { return &fakeClock{now: start} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	c.pending = append(c.pending, pending{c.now.Add(d), ch})
	return ch
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var kept []pending
	var fired []pending
	for _, p := range c.pending {
		if !p.deadline.After(c.now) {
			fired = append(fired, p)
		} else {
			kept = append(kept, p)
		}
	}
	c.pending = kept
	now := c.now
	c.mu.Unlock()
	for _, p := range fired {
		p.ch <- now
	}
}

// stubSetter records Apply calls; safe for concurrent use.
type stubSetter struct {
	mu      sync.Mutex
	applied []string
}

func (s *stubSetter) Apply(_ context.Context, path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied = append(s.applied, path)
	return nil
}

func (s *stubSetter) Describe() string { return "stub" }

func (s *stubSetter) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.applied)
}

// setupTestDaemon builds a daemon with a fake clock, a stub setter, and a
// minimal real config pointing at a tempdir containing one wallpaper.
func setupTestDaemon(t *testing.T, now time.Time) (*Daemon, *fakeClock, *stubSetter) {
	t.Helper()
	wall := t.TempDir()
	if err := os.WriteFile(filepath.Join(wall, "a.jpg"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		WallpaperDir: wall,
		Extensions:   []string{"jpg"},
		Daemon: config.DaemonConfig{
			RefreshInterval:  6 * time.Hour,
			WatchConfig:      false,
			DBusSleepWake:    false,
			SentinelFallback: false,
			RotationAt:       []time.Duration{3 * time.Hour},
		},
	}

	clk := newFakeClock(now)
	set := &stubSetter{}
	d, err := New(Options{
		Cfg:       cfg,
		StatePath: filepath.Join(t.TempDir(), "state.json"),
		Clock:     clk,
		Rand:      rand.New(rand.NewPCG(1, 2)),
		Setter:    set,
	})
	if err != nil {
		t.Fatal(err)
	}
	return d, clk, set
}

// TestRun_InitialEvaluate verifies the daemon applies a wallpaper on
// startup before blocking on the scheduler.
func TestRun_InitialEvaluate(t *testing.T) {
	d, _, set := setupTestDaemon(t, time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	if !waitFor(func() bool { return set.Count() >= 1 }, 2*time.Second) {
		t.Fatal("initial evaluate did not apply wallpaper")
	}

	cancel()
	<-done
}

// TestRun_ScheduledTick advances the fake clock past the next wake and
// asserts that the daemon evaluates again (but skips re-apply because
// ReusedToday short-circuits — so only the initial Apply is observed).
func TestRun_ScheduledTick(t *testing.T) {
	start := time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC)
	d, clk, set := setupTestDaemon(t, start)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	if !waitFor(func() bool { return set.Count() == 1 }, 2*time.Second) {
		t.Fatal("initial apply missing")
	}

	// Wait for the loop to register its After() call, then advance past it.
	if !waitFor(func() bool { return clk.pendingCount() >= 1 }, 2*time.Second) {
		t.Fatal("no pending After after initial evaluate")
	}
	clk.Advance(7 * time.Hour) // past the 6h refresh interval

	// Still the same date, so ReusedToday → no new apply. But the loop
	// should have progressed and re-registered a pending After.
	if !waitFor(func() bool { return clk.pendingCount() >= 1 }, 2*time.Second) {
		t.Fatal("scheduler did not re-arm after tick")
	}
	if set.Count() != 1 {
		t.Errorf("apply count = %d, want 1 (ReusedToday should short-circuit)", set.Count())
	}

	cancel()
	<-done
}

// TestRun_KickForcesReapply sends Kick() and verifies it bypasses the
// ReusedToday check, triggering a second Apply on the same date.
func TestRun_KickForcesReapply(t *testing.T) {
	d, _, set := setupTestDaemon(t, time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	if !waitFor(func() bool { return set.Count() == 1 }, 2*time.Second) {
		t.Fatal("initial apply missing")
	}

	d.Kick()
	if !waitFor(func() bool { return set.Count() == 2 }, 2*time.Second) {
		t.Errorf("Kick did not force re-apply, count=%d", set.Count())
	}

	cancel()
	<-done
}

// TestRun_ContextCancelShutsDown verifies Run returns promptly on ctx cancel.
func TestRun_ContextCancelShutsDown(t *testing.T) {
	d, _, _ := setupTestDaemon(t, time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not shut down on ctx cancel")
	}
}

// --- helpers ---

func (c *fakeClock) pendingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

func waitFor(pred func() bool, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if pred() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return pred()
}
