package timerange

import (
	"strings"
	"testing"
	"time"
)

func at(h, m int) time.Time { return time.Date(2026, 6, 15, h, m, 0, 0, time.UTC) }

func mustParse(t *testing.T, r string) Spec {
	t.Helper()
	start, end, err := ParseRange(r)
	if err != nil {
		t.Fatal(err)
	}
	return Spec{Start: start, End: end}
}

func TestParseRange_Invalid(t *testing.T) {
	cases := []struct{ rng, want string }{
		{"20:00", "expected HH:MM..HH:MM"},
		{"20:00..06:00..07:00", "expected HH:MM..HH:MM"},
		{"24:00..06:00", "invalid time_range start"},
		{"20:00..06:60", "invalid time_range end"},
		{"08:00..8:00", "is empty"},
	}
	for _, c := range cases {
		_, _, err := ParseRange(c.rng)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("ParseRange(%q) err = %v, want containing %q", c.rng, err, c.want)
		}
	}
}

func TestContains_HalfOpenAndWrap(t *testing.T) {
	day := mustParse(t, "06:00..20:00")
	night := mustParse(t, "20:00..06:00")
	cases := []struct {
		t          time.Time
		inDay, inN bool
	}{
		{at(5, 59), false, true},
		{at(6, 0), true, false},
		{at(12, 0), true, false},
		{at(19, 59), true, false},
		{at(20, 0), false, true},
		{at(0, 0), false, true},
		{at(23, 59), false, true},
	}
	for _, c := range cases {
		if got := day.Contains(c.t); got != c.inDay {
			t.Errorf("day.Contains(%s) = %v", c.t.Format("15:04"), got)
		}
		if got := night.Contains(c.t); got != c.inN {
			t.Errorf("night.Contains(%s) = %v", c.t.Format("15:04"), got)
		}
	}
}

func TestOverlaps(t *testing.T) {
	night := mustParse(t, "20:00..06:00")
	day := mustParse(t, "06:00..20:00")
	late := mustParse(t, "23:00..01:00")
	if Overlaps(&night, &day) {
		t.Error("adjacent ranges reported as overlapping")
	}
	if !Overlaps(&night, &late) {
		t.Error("nested wrap-around range not reported as overlapping")
	}
}

func TestNextBoundary(t *testing.T) {
	specs := []Spec{mustParse(t, "20:00..06:00")}
	cases := []struct{ now, want time.Time }{
		{at(12, 0), at(20, 0)},
		{at(20, 0), at(6, 0).AddDate(0, 0, 1)}, // strictly after now
		{at(3, 0), at(6, 0)},
		{at(22, 0), at(6, 0).AddDate(0, 0, 1)},
	}
	for _, c := range cases {
		got, ok := NextBoundary(c.now, specs)
		if !ok || !got.Equal(c.want) {
			t.Errorf("NextBoundary(%s) = %s, want %s", c.now.Format(time.RFC3339), got.Format(time.RFC3339), c.want.Format(time.RFC3339))
		}
	}
	if _, ok := NextBoundary(at(12, 0), nil); ok {
		t.Error("expected ok=false with no ranges")
	}
}
