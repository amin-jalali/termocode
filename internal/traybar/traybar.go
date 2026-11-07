// Package traybar turns the running termocode sessions and the agents in
// their terminal tabs into a short summary for an OS menu bar, tray or
// status bar (CMP-50): one title ("tc 2 waiting · 1 working"), a class
// for colour, and one line per session. `termocode bar` prints it in
// three forms: plain text, one JSON line (text, tooltip, class — the
// shape status-bar modules read) and the plugin format of menu-bar tools
// (title, "---", items with actions). The menu only attaches; it never
// ends a session, because that needs a confirm a menu cannot show.
package traybar

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/agentctl"
)

// Session is one running termocode session as the bar sees it.
type Session struct {
	Workspace string
	Attached  bool
	// Tabs are the terminal tabs from the session's control socket.
	Tabs []agentctl.Tab
	// Unreachable: the control socket did not answer (the UI is busy
	// or starting); the session still shows, without agents.
	Unreachable bool
}

// Summary counts sessions and agents by state.
type Summary struct {
	Sessions, Waiting, Working, Done int
}

// Classes of the summary, for a status bar's colour rules.
const (
	ClassNone    = "none"    // no session runs
	ClassIdle    = "idle"    // sessions, no agent busy or waiting
	ClassWorking = "working" // an agent works
	ClassWaiting = "waiting" // an agent waits for the user
)

// Formats of Render.
const (
	FormatText = "text"
	FormatJSON = "json"
	FormatMenu = "menu"
)

// Formats lists the forms Render knows, for usage lines.
var Formats = []string{FormatText, FormatJSON, FormatMenu}

// agents are the tabs that run an agent (a plain shell has none).
func agents(tabs []agentctl.Tab) []agentctl.Tab {
	var out []agentctl.Tab
	for _, t := range tabs {
		if t.Agent != "" {
			out = append(out, t)
		}
	}
	return out
}

// Summarize counts ss.
func Summarize(ss []Session) Summary {
	var s Summary
	for _, x := range ss {
		s.Sessions++
		for _, t := range agents(x.Tabs) {
			switch t.State {
			case "waiting":
				s.Waiting++
			case "working":
				s.Working++
			case "done":
				s.Done++
			}
		}
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// Title is the short bar text: waiting agents first (they need the
// user), then working ones; else done agents; else the session count.
func Title(s Summary) string {
	var parts []string
	if s.Waiting > 0 {
		parts = append(parts, fmt.Sprintf("%d waiting", s.Waiting))
	}
	if s.Working > 0 {
		parts = append(parts, fmt.Sprintf("%d working", s.Working))
	}
	switch {
	case len(parts) > 0:
	case s.Done > 0:
		parts = append(parts, fmt.Sprintf("%d done", s.Done))
	case s.Sessions > 0:
		parts = append(parts, plural(s.Sessions, "session", "sessions"))
	default:
		parts = append(parts, "no sessions")
	}
	return "tc " + strings.Join(parts, " · ")
}

// Class is the colour class of s.
func Class(s Summary) string {
	switch {
	case s.Waiting > 0:
		return ClassWaiting
	case s.Working > 0:
		return ClassWorking
	case s.Sessions > 0:
		return ClassIdle
	}
	return ClassNone
}

// short writes the folder with the home folder as ~.
func short(ws, home string) string {
	if home != "" {
		h := filepath.Clean(home)
		if ws == h {
			return "~"
		}
		if rest, ok := strings.CutPrefix(ws, h+string(filepath.Separator)); ok {
			return "~" + string(filepath.Separator) + rest
		}
	}
	return ws
}

func attachedWord(a bool) string {
	if a {
		return "attached"
	}
	return "detached"
}

// line is one session in the text form and the tooltip.
func line(x Session, home string) string {
	var who string
	switch ag := agents(x.Tabs); {
	case x.Unreachable:
		who = "no answer"
	case len(ag) == 0:
		who = "no agents"
	default:
		parts := make([]string, 0, len(ag))
		for _, t := range ag {
			parts = append(parts, strings.TrimSpace(t.Agent+" "+t.State))
		}
		who = strings.Join(parts, ", ")
	}
	return short(x.Workspace, home) + "  " + attachedWord(x.Attached) + "  " + who
}

// Text is the title and one line per session.
func Text(ss []Session, home string) string {
	var b strings.Builder
	b.WriteString(Title(Summarize(ss)))
	b.WriteByte('\n')
	for _, x := range ss {
		b.WriteString(line(x, home))
		b.WriteByte('\n')
	}
	return b.String()
}

type jsonSession struct {
	Workspace   string         `json:"workspace"`
	Attached    bool           `json:"attached"`
	Unreachable bool           `json:"unreachable,omitempty"`
	Agents      []agentctl.Tab `json:"agents,omitempty"`
}

type jsonBar struct {
	Text     string        `json:"text"`
	Tooltip  string        `json:"tooltip"`
	Class    string        `json:"class"`
	Sessions []jsonSession `json:"sessions"`
}

// JSON is one line: text, tooltip and class for a status-bar module,
// and the sessions with their agents for other scripts.
func JSON(ss []Session, home string) ([]byte, error) {
	sum := Summarize(ss)
	v := jsonBar{Text: Title(sum), Class: Class(sum), Sessions: []jsonSession{}}
	lines := make([]string, 0, len(ss))
	for _, x := range ss {
		lines = append(lines, line(x, home))
		v.Sessions = append(v.Sessions, jsonSession{Workspace: x.Workspace, Attached: x.Attached,
			Unreachable: x.Unreachable, Agents: agents(x.Tabs)})
	}
	if len(lines) == 0 {
		lines = append(lines, "no termocode session runs")
	}
	v.Tooltip = strings.Join(lines, "\n")
	return json.Marshal(v)
}

// menuSafe keeps a menu title on one line and free of the "|" that
// starts an item's options.
func menuSafe(s string) string {
	return strings.NewReplacer("|", "¦", "\n", " ", "\r", " ").Replace(s)
}

// menuQuote quotes an option value.
func menuQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ", "\r", " ").Replace(s) + `"`
}

// Menu is the plugin format of menu-bar tools: the title, "---", per
// session an item with its agents and an Attach sub item that runs
// `exe attach <folder>` in a terminal, then a refresh item.
func Menu(ss []Session, home, exe string) string {
	var b strings.Builder
	b.WriteString(menuSafe(Title(Summarize(ss))))
	b.WriteString("\n---\n")
	for _, x := range ss {
		b.WriteString(menuSafe(short(x.Workspace, home) + " · " + attachedWord(x.Attached)))
		b.WriteByte('\n')
		if x.Unreachable {
			b.WriteString("--no answer from this session\n")
		}
		for _, t := range agents(x.Tabs) {
			name := t.Agent
			if t.State != "" {
				name += " · " + t.State
			}
			b.WriteString("--" + menuSafe(name) + "\n")
		}
		fmt.Fprintf(&b, "--Attach | bash=%s param1=attach param2=%s terminal=true\n", menuQuote(exe), menuQuote(x.Workspace))
	}
	b.WriteString("---\nRefresh | refresh=true\n")
	return b.String()
}

// Render prints ss in format ("" is text).
func Render(format string, ss []Session, home, exe string) (string, error) {
	switch format {
	case "", FormatText:
		return Text(ss, home), nil
	case FormatJSON:
		b, err := JSON(ss, home)
		if err != nil {
			return "", err
		}
		return string(b) + "\n", nil
	case FormatMenu:
		return Menu(ss, home, exe), nil
	}
	return "", fmt.Errorf("unknown format %q (one of %s)", format, strings.Join(Formats, ", "))
}
