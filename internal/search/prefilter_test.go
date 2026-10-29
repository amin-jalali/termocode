package search

import (
	"math/rand"
	"strings"
	"testing"
)

// Spec: the built-in search skips a file at once when a literal query
// cannot be in it, and never skips a file that has a match.
func TestMayMatch(t *testing.T) {
	cases := []struct {
		query string
		opts  Options
		text  string
		want  bool
	}{
		{"needle", Options{}, "hay\nhay\n", false},
		{"needle", Options{}, "hay NEEDLE hay\n", true},  // smart-case folds
		{"Needle", Options{}, "hay needle hay\n", false}, // an upper-case letter: exact
		{"Needle", Options{}, "hay Needle\n", true},      //
		{"needle", Options{CaseSensitive: true}, "NEEDLE", false},
		{"nEEdle", Options{CaseSensitive: true}, "nEEdle", true},
		{"ask", Options{}, "aſk", true},  // ſ folds to s
		{"kit", Options{}, "Kit", true},  // Kelvin sign folds to k
		{"a.c", Options{}, "abc", false}, // literal: the dot is a dot
		{"a.c", Options{Regex: true}, "abc", true},
		{"^foo", Options{Regex: true}, "x\nfoo", true}, // regex: never skipped
		{"é", Options{}, "É", true},                    // non-ASCII query: never skipped
	}
	for _, c := range cases {
		lm, err := newLineMatcher(c.query, c.opts)
		if err != nil {
			t.Fatal(err)
		}
		if got := lm.mayMatch([]byte(c.text)); got != c.want {
			t.Errorf("mayMatch(%q, %+v, %q) = %v, want %v", c.query, c.opts, c.text, got, c.want)
		}
	}
}

// Spec: on random text, whenever a line matches, mayMatch says yes.
func TestMayMatchNeverMissesAMatch(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	alphabet := []string{"a", "b", "K", "k", "s", "S", "ſ", "K", "\n", " ", "é", "x"}
	queries := []string{"ab", "k", "sk", "Ks", "bx", "a b", "s"}
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for j := r.Intn(30); j > 0; j-- {
			b.WriteString(alphabet[r.Intn(len(alphabet))])
		}
		text := b.String()
		q := queries[r.Intn(len(queries))]
		opts := Options{CaseSensitive: r.Intn(3) == 0, WholeWord: r.Intn(3) == 0}
		lm, err := newLineMatcher(q, opts)
		if err != nil {
			t.Fatal(err)
		}
		if searchLines(text, "f", lm, 0) != nil && !lm.mayMatch([]byte(text)) {
			t.Fatalf("query %q %+v: text %q has a match but mayMatch said no", q, opts, text)
		}
	}
}
