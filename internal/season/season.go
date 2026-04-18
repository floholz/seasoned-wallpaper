// Package season parses user-defined date specifications and resolves which
// one wins for a given date.
//
// Priority tiers (lower number wins):
//  1. Specific date       (YYYY-MM-DD)
//  2. Annual date         (MM-DD)
//  3. Specific date range (YYYY-MM-DD..YYYY-MM-DD)
//  4. Annual date range   (MM-DD..MM-DD)
package season

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Kind int

const (
	KindAnnualDate Kind = iota + 1
	KindSpecificDate
	KindAnnualRange
	KindSpecificRange
)

func (k Kind) String() string {
	switch k {
	case KindSpecificDate:
		return "specific-date"
	case KindAnnualDate:
		return "annual-date"
	case KindSpecificRange:
		return "specific-range"
	case KindAnnualRange:
		return "annual-range"
	}
	return "unknown"
}

// priority returns the tier per SPEC.md; lower wins.
func (k Kind) priority() int {
	switch k {
	case KindSpecificDate:
		return 1
	case KindAnnualDate:
		return 2
	case KindSpecificRange:
		return 3
	case KindAnnualRange:
		return 4
	}
	return 99
}

// Spec is a parsed, validated season entry. For single dates, Start and End
// hold the same value. Year is 0 for recurring (annual) kinds.
type Spec struct {
	Name string
	Path string
	Kind Kind

	StartYear, StartMonth, StartDay int
	EndYear, EndMonth, EndDay       int
}

// Parse turns a raw YAML season entry into a typed Spec.
// Exactly one of date or dateRange must be non-empty.
func Parse(name, date, dateRange, path string) (Spec, error) {
	disp := displayName(name)
	if path == "" {
		return Spec{}, fmt.Errorf("season %s: path is required", disp)
	}
	haveDate := date != ""
	haveRange := dateRange != ""
	if haveDate == haveRange {
		return Spec{}, fmt.Errorf("season %s: exactly one of date or date_range is required", disp)
	}

	s := Spec{Name: name, Path: path}

	if haveDate {
		y, m, d, annual, err := parseOneDate(date)
		if err != nil {
			return Spec{}, fmt.Errorf("season %s: invalid date %q: %w", disp, date, err)
		}
		s.StartYear, s.StartMonth, s.StartDay = y, m, d
		s.EndYear, s.EndMonth, s.EndDay = y, m, d
		if annual {
			s.Kind = KindAnnualDate
		} else {
			s.Kind = KindSpecificDate
		}
		return s, nil
	}

	parts := strings.Split(dateRange, "..")
	if len(parts) != 2 {
		return Spec{}, fmt.Errorf("season %s: invalid date_range %q: expected start..end", disp, dateRange)
	}
	sy, sm, sd, sAnnual, err := parseOneDate(parts[0])
	if err != nil {
		return Spec{}, fmt.Errorf("season %s: invalid date_range start %q: %w", disp, parts[0], err)
	}
	ey, em, ed, eAnnual, err := parseOneDate(parts[1])
	if err != nil {
		return Spec{}, fmt.Errorf("season %s: invalid date_range end %q: %w", disp, parts[1], err)
	}
	if sAnnual != eAnnual {
		return Spec{}, fmt.Errorf("season %s: date_range endpoints must both be annual or both specific", disp)
	}

	s.StartYear, s.StartMonth, s.StartDay = sy, sm, sd
	s.EndYear, s.EndMonth, s.EndDay = ey, em, ed

	if sAnnual {
		s.Kind = KindAnnualRange
	} else {
		s.Kind = KindSpecificRange
		start := time.Date(sy, time.Month(sm), sd, 0, 0, 0, 0, time.Local)
		end := time.Date(ey, time.Month(em), ed, 0, 0, 0, 0, time.Local)
		if end.Before(start) {
			return Spec{}, fmt.Errorf("season %s: date_range end %q is before start %q", disp, parts[1], parts[0])
		}
	}
	return s, nil
}

// Match returns the highest-priority Spec that matches t, or nil if none match.
func Match(t time.Time, specs []Spec) *Spec {
	var best *Spec
	for i := range specs {
		s := &specs[i]
		if !matches(s, t) {
			continue
		}
		if best == nil || s.Kind.priority() < best.Kind.priority() {
			best = s
		}
	}
	return best
}

// NextMatch returns the next date (>= from, truncated to midnight in
// from.Location()) at which s begins to match, or ok=false if s will never
// match again.
func NextMatch(s *Spec, from time.Time) (time.Time, bool) {
	loc := from.Location()
	from = truncDay(from)
	mkDate := func(y, m, d int) time.Time {
		return time.Date(y, time.Month(m), d, 0, 0, 0, 0, loc)
	}
	switch s.Kind {
	case KindSpecificDate:
		d := mkDate(s.StartYear, s.StartMonth, s.StartDay)
		if d.Before(from) {
			return time.Time{}, false
		}
		return d, true

	case KindAnnualDate:
		for y := from.Year(); y <= from.Year()+1; y++ {
			d := mkDate(y, s.StartMonth, s.StartDay)
			if !d.Before(from) {
				return d, true
			}
		}
		return time.Time{}, false

	case KindSpecificRange:
		start := mkDate(s.StartYear, s.StartMonth, s.StartDay)
		end := mkDate(s.EndYear, s.EndMonth, s.EndDay)
		if end.Before(from) {
			return time.Time{}, false
		}
		if start.Before(from) {
			return from, true
		}
		return start, true

	case KindAnnualRange:
		// Already inside a range (possibly the tail of a wrap-around).
		if matches(s, from) {
			return from, true
		}
		for y := from.Year(); y <= from.Year()+1; y++ {
			d := mkDate(y, s.StartMonth, s.StartDay)
			if !d.Before(from) {
				return d, true
			}
		}
		return time.Time{}, false
	}
	return time.Time{}, false
}

// CheckConflicts reports a config error if any two specs in the same priority
// tier could match the same calendar date.
func CheckConflicts(specs []Spec) error {
	byKind := map[Kind][]*Spec{}
	for i := range specs {
		byKind[specs[i].Kind] = append(byKind[specs[i].Kind], &specs[i])
	}

	// Tier 1: specific dates — exact match.
	list1 := byKind[KindSpecificDate]
	for i := 0; i < len(list1); i++ {
		for j := i + 1; j < len(list1); j++ {
			a, b := list1[i], list1[j]
			if a.StartYear == b.StartYear && a.StartMonth == b.StartMonth && a.StartDay == b.StartDay {
				return fmt.Errorf("config: seasons %s and %s both pin %04d-%02d-%02d",
					displayName(a.Name), displayName(b.Name), a.StartYear, a.StartMonth, a.StartDay)
			}
		}
	}

	// Tier 2: annual dates — exact MM-DD.
	list2 := byKind[KindAnnualDate]
	for i := 0; i < len(list2); i++ {
		for j := i + 1; j < len(list2); j++ {
			a, b := list2[i], list2[j]
			if a.StartMonth == b.StartMonth && a.StartDay == b.StartDay {
				return fmt.Errorf("config: seasons %s and %s both pin annual %02d-%02d",
					displayName(a.Name), displayName(b.Name), a.StartMonth, a.StartDay)
			}
		}
	}

	// Tier 3: specific ranges — interval intersect.
	list3 := byKind[KindSpecificRange]
	for i := 0; i < len(list3); i++ {
		aStart := time.Date(list3[i].StartYear, time.Month(list3[i].StartMonth), list3[i].StartDay, 0, 0, 0, 0, time.Local)
		aEnd := time.Date(list3[i].EndYear, time.Month(list3[i].EndMonth), list3[i].EndDay, 0, 0, 0, 0, time.Local)
		for j := i + 1; j < len(list3); j++ {
			bStart := time.Date(list3[j].StartYear, time.Month(list3[j].StartMonth), list3[j].StartDay, 0, 0, 0, 0, time.Local)
			bEnd := time.Date(list3[j].EndYear, time.Month(list3[j].EndMonth), list3[j].EndDay, 0, 0, 0, 0, time.Local)
			if !aEnd.Before(bStart) && !bEnd.Before(aStart) {
				return fmt.Errorf("config: seasons %s and %s have overlapping specific ranges",
					displayName(list3[i].Name), displayName(list3[j].Name))
			}
		}
	}

	// Tier 4: annual ranges — enumerate day-of-year sets (year 2000 is leap).
	list4 := byKind[KindAnnualRange]
	sets := make([]map[int]bool, len(list4))
	for i, s := range list4 {
		sets[i] = annualRangeDays(s.StartMonth, s.StartDay, s.EndMonth, s.EndDay)
	}
	for i := 0; i < len(list4); i++ {
		for j := i + 1; j < len(list4); j++ {
			for day := range sets[i] {
				if sets[j][day] {
					return fmt.Errorf("config: seasons %s and %s have overlapping annual ranges",
						displayName(list4[i].Name), displayName(list4[j].Name))
				}
			}
		}
	}
	return nil
}

func matches(s *Spec, t time.Time) bool {
	y, m, d := t.Year(), int(t.Month()), t.Day()
	switch s.Kind {
	case KindSpecificDate:
		return y == s.StartYear && m == s.StartMonth && d == s.StartDay
	case KindAnnualDate:
		return m == s.StartMonth && d == s.StartDay
	case KindSpecificRange:
		return cmpYMD(y, m, d, s.StartYear, s.StartMonth, s.StartDay) >= 0 &&
			cmpYMD(y, m, d, s.EndYear, s.EndMonth, s.EndDay) <= 0
	case KindAnnualRange:
		return annualRangeContains(s.StartMonth, s.StartDay, s.EndMonth, s.EndDay, m, d)
	}
	return false
}

func cmpYMD(ay, am, ad, by, bm, bd int) int {
	if ay != by {
		if ay < by {
			return -1
		}
		return 1
	}
	return cmpMD(am, ad, bm, bd)
}

func annualRangeContains(sm, sd, em, ed, m, d int) bool {
	if cmpMD(sm, sd, em, ed) <= 0 {
		return cmpMD(m, d, sm, sd) >= 0 && cmpMD(m, d, em, ed) <= 0
	}
	// wraps year boundary
	return cmpMD(m, d, sm, sd) >= 0 || cmpMD(m, d, em, ed) <= 0
}

func cmpMD(am, ad, bm, bd int) int {
	if am != bm {
		if am < bm {
			return -1
		}
		return 1
	}
	if ad < bd {
		return -1
	}
	if ad > bd {
		return 1
	}
	return 0
}

func annualRangeDays(sm, sd, em, ed int) map[int]bool {
	out := map[int]bool{}
	wrap := cmpMD(sm, sd, em, ed) > 0
	t := time.Date(2000, time.Month(sm), sd, 0, 0, 0, 0, time.UTC)
	if !wrap {
		end := time.Date(2000, time.Month(em), ed, 0, 0, 0, 0, time.UTC)
		for !t.After(end) {
			out[int(t.Month())*100+t.Day()] = true
			t = t.AddDate(0, 0, 1)
		}
		return out
	}
	yearEnd := time.Date(2000, 12, 31, 0, 0, 0, 0, time.UTC)
	for !t.After(yearEnd) {
		out[int(t.Month())*100+t.Day()] = true
		t = t.AddDate(0, 0, 1)
	}
	t = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2000, time.Month(em), ed, 0, 0, 0, 0, time.UTC)
	for !t.After(end) {
		out[int(t.Month())*100+t.Day()] = true
		t = t.AddDate(0, 0, 1)
	}
	return out
}

func parseOneDate(s string) (year, month, day int, annual bool, err error) {
	parts := strings.Split(s, "-")
	switch len(parts) {
	case 2: // MM-DD
		if len(parts[0]) != 2 || len(parts[1]) != 2 {
			return 0, 0, 0, false, fmt.Errorf("expected MM-DD")
		}
		m, mErr := strconv.Atoi(parts[0])
		d, dErr := strconv.Atoi(parts[1])
		if mErr != nil || dErr != nil {
			return 0, 0, 0, false, fmt.Errorf("expected MM-DD")
		}
		if !validMonthDay(m, d, 2000) { // leap year permits 02-29
			return 0, 0, 0, false, fmt.Errorf("invalid month/day")
		}
		return 0, m, d, true, nil
	case 3: // YYYY-MM-DD
		if len(parts[0]) != 4 || len(parts[1]) != 2 || len(parts[2]) != 2 {
			return 0, 0, 0, false, fmt.Errorf("expected YYYY-MM-DD")
		}
		y, yErr := strconv.Atoi(parts[0])
		m, mErr := strconv.Atoi(parts[1])
		d, dErr := strconv.Atoi(parts[2])
		if yErr != nil || mErr != nil || dErr != nil {
			return 0, 0, 0, false, fmt.Errorf("expected YYYY-MM-DD")
		}
		if !validMonthDay(m, d, y) {
			return 0, 0, 0, false, fmt.Errorf("invalid month/day")
		}
		return y, m, d, false, nil
	}
	return 0, 0, 0, false, fmt.Errorf("expected MM-DD or YYYY-MM-DD")
}

func validMonthDay(m, d, year int) bool {
	if m < 1 || m > 12 || d < 1 {
		return false
	}
	t := time.Date(year, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	return int(t.Month()) == m && t.Day() == d && t.Year() == year
}

func displayName(n string) string {
	if n == "" {
		return "<unnamed>"
	}
	return strconv.Quote(n)
}

func truncDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}
