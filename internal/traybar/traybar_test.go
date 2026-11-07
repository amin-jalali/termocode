package traybar

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/agentctl"
)

func sample() []Session {
	return []Session{
		{Workspace: "/home/u/api", Attached: true, Tabs: []agentctl.Tab{
			{ID: "t1", Name: "zsh", Agent: "claude", State: "waiting"},
			{ID: "t2", Name: "build", Agent: "codex", State: "working"},
			{ID: "t3", Name: "shell"},
		}},
		{Workspace: "/home/u/web|site", Tabs: []agentctl.Tab{
			{ID: "t1", Name: "zsh", Agent: "claude", State: "done"},
		}},
		{Workspace: "/srv/old", Unreachable: true},
	}
}

// Spec (CMP-50): the summary counts sessions and the agents in their
// tabs by state; plain shells are no agents.
func TestSummarize(t *testing.T) {
	s := Summarize(sample())
	if s.Sessions != 3 || s.Waiting != 1 || s.Working != 1 || s.Done != 1 {
		t.Errorf("summary %+v", s)
	}
	if got := Summarize(nil); got != (Summary{}) {
		t.Errorf("empty summary %+v", got)
	}
}

// Spec (CMP-50): the bar title puts "waiting" first (it needs the user),
// then "working"; without agents it counts sessions; without sessions
// it says so.
func TestTitleAndClass(t *testing.T) {
	cases := []struct {
		s            Summary
		title, class string
	}{
		{Summary{}, "tc no sessions", ClassNone},
		{Summary{Sessions: 2}, "tc 2 sessions", ClassIdle},
		{Summary{Sessions: 1}, "tc 1 session", ClassIdle},
		{Summary{Sessions: 1, Done: 2}, "tc 2 done", ClassIdle},
		{Summary{Sessions: 1, Working: 1}, "tc 1 working", ClassWorking},
		{Summary{Sessions: 2, Waiting: 2, Working: 1}, "tc 2 waiting · 1 working", ClassWaiting},
	}
	for _, c := range cases {
		if got := Title(c.s); got != c.title {
			t.Errorf("Title(%+v) = %q, want %q", c.s, got, c.title)
		}
		if got := Class(c.s); got != c.class {
			t.Errorf("Class(%+v) = %q, want %q", c.s, got, c.class)
		}
	}
}

// Spec (CMP-50): the text form is the title, then one line per session
// with its folder (home as ~), attached or not, and its agents.
func TestText(t *testing.T) {
	out := Text(sample(), "/home/u")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("lines %q", out)
	}
	if lines[0] != "tc 1 waiting · 1 working" {
		t.Errorf("title %q", lines[0])
	}
	if !strings.Contains(lines[1], "~/api") || !strings.Contains(lines[1], "attached") ||
		!strings.Contains(lines[1], "claude waiting") || !strings.Contains(lines[1], "codex working") {
		t.Errorf("first session %q", lines[1])
	}
	if strings.Contains(lines[1], "shell") {
		t.Errorf("a plain shell tab is no agent: %q", lines[1])
	}
	if !strings.Contains(lines[2], "detached") || !strings.Contains(lines[2], "claude done") {
		t.Errorf("second session %q", lines[2])
	}
	if !strings.Contains(lines[3], "/srv/old") || !strings.Contains(lines[3], "no answer") {
		t.Errorf("unreachable session %q", lines[3])
	}
}

// Spec (CMP-50): the JSON form has the text, tooltip and class a status
// bar module reads, plus the sessions for other scripts.
func TestJSON(t *testing.T) {
	b, err := JSON(sample(), "/home/u")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Text     string `json:"text"`
		Tooltip  string `json:"tooltip"`
		Class    string `json:"class"`
		Sessions []struct {
			Workspace string         `json:"workspace"`
			Attached  bool           `json:"attached"`
			Agents    []agentctl.Tab `json:"agents"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	if v.Text != "tc 1 waiting · 1 working" || v.Class != ClassWaiting || !strings.Contains(v.Tooltip, "~/api") {
		t.Errorf("json %+v", v)
	}
	if len(v.Sessions) != 3 || v.Sessions[0].Workspace != "/home/u/api" || !v.Sessions[0].Attached || len(v.Sessions[0].Agents) != 2 {
		t.Errorf("sessions %+v", v.Sessions)
	}
	if strings.Contains(string(b), "\n") {
		t.Errorf("one JSON line per update: %q", b)
	}
}

// Spec (CMP-50): the menu form is the plugin format of menu-bar tools:
// title, "---", one item per session with an "Attach" sub item that runs
// `termocode attach <folder>` in a terminal, and a refresh item. A "|"
// in a folder name cannot break the item. No item ends a session (that
// needs a confirm the menu cannot show).
func TestMenu(t *testing.T) {
	out := Menu(sample(), "/home/u", "/usr/bin/termocode")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if lines[0] != "tc 1 waiting · 1 working" || lines[1] != "---" {
		t.Fatalf("head %q", out)
	}
	if !strings.Contains(out, `--Attach | bash="/usr/bin/termocode" param1=attach param2="/home/u/api" terminal=true`) {
		t.Errorf("no attach item: %q", out)
	}
	if !strings.Contains(out, "--claude · waiting") {
		t.Errorf("no agent line: %q", out)
	}
	for _, l := range lines {
		name, _, _ := strings.Cut(l, " | ")
		if strings.Contains(name, "|") {
			t.Errorf("a | in the title part breaks the item: %q", l)
		}
	}
	if strings.Contains(out, "param1=kill") {
		t.Error("the menu must not end sessions")
	}
	if !strings.HasSuffix(strings.TrimRight(out, "\n"), "Refresh | refresh=true") {
		t.Errorf("no refresh item: %q", out)
	}
	if got := Menu(nil, "/home/u", "termocode"); !strings.HasPrefix(got, "tc no sessions\n---\n") {
		t.Errorf("empty menu %q", got)
	}
}

// Spec (CMP-50): Render picks the form by name and refuses an unknown one.
func TestRender(t *testing.T) {
	for _, f := range []string{"", FormatText, FormatJSON, FormatMenu} {
		if _, err := Render(f, sample(), "/home/u", "termocode"); err != nil {
			t.Errorf("Render(%q): %v", f, err)
		}
	}
	if _, err := Render("yaml", nil, "", "termocode"); err == nil {
		t.Error("an unknown format must fail")
	}
}
