package season

import (
	"strings"
	"testing"
	"time"
)

func mustParse(t *testing.T, name, date, dateRange, path string) Spec {
	t.Helper()
	s, err := Parse(name, date, dateRange, path)
	if err != nil {
		t.Fatalf("Parse(%q,%q,%q,%q): unexpected error %v", name, date, dateRange, path, err)
	}
	return s
}

func TestParse_ValidForms(t *testing.T) {
	tests := []struct {
		name     string
		date     string
		dateRng  string
		wantKind Kind
	}{
		{"annual-date", "12-25", "", KindAnnualDate},
		{"annual-date leap", "02-29", "", KindAnnualDate},
		{"specific-date", "2026-04-05", "", KindSpecificDate},
		{"annual-range", "", "12-01..12-24", KindAnnualRange},
		{"annual-range wrap", "", "12-30..01-02", KindAnnualRange},
		{"specific-range", "", "2026-03-28..2026-03-30", KindSpecificRange},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := mustParse(t, tc.name, tc.date, tc.dateRng, "/tmp/p")
			if s.Kind != tc.wantKind {
				t.Fatalf("kind = %v, want %v", s.Kind, tc.wantKind)
			}
		})
	}
}

func TestParse_Errors(t *testing.T) {
	cases := []struct {
		name   string
		date   string
		rng    string
		path   string
		substr string
	}{
		{"missing-path", "12-25", "", "", "path is required"},
		{"neither", "", "", "/p", "exactly one of date or date_range"},
		{"both", "12-25", "12-01..12-10", "/p", "exactly one of date or date_range"},
		{"bad-format", "12-32", "", "/p", "invalid month/day"},
		{"wrong-sep", "12/25", "", "/p", "MM-DD or YYYY-MM-DD"},
		{"mixed-range", "", "2026-03-01..04-01", "/p", "must both be annual or both specific"},
		{"specific-range-backwards", "", "2026-03-10..2026-03-01", "/p", "before start"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.name, tc.date, tc.rng, tc.path)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.substr) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.substr)
			}
		})
	}
}

func TestMatch_Priority(t *testing.T) {
	specific := mustParse(t, "s-specific", "2026-04-05", "", "/p/specific")
	annual := mustParse(t, "s-annual", "04-05", "", "/p/annual")
	specRange := mustParse(t, "s-sr", "", "2026-04-01..2026-04-10", "/p/sr")
	annRange := mustParse(t, "s-ar", "", "04-01..04-10", "/p/ar")

	specs := []Spec{annRange, specRange, annual, specific}
	d := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)

	got := Match(d, specs)
	if got == nil || got.Name != "s-specific" {
		t.Fatalf("expected specific-date to win, got %+v", got)
	}

	// Remove specific; annual wins.
	specs = []Spec{annRange, specRange, annual}
	got = Match(d, specs)
	if got == nil || got.Name != "s-annual" {
		t.Fatalf("expected annual-date to win, got %+v", got)
	}

	// Remove annual; specific-range wins.
	specs = []Spec{annRange, specRange}
	got = Match(d, specs)
	if got == nil || got.Name != "s-sr" {
		t.Fatalf("expected specific-range to win, got %+v", got)
	}
}

func TestMatch_WrapAroundAnnualRange(t *testing.T) {
	s := mustParse(t, "turn", "", "12-30..01-02", "/p")
	specs := []Spec{s}
	for _, day := range []time.Time{
		time.Date(2026, 12, 30, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC),
	} {
		if Match(day, specs) == nil {
			t.Errorf("expected match on %s", day.Format("2006-01-02"))
		}
	}
	for _, day := range []time.Time{
		time.Date(2026, 12, 29, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 1, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	} {
		if Match(day, specs) != nil {
			t.Errorf("unexpected match on %s", day.Format("2006-01-02"))
		}
	}
}

func TestMatch_NoMatch(t *testing.T) {
	s := mustParse(t, "xmas", "12-25", "", "/p")
	d := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if Match(d, []Spec{s}) != nil {
		t.Fatal("unexpected match")
	}
}

func TestCheckConflicts(t *testing.T) {
	tests := []struct {
		name  string
		specs []Spec
		ok    bool
	}{
		{
			name: "no conflicts",
			specs: []Spec{
				mustParse(t, "a", "12-25", "", "/p"),
				mustParse(t, "b", "01-01", "", "/p"),
				mustParse(t, "c", "", "06-01..06-15", "/p"),
				mustParse(t, "d", "", "06-16..06-30", "/p"),
			},
			ok: true,
		},
		{
			name: "duplicate annual dates",
			specs: []Spec{
				mustParse(t, "a", "12-25", "", "/p"),
				mustParse(t, "b", "12-25", "", "/p"),
			},
			ok: false,
		},
		{
			name: "duplicate specific dates",
			specs: []Spec{
				mustParse(t, "a", "2026-04-05", "", "/p"),
				mustParse(t, "b", "2026-04-05", "", "/p"),
			},
			ok: false,
		},
		{
			name: "overlapping specific ranges",
			specs: []Spec{
				mustParse(t, "a", "", "2026-03-01..2026-03-10", "/p"),
				mustParse(t, "b", "", "2026-03-05..2026-03-15", "/p"),
			},
			ok: false,
		},
		{
			name: "overlapping annual ranges",
			specs: []Spec{
				mustParse(t, "a", "", "06-01..06-15", "/p"),
				mustParse(t, "b", "", "06-10..06-20", "/p"),
			},
			ok: false,
		},
		{
			name: "annual ranges touching at boundary",
			specs: []Spec{
				mustParse(t, "a", "", "06-01..06-15", "/p"),
				mustParse(t, "b", "", "06-15..06-30", "/p"),
			},
			ok: false,
		},
		{
			name: "overlapping wrap-around annual ranges",
			specs: []Spec{
				mustParse(t, "a", "", "12-30..01-05", "/p"),
				mustParse(t, "b", "", "01-03..01-10", "/p"),
			},
			ok: false,
		},
		{
			name: "range + date in different tiers do not conflict",
			specs: []Spec{
				mustParse(t, "a", "06-10", "", "/p"),
				mustParse(t, "b", "", "06-01..06-20", "/p"),
			},
			ok: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckConflicts(tc.specs)
			if tc.ok && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("expected conflict error, got nil")
			}
		})
	}
}

func TestNextMatch(t *testing.T) {
	loc := time.UTC
	from := time.Date(2026, 4, 18, 0, 0, 0, 0, loc)

	t.Run("annual-date after today", func(t *testing.T) {
		s := mustParse(t, "xmas", "12-25", "", "/p")
		got, ok := NextMatch(&s, from)
		if !ok || got.Year() != 2026 || got.Month() != 12 || got.Day() != 25 {
			t.Fatalf("got %v ok=%v", got, ok)
		}
	})

	t.Run("annual-date already passed this year rolls to next year", func(t *testing.T) {
		s := mustParse(t, "easter", "03-01", "", "/p")
		got, ok := NextMatch(&s, from)
		if !ok || got.Year() != 2027 {
			t.Fatalf("got %v ok=%v", got, ok)
		}
	})

	t.Run("annual-date today returns today", func(t *testing.T) {
		s := mustParse(t, "today", "04-18", "", "/p")
		got, ok := NextMatch(&s, from)
		if !ok || !got.Equal(from) {
			t.Fatalf("got %v ok=%v want %v", got, ok, from)
		}
	})

	t.Run("specific-date past returns false", func(t *testing.T) {
		s := mustParse(t, "old", "2025-01-01", "", "/p")
		if _, ok := NextMatch(&s, from); ok {
			t.Fatal("expected past")
		}
	})

	t.Run("specific-range currently in progress returns from", func(t *testing.T) {
		s := mustParse(t, "now", "", "2026-04-10..2026-04-25", "/p")
		got, ok := NextMatch(&s, from)
		if !ok || !got.Equal(from) {
			t.Fatalf("got %v ok=%v", got, ok)
		}
	})
}
