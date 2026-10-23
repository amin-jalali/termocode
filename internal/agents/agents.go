// Package agents knows the external AI coding CLIs that run in terminal
// tabs (Claude Code, Codex, Gemini CLI, OpenCode, Aider): how to find
// them in a command line, and what their screen says — working, waiting
// for the user (a permission prompt or a question) or done (back at
// their input box). The screen rules are small regex tables tested on
// sample screens (testdata/); they look only at the last lines.
package agents

import (
	"path"
	"regexp"
	"strings"
)

// Agent is one external CLI agent.
type Agent struct {
	ID    string // "claude"
	Title string // "Claude Code"
	Bin   string // the program name on PATH
	// Palette: listed as "Agent: Start <Title>" when Bin is on PATH.
	Palette bool
	// Packages are npm package names that run it through npx / bunx.
	Packages []string
	// MCP is how to give this agent termocode's MCP server for one
	// launch, without editing its own config files.
	MCP MCPRecipe
}

// Registry is every known agent, in palette order.
var Registry = []Agent{
	{ID: "claude", Title: "Claude Code", Bin: "claude", Palette: true, Packages: []string{"@anthropic-ai/claude-code"},
		MCP: MCPRecipe{Kind: MCPFlag, TempFile: "claude.json", Args: func(_, _, file string) []string {
			return []string{"--mcp-config", file}
		}}},
	{ID: "codex", Title: "Codex", Bin: "codex", Palette: true, Packages: []string{"@openai/codex"},
		MCP: MCPRecipe{Kind: MCPFlag, Args: func(exe, root, _ string) []string {
			return []string{
				"-c", "mcp_servers.termocode.command=" + tomlString(exe),
				"-c", "mcp_servers.termocode.args=[\"mcp\", \"--dir\", " + tomlString(root) + "]",
			}
		}}},
	{ID: "gemini", Title: "Gemini CLI", Bin: "gemini", Palette: true, Packages: []string{"@google/gemini-cli"},
		MCP: MCPRecipe{Kind: MCPNone, // no per-launch override known: the user adds it by hand
			NoteTitle: "add termocode by hand",
			Note:      "F1, then MCP: Copy termocode Server Config"}}, // the action first; it must fit one toast line
	{ID: "opencode", Title: "OpenCode", Bin: "opencode", Palette: true, Packages: []string{"opencode-ai"},
		MCP: MCPRecipe{Kind: MCPProjectFile, ProjectFile: "opencode.json"}},
	{ID: "aider", Title: "Aider", Bin: "aider", Packages: []string{"aider-chat"},
		MCP: MCPRecipe{Kind: MCPNone, // Aider has no MCP client
			NoteTitle: "no termocode tools",
			Note:      "Aider has no MCP support"}},
}

// ByID returns the agent with id.
func ByID(id string) (Agent, bool) {
	for _, a := range Registry {
		if a.ID == id {
			return a, true
		}
	}
	return Agent{}, false
}

// FromCommand finds the agent a shell command line starts: the first
// word after VAR=value assignments and the wrappers exec / command /
// time, by file name; npx / bunx / pnpx / pnpm dlx with the agent's
// package name count too.
func FromCommand(cmdline string) (Agent, bool) {
	words := strings.Fields(cmdline)
	i := 0
	for i < len(words) {
		w := words[i]
		if strings.Contains(w, "=") && !strings.HasPrefix(w, "=") && !strings.ContainsAny(w[:strings.Index(w, "=")], "/-.") {
			i++
			continue
		}
		if w == "exec" || w == "command" || w == "time" {
			i++
			continue
		}
		break
	}
	if i >= len(words) {
		return Agent{}, false
	}
	name := path.Base(words[i])
	switch name {
	case "npx", "bunx", "pnpx", "pnpm", "yarn":
		for _, w := range words[i+1:] {
			if strings.HasPrefix(w, "-") || w == "dlx" || w == "exec" {
				continue
			}
			pkg := w
			if at := strings.LastIndex(pkg, "@"); at > 0 {
				pkg = pkg[:at] // @scope/name@1.2 → @scope/name
			}
			for _, a := range Registry {
				for _, p := range a.Packages {
					if pkg == p {
						return a, true
					}
				}
				if pkg == a.Bin {
					return a, true
				}
			}
			break
		}
		return Agent{}, false
	}
	for _, a := range Registry {
		if name == a.Bin {
			return a, true
		}
	}
	return Agent{}, false
}

// State is what an agent's screen says.
type State int

const (
	Unknown State = iota // nothing matched: keep the last state
	Working              // thinking or running a tool
	Waiting              // asks the user: permission, a question, y/n
	Done                 // back at its input box, the turn is over
)

func (s State) String() string {
	switch s {
	case Working:
		return "working"
	case Waiting:
		return "waiting"
	case Done:
		return "done"
	}
	return "unknown"
}

// rules are the screen patterns of one agent.
type rules struct {
	waiting, working, done []*regexp.Regexp
}

func res(exprs ...string) []*regexp.Regexp {
	out := make([]*regexp.Regexp, len(exprs))
	for i, e := range exprs {
		out[i] = regexp.MustCompile(e)
	}
	return out
}

// anyWaiting is a y/n question in any agent.
var anyWaiting = res(`(?i)[\[(]y/n[\])]`, `\(Y\)es/\(N\)o`)

var table = map[string]rules{
	"claude": {
		// A numbered choice with the cursor on it, or the hints under a
		// question. Plain "Do you want to …" text is not enough: the
		// model may write that in its answer.
		waiting: res(`^\s*[│|]?\s*❯\s*\d+\.\s`, `(?i)enter to (select|confirm).*esc to cancel`, `(?i)\(esc\)\s*[│|]?\s*$`),
		working: res(`(?i)esc to interrupt`),
		done:    res(`(?i)\?\s+for shortcuts`, `^\s*│\s*>\s`, `(?i)(accept edits|plan mode|bypass permissions) on`),
	},
	"codex": {
		waiting: res(`(?i)would you like to (run|make|apply)`, `(?i)press enter to confirm or esc to cancel`, `(?i)^\s*›?\s*\d+\.\s*yes, proceed`),
		working: res(`(?i)esc to interrupt`),
		done:    res(`(?i)\?\s+for shortcuts`, `(?i)ask codex to do anything`, `(?i)context left`, `⏎ send`),
	},
	"gemini": {
		waiting: res(`(?i)waiting for user confirmation`, `(?i)allow execution`, `(?i)apply this change\?`, `(?i)yes, allow (once|always)`),
		working: res(`(?i)esc to cancel`),
		done:    res(`(?i)type your message`),
	},
	"opencode": {
		waiting: res(`(?i)permission required`, `(?i)allow once.*allow always`, `(?i)esc reject`),
		working: res(`(?i)esc (to )?interrupt`),
		done:    res(`(?i)enter send`),
	},
	"aider": {
		waiting: res(`(?i)\(Y\)es/\(N\)o`),
		working: res(`(?i)^\W*waiting for \S`),
		done:    res(`^(\w+)?> ?$`),
	},
}

// bottomLines is how many non-blank lines from the bottom count.
const bottomLines = 20

// Detect reads the last screen lines of agent id. Order: waiting, then
// working, then done (Claude Code keeps its input box on screen while it
// works). Unknown when nothing matches.
func Detect(id string, lines []string) State {
	r, ok := table[id]
	if !ok {
		return Unknown
	}
	var tail []string
	for i := len(lines) - 1; i >= 0 && len(tail) < bottomLines; i-- {
		l := strings.TrimRight(lines[i], " \t\r")
		if strings.TrimSpace(l) == "" {
			continue
		}
		tail = append(tail, l)
	}
	match := func(rs []*regexp.Regexp) bool {
		for _, l := range tail {
			for _, re := range rs {
				if re.MatchString(l) {
					return true
				}
			}
		}
		return false
	}
	switch {
	case match(r.waiting) || match(anyWaiting):
		return Waiting
	case match(r.working):
		return Working
	case match(r.done):
		return Done
	}
	return Unknown
}
