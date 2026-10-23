//go:build unix

package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/toast"
)

// agentCardWorkspace makes a temp workspace the cwd (aiWorkspaceRoot).
func agentCardWorkspace(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	mcpServeExecutable = func() (string, error) { return "/opt/tc/termocode", nil }
	t.Cleanup(func() { mcpServeExecutable = os.Executable })
	return dir
}

// Spec (MS-4): the merge keeps the user's other settings, adds one
// entry, is a no-op when the entry is there, and refuses a file that is
// not plain JSON.
func TestOpencodeMCPMerge(t *testing.T) {
	out, changed, err := opencodeMCPMerge(nil, "/opt/tc/termocode", "/w")
	want := `{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "termocode": {
      "command": [
        "/opt/tc/termocode",
        "mcp",
        "--dir",
        "/w"
      ],
      "enabled": true,
      "type": "local"
    }
  }
}
`
	if err != nil || !changed || string(out) != want {
		t.Fatalf("new file: %v %v\n%s", err, changed, out)
	}
	again, changed, err := opencodeMCPMerge(out, "/opt/tc/termocode", "/w")
	if err != nil || changed || string(again) != string(out) {
		t.Fatalf("second merge: %v %v", err, changed)
	}
	old := `{"theme":"x","mcp":{"mine":{"type":"local","command":["a"]}}}`
	out, changed, err = opencodeMCPMerge([]byte(old), "/opt/tc/termocode", "/w")
	if err != nil || !changed || !strings.Contains(string(out), `"theme": "x"`) || !strings.Contains(string(out), `"mine"`) || !strings.Contains(string(out), `"termocode"`) {
		t.Fatalf("merge: %v\n%s", err, out)
	}
	if _, _, err := opencodeMCPMerge([]byte("{\n // note\n \"a\": 1}"), "/x", "/w"); err == nil {
		t.Error("comments accepted")
	}
	if _, _, err := opencodeMCPMerge([]byte(`{"mcp":[]}`), "/x", "/w"); err == nil {
		t.Error("odd mcp accepted")
	}
}

// Spec (MS-4): OpenCode with no entry opens the project-file card and
// writes nothing yet; "Not now" and Esc write nothing and the card does
// not return; "Add" writes the merged file (mode kept) and a second
// launch asks no more; mcp_server off asks nothing.
func TestAgentMCPCardFlow(t *testing.T) {
	m := agentMCPTestModel(t)
	dir := agentCardWorkspace(t)
	file := filepath.Join(dir, "opencode.json")
	const mine = `{"theme":"x"}`
	if err := os.WriteFile(file, []byte(mine), 0o640); err != nil {
		t.Fatal(err)
	}
	a := agentByID(t, "opencode")

	card, _ := m.agentMCPCardFor(a)
	if card == nil {
		t.Fatal("no card")
	}
	m.openAgentMCPCard(card)
	if !m.confirmOpen || m.confirmKind != confirmKindAgentMCP || !strings.Contains(m.confirm.View(), "opencode.json") {
		t.Fatal("card not shown")
	}
	if b, _ := os.ReadFile(file); string(b) != mine {
		t.Fatal("written before the answer")
	}
	// C-1: it writes into the user's project folder: Not now comes first
	// and Enter alone writes nothing.
	checkCardRule(t, "add to the project file", *m, false)
	if got := m.confirm.Buttons()[0].ID; got != "skip" {
		t.Errorf("first button %q, want skip", got)
	}
	// The user learns before committing the file that it holds paths of
	// this machine.
	view := strings.Join(strings.Fields(m.confirm.View()), " ")
	for _, want := range []string{"absolute paths of this machine", "the termocode program", "this folder", "before you commit"} {
		if !strings.Contains(view, want) {
			t.Errorf("card misses %q:\n%s", want, view)
		}
	}

	// Deny: nothing written, no second card.
	m.confirmOpen = false
	m.handleConfirmSelect("skip")
	if b, _ := os.ReadFile(file); string(b) != mine {
		t.Fatalf("deny wrote: %s", b)
	}
	if c, _ := m.agentMCPCardFor(a); c != nil {
		t.Error("card came back after Not now")
	}

	// Esc is a no as well.
	m.agentMCPDeclined = nil
	card, _ = m.agentMCPCardFor(a)
	m.openAgentMCPCard(card)
	m.confirmOpen = false
	m.onAIConfirmClosed()
	if b, _ := os.ReadFile(file); string(b) != mine {
		t.Fatalf("esc wrote: %s", b)
	}

	// Add.
	m.agentMCPDeclined = nil
	card, _ = m.agentMCPCardFor(a)
	m.openAgentMCPCard(card)
	m.confirmOpen = false
	m.handleConfirmSelect("add")
	b, _ := os.ReadFile(file)
	if !strings.Contains(string(b), `"theme": "x"`) || !strings.Contains(string(b), `"termocode"`) {
		t.Fatalf("add: %s", b)
	}
	if fi, _ := os.Stat(file); fi.Mode().Perm() != 0o640 {
		t.Errorf("mode %o", fi.Mode().Perm())
	}
	if ents, _ := filepath.Glob(filepath.Join(dir, ".termocode-*")); len(ents) != 0 {
		t.Errorf("temp left: %v", ents)
	}
	if c, _ := m.agentMCPCardFor(a); c != nil {
		t.Error("asked again though the entry is there")
	}

	// off: no card even for a file without the entry.
	if err := os.WriteFile(file, []byte(mine), 0o640); err != nil {
		t.Fatal(err)
	}
	mcpServeSetMode(t, "off")
	if c, _ := m.agentMCPCardFor(a); c != nil {
		t.Error("card with mcp_server off")
	}
}

// Spec (MS-4): a broken opencode.json (comments) is never rewritten; the
// agent starts and a toast says why.
func TestAgentMCPCardBrokenFile(t *testing.T) {
	m := agentMCPTestModel(t)
	dir := agentCardWorkspace(t)
	const jsonc = "{\n // mine\n \"a\": 1\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(jsonc), 0o644); err != nil {
		t.Fatal(err)
	}
	card, c := m.agentMCPCardFor(agentByID(t, "opencode"))
	if card != nil || c == nil || !strings.Contains(toastText(*m), "not plain JSON") {
		t.Fatalf("card=%v toast=%q", card, toastText(*m))
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "opencode.json")); string(b) != jsonc {
		t.Error("file rewritten")
	}
}

// Spec (MS-4): Gemini CLI and Aider get one toast per run that says how
// to add termocode by hand; a second launch is quiet; Claude Code, Codex
// and OpenCode get none; off gets none.
func TestAgentMCPHandTipOnce(t *testing.T) {
	m := agentMCPTestModel(t)
	for _, id := range []string{"claude", "codex", "opencode"} {
		if c := m.agentMCPHandTip(agentByID(t, id)); c != nil {
			t.Errorf("%s got a tip", id)
		}
	}
	if !m.toast.Empty() {
		t.Fatal("toast for flag agents")
	}
	if c := m.agentMCPHandTip(agentByID(t, "gemini")); c == nil {
		t.Fatal("no tip for gemini")
	}
	if txt := toastText(*m); !strings.Contains(txt, "Copy termocode Server Config") {
		t.Errorf("toast %q", txt)
	}
	if c := m.agentMCPHandTip(agentByID(t, "gemini")); c != nil {
		t.Error("gemini tip twice")
	}
	if c := m.agentMCPHandTip(agentByID(t, "aider")); c == nil {
		t.Error("no tip for aider")
	}
	mcpServeSetMode(t, "off")
	m.agentMCPTipped = nil
	if c := m.agentMCPHandTip(agentByID(t, "gemini")); c != nil {
		t.Error("tip with mcp_server off")
	}
}

// MCP-14: at 80x24 the whole card shows: no "…and N more lines", the warning
// about absolute paths and what Not now does stay visible, also with a long
// folder path.
func TestAgentMCPCardFitsEightyByTwentyFour(t *testing.T) {
	for _, long := range []bool{false, true} {
		m := agentMCPTestModel(t)
		dir := agentCardWorkspace(t)
		if long {
			dir = filepath.Join(dir, strings.Repeat("a-long-folder-name-", 4), "project")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			t.Chdir(dir)
		}
		m.w, m.h = 80, 24
		card, _ := m.agentMCPCardFor(agentByID(t, "opencode"))
		if card == nil {
			t.Fatal("no card")
		}
		m.openAgentMCPCard(card)
		view := ansi.Strip(m.confirm.View())
		flat := strings.Join(strings.Fields(view), " ")
		if strings.Contains(view, "more line") {
			t.Errorf("long=%v: the card is cut:\n%s", long, view)
		}
		for _, want := range []string{"absolute paths of this machine", "before you commit it", "Not now: OpenCode starts without them. Esc is the same.", "opencode.json"} {
			if !strings.Contains(flat, want) {
				t.Errorf("long=%v: the card lacks %q:\n%s", long, want, view)
			}
		}
		if rows := strings.Count(strings.TrimRight(view, "\n"), "\n") + 1; rows > 24 {
			t.Errorf("long=%v: the card is %d rows high", long, rows)
		}
	}
}

// MCP-15: the hand tips (Gemini CLI, Aider) fit the toast: the title and the
// detail are not cut with "…", and the action (F1, MCP: Copy termocode
// Server Config) comes first in the detail.
func TestAgentMCPHandTipsFitTheToast(t *testing.T) {
	for _, c := range []struct{ id, detail string }{
		{"gemini", "F1, then MCP: Copy termocode Server Config"},
		{"aider", "Aider has no MCP support"},
	} {
		m := agentMCPTestModel(t)
		a := agentByID(t, c.id)
		if a.MCP.Note != c.detail {
			t.Errorf("%s: note = %q, want %q", c.id, a.MCP.Note, c.detail)
		}
		for _, w := range []int{150, 80} {
			m.toast = toast.New()
			if cmd := m.agentMCPHandTip(a); cmd == nil {
				t.Fatalf("%s: no tip", c.id)
			}
			var rows []string
			for _, row := range m.toast.View(w, 42) {
				rows = append(rows, ansi.Strip(row))
			}
			all := strings.Join(rows, "\n")
			if !strings.Contains(all, a.Title+": ") || (w == 150 && (!strings.Contains(all, c.detail) || strings.Contains(all, "…"))) {
				t.Errorf("%s at %d columns: the toast is cut:\n%s", c.id, w, all)
			}
			m.agentMCPTipped = nil
		}
		if n := ansi.StringWidth(a.MCP.Note); n > 44 {
			t.Errorf("%s: note is %d cells, the toast line holds about 44", c.id, n)
		}
	}
}
