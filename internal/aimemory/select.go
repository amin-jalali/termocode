package aimemory

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

// Hints are what a turn is about: the user's message and the open and
// attached files (absolute or workspace-relative).
type Hints struct {
	Text  string
	Paths []string
}

// newestFill is how many lessons a turn gets at least (the newest ones)
// when few lessons match.
const newestFill = 3

// Select picks the lessons for a turn, cheaply: lessons about one of the
// hint paths first, then lessons sharing at least two words (four or more
// letters) with the message, then the newest ones until there are three.
// The result is newest first inside each group and stays under maxN
// lessons and about maxBytes of text.
func Select(ls []Lesson, h Hints, maxBytes, maxN int) []Lesson {
	type scored struct {
		i, tier, words int
	}
	words := wordSet(h.Text)
	var hits []scored
	used := map[int]bool{}
	for i, l := range ls {
		switch {
		case pathMatch(l.Paths, h.Paths):
			hits = append(hits, scored{i, 2, 0})
		default:
			n := 0
			for w := range wordSet(l.Title + " " + l.Text) {
				if words[w] {
					n++
				}
			}
			if n >= 2 {
				hits = append(hits, scored{i, 1, n})
			}
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		x, y := hits[a], hits[b]
		if x.tier != y.tier {
			return x.tier > y.tier
		}
		if x.words != y.words {
			return x.words > y.words
		}
		return x.i > y.i // newer (later in the file) first
	})
	var out []Lesson
	size := 0
	add := func(i int) bool {
		if len(out) >= maxN {
			return false
		}
		n := len(ls[i].Title) + len(ls[i].Text) + 8
		if size+n > maxBytes && len(out) > 0 {
			return false
		}
		used[i] = true
		out = append(out, ls[i])
		size += n
		return true
	}
	for _, s := range hits {
		if !add(s.i) {
			break
		}
	}
	for i := len(ls) - 1; i >= 0 && len(out) < newestFill; i-- {
		if !used[i] && !add(i) {
			break
		}
	}
	return out
}

// pathMatch reports whether one lesson path names one of the hint files:
// equal, or the hint ends with "/<lesson path>".
func pathMatch(lessonPaths, hints []string) bool {
	for _, lp := range lessonPaths {
		lp = strings.TrimPrefix(filepath.ToSlash(lp), "./")
		if lp == "" {
			continue
		}
		for _, h := range hints {
			h = filepath.ToSlash(h)
			if h == lp || strings.HasSuffix(h, "/"+lp) {
				return true
			}
		}
	}
	return false
}

// wordSet is the lower-case words of s with four or more letters.
func wordSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(w)) >= 4 {
			out[w] = true
		}
	}
	return out
}
