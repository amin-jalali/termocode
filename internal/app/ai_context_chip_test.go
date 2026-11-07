package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/webproto"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Spec (CMP-7): the bar is five cells, filled in fifths and never empty
// for a used window; the level turns amber from 70 % and red from 90 %.
func TestAIContextBarAndLevel(t *testing.T) {
	for _, c := range []struct {
		pct  int
		bar  string
		tone theme.Color256
	}{
		{0, "▱▱▱▱▱", theme.AccentGreen},
		{1, "▰▱▱▱▱", theme.AccentGreen},
		{42, "▰▰▱▱▱", theme.AccentGreen},
		{69, "▰▰▰▱▱", theme.AccentGreen},
		{70, "▰▰▰▰▱", theme.AccentAmber},
		{89, "▰▰▰▰▱", theme.AccentAmber},
		{90, "▰▰▰▰▰", theme.AccentRedCoral},
		{140, "▰▰▰▰▰", theme.AccentRedCoral},
	} {
		if got := aiContextBar(c.pct, false); got != c.bar {
			t.Errorf("bar(%d) = %q, want %q", c.pct, got, c.bar)
		}
		if got := aiContextTone(c.pct); got != c.tone {
			t.Errorf("tone(%d) = %v, want %v", c.pct, got, c.tone)
		}
	}
	if got := aiContextBar(42, true); got != "##---" {
		t.Errorf("ascii bar = %q", got)
	}
}

// Spec (CMP-7): after a turn the chat header shows how full the model's
// window was ("ctx ▰▱▱▱▱ 1%"); a click on it opens AI: Usage.
func TestAIContextChipAfterTurn(t *testing.T) {
	p := &scriptedAIProvider{replies: []ai.Chunk{
		{Delta: "done", Usage: &ai.Usage{Model: "claude-sonnet-5-5", Input: 9000, Output: 100, Requests: 1, PeakContext: 9000}},
	}}
	m, _ := aiIDETestModel(t, p)
	m.ai.cfg = ai.Config{Kind: ai.KindAnthropic, Model: "claude-sonnet-5-5"}
	m.output = newOutputStore()
	m.actionsOpen, m.actionsPinned = true, true
	m.w, m.h = 200, 40
	if got := m.aiContextChipLabel(); got != "" {
		t.Fatalf("chip before any turn = %q", got)
	}
	driveAIChat(t, m, m.sendAIChat("hi"), nil)
	tokens, window, pct, ok := m.ai.contextUse()
	if !ok || tokens != 9000 || window <= 0 || pct != 1 {
		t.Fatalf("context use = %d / %d (%d %%, %v)", tokens, window, pct, ok)
	}
	w := m.actionsColumnWidth()
	head := ansi.Strip(m.renderActionsHeader(w))
	i := strings.Index(head, "ctx ")
	if i < 0 || !strings.Contains(head, "1%") {
		t.Fatalf("no context chip in header %q", head)
	}
	x := m.w - w + ansi.StringWidth(head[:i]) + 1
	if id := m.hitActionsButton(x, 0); id != "ctx" {
		t.Fatalf("hit at chip = %q", id)
	}
	if id := m.aiPanelTargetAt(x, 0); id != "ai:ctx" {
		t.Fatalf("target at chip = %q", id)
	}
	m.handleAIPanelMouse(tea.MouseMsg{X: x, Y: 0, Type: tea.MouseLeft, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !m.agentsShowing() || !m.ai.agents.usage {
		t.Fatal("a click on the context chip did not open the usage view")
	}
}

// Spec (CMP-7): the chip belongs to its chat: another chat shows none
// until it has a turn of its own; an unknown window shows none.
func TestAIContextChipPerChat(t *testing.T) {
	agentsUnicode(t) // the expected text uses the Unicode glyphs
	m, _ := aiProfileTestModel(t)
	m.ai.noteContext(ai.Usage{Model: "x", PeakContext: 420}, 1000)
	if got := m.aiContextChipLabel(); got != "ctx ▰▰▱▱▱ 42%" {
		t.Fatalf("label = %q", got)
	}
	m.ai.conv = ai.NewConversation()
	if got := m.aiContextChipLabel(); got != "" {
		t.Fatalf("another chat shows %q", got)
	}
	m.ai.noteContext(ai.Usage{Model: "x", PeakContext: 420}, 0)
	if _, _, _, ok := m.ai.contextUse(); ok {
		t.Fatal("an unknown window still counts")
	}
}

// Spec (CMP-7): the chip drops before the profile chip in a narrow
// panel and the header never grows past the panel.
func TestAIContextChipNarrow(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.ai.noteContext(ai.Usage{Model: "x", PeakContext: 950}, 1000)
	for w := 20; w <= 90; w++ {
		head := ansi.Strip(m.renderActionsHeader(w))
		if got := ansi.StringWidth(head); got != w {
			t.Fatalf("width %d: header is %d cells: %q", w, got, head)
		}
		if strings.Contains(head, "ctx ") && !strings.Contains(head, "default") {
			t.Fatalf("width %d: context chip without the profile chip: %q", w, head)
		}
	}
	if head := ansi.Strip(m.renderActionsHeader(120)); !strings.Contains(head, "95%") {
		t.Fatalf("wide header has no context chip: %q", head)
	}
}

// Spec (CMP-7): the ⋯ menu row "Usage" names the context use, so a
// narrow panel still shows it.
func TestAIContextInMoreMenu(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.ai.noteContext(ai.Usage{Model: "x", PeakContext: 420}, 1000)
	if got := aiUsageMoreHint(m.ai); got != "context 42% · tokens and cost" {
		t.Fatalf("hint = %q", got)
	}
	m.ai.conv = ai.NewConversation()
	if got := aiUsageMoreHint(m.ai); got != "tokens and cost" {
		t.Fatalf("hint without use = %q", got)
	}
}

// Spec (CMP-7, web parity): the chat topic carries the context use of
// the open chat, and a state delta updates it.
func TestWebChatContextUse(t *testing.T) {
	m, _ := aiProfileTestModel(t)
	m.ai.noteContext(ai.Usage{Model: "x", PeakContext: 420}, 1000)
	snap := m.webChatBuild(&webChatPub{})
	if snap.CtxPct != 42 || snap.CtxTokens != 420 || snap.CtxWindow != 1000 {
		t.Fatalf("snap context = %d %% (%d / %d)", snap.CtxPct, snap.CtxTokens, snap.CtxWindow)
	}
	base, _ := json.Marshal(webproto.ChatSnap{ID: "c", Messages: []webproto.ChatMsg{}})
	pct, tok, win := 77, 770, 1000
	d, _ := json.Marshal(webproto.ChatDelta{Op: webproto.ChatState, CtxPct: &pct, CtxTokens: &tok, CtxWindow: &win})
	out, err := webApplyChat(base, d)
	if err != nil {
		t.Fatal(err)
	}
	var got webproto.ChatSnap
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.CtxPct != 77 || got.CtxTokens != 770 || got.CtxWindow != 1000 {
		t.Fatalf("applied context = %+v", got)
	}
	// A delta that merges keeps the newest context.
	a := webChatDelta{Op: webproto.ChatState, CtxPct: &pct}
	n := 80
	j, ok := a.coalesce(webChatDelta{Op: webproto.ChatState, CtxPct: &n})
	if !ok || *j.(webChatDelta).CtxPct != 80 {
		t.Fatalf("coalesce = %+v %v", j, ok)
	}
}
