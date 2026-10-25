package lspinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/amin-jalali/termocode/internal/jsonc"
)

// devcontainerLangs maps a language id to the tools that serve it.
var devcontainerLangs = map[string][]string{
	"go":     {"gopls", "dlv"},
	"python": {"pyright", "debugpy"},
	"node":   {"ts_ls", "js-debug"},
	"rust":   {"rust_analyzer"},
	"cpp":    {"clangd"},
	"lua":    {"lua_ls"},
	"shell":  {"bashls"},
	"toml":   {"taplo"},
}

// devcontainerExtensions maps extension ids (lower case) to a language id.
var devcontainerExtensions = map[string]string{
	"golang.go":                             "go",
	"ms-python.python":                      "python",
	"ms-python.vscode-pylance":              "python",
	"dbaeumer.vscode-eslint":                "node",
	"ms-vscode.js-debug":                    "node",
	"rust-lang.rust-analyzer":               "rust",
	"ms-vscode.cpptools":                    "cpp",
	"llvm-vs-code-extensions.vscode-clangd": "cpp",
	"sumneko.lua":                           "lua",
	"tamasfe.even-better-toml":              "toml",
	"timonwong.shellcheck":                  "shell",
}

// devcontainerWords maps a word found in a feature id or image name to a
// language id. Matched on whole parts split at "/", ":", "-" and so on.
var devcontainerWords = map[string]string{
	"go": "go", "golang": "go",
	"python": "python",
	"node":   "node", "javascript": "node", "typescript": "node",
	"rust": "rust",
	"cpp":  "cpp", "gcc": "cpp", "clang": "cpp",
}

func devcontainerWord(s string) string {
	parts := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return r == '/' || r == ':' || r == '-' || r == '_' || r == '.' || r == '@'
	})
	for _, p := range parts {
		if l, ok := devcontainerWords[p]; ok {
			return l
		}
	}
	return ""
}

// DevContainerFile returns the dev container config path of a workspace, or "".
func DevContainerFile(root string) string {
	for _, rel := range []string{filepath.Join(".devcontainer", "devcontainer.json"), ".devcontainer.json"} {
		p := filepath.Join(root, rel)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// DevContainerTools reads the workspace's dev container file (JSONC) and
// returns the registry tools it implies, in registry order. It only reads
// the file: no network, no docker. Missing or broken files give nil.
func DevContainerTools(root string) []Tool {
	p := DevContainerFile(root)
	if p == "" {
		return nil
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	return devContainerToolsFrom(b)
}

func devContainerToolsFrom(src []byte) []Tool {
	var cfg struct {
		Image          string                     `json:"image"`
		Features       map[string]json.RawMessage `json:"features"`
		Customizations struct {
			Vscode struct {
				Extensions []string `json:"extensions"`
			} `json:"vscode"`
		} `json:"customizations"`
	}
	if json.Unmarshal(jsonc.Strip(src), &cfg) != nil {
		return nil
	}
	langs := map[string]bool{}
	add := func(l string) {
		if l != "" {
			langs[l] = true
		}
	}
	add(devcontainerWord(cfg.Image))
	for k := range cfg.Features {
		add(devcontainerWord(k))
	}
	for _, e := range cfg.Customizations.Vscode.Extensions {
		add(devcontainerExtensions[strings.ToLower(e)])
	}
	want := map[string]bool{}
	for l := range langs {
		for _, n := range devcontainerLangs[l] {
			want[n] = true
		}
	}
	var out []Tool
	for _, t := range All() {
		if want[t.Name] {
			out = append(out, t)
		}
	}
	return out
}

// MissingOnly keeps the tools that available reports as missing.
func MissingOnly(ts []Tool, available func(Tool) bool) []Tool {
	var out []Tool
	for _, t := range ts {
		if !available(t) {
			out = append(out, t)
		}
	}
	return out
}

// devcontainerAskedFile remembers the workspaces whose dev container
// suggestion was shown, so it appears once per workspace.
const devcontainerAskedFile = "devcontainer-asked.json"

type devcontainerAskedState struct {
	Workspaces []string `json:"workspaces"`
}

func devcontainerAskedPath() (string, error) {
	root, err := ToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, devcontainerAskedFile), nil
}

func loadDevContainerAsked() devcontainerAskedState {
	var s devcontainerAskedState
	if p, err := devcontainerAskedPath(); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &s)
		}
	}
	return s
}

// DevContainerAsked reports whether the suggestion was already shown for root.
func DevContainerAsked(root string) bool {
	for _, w := range loadDevContainerAsked().Workspaces {
		if w == root {
			return true
		}
	}
	return false
}

// MarkDevContainerAsked records root (best effort).
func MarkDevContainerAsked(root string) error {
	p, err := devcontainerAskedPath()
	if err != nil {
		return err
	}
	s := loadDevContainerAsked()
	s.Workspaces = append(s.Workspaces, root)
	sort.Strings(s.Workspaces)
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
