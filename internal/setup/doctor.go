package setup

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Status is the outcome of one doctor check.
type Status int

const (
	OK      Status = iota // requirement met
	Warn                  // optional / cannot verify
	Missing               // required tool not found
)

// Check is one line of the doctor report.
type Check struct {
	Group  string
	Name   string
	Status Status
	Detail string // path / version on success, install hint otherwise
}

// lookPath and runOutput are seams so tests can fake the environment.
var (
	lookPath  = exec.LookPath
	runOutput = func(name string, args ...string) (string, error) {
		out, err := exec.Command(name, args...).Output()
		return string(out), err
	}
)

type tool struct {
	group, bin, label, hint string
	required                bool
}

// doctorTools lists every external binary termocode uses. Shared by the
// in-app doctor and `termocode setup`.
var doctorTools = []tool{
	{"core", "nvim", "neovim (editor engine)", "https://neovim.io/ (≥ 0.10)", true},
	{"core", "rg", "ripgrep (workspace search)", "apt/brew install ripgrep (pure-Go fallback is used otherwise)", false},
	{"core", "git", "git (Source Control)", "apt/brew install git", false},
	{"lsp", "gopls", "gopls (Go)", "go install golang.org/x/tools/gopls@latest", false},
	{"lsp", "pyright-langserver", "pyright (Python)", "npm i -g pyright", false},
	{"lsp", "pylsp", "python-lsp-server (Python)", "pipx install python-lsp-server", false},
	{"lsp", "typescript-language-server", "ts_ls (JS/TS)", "npm i -g typescript typescript-language-server", false},
	{"lsp", "rust-analyzer", "rust-analyzer (Rust)", "rustup component add rust-analyzer", false},
	{"lsp", "clangd", "clangd (C/C++)", "apt install clangd", false},
	{"lsp", "lua-language-server", "lua_ls (Lua)", "https://github.com/LuaLS/lua-language-server", false},
	{"dap", "dlv", "delve (Go debugger)", "go install github.com/go-delve/delve/cmd/dlv@latest", false},
	{"dap", "node", "node (JS debugger)", "https://nodejs.org/", false},
}

// Doctor runs every read-only check. It never installs anything, so it is
// safe to call from inside the running app.
func Doctor() []Check {
	var out []Check
	for _, t := range doctorTools {
		c := Check{Group: t.group, Name: t.label}
		path, err := lookPath(t.bin)
		switch {
		case err == nil:
			c.Status, c.Detail = OK, path
			if t.bin == "nvim" {
				if v, err := runOutput("nvim", "--version"); err == nil {
					c.Detail = firstLine(v) + " · " + path
				}
			}
		case t.required:
			c.Status, c.Detail = Missing, t.hint
		default:
			c.Status, c.Detail = Warn, t.hint
		}
		out = append(out, c)
	}

	term := Check{Group: "terminal", Name: "truecolor"}
	switch ct := os.Getenv("COLORTERM"); ct {
	case "truecolor", "24bit":
		term.Status, term.Detail = OK, "COLORTERM="+ct
	default:
		term.Status = Warn
		term.Detail = "COLORTERM not set — run `termocode setup --test-colors` to check visually"
	}
	out = append(out, term)

	font := Check{Group: "terminal", Name: "Nerd Font"}
	if hasNerdFont() {
		font.Status, font.Detail = OK, "found via fc-list"
	} else {
		font.Status = Warn
		font.Detail = "not detected — run `termocode setup` (Linux) or install " + jbmName
	}
	out = append(out, font)
	return out
}

// Report renders checks as plain text for the preview overlay.
func Report(checks []Check) string {
	var b strings.Builder
	group := ""
	missing := 0
	for _, c := range checks {
		if c.Group != group {
			if group != "" {
				b.WriteString("\n")
			}
			group = c.Group
			fmt.Fprintf(&b, "== %s ==\n", group)
		}
		mark := "✓"
		switch c.Status {
		case Warn:
			mark = "?"
		case Missing:
			mark = "✗"
			missing++
		}
		fmt.Fprintf(&b, "%s %s — %s\n", mark, c.Name, c.Detail)
	}
	b.WriteString("\n")
	if missing == 0 {
		b.WriteString("All required tools found. '?' rows are optional.\n")
	} else {
		fmt.Fprintf(&b, "%d required tool(s) missing.\n", missing)
	}
	b.WriteString("Run `termocode setup` in a shell to install the Nerd Font and gopls.")
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}
