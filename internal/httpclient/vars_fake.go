package httpclient

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// Word lists for the fake data variables (plain, neutral words).
var (
	fakeFirstNames = []string{"Alex", "Anna", "Ben", "Clara", "David", "Emma", "Felix", "Grace", "Hana", "Ivan",
		"Julia", "Karl", "Lena", "Marco", "Nina", "Omar", "Paula", "Rosa", "Sam", "Tara", "Victor", "Yuki", "Zoe"}
	fakeLastNames = []string{"Adams", "Berg", "Costa", "Diaz", "Evans", "Fischer", "Garcia", "Hill", "Ito", "Jensen",
		"Kim", "Lopez", "Meyer", "Novak", "Olsen", "Park", "Rossi", "Silva", "Tanaka", "Weber", "Young"}
	fakeWords = []string{"apple", "river", "stone", "cloud", "green", "light", "paper", "quiet", "table", "window",
		"garden", "silver", "orange", "bridge", "forest", "music", "ocean", "pencil", "rabbit", "summer", "tiger",
		"winter", "yellow", "candle", "dragon", "planet", "rocket", "shadow", "violet", "harbor"}
)

const maxFakeWords = 100

func pick(list []string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(list))))
	if err != nil {
		return list[0]
	}
	return list[n.Int64()]
}

// fakeValue makes the $random.* fake data values. handled is false for
// names that are not fake data functions.
func fakeValue(fn, arg string) (v string, ok, handled bool) {
	switch fn {
	case "$random.email", "$random.firstName", "$random.lastName", "$random.fullName":
		if arg != "" {
			return "", false, true
		}
	case "$random.words":
	default:
		return "", false, false
	}
	switch fn {
	case "$random.email":
		n, _ := rand.Int(rand.Reader, big.NewInt(100))
		return fmt.Sprintf("%s.%s%02d@example.com", strings.ToLower(pick(fakeFirstNames)),
			strings.ToLower(pick(fakeLastNames)), n.Int64()), true, true
	case "$random.firstName":
		return pick(fakeFirstNames), true, true
	case "$random.lastName":
		return pick(fakeLastNames), true, true
	case "$random.fullName":
		return pick(fakeFirstNames) + " " + pick(fakeLastNames), true, true
	}
	count := 3
	if arg != "" {
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 || n > maxFakeWords {
			return "", false, true
		}
		count = n
	}
	words := make([]string, count)
	for i := range words {
		words[i] = pick(fakeWords)
	}
	return strings.Join(words, " "), true, true
}

// customDatetime is $datetime / $localDatetime with a quoted format
// ("YYYY-MM-DD" or 'DD.MM.YYYY') and an optional "<n> <unit>" offset.
func customDatetime(fn, arg string, now time.Time) (string, bool) {
	q := arg[0]
	end := strings.IndexByte(arg[1:], q)
	if end < 1 {
		return "", false
	}
	format := arg[1 : end+1]
	t, ok := applyOffset(now, strings.Fields(arg[end+2:]))
	if !ok {
		return "", false
	}
	if fn == "$datetime" {
		t = t.UTC()
	} else {
		t = t.Local()
	}
	return formatDayTokens(t, format), true
}

// dayTokens are the format tokens, longest first so "YYYY" wins over "YY".
var dayTokens = []string{"YYYY", "YY", "MMMM", "MMM", "MM", "M", "DD", "D", "dddd", "ddd",
	"HH", "H", "hh", "h", "mm", "m", "ss", "s", "SSS", "A", "a", "ZZ", "Z"}

// formatDayTokens writes t with the common date tokens; text in [brackets]
// is kept as written, any other character is copied.
func formatDayTokens(t time.Time, format string) string {
	var b strings.Builder
	for i := 0; i < len(format); {
		if format[i] == '[' {
			if j := strings.IndexByte(format[i:], ']'); j > 0 {
				b.WriteString(format[i+1 : i+j])
				i += j + 1
				continue
			}
		}
		matched := false
		for _, tok := range dayTokens {
			if strings.HasPrefix(format[i:], tok) {
				b.WriteString(dayToken(t, tok))
				i += len(tok)
				matched = true
				break
			}
		}
		if !matched {
			b.WriteByte(format[i])
			i++
		}
	}
	return b.String()
}

func dayToken(t time.Time, tok string) string {
	switch tok {
	case "YYYY":
		return t.Format("2006")
	case "YY":
		return t.Format("06")
	case "MMMM":
		return t.Format("January")
	case "MMM":
		return t.Format("Jan")
	case "MM":
		return t.Format("01")
	case "M":
		return strconv.Itoa(int(t.Month()))
	case "DD":
		return t.Format("02")
	case "D":
		return strconv.Itoa(t.Day())
	case "dddd":
		return t.Format("Monday")
	case "ddd":
		return t.Format("Mon")
	case "HH":
		return t.Format("15")
	case "H":
		return strconv.Itoa(t.Hour())
	case "hh":
		return t.Format("03")
	case "h":
		return t.Format("3")
	case "mm":
		return t.Format("04")
	case "m":
		return strconv.Itoa(t.Minute())
	case "ss":
		return t.Format("05")
	case "s":
		return strconv.Itoa(t.Second())
	case "SSS":
		return t.Format(".000")[1:]
	case "A":
		return t.Format("PM")
	case "a":
		return t.Format("pm")
	case "ZZ":
		return t.Format("-0700")
	}
	return t.Format("-07:00") // Z
}
