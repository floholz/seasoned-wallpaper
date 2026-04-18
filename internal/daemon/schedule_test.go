package daemon

import (
	"testing"
	"time"
)

func TestNextRotation_ListMode_PicksNextFutureTime(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 9, 0, 0, 0, loc)
	at := []time.Duration{0, 6 * time.Hour, 12 * time.Hour, 18 * time.Hour}

	got := NextRotation(now, at, 0)
	want := time.Date(2026, 6, 15, 12, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextRotation = %s, want %s", got, want)
	}
}

func TestNextRotation_ListMode_RollsToTomorrow(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 23, 30, 0, 0, loc)
	at := []time.Duration{0, 12 * time.Hour}

	got := NextRotation(now, at, 0)
	want := time.Date(2026, 6, 16, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextRotation = %s, want %s", got, want)
	}
}

func TestNextRotation_ListMode_ExactlyAtTimeSkipsToNext(t *testing.T) {
	loc := time.UTC
	// At exactly 12:00 with 12:00 in the list, the next fire must be
	// strictly after now — otherwise we'd busy-loop.
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, loc)
	at := []time.Duration{0, 12 * time.Hour}

	got := NextRotation(now, at, 0)
	want := time.Date(2026, 6, 16, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextRotation = %s, want %s", got, want)
	}
}

func TestNextRotation_AnchorMode_BeforeAnchor(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 1, 0, 0, 0, loc)
	// Anchor 03:00, interval 8h → 03:00, 11:00, 19:00, then 03:00 tomorrow.
	at := []time.Duration{3 * time.Hour}
	got := NextRotation(now, at, 8*time.Hour)
	want := time.Date(2026, 6, 15, 3, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextRotation = %s, want %s", got, want)
	}
}

func TestNextRotation_AnchorMode_MidDay(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 13, 0, 0, 0, loc)
	at := []time.Duration{3 * time.Hour}
	got := NextRotation(now, at, 8*time.Hour)
	want := time.Date(2026, 6, 15, 19, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextRotation = %s, want %s", got, want)
	}
}

func TestNextRotation_AnchorMode_AfterLastFireToday(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 20, 0, 0, 0, loc)
	at := []time.Duration{3 * time.Hour}
	got := NextRotation(now, at, 8*time.Hour)
	want := time.Date(2026, 6, 16, 3, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextRotation = %s, want %s", got, want)
	}
}

func TestNextRotation_AnchorMode_ExactlyAtAnchor(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 3, 0, 0, 0, loc)
	at := []time.Duration{3 * time.Hour}
	got := NextRotation(now, at, 8*time.Hour)
	want := time.Date(2026, 6, 15, 11, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextRotation at exact anchor = %s, want %s", got, want)
	}
}

func TestNextRotation_AnchorMode_IntervalLongerThanDay(t *testing.T) {
	loc := time.UTC
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, loc)
	// Anchor midnight, 7-day interval anchored on 2026-06-15.
	at := []time.Duration{0}
	got := NextRotation(now, at, 7*24*time.Hour)
	want := time.Date(2026, 6, 22, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("NextRotation = %s, want %s", got, want)
	}
}

// Europe/Berlin springs forward at 02:00 → 03:00 on 2026-03-29.
// A 03:00 anchor on that date must still resolve to a sensible future
// instant.
func TestNextRotation_AcrossDSTSpringForward(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("tzdata unavailable")
	}
	now := time.Date(2026, 3, 28, 22, 0, 0, 0, loc)
	got := NextRotation(now, []time.Duration{3 * time.Hour}, 0)
	if !got.After(now) {
		t.Fatalf("NextRotation %s not after now %s", got, now)
	}
	if got.Sub(now) > 8*time.Hour {
		t.Errorf("NextRotation %s more than 8h after now %s", got, now)
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
