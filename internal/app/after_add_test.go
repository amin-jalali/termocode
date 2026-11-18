//go:build unix

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/afteradd"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// afterAddTestRule is a fake rule: one check that finds a database in a
// container, and a suggestion to add it as a connection.
func afterAddTestRule() afteradd.Rule {
	return afteradd.Rule{Kind: afteradd.KindSSH,
		Checks: []afteradd.Check{{Name: "dbs", Run: func(context.Context, afteradd.Event) ([]afteradd.Finding, error) {
			return []afteradd.Finding{{Text: "postgres in container shop-db (port 5432)"}}, nil
		}}},
		Suggest: func(ev afteradd.Event, fs []afteradd.Finding) *afteradd.Suggestion {
			return &afteradd.Suggestion{Title: "Found a database on " + ev.Item,
				Action: afteradd.Action{ID: "db-add", Label: "Add DB connection", Op: "db-new-connection"}}
		},
	}
}

// afterAddRun runs one event through the engine the way Update does.
func afterAddRun(t *testing.T, m *Model, ev afteradd.Event) {
	t.Helper()
	cmd := m.afterAdd(ev)
	if cmd == nil {
		t.Fatal("afterAdd returned no command")
	}
	msg, ok := cmd().(afterAddMsg)
	if !ok {
		t.Fatal("not an afterAddMsg")
	}
	m.onAfterAdd(msg)
}

// Spec (owner 2026-11-15, after-add 1): an add event runs its read-only
// checks and shows ONE card: what was found, Not now first (the safe,
// focused choice), the action second. The action is an existing palette
// op and runs only on the user's press. Not now is remembered for that
// item: the same host does not ask again, even after a restart.
func TestAfterAddCardFlow(t *testing.T) {
	m := agentMCPTestModel(t)
	m.afterAddEngine().Add(afterAddTestRule())
	var ran []string
	afterAddRunOp = func(_ *Model, op, arg string) tea.Cmd { ran = append(ran, op+"|"+arg); return nil }
	t.Cleanup(func() { afterAddRunOp = afterAddRunOpReal })

	ev := afteradd.Event{Kind: afteradd.KindSSH, Item: "web1"}
	afterAddRun(t, m, ev)
	if !m.confirmOpen || m.confirmKind != confirmKindAfterAdd {
		t.Fatal("no after-add card")
	}
	view := strings.Join(strings.Fields(m.confirm.View()), " ")
	for _, want := range []string{"Found a database on web1", "postgres in container shop-db", "Add DB connection", "Not now"} {
		if !strings.Contains(view, want) {
			t.Errorf("card misses %q:\n%s", want, view)
		}
	}
	checkCardRule(t, "after-add suggestion", *m, false)
	if len(ran) != 0 {
		t.Fatal("the action ran before the press")
	}

	// The action: the palette op runs, nothing is remembered.
	m.confirmOpen = false
	m.handleConfirmSelect("do")
	if strings.Join(ran, ",") != "db-new-connection|" {
		t.Fatalf("ran %v", ran)
	}
	afterAddRun(t, m, ev)
	if !m.confirmOpen {
		t.Fatal("after the action the card does not come back for a new add")
	}

	// Not now: remembered in the config folder, for this item only.
	m.confirmOpen = false
	m.handleConfirmSelect("skip")
	afterAddRun(t, m, ev)
	if m.confirmOpen {
		t.Fatal("Not now did not stick")
	}
	dir, _ := configDir()
	if b, err := os.ReadFile(filepath.Join(dir, "after_add.json")); err != nil || !strings.Contains(string(b), "ssh:web1|db-add") {
		t.Fatalf("not saved: %v %s", err, b)
	}
	afterAddRun(t, m, afteradd.Event{Kind: afteradd.KindSSH, Item: "web2"})
	if !m.confirmOpen {
		t.Fatal("another host lost its card")
	}
	m.confirmOpen = false
	m.handleConfirmSelect("skip")
	if len(ran) != 1 {
		t.Fatalf("Not now ran an action: %v", ran)
	}

	// A fresh engine (a restart) reads the same file.
	m.afterAddS = nil
	m.afterAddEngine().Add(afterAddTestRule())
	afterAddRun(t, m, ev)
	if m.confirmOpen {
		t.Fatal("Not now forgotten after a restart")
	}
}

// A card that arrives while another dialog is open waits and shows when
// that one is answered; nothing is lost and nothing jumps over the open
// dialog. Esc on the card is Not now.
func TestAfterAddCardWaitsForAnOpenDialog(t *testing.T) {
	m := agentMCPTestModel(t)
	m.afterAddEngine().Add(afterAddTestRule())
	afterAddRunOp = func(*Model, string, string) tea.Cmd { return nil }
	t.Cleanup(func() { afterAddRunOp = afterAddRunOpReal })

	m.confirmOpen, m.confirmKind = true, confirmKindAgentMCP // someone else's card
	afterAddRun(t, m, afteradd.Event{Kind: afteradd.KindSSH, Item: "web1"})
	if m.confirmKind != confirmKindAgentMCP {
		t.Fatal("the suggestion jumped over the open dialog")
	}
	m.confirmOpen = false
	m.onAfterAddRetry(afterAddRetryMsg{})
	if !m.confirmOpen || m.confirmKind != confirmKindAfterAdd {
		t.Fatal("the waiting card did not show")
	}
	m.confirmOpen = false
	m.onAIConfirmClosed()
	if !m.afterAddEngine().Later().Has("ssh:web1|db-add") {
		t.Fatal("Esc is not Not now")
	}
}

// A card that arrives while the user types in a picker or a prompt
// waits too: opened on top it took the next keys, and Enter answered
// Not now for good without the user seeing the card.
func TestAfterAddCardWaitsForAPickerOrPrompt(t *testing.T) {
	m := agentMCPTestModel(t)
	m.afterAddEngine().Add(afterAddTestRule())
	afterAddRunOp = func(*Model, string, string) tea.Cmd { return nil }
	t.Cleanup(func() { afterAddRunOp = afterAddRunOpReal })

	for i, open := range []func(){func() { m.pickerOpen = true }, func() { m.promptOpen = true }} {
		open()
		afterAddRun(t, m, afteradd.Event{Kind: afteradd.KindSSH, Item: "web" + string(rune('1'+i))})
		if m.confirmOpen {
			t.Fatalf("case %d: the card opened over the open picker or prompt", i)
		}
		m.pickerOpen, m.promptOpen = false, false
		m.onAfterAddRetry(afterAddRetryMsg{})
		if !m.confirmOpen || m.confirmKind != confirmKindAfterAdd {
			t.Fatalf("case %d: the waiting card did not show", i)
		}
		m.confirmOpen = false
		m.onAIConfirmClosed()
	}
}

// The web sees the same card (kind suggest): Not now is the safe choice,
// Do it runs the op on the laptop and closes the laptop's card.
func TestAfterAddWebCard(t *testing.T) {
	m := agentMCPTestModel(t)
	m.afterAddEngine().Add(afterAddTestRule())
	var ran []string
	afterAddRunOp = func(_ *Model, op, _ string) tea.Cmd { ran = append(ran, op); return nil }
	t.Cleanup(func() { afterAddRunOp = afterAddRunOpReal })

	afterAddRun(t, m, afteradd.Event{Kind: afteradd.KindSSH, Item: "web1"})
	refs := m.webCards(&webCardsPub{}, false)
	var ref *webCardRef
	for i := range refs {
		if refs[i].card.Kind == webproto.CardSuggest {
			ref = &refs[i]
		}
	}
	if ref == nil {
		t.Fatal("no suggest card for the web")
	}
	c := ref.card
	if c.Title != "Found a database on web1" || !strings.Contains(c.Body, "postgres in container shop-db") || !strings.Contains(c.Body, "Add DB connection") {
		t.Fatalf("card %+v", c)
	}
	if len(c.Choices) != 2 || c.Choices[0].ID != webproto.ChoiceLater || !c.Choices[0].Safe || c.Choices[1].ID != webproto.ChoiceDo {
		t.Fatalf("choices %+v", c.Choices)
	}
	m.webAnswerCard(ref, webproto.ChoiceDo, nil)
	if strings.Join(ran, ",") != "db-new-connection" || m.confirmOpen {
		t.Fatalf("ran %v open %v", ran, m.confirmOpen)
	}
	for _, r := range m.webCards(&webCardsPub{}, false) {
		if r.card.Kind == webproto.CardSuggest {
			t.Fatal("answered card still listed")
		}
	}
}
