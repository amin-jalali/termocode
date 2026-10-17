package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/agents"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
)

// termTestModel is a Model with an open terminal panel and two tabs.
func termTestModel(w int) Model {
	return Model{w: w, h: 24, showExp: false, termOpen: true, terminalRows: 8,
		panelActive: panelKindTerminal,
		terminalTabs: []terminalTab{
			{BufID: 5, Name: "proj", Cwd: "/p", Shell: "bash", LastExit: -1},
			{BufID: 7, Name: "tmp", Cwd: "/tmp", Shell: "bash", LastExit: -1},
		},
		terminalActiveTab: 1,
	}
}

// Spec (T1): the OSC events drive each tab: C sets the command and
// busy, D the exit code, the Fix chip only on the tab on show, OSC 7 the
// folder and the tab name; a remote host's OSC 7 is ignored; events of
// unknown buffers are dropped; a hidden tab still updates.
func TestOnTermOSC(t *testing.T) {
	m := termTestModel(100)
	m.onTermOSC(7, "\x1b]133;C;cmdline_url=go test ./...")
	if tb := m.terminalTabs[1]; !tb.Integrated || !tb.Busy || tb.LastCmd != "go test ./..." {
		t.Fatalf("after C: %+v", tb)
	}
	m.onTermOSC(7, "\x1b]133;D;1")
	if tb := m.terminalTabs[1]; tb.Busy || tb.LastExit != 1 || !tb.FixChip {
		t.Fatalf("after D;1 on the visible tab: %+v", tb)
	}
	// Not on show: no chip.
	m.onTermOSC(5, "\x1b]133;C;cmdline_url=false")
	m.onTermOSC(5, "\x1b]133;D;1")
	if tb := m.terminalTabs[0]; tb.LastExit != 1 || tb.FixChip {
		t.Errorf("hidden-from-view tab got the chip: %+v", tb)
	}
	// Ctrl+C is not a failure to fix.
	m.onTermOSC(7, "\x1b]133;C;cmdline_url=sleep 9")
	m.onTermOSC(7, "\x1b]133;D;130")
	if m.terminalTabs[1].FixChip {
		t.Error("chip after Ctrl+C")
	}
	// The next command clears it.
	m.onTermOSC(7, "\x1b]133;C;cmdline_url=false")
	m.onTermOSC(7, "\x1b]133;D;1")
	m.onTermOSC(7, "\x1b]133;C;cmdline_url=ls")
	if m.terminalTabs[1].FixChip {
		t.Error("chip stays on the next command")
	}
	m.onTermOSC(7, "\x1b]7;file:///home/u/my%20app")
	if tb := m.terminalTabs[1]; tb.Cwd != "/home/u/my app" || tb.Name != "my app" || m.terminalCwd != "/home/u/my app" {
		t.Errorf("after OSC 7: %+v cwd %q", tb, m.terminalCwd)
	}
	m.onTermOSC(7, "\x1b]7;file://some-remote-host.example/srv")
	if m.terminalTabs[1].Cwd != "/home/u/my app" {
		t.Error("a remote OSC 7 moved the tab")
	}
	m.onTermOSC(99, "\x1b]133;D;1") // unknown buffer
	m.onTermOSC(7, "\x1b]0;title")  // not ours
	// Hidden panel (Ctrl+T): the tabs still follow their shells.
	m.hiddenTerminalTabs, m.terminalTabs = m.terminalTabs, nil
	m.onTermOSC(5, "\x1b]133;D;0")
	if m.hiddenTerminalTabs[0].LastExit != 0 {
		t.Error("a hidden tab missed its event")
	}
}

// Spec (U8): `claude` in a shell makes the tab an agent tab until that
// command ends; an agent started from the palette keeps its id when the
// shell reports it.
func TestOnTermOSCAgent(t *testing.T) {
	m := termTestModel(100)
	m.onTermOSC(7, "\x1b]133;C;cmdline_url=claude --continue")
	if tb := m.terminalTabs[1]; tb.Agent != "claude" || tb.AgentState != agents.Working || tb.agentTitle() != "Claude Code" {
		t.Fatalf("agent not seen: %+v", tb)
	}
	if m.panelEntryName(panelBarEntry{Kind: panelKindTerminal, Index: 1}) != "Claude Code" {
		t.Error("the tab is not named after the agent")
	}
	m.onTermOSC(7, "\x1b]133;D;0")
	if tb := m.terminalTabs[1]; tb.Agent != "" || tb.Name != "tmp" {
		t.Errorf("agent stays after it quit: %+v", tb)
	}
}

// Spec (T2): the chip shows only while AI is set up; it sits after the
// name inside the tab, the click lands on it, and the bar keeps its
// width at 80 columns.
func TestTabBarFixChip(t *testing.T) {
	m := termTestModel(80)
	m.terminalTabs[1].FixChip = true
	m.terminalTabs[1].LastCmd, m.terminalTabs[1].LastExit = "false", 1
	w := m.editorPaneWidth()
	if strings.Contains(stripANSITerminal(m.renderTerminalTabBar(w)), "Fix?") {
		t.Error("chip without AI")
	}
	m.ai = newAIState()
	m.ai.provider = fakeAIProvider{}
	bar := m.renderTerminalTabBar(w)
	if lipgloss.Width(bar) != w {
		t.Fatalf("bar width %d, want %d", lipgloss.Width(bar), w)
	}
	plain := stripANSITerminal(bar)
	if !strings.Contains(plain, "tmp "+termFixChipLabel()+" "+theme.GlyphClose.String()) {
		t.Errorf("chip not after the name: %q", plain)
	}
	layout := m.computeTerminalTabBarLayout(w)
	slot := layout.Tabs[1]
	if slot.ChipEnd-slot.ChipStart != termFixChipW {
		t.Fatalf("chip slot %+v", slot)
	}
	if got := cellSlice(plain, slot.ChipStart, slot.ChipEnd); got != termFixChipLabel() {
		t.Errorf("chip cells %q", got)
	}
	y := m.terminalTabBarRowAbsolute()
	x0 := m.w - w
	if hit, idx := m.terminalTabBarHitTest(x0+slot.ChipStart+2, y); hit != terminalHitFixChip || idx != 1 {
		t.Errorf("chip click: %v %d", hit, idx)
	}
	if hit, _ := m.terminalTabBarHitTest(x0+slot.CloseStart, y); hit != terminalHitTabClose {
		t.Errorf("close-x moved: %v", hit)
	}
	if hit, _ := m.terminalTabBarHitTest(x0+slot.Start+3, y); hit != terminalHitTabActivate {
		t.Errorf("name click: %v", hit)
	}
	m.zenMode = true
	if m.termTabHasChip(panelBarEntry{Kind: panelKindTerminal, Index: 1}) {
		t.Error("chip in zen mode")
	}
}

// cellSlice returns cells [a, b) of a plain one-line string.
func cellSlice(s string, a, b int) string {
	r := []rune(s)
	if b > len(r) {
		b = len(r)
	}
	return string(r[a:b])
}

// Spec (U8): an agent tab shows its state mark in the glyph cell.
func TestTabBarAgentMark(t *testing.T) {
	m := termTestModel(100)
	for _, st := range []agents.State{agents.Working, agents.Waiting, agents.Done} {
		m.terminalTabs[0].Agent, m.terminalTabs[0].AgentState = "codex", st
		plain := stripANSITerminal(m.renderTerminalTabBar(m.editorPaneWidth()))
		mark, _ := agentMark(st)
		if !strings.Contains(plain, " "+mark+" Codex ") {
			t.Errorf("%s: no mark %q: %q", st, mark, plain)
		}
	}
}

// Spec (T2): the prompt names the command, the exit code and the folder;
// secrets in the command and the output are redacted; the output keeps
// its end within the cap.
func TestTermFixPromptAndRedact(t *testing.T) {
	p := termFixPrompt("curl -H 'Authorization: Bearer abcdefghijklmnopqrstuvwxyz' x", 6, "/srv/app")
	for _, want := range []string{"exit code 6", "/srv/app", "Bearer [REDACTED]", "one fixed command"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q:\n%s", want, p)
		}
	}
	out := termFixRedact(strings.Repeat("é", termFixMaxBytes) + "\nkey sk-ant-abcdefghijklmnop end")
	if len(out) > termFixMaxBytes+16 || !strings.HasSuffix(out, "key [REDACTED] end") || !strings.HasPrefix(out, "… [cut]\n") {
		t.Errorf("cap / redact: len %d, tail %q", len(out), out[len(out)-30:])
	}
	if strings.ContainsRune(out, '\uFFFD') {
		t.Error("cut inside a rune")
	}
}

// Spec (U8): "Agent: Start …" rows show only for programs on PATH; the
// docs catalog and the AI policy know every row.
func TestAgentPaletteFilter(t *testing.T) {
	defer func(f func(string) (string, error)) { agentLookPath = f }(agentLookPath)
	agentLookPath = func(bin string) (string, error) {
		if bin == "codex" {
			return "/usr/bin/codex", nil
		}
		return "", errors.New("not found")
	}
	items := agentPaletteFilter(append([]picker.Item{{ID: "tasks-run"}}, agentPaletteItems()...))
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	if strings.Join(ids, ",") != "tasks-run,agent-start-codex" {
		t.Errorf("ids %v", ids)
	}
	if len(agentPaletteItems()) != 4 {
		t.Errorf("want 4 palette agents")
	}
	for _, it := range agentPaletteItems() {
		if p, ok := aiActionPolicyFor(it.ID); !ok || p.Gate != aiGateConfirm {
			t.Errorf("%s: policy %+v", it.ID, p)
		}
	}
}

// Spec (T1): the spawn spec starts bash with the script, unless the
// setting is off or the shell has no script.
func TestTermShellSpec(t *testing.T) {
	dir := withConfigDir(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if s := termShellSpecJSON("/bin/bash"); !strings.Contains(s, `"--init-file"`) || !strings.Contains(s, `"plain":"/bin/bash"`) {
		t.Errorf("bash spec %s", s)
	}
	if s := termShellSpecJSON("/bin/sh"); s != `{"plain":"/bin/sh"}` {
		t.Errorf("sh spec %s", s)
	}
	_ = os.MkdirAll(filepath.Join(dir, "termocode"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, "termocode", "config.json"), []byte(`{"terminal_shell_integration": false}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if s := termShellSpecJSON("/bin/bash"); s != `{"plain":"/bin/bash"}` {
		t.Errorf("off: %s", s)
	}
}
