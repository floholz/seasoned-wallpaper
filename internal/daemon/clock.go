package daemon

import "time"

// Clock is the daemon's view of wall time. The real implementation is a
// thin wrapper over the time package; tests inject a controllable fake.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
