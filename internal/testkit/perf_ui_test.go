package testkit

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/perfbudget"
)

// Perf guards for the palette, pickers, AI chat, result grid and GRAPH
// (PERF-4). Like perf_budget_test.go they run only with TERMOCODE_PERF=1
// and check internal/perfbudget/budgets.txt.

// median is the middle of ds.
func median(ds []time.Duration) time.Duration { return percentile(ds, 50) }

// typeTimed types each key of keys into the open search field and times
// the frame that shows the field with what was typed so far.
func typeTimed(t *testing.T, d *Driver, keys []string, field func(typed string) func(string) bool) []time.Duration {
	t.Helper()
	typed := ""
	var took []time.Duration
	for _, k := range keys {
		msg := KeyMsg(k)
		if k == "backspace" {
			typed = typed[:len(typed)-1]
		} else {
			typed += k
		}
		dt, ok := waitFrame(d, msg, field(typed), 5*time.Second)
		if !ok {
			t.Fatalf("after %q the field does not show %q:\n%s", k, typed, screen(d))
		}
		took = append(took, dt)
	}
	return took
}

// searchShows matches a frame whose picker search field holds typed
// (the field row ends with the caret).
func searchShows(typed string) func(string) bool {
	return func(s string) bool {
		for _, l := range strings.Split(s, "\n") {
			if typed == "" {
				if strings.Contains(l, "Type to search") {
					return true
				}
			} else if strings.Contains(l, " "+typed+"▏") {
				return true
			}
		}
		return false
	}
}

var filterKeys = []string{"g", "i", "t", " ", "l", "o", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace"}

// One typed letter in the command palette (every command listed): the
// median time from the key to the frame with the filtered list.
func TestPerfPaletteFilterKey(t *testing.T) {
	perfbudget.Require(t)
	d := New(t, Options{W: 120, H: 40})
	if _, ok := waitFrame(d, KeyMsg("f1"), func(s string) bool { return strings.Contains(s, "Command Palette") }, 5*time.Second); !ok {
		t.Fatalf("F1 did not open the palette:\n%s", screen(d))
	}
	took := typeTimed(t, d, filterKeys, searchShows)
	t.Logf("palette key to frame: p50 %v, max %v", median(took), percentile(took, 100))
	perfbudget.Check(t, "palette_filter_key", ms(median(took)))
}

// The commit picker (Git: Show Log…, 200 commits of a 300-commit
// history): opening it (git log included) and one typed letter.
func TestPerfCommitPicker(t *testing.T) {
	perfbudget.Require(t)
	d, _ := scmDriver(t, func(r *repo) { bigHistory(r, 300) })
	var open []time.Duration
	for i := 0; i < 5; i++ {
		dt, ok := waitFrame(d, app.DevActionMsg{ID: "palette:git-log"}, func(s string) bool {
			return strings.Contains(s, "Git Log") && strings.Contains(s, "change number 300")
		}, 5*time.Second)
		if !ok {
			t.Fatalf("Git: Show Log did not open:\n%s", screen(d))
		}
		open = append(open, dt)
		if i < 4 {
			d.Key("esc")
		}
	}
	keys := []string{"c", "h", "a", "n", "g", "e", "backspace", "backspace", "backspace", "backspace", "backspace", "backspace"}
	took := typeTimed(t, d, keys, searchShows)
	t.Logf("commit picker: open p50 %v; key p50 %v", median(open), median(took))
	perfbudget.Check(t, "commit_picker_open", ms(median(open)))
	perfbudget.Check(t, "commit_picker_filter_key", ms(median(took)))
}

// The AI profile picker: opening it (median of five).
func TestPerfProfilePickerOpen(t *testing.T) {
	perfbudget.Require(t)
	d := New(t, Options{W: 160, H: 44, Setup: func(t testing.TB, _ string) {
		t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
		t.Setenv("TERMOCODE_AI_MODEL", "qwen2.5-coder")
	}})
	var open []time.Duration
	for i := 0; i < 5; i++ {
		dt, ok := waitFrame(d, app.DevActionMsg{ID: "palette:ai-profile-switch"}, func(s string) bool {
			return strings.Contains(s, "AI Profile") && strings.Contains(s, "Edit a profile")
		}, 5*time.Second)
		if !ok {
			t.Fatalf("the profile picker did not open:\n%s", screen(d))
		}
		open = append(open, dt)
		d.Key("esc")
	}
	perfbudget.Check(t, "profile_picker_open", ms(median(open)))
}

// A streamed token at the end of a 500-message AI chat (Markdown answers
// with code blocks): the median time from the token to the frame that
// shows it.
func TestPerfAIChatStreamToken(t *testing.T) {
	perfbudget.Require(t)
	d := New(t, Options{W: 200, H: 50, Setup: func(t testing.TB, _ string) {
		t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
		t.Setenv("TERMOCODE_AI_MODEL", "qwen2.5-coder")
	}})
	d.Palette("ai-open-chat")
	d.Action("aidemo-long:500")
	if !d.WaitForText("And how do I fix it?", 5*time.Second) {
		t.Fatalf("the long chat did not show:\n%s", screen(d))
	}
	var took []time.Duration
	for i := 0; i < 60; i++ {
		word := fmt.Sprintf("w%03d", i)
		tok := " " + word
		switch i % 10 {
		case 4:
			tok = " **" + word + "**"
		case 7:
			tok = " `" + word + "`\n\n"
		}
		dt, ok := waitFrame(d, app.DevActionMsg{ID: "aitoken:" + tok}, func(s string) bool { return strings.Contains(s, word) }, 5*time.Second)
		if !ok {
			t.Fatalf("token %d (%q) did not show:\n%s", i, tok, screen(d))
		}
		took = append(took, dt)
	}
	t.Logf("token to frame: p50 %v, p95 %v", median(took), percentile(took, 95))
	perfbudget.Check(t, "ai_chat_stream_token", ms(median(took)))
}

// Paging through a result grid of 100 000 rows (real nvim shows the
// page): the median time from ] to the frame with the next page.
func TestPerfDBGridPage100k(t *testing.T) {
	perfbudget.Require(t)
	d := editorDriver(t, nil)
	d.Settle(time.Second)
	d.Action("dbdemo-big:100000")
	if !d.WaitForText("page 1 of 1000", 10*time.Second) {
		t.Fatalf("the big result did not show:\n%s", screen(d))
	}
	var took []time.Duration
	for p := 2; p <= 13; p++ {
		want := fmt.Sprintf("page %d of 1000", p)
		dt, ok := waitFrame(d, app.DevActionMsg{ID: "dbkey:]"}, func(s string) bool { return strings.Contains(s, want) }, 5*time.Second)
		if !ok {
			t.Fatalf("] did not show %s:\n%s", want, screen(d))
		}
		took = append(took, dt)
	}
	t.Logf("page change: p50 %v, max %v", median(took), percentile(took, 100))
	perfbudget.Check(t, "db_grid_page_100k", ms(median(took)))
}

// A GRAPH refresh in a 5 000-commit history: a commit made outside,
// then Git: Refresh; the median time to the new commit in the GRAPH.
func TestPerfSCMGraphRefresh5k(t *testing.T) {
	perfbudget.Require(t)
	d, r := scmDriver(t, func(r *repo) { bigHistory(r, 5000) })
	var took []time.Duration
	for i := 0; i < 5; i++ {
		subject := fmt.Sprintf("zq%d", i) // short: the GRAPH row cuts long subjects
		r.write(fmt.Sprintf("outside%d.txt", i), "x\n")
		r.git("add", ".")
		r.git("commit", "-q", "-m", subject)
		dt, ok := waitFrame(d, app.DevActionMsg{ID: "palette:git-refresh"}, func(s string) bool { return strings.Contains(s, subject) }, 10*time.Second)
		if !ok {
			t.Fatalf("the GRAPH does not show %q:\n%s", subject, screen(d))
		}
		took = append(took, dt)
	}
	slices.Sort(took)
	t.Logf("refresh: p50 %v, max %v", median(took), took[len(took)-1])
	perfbudget.Check(t, "scm_graph_refresh_5k", ms(median(took)))
}
