package daemon

import "time"

// NextWake returns the earlier of:
//
//   - the next local midnight after now (so today's pick is re-evaluated
//     when the date rolls)
//   - now + refresh (safety net against clock drift and missed wake-ups).
//
// Season boundaries always coincide with a midnight, so they don't need to
// be enumerated separately. This keeps the scheduler trivially correct.
func NextWake(now time.Time, refresh time.Duration) time.Time {
	midnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	if refresh <= 0 {
		return midnight
	}
	refreshAt := now.Add(refresh)
	if refreshAt.Before(midnight) {
		return refreshAt
	}
	return midnight
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
