// Package timerange parses daily clock-time windows ("HH:MM..HH:MM"), used
// to restrict a season to part of the day — e.g. darker wallpapers at night.
//
// Ranges are half-open: the start minute is included, the end minute is
// not. "20:00..06:00" is active from 20:00 up to (but excluding) 06:00 the
// next morning; ranges whose end is earlier than their start wrap midnight.
package timerange

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Spec is a parsed, validated daily window. Start and End are offsets from
// local midnight with minute resolution.
type Spec struct {
	Start time.Duration
	End   time.Duration
}

// String renders the range back as "HH:MM..HH:MM".
func (s Spec) String() string {
	return FormatClock(s.Start) + ".." + FormatClock(s.End)
}

// ParseRange parses "HH:MM..HH:MM" into start and end offsets from
// midnight. Equal start and end (an empty range) is rejected.
func ParseRange(s string) (start, end time.Duration, err error) {
	parts := strings.Split(s, "..")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time_range %q: expected HH:MM..HH:MM", s)
	}
	if start, err = ParseClock(parts[0]); err != nil {
		return 0, 0, fmt.Errorf("invalid time_range start %q: %w", parts[0], err)
	}
	if end, err = ParseClock(parts[1]); err != nil {
		return 0, 0, fmt.Errorf("invalid time_range end %q: %w", parts[1], err)
	}
	if start == end {
		return 0, 0, fmt.Errorf("time_range %q is empty (start equals end); omit time_range for all day", s)
	}
	return start, end, nil
}

// Overlaps reports whether a and b share at least one minute of the day.
func Overlaps(a, b *Spec) bool {
	for m := time.Duration(0); m < 24*time.Hour; m += time.Minute {
		if a.containsOffset(m) && b.containsOffset(m) {
			return true
		}
	}
	return false
}

// Contains reports whether t's local clock time falls inside s.
func (s *Spec) Contains(t time.Time) bool {
	return s.containsOffset(clockOffset(t))
}

func (s *Spec) containsOffset(off time.Duration) bool {
	if s.Start < s.End {
		return off >= s.Start && off < s.End
	}
	// wraps midnight
	return off >= s.Start || off < s.End
}

// NextBoundary returns the earliest moment strictly after now at which any
// window starts or ends — i.e. the next time the active source may change.
// ok is false when specs is empty.
func NextBoundary(now time.Time, specs []Spec) (time.Time, bool) {
	if len(specs) == 0 {
		return time.Time{}, false
	}
	loc := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	tomorrow := today.AddDate(0, 0, 1)

	var best time.Time
	consider := func(t time.Time) {
		if t.After(now) && (best.IsZero() || t.Before(best)) {
			best = t
		}
	}
	for _, s := range specs {
		for _, off := range []time.Duration{s.Start, s.End} {
			consider(today.Add(off))
			consider(tomorrow.Add(off))
		}
	}
	return best, !best.IsZero()
}

// ParseClock parses "HH:MM" (0-23 : 0-59) into an offset from midnight.
// Accepts "6:00" as well as "06:00".
func ParseClock(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	colon := strings.IndexByte(s, ':')
	if colon <= 0 || colon == len(s)-1 {
		return 0, errors.New("expected HH:MM")
	}
	h, err := parseBoundedInt(s[:colon], 0, 23)
	if err != nil {
		return 0, fmt.Errorf("hour %w", err)
	}
	m, err := parseBoundedInt(s[colon+1:], 0, 59)
	if err != nil {
		return 0, fmt.Errorf("minute %w", err)
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute, nil
}

// FormatClock renders an offset from midnight as "HH:MM".
func FormatClock(d time.Duration) string {
	h := int(d / time.Hour)
	m := int((d % time.Hour) / time.Minute)
	return fmt.Sprintf("%02d:%02d", h, m)
}

// clockOffset is t's wall-clock time of day. Built from the clock fields
// rather than t.Sub(midnight) so DST transitions don't skew it.
func clockOffset(t time.Time) time.Duration {
	h, m, s := t.Clock()
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(s)*time.Second
}

func parseBoundedInt(s string, lo, hi int) (int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("missing value")
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("%q is not a number", s)
		}
		n = n*10 + int(c-'0')
		if n > hi {
			return 0, fmt.Errorf("%q out of range [%d, %d]", s, lo, hi)
		}
	}
	if n < lo {
		return 0, fmt.Errorf("%q out of range [%d, %d]", s, lo, hi)
	}
	return n, nil
}
