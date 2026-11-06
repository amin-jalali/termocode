package schedule

import (
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // the DST tables must not depend on the runner's zoneinfo
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("bad time %q: %v", s, err)
	}
	return v
}

func TestParseText(t *testing.T) {
	ok := []struct {
		in   string
		want Spec
	}{
		{"hourly", Spec{Preset: "hourly"}},
		{"hourly :15", Spec{Preset: "hourly", Time: ":15"}},
		{"daily 09:00", Spec{Preset: "daily", Time: "09:00"}},
		{"  Weekdays   9:30 ", Spec{Preset: "weekdays", Time: "9:30"}},
		{"weekly mon 09:00", Spec{Preset: "weekly", Day: "mon", Time: "09:00"}},
		{"monthly 15 08:00", Spec{Preset: "monthly", Day: "15", Time: "08:00"}},
		{"0 9 * * 1-5", Spec{Cron: "0 9 * * 1-5"}},
		{"cron */15  2 * * *", Spec{Cron: "*/15 2 * * *"}},
	}
	for _, c := range ok {
		got, err := ParseText(c.in)
		if err != nil {
			t.Errorf("ParseText(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseText(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
	bad := []string{"", "   ", "yearly 09:00", "daily", "daily 09:00 extra", "weekly 09:00",
		"monthly 09:00", "0 9 * *", "0 9 * * * *", strings.Repeat("x", 300)}
	for _, in := range bad {
		if _, err := ParseText(in); err == nil {
			t.Errorf("ParseText(%q): want an error", in)
		}
	}
}

func TestCompileErrors(t *testing.T) {
	bad := []Spec{
		{},
		{Preset: "daily", Time: "09:00", Cron: "0 9 * * *"}, // both
		{Preset: "daily", Time: "24:00"},
		{Preset: "daily", Time: "09:60"},
		{Preset: "daily", Time: "nine"},
		{Preset: "daily"}, // daily needs a time
		{Preset: "hourly", Time: ":75"},
		{Preset: "weekly", Day: "funday", Time: "09:00"},
		{Preset: "monthly", Day: "29", Time: "09:00"}, // not every month has it
		{Preset: "monthly", Day: "0", Time: "09:00"},
		{Preset: "daily", Time: "09:00", TZ: "Mars/Olympus"},
		{Cron: "60 * * * *"},
		{Cron: "* 24 * * *"},
		{Cron: "* * 0 * *"},
		{Cron: "* * * 13 *"},
		{Cron: "* * * * 8"},
		{Cron: "*/0 * * * *"},
		{Cron: "5-1 * * * *"},
		{Cron: "a * * * *"},
		{Cron: "1,,2 * * * *"},
		{Cron: "0 0 30 2 *"}, // never runs
		{Cron: "0 0 31 4,6,9,11 *"},
	}
	for _, sp := range bad {
		if _, err := Compile(sp); err == nil {
			t.Errorf("Compile(%+v): want an error", sp)
		}
	}
}

// TestNextTable: the DST rules in two zones. A fixed-time slot that falls
// in a spring-forward gap runs once at the change; a fixed-time slot in a
// fall-back overlap runs once, at its first occurrence. A schedule whose
// hour is * (hourly) follows real time instead: gap slots do not exist,
// overlap slots run at both occurrences.
func TestNextTable(t *testing.T) {
	cases := []struct {
		name  string
		spec  Spec
		after string
		want  string
	}{
		// Europe/Berlin 2026: spring 29 Mar 02:00→03:00, fall 25 Oct 03:00→02:00.
		{"berlin daily keeps local time on DST day", Spec{Preset: "daily", Time: "09:00", TZ: "Europe/Berlin"},
			"2026-03-28T09:00:00+01:00", "2026-03-29T09:00:00+02:00"},
		{"berlin daily in gap runs at the change", Spec{Preset: "daily", Time: "02:30", TZ: "Europe/Berlin"},
			"2026-03-28T12:00:00+01:00", "2026-03-29T03:00:00+02:00"},
		{"berlin daily after the gap day", Spec{Preset: "daily", Time: "02:30", TZ: "Europe/Berlin"},
			"2026-03-29T03:00:00+02:00", "2026-03-30T02:30:00+02:00"},
		{"berlin daily overlap first occurrence", Spec{Preset: "daily", Time: "02:30", TZ: "Europe/Berlin"},
			"2026-10-24T12:00:00+02:00", "2026-10-25T02:30:00+02:00"},
		{"berlin daily overlap runs once", Spec{Preset: "daily", Time: "02:30", TZ: "Europe/Berlin"},
			"2026-10-25T02:30:00+02:00", "2026-10-26T02:30:00+01:00"},
		{"berlin hourly overlap runs twice", Spec{Preset: "hourly", Time: ":30", TZ: "Europe/Berlin"},
			"2026-10-25T02:30:00+02:00", "2026-10-25T02:30:00+01:00"},
		{"berlin hourly skips the gap", Spec{Preset: "hourly", Time: ":30", TZ: "Europe/Berlin"},
			"2026-03-29T01:30:00+01:00", "2026-03-29T03:30:00+02:00"},
		{"berlin weekdays over the weekend", Spec{Preset: "weekdays", Time: "09:00", TZ: "Europe/Berlin"},
			"2026-03-27T10:00:00+01:00", "2026-03-30T09:00:00+02:00"},
		{"berlin weekly sunday in gap", Spec{Preset: "weekly", Day: "sun", Time: "02:30", TZ: "Europe/Berlin"},
			"2026-03-22T02:30:00+01:00", "2026-03-29T03:00:00+02:00"},
		{"berlin monthly", Spec{Preset: "monthly", Day: "15", Time: "08:00", TZ: "Europe/Berlin"},
			"2026-01-15T08:00:00+01:00", "2026-02-15T08:00:00+01:00"},
		{"berlin cron gap slots collapse to one", Spec{Cron: "*/15 2 * * *", TZ: "Europe/Berlin"},
			"2026-03-29T01:59:00+01:00", "2026-03-29T03:00:00+02:00"},
		{"berlin cron after collapsed gap", Spec{Cron: "*/15 2 * * *", TZ: "Europe/Berlin"},
			"2026-03-29T03:00:00+02:00", "2026-03-30T02:00:00+02:00"},
		{"berlin cron fixed hour overlap once", Spec{Cron: "*/15 2 * * *", TZ: "Europe/Berlin"},
			"2026-10-25T02:45:00+02:00", "2026-10-26T02:00:00+01:00"},

		// America/New_York 2026: spring 8 Mar 02:00→03:00, fall 1 Nov 02:00→01:00.
		{"ny daily in gap", Spec{Preset: "daily", Time: "02:30", TZ: "America/New_York"},
			"2026-03-07T12:00:00-05:00", "2026-03-08T03:00:00-04:00"},
		{"ny daily overlap first", Spec{Preset: "daily", Time: "01:30", TZ: "America/New_York"},
			"2026-10-31T12:00:00-04:00", "2026-11-01T01:30:00-04:00"},
		{"ny daily overlap once", Spec{Preset: "daily", Time: "01:30", TZ: "America/New_York"},
			"2026-11-01T01:30:00-04:00", "2026-11-02T01:30:00-05:00"},
		{"ny hourly overlap twice", Spec{Preset: "hourly", TZ: "America/New_York"},
			"2026-11-01T01:00:00-04:00", "2026-11-01T01:00:00-05:00"},
		{"ny hourly after overlap", Spec{Preset: "hourly", TZ: "America/New_York"},
			"2026-11-01T01:00:00-05:00", "2026-11-01T02:00:00-05:00"},
		{"ny cron dom or dow", Spec{Cron: "0 9 1 * 1", TZ: "America/New_York"},
			"2026-06-01T09:00:00-04:00", "2026-06-08T09:00:00-04:00"},
		{"ny cron dom only", Spec{Cron: "0 9 1 * *", TZ: "America/New_York"},
			"2026-06-01T09:00:00-04:00", "2026-07-01T09:00:00-04:00"},
		{"ny cron leap day", Spec{Cron: "0 0 29 2 *", TZ: "America/New_York"},
			"2026-01-01T00:00:00-05:00", "2028-02-29T00:00:00-05:00"},
		{"ny cron dow 7 is sunday", Spec{Cron: "0 12 * * 7", TZ: "America/New_York"},
			"2026-11-06T10:00:00-05:00", "2026-11-08T12:00:00-05:00"},
		{"ny cron names", Spec{Cron: "0 12 * JAN-mar Sun", TZ: "America/New_York"},
			"2026-11-06T10:00:00-05:00", "2027-01-03T12:00:00-05:00"},
		{"ny cron dow range wraps 5-7", Spec{Cron: "0 8 * * 5-7", TZ: "America/New_York"},
			"2026-11-08T09:00:00-05:00", "2026-11-13T08:00:00-05:00"},

		{"utc every minute", Spec{Cron: "* * * * *", TZ: "UTC"},
			"2026-11-06T10:00:30Z", "2026-11-06T10:01:00Z"},
		{"utc step range", Spec{Cron: "10-40/15 * * * *", TZ: "UTC"},
			"2026-11-06T10:26:00Z", "2026-11-06T10:40:00Z"},
		{"utc step from value", Spec{Cron: "50/5 * * * *", TZ: "UTC"},
			"2026-11-06T10:56:00Z", "2026-11-06T11:50:00Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, err := Compile(c.spec)
			if err != nil {
				t.Fatalf("Compile: %v", err)
			}
			got := s.Next(mustTime(t, c.after))
			want := mustTime(t, c.want)
			if !got.Equal(want) {
				t.Fatalf("Next(%s) = %s, want %s", c.after, got.Format(time.RFC3339), c.want)
			}
			if got.Location() != s.Location() {
				t.Fatalf("Next is in %v, want %v", got.Location(), s.Location())
			}
		})
	}
}

// TestNextWalk walks a year of slots in both zones: always strictly
// later, and (outside DST gaps) on the wall time the schedule names.
func TestNextWalk(t *testing.T) {
	specs := []Spec{
		{Preset: "hourly", Time: ":30"},
		{Preset: "daily", Time: "02:30"},
		{Preset: "weekdays", Time: "09:00"},
		{Preset: "weekly", Day: "sun", Time: "02:00"},
		{Preset: "monthly", Day: "28", Time: "23:59"},
		{Cron: "*/20 1-3 * * *"},
	}
	for _, tz := range []string{"Europe/Berlin", "America/New_York"} {
		for _, sp := range specs {
			sp.TZ = tz
			s, err := Compile(sp)
			if err != nil {
				t.Fatalf("Compile(%+v): %v", sp, err)
			}
			at := mustTime(t, "2026-01-01T00:00:00Z")
			end := mustTime(t, "2027-01-01T00:00:00Z")
			for n := 0; at.Before(end); n++ {
				next := s.Next(at)
				if !next.After(at) {
					t.Fatalf("%+v: Next(%s) = %s, not later", sp, at, next)
				}
				w := next.In(s.Location())
				if !s.matches(w) && !s.inGap(w) {
					t.Fatalf("%+v: Next(%s) = %s does not match", sp, at, w)
				}
				at = next
			}
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"hourly", "hourly :15", "daily 09:00", "weekdays 9:30",
		"weekly mon 09:00", "monthly 28 23:59", "0 9 * * 1-5", "*/7 */5 1-31/3 jan-dec/2 0-7",
		"cron 0 0 29 2 *", "5,10-20/3 * * * sun,sat", "* * * * *"} {
		f.Add(s, uint8(0))
	}
	zones := []string{"UTC", "Europe/Berlin", "America/New_York", "Australia/Lord_Howe"}
	after := time.Date(2026, 3, 29, 0, 30, 0, 0, time.UTC)
	f.Fuzz(func(t *testing.T, text string, zone uint8) {
		sp, err := ParseText(text)
		if err != nil {
			return
		}
		sp.TZ = zones[int(zone)%len(zones)]
		s, err := Compile(sp)
		if err != nil {
			return
		}
		next := s.Next(after)
		if next.IsZero() {
			t.Fatalf("%q compiled but never runs", text)
		}
		if !next.After(after) {
			t.Fatalf("%q: Next(%s) = %s", text, after, next)
		}
		if again := s.Next(next); !again.IsZero() && !again.After(next) {
			t.Fatalf("%q: Next(%s) = %s", text, next, again)
		}
	})
}
