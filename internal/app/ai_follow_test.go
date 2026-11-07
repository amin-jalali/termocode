package app

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/ext"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// Follow Agent (CMP-68): while it is on, the editor jumps to the file and
// line each agent edit lands on, so the user watches the work happen.

func TestAIFirstChangedLine(t *testing.T) {
	for _, tc := range []struct {
		old, new string
		want     int
	}{
		{"", "", 0},
		{"a\nb\n", "a\nb\n", 0},
		{"", "x\n", 1},
		{"a\nb\nc\n", "a\nB\nc\n", 2},
		{"a\nb\n", "a\nb\nc\n", 3},
		{"a\nb\nc\n", "a\nb\n", 3},
		{"a\nb\nc", "a\nb\nc\n", 3},
		{"x\n", "", 1},
	} {
		if got := aiFirstChangedLine(tc.old, tc.new); got != tc.want {
			t.Errorf("aiFirstChangedLine(%q, %q) = %d, want %d", tc.old, tc.new, got, tc.want)
		}
	}
}

// The palette has the toggle, the AI policy lets the agent turn it on
// only after a confirm, and the status bar says when it is on.
func TestAIFollowAgentToggle(t *testing.T) {
	found := false
	for _, e := range aiPaletteItems() {
		if e.id == "ai-follow-agent" && e.title == "AI: Toggle Follow Agent" {
			found = true
		}
	}
	if !found {
		t.Fatal("palette entry missing")
	}
	if p, ok := aiActionPolicies["ai-follow-agent"]; !ok || p.Gate != aiGateConfirm {
		t.Fatalf("AI policy %+v ok=%v, want a confirm policy", p, ok)
	}
	m := &Model{w: 120, h: 30, ai: newAIState()}
	status := func() string {
		var out []string
		for _, s := range m.aiFollowStatusTexts() {
			out = append(out, ext.PlainText(s))
		}
		return strings.Join(out, "|")
	}
	if m.ai.follow || status() != "" {
		t.Fatalf("on by default (status %q)", status())
	}
	if _, ok := m.dispatchAIPalette("ai-follow-agent"); !ok {
		t.Fatal("palette id not handled")
	}
	if !m.ai.follow || !strings.Contains(status(), "Following AI") {
		t.Fatalf("follow %v status %q", m.ai.follow, status())
	}
	if st := m.statusState(); !strings.Contains(strings.Join(st.Ext, "|"), "Following AI") {
		t.Fatalf("status bar misses the hint: %q", st.Ext)
	}
	m.dispatchAIPalette("ai-follow-agent")
	if m.ai.follow || status() != "" {
		t.Fatal("second toggle did not turn it off")
	}
}

func curBufLine(t *testing.T, c *nvim.Client) (string, int) {
	t.Helper()
	name, err := c.EvalLuaString(`return vim.api.nvim_buf_get_name(0)`)
	if err != nil {
		t.Fatal(err)
	}
	row, err := c.EvalLuaString(`return tostring(vim.api.nvim_win_get_cursor(0)[1])`)
	if err != nil {
		t.Fatal(err)
	}
	return name, atoiSafe(row)
}

func followEditProvider() *scriptedAIProvider {
	return &scriptedAIProvider{replies: []ai.Chunk{toolCall("edit_file", map[string]any{"path": "a.txt", "old_text": "three", "new_text": "THREE"})}}
}

// Follow on, user in the chat: the editor opens a.txt at the edited line
// and the model is told the user watches it.
func TestAIFollowAgentJumpsToEdit(t *testing.T) {
	p := followEditProvider()
	m, c, dir := aiSyncModel(t, p, map[string]string{"a.txt": "one\ntwo\nthree\nfour\n", "b.txt": "other\n"})
	m.openFileAtLine(filepath.Join(dir, "b.txt"), 1)
	m.ai.follow = true
	m.ai.focused = true // typing in the chat
	driveAIChat(t, m, m.sendAIChat("edit a"), nil)
	if r := oneResult(t, p); !strings.Contains(r, "Follow Agent") {
		t.Fatalf("result %q, want the follow note", r)
	}
	name, row := curBufLine(t, c)
	if name != filepath.Join(dir, "a.txt") || row != 3 {
		t.Fatalf("editor at %s:%d, want a.txt:3", name, row)
	}
	if !m.ai.focused {
		t.Error("follow took the focus away from the chat")
	}
	if got := m.ai.followAt; got != "a.txt:3" {
		t.Errorf("followAt %q", got)
	}
}

// Follow on, but the user works in the editor: no jump (the agent never
// pulls the cursor away from the user's own typing).
func TestAIFollowAgentPausesWhileUserEdits(t *testing.T) {
	p := followEditProvider()
	m, c, dir := aiSyncModel(t, p, map[string]string{"a.txt": "one\ntwo\nthree\nfour\n", "b.txt": "other\n"})
	b := filepath.Join(dir, "b.txt")
	m.openFileAtLine(b, 1)
	m.ai.follow = true
	m.ai.focused = false
	m.focus = FocusEditor
	driveAIChat(t, m, m.sendAIChat("edit a"), nil)
	if r := oneResult(t, p); strings.Contains(r, "Follow Agent") {
		t.Fatalf("result %q tells of a jump that did not happen", r)
	}
	if name, _ := curBufLine(t, c); name != b {
		t.Fatalf("editor moved to %s while the user edits b.txt", name)
	}
}

// Follow off (the default): the editor stays where the user left it.
func TestAIFollowAgentOffStays(t *testing.T) {
	p := followEditProvider()
	m, c, dir := aiSyncModel(t, p, map[string]string{"a.txt": "one\ntwo\nthree\nfour\n", "b.txt": "other\n"})
	b := filepath.Join(dir, "b.txt")
	m.openFileAtLine(b, 1)
	m.ai.focused = true
	driveAIChat(t, m, m.sendAIChat("edit a"), nil)
	if name, _ := curBufLine(t, c); name != b {
		t.Fatalf("editor moved to %s with follow off", name)
	}
}
