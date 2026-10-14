package ai

import (
	"strings"
	"testing"
	"time"
)

// Typing simulation: a user types a small Go file at ~5 keys/s with short
// pauses (after "(" / "." and at line ends). A counting fake model answers
// every request; it returns the right line when the caret is on code and
// a made-up "func helper()" when the line is empty or finished — the
// "nonsense new function" users complain about. The test measures
// requests per minute and nonsense shown, for the old policy (debounce
// only) and the current one (trigger rules + filter + type-through).

const simScript = `func main() {
	name := "world"
	msg := greet(name)
	fmt.Println(msg)
}

func greet(n string) string {
	if n == "" {
		return "hello"
	}
	return "hello " + n
}
`

const (
	simKey      = 200 * time.Millisecond
	simDebounce = 300 * time.Millisecond
	simBlankExt = 700 * time.Millisecond
)

type simResult struct {
	requests, shown, nonsense int
	elapsed                   time.Duration
}

func (r simResult) perMinute() float64 { return float64(r.requests) / r.elapsed.Minutes() }

func simulateTyping(newPolicy bool) simResult {
	const prefix = "package main\n\nimport \"fmt\"\n\n"
	lines := strings.SplitAfter(simScript, "\n")
	var res simResult
	var cache InlineCache
	typed := ""
	nlPause := 0
	for li, line := range lines {
		// nvim autoindent: the indent arrives with the Enter keystroke.
		body := strings.TrimLeft(line, "\t")
		typed += line[:len(line)-len(body)]
		for ci := 0; ci < len(body); ci++ {
			ch := body[ci]
			typed += string(ch)
			pause := simKey
			switch ch {
			case '(', '.':
				pause = 450 * time.Millisecond
			case '\n':
				nlPause++
				pause = 600 * time.Millisecond
				if nlPause%2 == 0 {
					pause = 1500 * time.Millisecond
				}
				if li+1 < len(lines) {
					next := lines[li+1]
					typed += next[:len(next)-len(strings.TrimLeft(next, "\t"))]
				}
			}
			res.elapsed += pause
			before := prefix + typed
			c := InlineContext{Filetype: "go", Before: before, After: "\n",
				LineBefore: before[strings.LastIndexByte(before, '\n')+1:]}
			row := strings.Count(before, "\n")
			if newPolicy {
				if _, ok := cache.Match(0, row, c.LineBefore); ok {
					res.shown++
					continue
				}
			}
			if pause < simDebounce {
				continue
			}
			if newPolicy {
				if ok, _ := ShouldAutoTrigger(c); !ok {
					continue
				}
				if NeedsLongerPause(c) && pause < simDebounce+simBlankExt {
					continue
				}
			} else if strings.TrimSpace(c.Before+c.After) == "" {
				continue
			}
			res.requests++
			// Fake model: the right line when typing, nonsense otherwise.
			want := ""
			if ch != '\n' {
				want = strings.TrimSpace(line)
				if strings.TrimSpace(c.LineBefore+body[ci+1:]) == strings.TrimSpace(c.LineBefore) {
					want = "" // the line is already complete
				}
			}
			raw := "\n\nfunc helper() {\n\treturn\n}"
			if want != "" {
				raw = want
			}
			comp, ok := ParseInlineResponse(raw)
			if !ok {
				continue
			}
			text := comp.InsertText
			if newPolicy {
				if text, ok = FilterInlineCompletion(c, text, false); !ok {
					continue
				}
			}
			r := ResolveInlineCompletion(c.LineBefore, c.LineAfter, text)
			if strings.TrimSpace(r.Insert) == "" {
				continue
			}
			res.shown++
			if strings.Contains(r.Insert, "helper") {
				res.nonsense++
			}
			if newPolicy && r.DeleteBefore == 0 {
				cache.Remember(0, row, c.LineBefore, r.Insert)
			}
		}
	}
	return res
}

func TestInlineTypingSimulation(t *testing.T) {
	old := simulateTyping(false)
	cur := simulateTyping(true)
	t.Logf("old policy: %d requests in %v (%.1f/min), %d shown, %d nonsense",
		old.requests, old.elapsed, old.perMinute(), old.shown, old.nonsense)
	t.Logf("new policy: %d requests in %v (%.1f/min), %d shown, %d nonsense",
		cur.requests, cur.elapsed, cur.perMinute(), cur.shown, cur.nonsense)
	if old.nonsense == 0 {
		t.Fatal("simulation should reproduce the nonsense with the old policy")
	}
	if cur.nonsense != 0 {
		t.Errorf("new policy still shows %d nonsense suggestions", cur.nonsense)
	}
	if cur.requests >= old.requests {
		t.Errorf("new policy sends %d requests, old %d: want fewer", cur.requests, old.requests)
	}
}
