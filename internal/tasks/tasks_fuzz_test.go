package tasks

import (
	"strings"
	"testing"
)

// FuzzTasksJSON reads any tasks.json; no panic, and every task has a
// label.
func FuzzTasksJSON(f *testing.F) {
	for _, s := range []string{
		`{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"go build ./...","group":{"kind":"build","isDefault":true},"problemMatcher":["$go"]}]}`,
		`[{"label":"t","command":"make","group":"test","problemMatcher":"$generic","env":{"A":"1"}}]`,
		`{"tasks":[{"command":"no label"},{"label":""}]}`,
		`{"tasks":{}}`, `[]`, `x`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		ts, err := Parse(b)
		if err != nil {
			return
		}
		for _, task := range ts {
			if strings.TrimSpace(task.Label) == "" {
				t.Fatalf("task without a label: %+v", task)
			}
		}
	})
}

// FuzzProblemMatchers scans any task output. Problems point at a line
// from 1 up, a column from 1 up, and a path; links lie inside the text.
func FuzzProblemMatchers(f *testing.F) {
	for _, s := range []string{
		"main.go:12:5: undefined: x\n./pkg/a_test.go:3: error: bad",
		"src/app.ts(4,9): error TS2304: Cannot find name 'y'.",
		"error[E0308]: mismatched types\n  --> src/main.rs:4:5",
		"\x1b[31mfoo.py:7:1: warning: unused\x1b[0m",
		"C:\\x\\y.c:1:2: note: hi",
		"see (lib/a.dart:99999999999999999999:1)",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, text string) {
		for _, p := range Match([]string{"$generic", "$go", "$tsc", "$rustc"}, strings.Split(text, "\n")) {
			if p.Line < 1 || p.Col < 1 || p.Path == "" || p.Message == "" {
				t.Fatalf("bad problem %+v", p)
			}
		}
		for _, l := range ParseLinks(text) {
			if l.Start < 0 || l.End > len(text) || l.Start >= l.End || l.Line < 1 || l.Col < 0 {
				t.Fatalf("bad link %+v in %q", l, text)
			}
		}
		_ = StripANSI(text)
	})
}
