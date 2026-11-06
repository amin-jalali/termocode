package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fuzzToken is a fake key (split so no scanner sees a real one).
var fuzzToken = "sk-" + "fuzz0123456789abcdefghij"

// FuzzParse: Parse never panics on any input, an error is a
// *ParseError of at most MaxErrorChars characters, no message echoes
// the fake key, and a good file has its steps typed.
func FuzzParse(f *testing.F) {
	files, _ := filepath.Glob("testdata/good/*.jsonc")
	for _, p := range files {
		if b, err := os.ReadFile(p); err == nil {
			f.Add(string(b))
		}
	}
	for _, c := range badCases {
		if len(c.src) < 4096 {
			f.Add(c.src)
		}
	}
	f.Add(`{"version":1,"name":"a","steps":[{"type":"ai","prompt":"` + fuzzToken + `"}]}`)
	f.Add(`{"version":1,"name":"a","vars":{"` + fuzzToken + `":1},"steps":[{"type":"tests"}]}`)
	f.Add(`{"version":1,"name":"a","steps":[{"type":"` + fuzzToken + `"}]}`)
	f.Add(`{"version":1,"name":"` + fuzzToken + `","steps":[{"type":"write","to":"` + fuzzToken + `"}]}`)
	f.Fuzz(func(t *testing.T, src string) {
		file, warns, err := Parse("fuzz.jsonc", []byte(src))
		if err != nil {
			if _, ok := err.(*ParseError); !ok {
				t.Fatalf("error type %T", err)
			}
			if len(err.Error()) > MaxErrorChars {
				t.Fatalf("error is %d chars", len(err.Error()))
			}
			if strings.Contains(err.Error(), fuzzToken) {
				t.Fatalf("error echoes the key: %s", err)
			}
		} else if file == nil || file.Version != Version || len(file.Steps) == 0 {
			t.Fatalf("good file without steps: %+v", file)
		}
		for _, w := range warns {
			if strings.Contains(w.String(), fuzzToken) {
				t.Fatalf("warning echoes the key: %s", w)
			}
		}
	})
}
