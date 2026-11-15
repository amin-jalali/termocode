package now

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// maxLater caps how far Later may push an item.
const maxLater = 365 * 24 * time.Hour

// ParseLater reads the free text of "Later": "in 2 hours", "2h", "in 30
// min", "in 3 days", "tonight" (18:00), "tomorrow" (9:00), "tomorrow
// 14:30", "next week" (next Monday 9:00).
func ParseLater(s string, now time.Time) (time.Time, error) {
	f := strings.Fields(strings.ToLower(strings.TrimSpace(s)))
	if len(f) == 0 {
		return time.Time{}, errors.New("say when, e.g. in 2 hours")
	}
	day := startOfDay(now)
	switch f[0] {
	case "tonight":
		if len(f) == 1 {
			return future(day.Add(18*time.Hour), now)
		}
	case "tomorrow":
		at := 9 * time.Hour
		if len(f) == 2 {
			d, ok := clock(f[1])
			if !ok {
				return time.Time{}, errors.New("say a time like 9:00")
			}
			at = d
		} else if len(f) > 2 {
			break
		}
		return day.AddDate(0, 0, 1).Add(at), nil
	case "next":
		if len(f) == 2 && f[1] == "week" {
			days := (8 - int(now.Weekday())) % 7
			if days == 0 {
				days = 7
			}
			return day.AddDate(0, 0, days).Add(9 * time.Hour), nil
		}
	case "in":
		f = f[1:]
	}
	if d, ok := span(strings.Join(f, " ")); ok {
		return now.Add(d), nil
	}
	return time.Time{}, errors.New("say when, e.g. in 2 hours, tomorrow 9:00, next week")
}

func future(t, now time.Time) (time.Time, error) {
	if !t.After(now) {
		return time.Time{}, errors.New("that time is over")
	}
	return t, nil
}

// clock reads "9", "9:00" or "14:30".
func clock(s string) (time.Duration, bool) {
	h, m, _ := strings.Cut(s, ":")
	hh, err := strconv.Atoi(h)
	if err != nil || hh < 0 || hh > 23 {
		return 0, false
	}
	mm := 0
	if m != "" {
		if mm, err = strconv.Atoi(m); err != nil || mm < 0 || mm > 59 {
			return 0, false
		}
	}
	return time.Duration(hh)*time.Hour + time.Duration(mm)*time.Minute, true
}

var units = map[string]time.Duration{
	"m": time.Minute, "min": time.Minute, "mins": time.Minute, "minute": time.Minute, "minutes": time.Minute,
	"h": time.Hour, "hour": time.Hour, "hours": time.Hour, "hr": time.Hour, "hrs": time.Hour,
	"d": 24 * time.Hour, "day": 24 * time.Hour, "days": 24 * time.Hour,
	"w": 7 * 24 * time.Hour, "week": 7 * 24 * time.Hour, "weeks": 7 * 24 * time.Hour,
}

// span reads "2 hours", "2h", "30 min".
func span(s string) (time.Duration, bool) {
	s = strings.ReplaceAll(s, " ", "")
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:i])
	u, ok := units[s[i:]]
	if err != nil || !ok || n <= 0 {
		return 0, false
	}
	d := time.Duration(n) * u
	if d/u != time.Duration(n) || d > maxLater {
		return 0, false
	}
	return d, true
}
