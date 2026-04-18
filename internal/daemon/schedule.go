package daemon

import "time"

// NextRotation returns the next local wall-clock moment at which the
// daemon should force a new wallpaper pick, given the rotation schedule.
//
//   - If interval == 0 (list mode), the next fire is the earliest of
//     today's at[i] that is strictly after now, or tomorrow's at[0] if
//     all of today's have passed.
//
//   - If interval > 0 (anchor mode), at[0] is interpreted as the clock
//     anchor, and the schedule is anchor + k*interval for the smallest k
//     that yields a moment strictly after now.
//
// at must be non-empty and sorted ascending. Offsets are relative to
// local midnight of now.
func NextRotation(now time.Time, at []time.Duration, interval time.Duration) time.Time {
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	if interval > 0 {
		anchor := today.Add(at[0])
		if anchor.After(now) {
			return anchor
		}
		elapsed := now.Sub(anchor)
		k := int64(elapsed/interval) + 1
		return anchor.Add(time.Duration(k) * interval)
	}

	for _, off := range at {
		t := today.Add(off)
		if t.After(now) {
			return t
		}
	}
	return today.AddDate(0, 0, 1).Add(at[0])
}

// Drifted reports true when the actual wake-up happened so much later than
// the scheduled wake-up that we should assume the machine slept. Used as a
// fallback to the D-Bus sleep/wake listener.
func Drifted(scheduled, actual time.Time, sleep time.Duration) bool {
	if sleep <= 0 {
		return false
	}
	lateness := actual.Sub(scheduled)
	return lateness >= sleep // ≥ 2× total wait time = scheduled sleep + same again late
}
