package nowview

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var update = flag.Bool("update", false, "rewrite the golden frames")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs:\n got:\n%s\nwant:\n%s", name, got, want)
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func typeText(p interface{ Key(tea.KeyMsg) Action }, s string) {
	for _, r := range s {
		p.Key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func sample() Data {
	return Data{
		Project: "shop-api", Branch: "main", CI: "CI ✓ 12/13", Running: 1,
		Turn: []Row{
			{ID: "card:c-1", Kind: "card", Title: "checkout-fix asks to run tests", Sub: "agent · lane checkout-fix", Why: "Waits for your OK", Rule: "Agents", Age: "12m", WhyLong: "the agent waits for your OK since 11:43"},
			{ID: "ci:feat/retry-webhook", Kind: "ci", Title: "CI red on feat/retry-webhook", Sub: "CI · e2e-linux failed", Why: "Your branch is red", Rule: "Red CI", Age: "40m", Fix: true, WhyLong: "e2e-linux failed on your branch at 11:55"},
			{ID: "pr:412", Kind: "pr", Title: "#412 Add retry to webhook sender", Sub: "PR · Sara asked you", Why: "asked to review", Rule: "Reviews", Age: "1d"},
		},
		Overdue: []Row{{ID: "task:x", Kind: "task", Title: "ENG-198 Update API docs", Sub: "Linear · due Mon", Why: "3 days late", Age: "3d"}},
		Waiting: []Row{{ID: "lane:a", Kind: "lane", Title: "migrate-db", Why: "Waits on Sara", Age: "3h"}},
		Other:   []Row{{ID: "run:r", Kind: "run", Title: "docs-sync agent", Why: "Running", Age: "4m"}},
	}
}

// Spec (Now, TUI): a full page like the Agent Board: the top bar names
// the project and "N need you", tabs Your turn / Waiting on others /
// All, then YOUR TURN rows (▶, title, where, why, [rule], age) with the
// selected row's "why:" line and its keys, OVERDUE with "T move all to
// tomorrow", the other two lists one key away, and the keys bar.
func TestNowPageFrame(t *testing.T) {
	p := New(sample())
	p.SetSize(120, 24)
	p.Key(key("j"))
	out := ansi.Strip(p.View())
	if lines := strings.Split(out, "\n"); len(lines) != 24 {
		t.Fatalf("%d lines, want 24", len(lines))
	}
	golden(t, "now_120x24", out)
	for _, w := range []string{"◉ Now", "4 need you", "YOUR TURN", "[Red CI]", "why: e2e-linux failed on your branch at 11:55", "OVERDUE", "move all to tomorrow", "j/k move", "e done", "h later", "m mute"} {
		if !strings.Contains(out, w) {
			t.Errorf("frame lacks %q", w)
		}
	}
}

// Spec (Now, TUI): with nothing on the user's turn the page says
// "All clear. Nothing waits on you." with the other lists one key away.
func TestNowPageAllClear(t *testing.T) {
	d := sample()
	d.Turn, d.Overdue = nil, nil
	p := New(d)
	p.SetSize(120, 8)
	out := ansi.Strip(p.View())
	golden(t, "now_clear_120x8", out)
	if !strings.Contains(out, "All clear. Nothing waits on you.") || !strings.Contains(out, "Waiting on others 1") {
		t.Errorf("all clear frame:\n%s", out)
	}
}

// Spec (Now, TUI): the keys are the web's: j/k move, e done, h later
// (free text on the bottom line), m mute (y the item, r its repo, esc
// keeps it), u undo, f fix, enter open, T moves all overdue to tomorrow,
// tab switches lists, esc/q closes. Nothing acts by itself: each key is
// one action the app runs.
func TestNowPageKeys(t *testing.T) {
	p := New(sample())
	p.SetSize(120, 24)
	if a := p.Key(key("e")); a.Do != "done" || a.ID != "card:c-1" {
		t.Fatalf("e: %+v", a)
	}
	p.Key(key("j"))
	if a := p.Key(key("f")); a.Do != "fix" || a.ID != "ci:feat/retry-webhook" {
		t.Fatalf("f: %+v", a)
	}
	if a := p.Key(key("h")); a.Do != "" {
		t.Fatalf("h asks first: %+v", a)
	}
	if !strings.Contains(ansi.Strip(p.View()), "Later: CI red on feat/retry-webhook") {
		t.Fatal("no later prompt")
	}
	typeText(p, "in 2 hours")
	if a := p.Key(key("enter")); a.Do != "later" || a.ID != "ci:feat/retry-webhook" || a.Text != "in 2 hours" {
		t.Fatalf("later: %+v", a)
	}
	p.Key(key("m"))
	if !strings.Contains(ansi.Strip(p.View()), "Mute") {
		t.Fatal("no mute prompt")
	}
	if a := p.Key(key("r")); a.Do != "mute" || !a.Repo {
		t.Fatalf("mute repo: %+v", a)
	}
	p.Key(key("m"))
	if a := p.Key(key("esc")); a.Do != "" {
		t.Fatalf("esc on mute: %+v", a)
	}
	if a := p.Key(key("u")); a.Do != "undo" {
		t.Fatalf("u: %+v", a)
	}
	p.Key(key("j"))
	if a := p.Key(key("enter")); a.Do != "open" || a.ID != "pr:412" {
		t.Fatalf("enter: %+v", a)
	}
	if a := p.Key(key("T")); a.Do != "tomorrow" {
		t.Fatalf("T: %+v", a)
	}
	p.Key(tea.KeyMsg{Type: tea.KeyTab})
	if !strings.Contains(ansi.Strip(p.View()), "migrate-db") {
		t.Fatal("tab does not show Waiting on others")
	}
	if a := p.Key(key("esc")); a.Do != "close" {
		t.Fatalf("esc: %+v", a)
	}
}

func reviewSample() Review {
	return Review{
		Number: 412, Title: "Add retry to webhook sender", Author: "Sara", Files: 4, Add: 44, Del: 6, Head: "3f2a1c9", I: 1, N: 5,
		Risk: "medium", Reasons: []string{"network code (path rule)", "retry can send twice"},
		Question: "Webhooks fail on short network drops.", Task: "ENG-231", Solution: "Retry 3 times with backoff. Stop on 4xx errors.",
		Groups: []Group{{Title: "Retry loop with backoff", Detail: "webhook/send.go", Add: 18, Del: 2, Main: true}, {Title: "Rest: config, test", Detail: "2 files"}},
		Proof: []Proof{{Name: "e2e-linux 1 failed", State: "fail", SHA: "3f2a1c9", Fix: true}, {Name: "race test", State: "stale", SHA: "9b1e0d2", Again: true},
			{Name: "unit tests 212", State: "ok", SHA: "3f2a1c9"}, {Name: "e2e-macos", State: "notrun"}},
		Suggestion: "approve after e2e is green", Notes: 1,
	}
}

// Spec (Review, TUI): the same order as the web: question, solution,
// changes main first, proof rows with their commit (stale: "old code",
// g run again; red: f fix, l log), risks, suggestion; the bottom bar has
// o/s/c/a and "approve ≠ merge · posts APPROVE on <commit>".
func TestReviewPageFrame(t *testing.T) {
	r := NewReview(reviewSample())
	r.SetSize(120, 22)
	out := ansi.Strip(r.View())
	golden(t, "review_120x22", out)
	for _, w := range []string{"Review 1 of 5 · #412 Add retry to webhook sender", "MEDIUM RISK", "QUESTION", "ENG-231", "SOLUTION", "CHANGES · main first",
		"PROOF · 3f2a1c9", "old code 9b1e0d2", "run again", "not run", "RISKS", "SUGGESTION", "a approve & next", "approve ≠ merge · posts APPROVE on 3f2a1c9"} {
		if !strings.Contains(out, w) {
			t.Errorf("review frame lacks %q", w)
		}
	}
	r.Note("✓ approved #411 on 2d1c0b9", true)
	if out := ansi.Strip(r.View()); !strings.Contains(out, "✓ approved #411 on 2d1c0b9") || !strings.Contains(out, "u undo") {
		t.Errorf("undo note:\n%s", out)
	}
}

// Spec (Review, TUI): o open full, s skip, c comment (text on the bottom
// line), a approve & next (bound to the commit shown), f fix, g run
// again, l log, u undo, esc back to Now.
func TestReviewPageKeys(t *testing.T) {
	r := NewReview(reviewSample())
	r.SetSize(120, 22)
	if a := r.Key(key("a")); a.Do != "approve" || a.Number != 412 || a.Head != "3f2a1c9" {
		t.Fatalf("a: %+v", a)
	}
	if a := r.Key(key("s")); a.Do != "skip" {
		t.Fatalf("s: %+v", a)
	}
	if a := r.Key(key("o")); a.Do != "openfull" {
		t.Fatalf("o: %+v", a)
	}
	r.Key(key("c"))
	typeText(r, "Please add a test.")
	if a := r.Key(key("enter")); a.Do != "comment" || a.Text != "Please add a test." || a.Head != "3f2a1c9" {
		t.Fatalf("comment: %+v", a)
	}
	if a := r.Key(key("f")); a.Do != "fix" || a.Text != "e2e-linux 1 failed" {
		t.Fatalf("f: %+v", a)
	}
	if a := r.Key(key("g")); a.Do != "again" || a.Text != "race test" {
		t.Fatalf("g: %+v", a)
	}
	if a := r.Key(key("u")); a.Do != "undo" {
		t.Fatalf("u: %+v", a)
	}
	if a := r.Key(key("esc")); a.Do != "close" {
		t.Fatalf("esc: %+v", a)
	}
}
