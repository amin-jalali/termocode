package app

import (
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/diffview"
)

// The compare card's keys go through the review host: y copies the
// script (its toast shows on top of the card), Esc closes the card and
// drops it.
func TestDBCompareCardKeysCopyAndClose(t *testing.T) {
	withConfigDir(t)
	var mu sync.Mutex
	var clip []string
	defer SetClipboardSink(func(s string) { mu.Lock(); clip = append(clip, s); mu.Unlock() })()
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := m0.(Model)
	m.runDevAction("dbdemo:compare")
	if !m.reviewOpen {
		t.Fatal("card not open")
	}
	press := func(k string) {
		var msg tea.KeyMsg
		switch k {
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, cmd := m.Update(msg)
		m = next.(Model)
		if k == "esc" && cmd != nil {
			next, _ = m.Update(cmd())
			m = next.(Model)
		}
	}
	press("y")
	mu.Lock()
	got := strings.Join(clip, "")
	mu.Unlock()
	for _, want := range []string{"-- review before running", `DROP TABLE "public"."legacy_sessions";`, `ALTER TABLE "public"."users" ADD COLUMN "plan" text NOT NULL DEFAULT 'free'::text;`, "-- REVIEW:"} {
		if !strings.Contains(got, want) {
			t.Errorf("copied script lacks %q:\n%s", want, got)
		}
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Migration script copied") || !strings.Contains(v, "Compare schemas") {
		t.Errorf("no toast over the card:\n%s", v)
	}
	press("esc")
	if m.reviewOpen || (m.ai != nil && m.ai.review != nil) {
		t.Errorf("esc did not close the card")
	}
}

// F1 on the compare pickers and card opens the Compare schemas section
// of the guide; a click on the card's button copies the script.
func TestDBCompareHelpTopicAndButtonClick(t *testing.T) {
	withConfigDir(t)
	var mu sync.Mutex
	var clip []string
	defer SetClipboardSink(func(s string) { mu.Lock(); clip = append(clip, s); mu.Unlock() })()
	want := "guides/databases.md#compare-schemas"
	for _, sc := range []string{"compare-pick", "compare"} {
		m0, _ := New().Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		m := m0.(Model)
		m.runDevAction("dbdemo:" + sc)
		if got := m.helpTopicForFocus(); got != want {
			t.Errorf("%s: help topic %q, want %q", sc, got, want)
		}
		if sc != "compare" {
			continue
		}
		var x, y int
		for _, h := range m.aiS().review.card.Hits() {
			if h.ID == "copy-script" {
				x, y = h.X+1, h.Y
			}
		}
		if x == 0 {
			t.Fatal("no copy button hit")
		}
		next, _ := m.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
		m = next.(Model)
		mu.Lock()
		n := len(clip)
		mu.Unlock()
		if n != 1 || !strings.Contains(clip[0], "-- review before running") {
			t.Errorf("click copied %d scripts", n)
		}
	}
}

// An AI change that arrives while the compare card is open gets its own
// review card (it is not added to the read-only compare card).
func TestDBCompareCardMakesRoomForAnAIReview(t *testing.T) {
	withConfigDir(t)
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m := m0.(Model)
	m.runDevAction("dbdemo:compare")
	m.addAIReview(&aiReviewItem{file: diffview.File{Name: "a.go", Old: "a\n", New: "b\n"}})
	r := m.aiS().review
	if r == nil || !r.card.Review() || r.onAction != nil || len(r.items) != 1 || r.card.Files() != 1 {
		t.Fatalf("AI change joined the compare card: review=%v", r != nil && r.card.Review())
	}
}

// The "Comparing…" toast goes away when the card opens: it must not cover
// the card's buttons.
func TestDBCompareProgressToastGoesWhenCardOpens(t *testing.T) {
	withConfigDir(t)
	m0, _ := New().Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	m := m0.(Model)
	m.runDevAction("dbdemo:compare-pick")
	m.pickerOpen = false
	cs := m.databases.compare
	cs.sides[1].sessKey, cs.sides[2].sessKey = "a", "b"
	m.databases.sessions = map[string]dbclient.Session{"a": nil, "b": nil}
	m.databases.sessions["a"], m.databases.sessions["b"] = fakeNilSession{}, fakeNilSession{}
	m.databaseCompareRun(cs.sides[1], cs.sides[2]) // pushes the progress toast
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Comparing") {
		t.Fatalf("no progress toast:\n%s", v)
	}
	src, tgt := dbCompareDemoSchemas()
	m.onDBCompareReady(dbCompareReadyMsg{gen: cs.gen, src: cs.sides[1], tgt: cs.sides[2], cmp: dbclient.CompareSchemas(src, tgt)})
	if v := ansi.Strip(m.View()); strings.Contains(v, "Comparing") || !strings.Contains(v, "Copy migration script") {
		t.Fatalf("toast still over the card:\n%s", v)
	}
}

type fakeNilSession struct{ dbclient.Session }
