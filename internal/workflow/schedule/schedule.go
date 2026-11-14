// Package schedule turns a workflow schedule (a preset or a 5-field cron
// line, in an IANA time zone) into run times, and fires due workflows
// (Scheduler). It is pure: no files, no processes; the caller maps its
// file format to Spec and injects the clock, the precheck and the run.
//
// DST rules, the same as classic cron:
//   - A fixed-time slot (the hour field is not *) that falls in a
//     spring-forward gap runs once, at the change (02:30 → 03:00).
//   - A fixed-time slot in a fall-back overlap runs once, at its first
//     occurrence.
//   - A schedule whose hour field is * (hourly) follows real time: gap
//     slots do not exist, overlap slots run at both occurrences.
package schedule

import (
	"errors"
	"fmt"
	"math/bits"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Spec is a schedule as the user wrote it. Exactly one of Preset and
// Cron is set.
type Spec struct {
	// Preset: hourly, daily, weekdays, weekly or monthly.
	Preset string
	// Time: "HH:MM" (daily, weekdays, weekly, monthly), ":MM" or "MM"
	// for hourly (default :00).
	Time string
	// Day: the weekday for weekly ("mon".."sun", default mon), the day of
	// the month for monthly ("1".."28", default 1; every month has it).
	Day string
	// Cron: "min hour day-of-month month day-of-week".
	Cron string
	// TZ: an IANA zone such as "Europe/Berlin"; "" is the machine's zone.
	TZ string
}

// MaxTextLen caps a schedule text and a cron line.
const MaxTextLen = 200

// ParseText reads the one-line form: "hourly [:MM]", "daily HH:MM",
// "weekdays HH:MM", "weekly <day> HH:MM", "monthly <1-28> HH:MM", or a
// 5-field cron line (optionally after "cron"). The zone is not part of
// the text. The result still needs Compile.
func ParseText(s string) (Spec, error) {
	if len(s) > MaxTextLen {
		return Spec{}, fmt.Errorf("schedule is longer than %d characters", MaxTextLen)
	}
	f := strings.Fields(s)
	if len(f) == 0 {
		return Spec{}, errors.New("schedule is empty")
	}
	head := strings.ToLower(f[0])
	if head == "cron" {
		f = f[1:]
		head = ""
	}
	switch head {
	case "hourly":
		switch len(f) {
		case 1:
			return Spec{Preset: head}, nil
		case 2:
			return Spec{Preset: head, Time: f[1]}, nil
		}
		return Spec{}, errors.New(`use "hourly" or "hourly :MM"`)
	case "daily", "weekdays":
		if len(f) != 2 {
			return Spec{}, fmt.Errorf(`use "%s HH:MM"`, head)
		}
		return Spec{Preset: head, Time: f[1]}, nil
	case "weekly", "monthly":
		if len(f) != 3 {
			if head == "weekly" {
				return Spec{}, errors.New(`use "weekly <day> HH:MM", for example "weekly mon 09:00"`)
			}
			return Spec{}, errors.New(`use "monthly <1-28> HH:MM", for example "monthly 1 09:00"`)
		}
		return Spec{Preset: head, Day: f[1], Time: f[2]}, nil
	}
	if len(f) != 5 {
		return Spec{}, errors.New(`use a preset (hourly, daily, weekdays, weekly, monthly) or 5 cron fields "min hour day month weekday"`)
	}
	return Spec{Cron: strings.Join(f, " ")}, nil
}

// Schedule is a compiled Spec. It is safe for concurrent use.
type Schedule struct {
	spec    Spec
	loc     *time.Location
	minute  uint64 // bit n: minute n
	hour    uint64
	dom     uint64 // bit n: day n (1..31)
	month   uint64 // bit n: month n (1..12)
	dow     uint64 // bit n: weekday n (0 = Sunday)
	domStar bool
	dowStar bool
	// hourStar: the hour field is *, so the schedule follows real time
	// across DST (see the package doc).
	hourStar bool
}

// Compile checks a Spec and builds its Schedule.
func Compile(sp Spec) (*Schedule, error) {
	var line string
	switch {
	case sp.Preset != "" && sp.Cron != "":
		return nil, errors.New("schedule has both a preset and cron; use one")
	case sp.Preset != "":
		l, err := presetCron(sp)
		if err != nil {
			return nil, err
		}
		line = l
	case sp.Cron != "":
		line = sp.Cron
	default:
		return nil, errors.New("schedule needs a preset or cron")
	}
	s, err := parseCron(line)
	if err != nil {
		return nil, err
	}
	loc, err := loadZone(sp.TZ)
	if err != nil {
		return nil, err
	}
	s.spec, s.loc = sp, loc
	return s, nil
}

// Spec returns the schedule as written.
func (s *Schedule) Spec() Spec { return s.spec }

// Location is the schedule's time zone; Next returns times in it.
func (s *Schedule) Location() *time.Location { return s.loc }

func loadZone(tz string) (*time.Location, error) {
	if tz == "" || tz == "Local" {
		return time.Local, nil
	}
	if len(tz) > 64 || strings.ContainsAny(tz, " \\") || strings.Contains(tz, "..") || strings.HasPrefix(tz, "/") {
		return nil, fmt.Errorf("time zone %q is not an IANA zone like Europe/Berlin", tz)
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("time zone %q is not an IANA zone like Europe/Berlin", tz)
	}
	return loc, nil
}

var weekdays = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

var months = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

func presetCron(sp Spec) (string, error) {
	p := strings.ToLower(sp.Preset)
	if p == "hourly" {
		if sp.Day != "" {
			return "", errors.New("hourly takes no day")
		}
		m := 0
		if sp.Time != "" {
			v, err := strconv.Atoi(strings.TrimPrefix(sp.Time, ":"))
			if err != nil || v < 0 || v > 59 || len(sp.Time) > 3 {
				return "", fmt.Errorf("hourly time %q: use :MM, for example :15", sp.Time)
			}
			m = v
		}
		return fmt.Sprintf("%d * * * *", m), nil
	}
	h, m, err := clockTime(sp.Time)
	if err != nil {
		return "", fmt.Errorf("%s: %w", p, err)
	}
	switch p {
	case "daily", "weekdays":
		if sp.Day != "" {
			return "", fmt.Errorf("%s takes no day", p)
		}
		if p == "weekdays" {
			return fmt.Sprintf("%d %d * * 1-5", m, h), nil
		}
		return fmt.Sprintf("%d %d * * *", m, h), nil
	case "weekly":
		d := 1
		if sp.Day != "" {
			v, ok := weekdays[strings.ToLower(sp.Day)]
			if !ok {
				return "", fmt.Errorf("weekly day %q: use mon, tue, wed, thu, fri, sat or sun", sp.Day)
			}
			d = v
		}
		return fmt.Sprintf("%d %d * * %d", m, h, d), nil
	case "monthly":
		d := 1
		if sp.Day != "" {
			v, err := strconv.Atoi(sp.Day)
			if err != nil || v < 1 || v > 28 {
				return "", fmt.Errorf("monthly day %q: use 1 to 28 (every month has them); cron can do more", sp.Day)
			}
			d = v
		}
		return fmt.Sprintf("%d %d %d * *", m, h, d), nil
	}
	return "", fmt.Errorf("preset %q: use hourly, daily, weekdays, weekly or monthly", sp.Preset)
}

func clockTime(s string) (h, m int, err error) {
	bad := fmt.Errorf("time %q: use HH:MM, for example 09:00", s)
	hs, ms, ok := strings.Cut(s, ":")
	if !ok || len(hs) < 1 || len(hs) > 2 || len(ms) != 2 {
		return 0, 0, bad
	}
	h, err1 := strconv.Atoi(hs)
	m, err2 := strconv.Atoi(ms)
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 || hs[0] == '+' || hs[0] == '-' {
		return 0, 0, bad
	}
	return h, m, nil
}

func parseCron(line string) (*Schedule, error) {
	if len(line) > MaxTextLen {
		return nil, fmt.Errorf("cron is longer than %d characters", MaxTextLen)
	}
	f := strings.Fields(line)
	if len(f) != 5 {
		return nil, fmt.Errorf("cron %q: needs 5 fields: min hour day month weekday", line)
	}
	s := &Schedule{}
	var err error
	if s.minute, err = cronField(f[0], 0, 59, nil, "minute"); err != nil {
		return nil, err
	}
	if s.hour, err = cronField(f[1], 0, 23, nil, "hour"); err != nil {
		return nil, err
	}
	if s.dom, err = cronField(f[2], 1, 31, nil, "day of month"); err != nil {
		return nil, err
	}
	if s.month, err = cronField(f[3], 1, 12, months, "month"); err != nil {
		return nil, err
	}
	if s.dow, err = cronField(f[4], 0, 7, weekdays, "weekday"); err != nil {
		return nil, err
	}
	if s.dow&(1<<7) != 0 { // 7 is Sunday too
		s.dow = s.dow&^(1<<7) | 1
	}
	s.hourStar = strings.HasPrefix(f[1], "*") && s.hour == 1<<24-1
	s.domStar = strings.HasPrefix(f[2], "*")
	s.dowStar = strings.HasPrefix(f[4], "*")
	if !s.canRun() {
		return nil, fmt.Errorf("cron %q never runs (no month has that day)", line)
	}
	return s, nil
}

// cronField parses one field into a bit set over [lo, hi].
func cronField(field string, lo, hi int, names map[string]int, what string) (uint64, error) {
	bad := func(why string) (uint64, error) {
		return 0, fmt.Errorf("cron %s %q: %s", what, field, why)
	}
	var set uint64
	for _, part := range strings.Split(field, ",") {
		if part == "" {
			return bad("empty list item")
		}
		rng, stepS, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			v, err := strconv.Atoi(stepS)
			if err != nil || v < 1 || v > hi-lo+1 || stepS[0] == '+' {
				return bad(fmt.Sprintf("step must be 1 to %d", hi-lo+1))
			}
			step = v
		}
		var a, b int
		if rng == "*" {
			a, b = lo, hi
			if what == "weekday" {
				b = 6
			}
		} else {
			as, bs, isRange := strings.Cut(rng, "-")
			var err error
			if a, err = cronValue(as, lo, hi, names); err != nil {
				return bad(err.Error())
			}
			b = a
			if isRange {
				if b, err = cronValue(bs, lo, hi, names); err != nil {
					return bad(err.Error())
				}
				if b < a {
					return bad("range goes backwards")
				}
			} else if hasStep {
				b = hi // "a/n" means a..max every n
			}
		}
		for v := a; v <= b; v += step {
			set |= 1 << uint(v)
		}
	}
	return set, nil
}

func cronValue(s string, lo, hi int, names map[string]int) (int, error) {
	if v, ok := names[strings.ToLower(s)]; ok {
		return v, nil
	}
	if s == "" || len(s) > 2 || s[0] < '0' || s[0] > '9' {
		return 0, fmt.Errorf("%q is not a number from %d to %d", s, lo, hi)
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < lo || v > hi {
		return 0, fmt.Errorf("%q is not a number from %d to %d", s, lo, hi)
	}
	return v, nil
}

// canRun: some month in the set has some day in the set (when only the
// day of the month limits the days).
func (s *Schedule) canRun() bool {
	if s.domStar || !s.dowStar {
		return true
	}
	maxDay := [13]int{0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	first := bits.TrailingZeros64(s.dom)
	for m := 1; m <= 12; m++ {
		if s.month&(1<<uint(m)) != 0 && first <= maxDay[m] {
			return true
		}
	}
	return false
}

func (s *Schedule) dayMatches(month time.Month, day int, wd time.Weekday) bool {
	if s.month&(1<<uint(month)) == 0 {
		return false
	}
	domOK := s.dom&(1<<uint(day)) != 0
	dowOK := s.dow&(1<<uint(wd)) != 0
	if s.domStar || s.dowStar {
		return domOK && dowOK
	}
	return domOK || dowOK
}

// matches: the local wall time w is one of the schedule's slots.
func (s *Schedule) matches(w time.Time) bool {
	return w.Second() == 0 && w.Nanosecond() == 0 &&
		s.minute&(1<<uint(w.Minute())) != 0 && s.hour&(1<<uint(w.Hour())) != 0 &&
		s.dayMatches(w.Month(), w.Day(), w.Weekday())
}

// inGap: w is the instant of a spring-forward change (where gap slots run).
func (s *Schedule) inGap(w time.Time) bool {
	start, _ := w.ZoneBounds()
	if !w.Equal(start) {
		return false
	}
	_, before := w.Add(-time.Second).Zone()
	_, after := w.Zone()
	return after > before
}

// maxDays is how far Next looks: the Gregorian calendar repeats every
// 400 years (146097 days, a whole number of weeks), so a day and weekday
// pair ("March 22 on a Sunday", 11 years apart; Feb 29 on a Monday, up
// to 40) that ever comes again comes within it. Only such rare schedules
// walk that far; each day is a few bit tests.
const maxDays = 146097 + 2

// Next returns the first run time strictly after `after`, in the
// schedule's zone, or the zero time when there is none.
func (s *Schedule) Next(after time.Time) time.Time {
	local := after.In(s.loc)
	// Day d-1 too: a slot of the previous wall day can still be ahead in
	// a fall-back overlap.
	y, m, d := local.Date()
	var best time.Time
	foundAt := -1
	for i := -1; i < maxDays; i++ {
		day := time.Date(y, m, d+i, 0, 0, 0, 0, time.UTC) // calendar only
		if !s.dayMatches(day.Month(), day.Day(), day.Weekday()) {
			continue
		}
		if t, ok := s.dayFirst(day, after); ok && (best.IsZero() || t.Before(best)) {
			best = t
			if foundAt < 0 {
				foundAt = i
			}
		}
		// One more calendar day after the first hit: a DST change near
		// midnight can make wall order and real order differ.
		if foundAt >= 0 && i >= foundAt+1 {
			break
		}
	}
	return best
}

// dayFirst is the earliest run instant of the wall day `day` after `after`.
func (s *Schedule) dayFirst(day, after time.Time) (time.Time, bool) {
	var best time.Time
	found := false
	for h := 0; h < 24; h++ {
		if s.hour&(1<<uint(h)) == 0 {
			continue
		}
		for mi := 0; mi < 60; mi++ {
			if s.minute&(1<<uint(mi)) == 0 {
				continue
			}
			wall := day.Add(time.Duration(h)*time.Hour + time.Duration(mi)*time.Minute)
			// The real instant is within wall-14h .. wall+12h.
			if !wall.Add(13 * time.Hour).After(after) {
				continue
			}
			if found && wall.Add(-15*time.Hour).After(best) {
				return best, true
			}
			for _, t := range s.instants(wall) {
				if t.After(after) && (!found || t.Before(best)) {
					best, found = t, true
				}
			}
		}
	}
	return best, found
}

// instants maps a wall time (given as UTC fields) to the run instants
// in the zone, by the DST rules in the package doc.
func (s *Schedule) instants(wall time.Time) []time.Time {
	var offs []int
	for k := -2; k <= 2; k++ {
		_, off := wall.Add(time.Duration(k) * 12 * time.Hour).In(s.loc).Zone()
		dup := false
		for _, o := range offs {
			dup = dup || o == off
		}
		if !dup {
			offs = append(offs, off)
		}
	}
	var valid []time.Time
	var latest time.Time
	for _, off := range offs {
		t := wall.Add(-time.Duration(off) * time.Second).In(s.loc)
		if t.After(latest) {
			latest = t
		}
		y, m, d := t.Date()
		if y == wall.Year() && m == wall.Month() && d == wall.Day() && t.Hour() == wall.Hour() && t.Minute() == wall.Minute() {
			valid = append(valid, t)
		}
	}
	sort.Slice(valid, func(i, j int) bool { return valid[i].Before(valid[j]) })
	switch {
	case len(valid) == 0: // spring-forward gap
		if s.hourStar {
			return nil
		}
		start, _ := latest.ZoneBounds()
		return []time.Time{start.In(s.loc)}
	case len(valid) > 1 && !s.hourStar: // fall-back overlap: first only
		return valid[:1]
	}
	return valid
}
