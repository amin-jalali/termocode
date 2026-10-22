package agents

import (
	"bytes"
	"encoding/json"
	"strings"
)

// MCPKind is how an agent CLI can be given termocode's MCP server.
type MCPKind int

const (
	// MCPNone: no per-launch way is known. The user adds `termocode mcp`
	// by hand (F1 → MCP: Copy termocode Server Config).
	MCPNone MCPKind = iota
	// MCPFlag: command-line flags carry it; nothing on disk of the
	// user's changes.
	MCPFlag
	// MCPProjectFile: only a project file works. Writing it needs the
	// user's yes first.
	MCPProjectFile
)

// MCPRecipe is the launch recipe of one agent. Data, not run-time
// guessing: each agent CLI's flags are checked on the manual checklist
// (Claude Code `--mcp-config` and Codex `-c key=value` come from the
// plan; they were not run against the real CLIs here).
type MCPRecipe struct {
	Kind MCPKind
	// TempFile is set when the flag takes a file: the config JSON goes
	// to a private temp file, named after this suffix, and Args gets its
	// path.
	TempFile string
	// Args returns the extra command-line words for MCPFlag: exe is the
	// termocode binary, root the workspace, file the temp file ("" when
	// TempFile is empty).
	Args func(exe, root, file string) []string
	// ProjectFile is the file MCPProjectFile merges into, relative to
	// the workspace.
	ProjectFile string
}

// tomlString quotes s as a TOML basic string (what Codex's -c reads).
func tomlString(s string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a JSON string is a valid TOML basic string
	return strings.TrimRight(b.String(), "\n")
}

// ShellWord quotes w for a POSIX shell and for fish: single quotes. A
// word with a quote, a control character or a newline has no quoting
// both agree on, so it is refused (ok=false) rather than guessed.
func ShellWord(w string) (string, bool) {
	for _, r := range w {
		if r == '\'' || r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	if w != "" && !strings.ContainsAny(w, " \t\"\\$`!&|;<>()*?[]{}#~=%^") {
		return w, true
	}
	return "'" + w + "'", true
}

// MCPLine is the command line that starts the agent with termocode's
// MCP server: Bin plus the recipe's words. ok=false when the agent has
// no MCPFlag recipe or a word cannot be quoted safely.
func (a Agent) MCPLine(exe, root, file string) (line string, ok bool) {
	if a.MCP.Kind != MCPFlag || a.MCP.Args == nil {
		return "", false
	}
	words := append([]string{a.Bin}, a.MCP.Args(exe, root, file)...)
	for i, w := range words {
		q, ok := ShellWord(w)
		if !ok {
			return "", false
		}
		words[i] = q
	}
	return strings.Join(words, " "), true
}
