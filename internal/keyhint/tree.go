// Package keyhint is the key hint popup: after the first key of a
// multi-key sequence (the nvim leader), a small box lists the next keys
// and what they do. This file is the pure part: which keys can follow a
// prefix. The tracker (tracker.go) decides when to show it; view.go draws it.
package keyhint

import (
	"sort"
	"strconv"
	"strings"
)

// Mapping is one normal-mode mapping as nvim reports it.
type Mapping struct {
	LHS  string // the keys, e.g. "<Space>f" or "\x"
	Desc string // nvim's desc option, may be empty
	RHS  string // the right side, used when Desc is empty
}

// Entry is one possible next key under a prefix.
type Entry struct {
	Key   string // one key token, e.g. "f" or "<space>"
	Desc  string // what it does; for a group, "+N more" when it has no text
	Group bool   // more keys follow: pressing it opens the next level
	Count int    // mappings below a group
}

// Tokens splits keys into one token per key. "<Name>" is one token, in
// lower case, so "<Space>" and "<space>" compare equal.
func Tokens(keys string) []string {
	var out []string
	for i := 0; i < len(keys); {
		if keys[i] == '<' {
			if j := strings.IndexByte(keys[i:], '>'); j > 1 && !strings.ContainsAny(keys[i+1:i+j], "< ") {
				out = append(out, strings.ToLower(keys[i:i+j+1]))
				i += j + 1
				continue
			}
		}
		r := []rune(keys[i:])[0]
		out = append(out, string(r))
		i += len(string(r))
	}
	return out
}

// HasPrefix reports whether toks starts with prefix.
func HasPrefix(toks, prefix []string) bool {
	if len(toks) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if toks[i] != p {
			return false
		}
	}
	return true
}

// describe is the text shown for a mapping.
func describe(m Mapping) string {
	if d := strings.TrimSpace(m.Desc); d != "" {
		return d
	}
	r := strings.TrimSpace(m.RHS)
	r = strings.TrimPrefix(r, "<Cmd>")
	r = strings.TrimSuffix(r, "<CR>")
	return strings.TrimSpace(r)
}

// Entries lists the keys that can follow prefix, sorted by key. A key
// with longer mappings below it is a group.
func Entries(maps []Mapping, prefix []string) []Entry {
	by := map[string]*Entry{}
	for _, m := range maps {
		toks := Tokens(m.LHS)
		if len(toks) <= len(prefix) || !HasPrefix(toks, prefix) {
			continue
		}
		k := toks[len(prefix)]
		e := by[k]
		if e == nil {
			e = &Entry{Key: k}
			by[k] = e
		}
		if len(toks) == len(prefix)+1 {
			e.Desc = describe(m)
			continue
		}
		e.Group = true
		e.Count++
	}
	out := make([]Entry, 0, len(by))
	for _, e := range by {
		if e.Group && e.Desc == "" {
			e.Desc = "+" + strconv.Itoa(e.Count) + " more"
		}
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := strings.ToLower(out[i].Key), strings.ToLower(out[j].Key)
		if a != b {
			return a < b
		}
		return out[i].Key > out[j].Key // lower case first
	})
	return out
}
