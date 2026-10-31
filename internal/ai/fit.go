package ai

import (
	"strconv"
	"strings"
)

// Adaptive cut of tool results (OMX-25). Tools cap their own output, but
// a few big results can still fill the model's window late in a turn.
// After each step the results are cut to half of what is still free
// (the window minus the last prompt and room for the answer), shared by
// the step's calls; a cut result keeps its start and end and says so.

// minFitChars is the least a cut result keeps.
const minFitChars = 4000

// fitCharsPerToken is a careful guess (code is about 3–4 chars a token).
const fitCharsPerToken = 3

// fitToolResults cuts results for a window of window tokens whose last
// prompt was prompt tokens (window 0: unknown, nothing is cut).
func fitToolResults(results []string, window, prompt int) []string {
	if window <= 0 || len(results) == 0 {
		return results
	}
	reserve := min(anthropicMaxTokens, window/4)
	free := window - prompt - reserve
	limit := minFitChars
	if free > 0 {
		limit = max(minFitChars, free/2/len(results)*fitCharsPerToken)
	}
	out := results
	for i, r := range results {
		if len(r) <= limit {
			continue
		}
		if &out[0] == &results[0] {
			out = append([]string(nil), results...)
		}
		out[i] = cutMiddle(r, limit)
	}
	return out
}

// cutMiddle keeps about limit bytes of s: two thirds from the start, one
// third from the end, on line breaks when there is one near.
func cutMiddle(s string, limit int) string {
	head, tail := limit*2/3, limit/3
	h := s[:head]
	if j := strings.LastIndexByte(h, '\n'); j > head/2 {
		h = h[:j+1]
	}
	for len(h) > 0 && !validCut(h) {
		h = h[:len(h)-1]
	}
	t := s[len(s)-tail:]
	if j := strings.IndexByte(t, '\n'); j >= 0 && j < tail/2 {
		t = t[j+1:]
	}
	for len(t) > 0 && t[0]&0xC0 == 0x80 { // not in the middle of a UTF-8 rune
		t = t[1:]
	}
	cut := len(s) - len(h) - len(t)
	return h + "\n[… " + strconv.Itoa(cut) + " characters cut to fit the context window; ask for a smaller part (a line range, a narrower search) to see them …]\n" + t
}

// validCut reports whether s does not end inside a UTF-8 rune.
func validCut(s string) bool {
	n := len(s)
	for i := 1; i <= 4 && i <= n; i++ {
		c := s[n-i]
		if c&0xC0 != 0x80 { // a rune's first byte
			need := 1
			switch {
			case c >= 0xF0:
				need = 4
			case c >= 0xE0:
				need = 3
			case c >= 0xC0:
				need = 2
			}
			return need <= i
		}
	}
	return false
}
