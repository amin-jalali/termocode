package httpclient

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// API-4: fake data ($random.email / names / words) and custom date
// formats for $datetime and $localDatetime.
func TestResolveFakeDataVars(t *testing.T) {
	rv := &Resolver{Now: fixedNow}
	forms := map[string]*regexp.Regexp{
		"{{$random.email}}":     regexp.MustCompile(`^[a-z]+\.[a-z]+[0-9]{2}@example\.com$`),
		"{{$random.firstName}}": regexp.MustCompile(`^[A-Z][a-z]+$`),
		"{{$random.lastName}}":  regexp.MustCompile(`^[A-Z][a-z]+$`),
		"{{$random.fullName}}":  regexp.MustCompile(`^[A-Z][a-z]+ [A-Z][a-z]+$`),
		"{{$random.words}}":     regexp.MustCompile(`^[a-z]+( [a-z]+){2}$`),
		"{{$random.words 5}}":   regexp.MustCompile(`^[a-z]+( [a-z]+){4}$`),
		"{{$random.words 1}}":   regexp.MustCompile(`^[a-z]+$`),
	}
	for in, re := range forms {
		got, un := rv.Resolve(Request{URL: in})
		if !re.MatchString(got.URL) || len(un) != 0 {
			t.Errorf("%s → %q (%v), want %s", in, got.URL, un, re)
		}
		if !rv.Known(strings.Trim(in, "{}")) {
			t.Errorf("Known(%s) = false", in)
		}
	}
	for _, in := range []string{
		"{{$random.words 0}}", "{{$random.words 101}}", "{{$random.words x}}",
		"{{$random.email x}}", "{{$random.nope}}",
	} {
		got, un := rv.Resolve(Request{URL: in})
		if got.URL != in || len(un) == 0 {
			t.Errorf("%s → %q (%v), want it left as written", in, got.URL, un)
		}
	}
}

// The same fake value used twice in one send is the same value.
func TestResolveFakeDataStableInOneSend(t *testing.T) {
	rv := &Resolver{Now: fixedNow}
	got, _ := rv.Resolve(Request{URL: "{{$random.email}}|{{$random.email}}"})
	a, b, _ := strings.Cut(got.URL, "|")
	if a != b {
		t.Fatalf("%q != %q", a, b)
	}
}

func TestResolveCustomDateFormats(t *testing.T) {
	rv := &Resolver{Now: fixedNow}
	now := fixedNow() // 2026-10-16 09:12:33 UTC, a Friday
	loc := now.Local()
	good := map[string]string{
		`{{$datetime "YYYY-MM-DD"}}`:          "2026-10-16",
		`{{$datetime 'DD/MM/YY'}}`:            "16/10/26",
		`{{$datetime "YYYY-MM-DD HH:mm:ss"}}`: "2026-10-16 09:12:33",
		`{{$datetime "YYYY-MM-DD" 1 d}}`:      "2026-10-17",
		`{{$datetime "D.M.YYYY h:m A" -2 h}}`: "16.10.2026 7:12 AM",
		`{{$datetime "ddd, MMM D"}}`:          "Fri, Oct 16",
		`{{$datetime "dddd MMMM"}}`:           "Friday October",
		`{{$datetime "hh:mm a SSS"}}`:         "09:12 am 000",
		`{{$datetime "YYYYMMDD[T]HHmmss"}}`:   "20261016T091233",
		`{{$datetime "Z"}}`:                   "+00:00",
		`{{$localDatetime "YYYY-MM-DD"}}`:     loc.Format("2006-01-02"),
		`{{$localDatetime "HH:mm ZZ" 30 m}}`:  loc.Add(30 * time.Minute).Format("15:04 -0700"),
	}
	for in, want := range good {
		got, un := rv.Resolve(Request{URL: in})
		if got.URL != want || len(un) != 0 {
			t.Errorf("%s → %q (%v), want %q", in, got.URL, un, want)
		}
	}
	for _, in := range []string{
		`{{$datetime "YYYY}}`, `{{$datetime ""}}`, `{{$datetime "YYYY" 1}}`, `{{$datetime "YYYY" 1 q}}`,
	} {
		got, un := rv.Resolve(Request{URL: in})
		if got.URL != in || len(un) == 0 {
			t.Errorf("%s → %q (%v), want it left as written", in, got.URL, un)
		}
	}
}
