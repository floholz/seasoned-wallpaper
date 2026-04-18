package daemon

import (
	"testing"
	"time"
)

func TestNextWake_PicksRefreshBeforeMidnight(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 9, 0, 0, 0, loc)
	got := NextWake(now, 2*time.Hour)
	want := time.Date(2026, 6, 15, 11, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextWake = %s, want %s", got, want)
	}
}

func TestNextWake_PicksMidnightWhenRefreshTooFar(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 22, 30, 0, 0, loc)
	got := NextWake(now, 6*time.Hour) // would cross midnight
	want := time.Date(2026, 6, 16, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextWake = %s, want %s", got, want)
	}
}

func TestNextWake_ZeroRefreshGivesMidnight(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 9, 0, 0, 0, loc)
	got := NextWake(now, 0)
	want := time.Date(2026, 6, 16, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextWake = %s, want %s", got, want)
	}
}

// Europe/Berlin springs forward at 02:00 -> 03:00 on 2026-03-29.
// NextWake at 22:00 local on 2026-03-28 with a 6h refresh must still
// resolve to a sensible instant — the wake time may be at or after
// the DST jump, but the returned value must be strictly after now.
func TestNextWake_AcrossDSTSpringForward(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	now := time.Date(2026, 3, 28, 22, 0, 0, 0, loc)
	got := NextWake(now, 6*time.Hour)
	if !got.After(now) {
		t.Fatalf("NextWake %s not after now %s", got, now)
	}
	if got.Sub(now) > 8*time.Hour {
		t.Errorf("NextWake %s more than 8h after now %s", got, now)
	}
}

func TestDrifted(t *testing.T) {
	base := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	sched := base
	sleep := time.Hour

	if Drifted(sched, base.Add(30*time.Minute), sleep) {
		t.Error("30min lateness should not flag drift")
	}
	if !Drifted(sched, base.Add(2*time.Hour+time.Second), sleep) {
		t.Error("2h lateness should flag drift (>= 2x sleep)")
	}
	if Drifted(sched, base, 0) {
		t.Error("zero sleep should never drift")
	}
}
