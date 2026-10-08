// Package lspinstall is termocode's language-server / debug-adapter
// installer: a single registry of known tools (ported from mobocode's
// LanguageRegistry + toolchain catalog), the install recipes per
// InstallKind, and the managed tools dir
// (~/.local/share/termocode/tools/<name>/ with shims in tools/bin).
//
// The registry is the single source of truth for:
//   - the nvim LSP `servers` table (see LuaServersTable, used by lsp_lua.go),
//   - the in-editor manager (palette "LSP: Manage Language Servers…" /
//     "DAP: Install Adapter…"),
//   - `termocode setup`'s language-server report.
//
// Pure logic (registry lookups, plans, asset names, failure classification)
// lives apart from the I/O in install.go so it is unit-testable without
// touching the network.
package lspinstall

import (
	"sort"
	"strings"
)

// Category separates language servers from debug adapters.
type Category int

const (
	CategoryLSP Category = iota
	CategoryDAP
)

// InstallKind says how a tool is installed.
type InstallKind int

const (
	// KindGo: `go install <Spec>@<Version>` with GOBIN=<dir>/bin.
	KindGo InstallKind = iota
	// KindNpm: `npm install --prefix <dir> <Spec>@<Version> <Extra...>`.
	KindNpm
	// KindPip: venv at <dir>/venv, then `pip install <Spec>[==Version]`.
	KindPip
	// KindCargo: `cargo install --root <dir> <Spec> [--version V] <Extra...>`.
	KindCargo
	// KindRustup: `rustup component add <Spec>`; the shim points at
	// `rustup which <Bin>` (rustup owns the files, not the tools dir).
	KindRustup
	// KindGitHubRelease: download a release asset (Spec = owner/repo) and
	// unpack it into <dir>.
	KindGitHubRelease
)

func (k InstallKind) String() string {
	switch k {
	case KindGo:
		return "go"
	case KindNpm:
		return "npm"
	case KindPip:
		return "pip"
	case KindCargo:
		return "cargo"
	case KindRustup:
		return "rustup"
	case KindGitHubRelease:
		return "github-release"
	}
	return "unknown"
}

// Tool is one installable language server or debug adapter.
type Tool struct {
	// Name is the stable id. For LSP servers it is also the
	// `vim.lsp.start{name=…}` client name (gopls, ts_ls, …).
	Name     string
	Label    string // human language label, e.g. "Go"
	Category Category

	// Filetypes are nvim filetypes this tool serves.
	Filetypes []string
	// Bin is the executable name looked up on PATH (and the shim name in
	// tools/bin).
	Bin string

	InstallKind InstallKind
	// Spec is the package / module / crate / component / owner/repo.
	Spec string
	// Version is "latest" or a pinned version (no leading "v" for
	// github-release; the Tag template adds it when needed).
	Version string
	// Extra are additional packages (npm) or flags (cargo).
	Extra []string

	// Exec is the shim command line. "{dir}" expands to the tool dir and
	// "{version}" to Version. Empty → the kind's default location for Bin.
	Exec []string
	// Needs lists host executables that must exist at install AND run
	// time beyond the kind's own toolchain (e.g. js-debug needs node).
	Needs []string

	// GitHub release only.
	Tag   string // e.g. "v{version}" or "{version}"
	Asset string // e.g. "lua-language-server-{version}-{os}-{arch}.tar.gz"
	// OS / Arch map GOOS / GOARCH to the asset's {os} / {arch} tokens. A
	// nil map means "any, use the Go name"; a non-nil map without the host
	// key means unsupported.
	OS   map[string]string
	Arch map[string]string
	// Platforms, when set, whitelists "goos/goarch" pairs.
	Platforms []string

	// LSP only: the command nvim spawns (first element normally == Bin) and
	// the root markers.
	Cmd  []string
	Root []string

	// Fallback is a manual install hint shown when no automatic recipe fits
	// this host (e.g. clangd on linux/arm64).
	Fallback string
}

// tools is the registry. Order is the display order in the manager and the
// preference order when several servers share a filetype (first = default
// suggestion).
var tools = []Tool{
	// ── Language servers ────────────────────────────────────────────────
	{
		Name: "gopls", Label: "Go", Category: CategoryLSP,
		Filetypes:   []string{"go", "gomod", "gowork", "gotmpl"},
		Bin:         "gopls",
		InstallKind: KindGo, Spec: "golang.org/x/tools/gopls", Version: "latest",
		Cmd:  []string{"gopls"},
		Root: []string{"go.mod", "go.work", ".git"},
	},
	{
		Name: "pyright", Label: "Python", Category: CategoryLSP,
		Filetypes:   []string{"python"},
		Bin:         "pyright-langserver",
		InstallKind: KindNpm, Spec: "pyright", Version: "latest",
		Cmd:  []string{"pyright-langserver", "--stdio"},
		Root: []string{"pyproject.toml", "setup.py", "setup.cfg", "requirements.txt", "Pipfile", ".git"},
	},
	{
		Name: "pylsp", Label: "Python", Category: CategoryLSP,
		Filetypes:   []string{"python"},
		Bin:         "pylsp",
		InstallKind: KindPip, Spec: "python-lsp-server", Version: "latest",
		Cmd:  []string{"pylsp"},
		Root: []string{"pyproject.toml", "setup.py", ".git"},
	},
	{
		Name: "ts_ls", Label: "JS/TS", Category: CategoryLSP,
		Filetypes:   []string{"javascript", "javascriptreact", "typescript", "typescriptreact"},
		Bin:         "typescript-language-server",
		InstallKind: KindNpm, Spec: "typescript-language-server", Version: "latest",
		Extra: []string{"typescript"},
		Cmd:   []string{"typescript-language-server", "--stdio"},
		Root:  []string{"package.json", "tsconfig.json", "jsconfig.json", ".git"},
	},
	{
		Name: "rust_analyzer", Label: "Rust", Category: CategoryLSP,
		Filetypes:   []string{"rust"},
		Bin:         "rust-analyzer",
		InstallKind: KindRustup, Spec: "rust-analyzer", Version: "stable",
		Cmd:  []string{"rust-analyzer"},
		Root: []string{"Cargo.toml", ".git"},
	},
	{
		Name: "clangd", Label: "C/C++", Category: CategoryLSP,
		Filetypes:   []string{"c", "cpp", "objc", "objcpp"},
		Bin:         "clangd",
		InstallKind: KindGitHubRelease, Spec: "clangd/clangd", Version: "19.1.2",
		Tag:   "{version}",
		Asset: "clangd-{os}-{version}.zip",
		OS:    map[string]string{"linux": "linux", "darwin": "mac"},
		// Upstream only publishes x86_64 Linux builds (the mac build runs
		// under Rosetta on Apple silicon).
		Platforms: []string{"linux/amd64", "darwin/amd64", "darwin/arm64"},
		Exec:      []string{"{dir}/clangd_{version}/bin/clangd"},
		Cmd:       []string{"clangd"},
		Root:      []string{"compile_commands.json", "compile_flags.txt", ".clangd", ".git"},
		Fallback:  "install clangd with your package manager (e.g. sudo apt install clangd)",
	},
	{
		Name: "lua_ls", Label: "Lua", Category: CategoryLSP,
		Filetypes:   []string{"lua"},
		Bin:         "lua-language-server",
		InstallKind: KindGitHubRelease, Spec: "LuaLS/lua-language-server", Version: "3.13.5",
		Tag:   "{version}",
		Asset: "lua-language-server-{version}-{os}-{arch}.tar.gz",
		OS:    map[string]string{"linux": "linux", "darwin": "darwin"},
		Arch:  map[string]string{"amd64": "x64", "arm64": "arm64"},
		Exec:  []string{"{dir}/bin/lua-language-server"},
		Cmd:   []string{"lua-language-server"},
		Root:  []string{".luarc.json", ".luarc.jsonc", ".luacheckrc", ".git"},
	},
	{
		Name: "bashls", Label: "Shell", Category: CategoryLSP,
		Filetypes:   []string{"sh", "bash"},
		Bin:         "bash-language-server",
		InstallKind: KindNpm, Spec: "bash-language-server", Version: "latest",
		Cmd:  []string{"bash-language-server", "start"},
		Root: []string{".git"},
	},
	{
		Name: "taplo", Label: "TOML", Category: CategoryLSP,
		Filetypes:   []string{"toml"},
		Bin:         "taplo",
		InstallKind: KindCargo, Spec: "taplo-cli", Version: "latest",
		Extra: []string{"--locked", "--features", "lsp"},
		Cmd:   []string{"taplo", "lsp", "stdio"},
		Root:  []string{".taplo.toml", "taplo.toml", ".git"},
	},

	// ── Debug adapters ──────────────────────────────────────────────────
	{
		Name: "dlv", Label: "Go", Category: CategoryDAP,
		Filetypes:   []string{"go"},
		Bin:         "dlv",
		InstallKind: KindGo, Spec: "github.com/go-delve/delve/cmd/dlv", Version: "latest",
	},
	{
		Name: "debugpy", Label: "Python", Category: CategoryDAP,
		Filetypes:   []string{"python"},
		Bin:         "debugpy-adapter",
		InstallKind: KindPip, Spec: "debugpy", Version: "latest",
		Exec: []string{"{dir}/venv/bin/python", "-m", "debugpy.adapter"},
	},
	{
		Name: "js-debug", Label: "JS/TS", Category: CategoryDAP,
		Filetypes:   []string{"javascript", "javascriptreact", "typescript", "typescriptreact"},
		Bin:         "js-debug-adapter",
		InstallKind: KindGitHubRelease, Spec: "microsoft/vscode-js-debug", Version: "1.96.0",
		Tag:   "v{version}",
		Asset: "js-debug-dap-v{version}.tar.gz",
		// Pure JS: any OS / arch with node works.
		Exec:  []string{"node", "{dir}/js-debug/src/dapDebugServer.js"},
		Needs: []string{"node"},
	},
}

// All returns every registered tool (copy of the slice header; treat the
// entries as read-only).
func All() []Tool { return tools }

// ByCategory returns the tools of one category in registry order.
func ByCategory(c Category) []Tool {
	var out []Tool
	for _, t := range tools {
		if t.Category == c {
			out = append(out, t)
		}
	}
	return out
}

// Lookup returns the tool with the given name.
func Lookup(name string) (Tool, bool) {
	for _, t := range tools {
		if t.Name == name {
			return t, true
		}
	}
	return Tool{}, false
}

// ForFiletype returns the tools of category c that serve filetype ft, in
// preference order.
func ForFiletype(c Category, ft string) []Tool {
	if ft == "" {
		return nil
	}
	var out []Tool
	for _, t := range tools {
		if t.Category != c {
			continue
		}
		for _, x := range t.Filetypes {
			if x == ft {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// Filetypes returns every filetype covered by category c, sorted.
func Filetypes(c Category) []string {
	seen := map[string]bool{}
	var out []string
	for _, t := range ByCategory(c) {
		for _, ft := range t.Filetypes {
			if !seen[ft] {
				seen[ft] = true
				out = append(out, ft)
			}
		}
	}
	sort.Strings(out)
	return out
}

// DisplayName is "gopls (Go)".
func (t Tool) DisplayName() string {
	if t.Label == "" {
		return t.Name
	}
	return t.Name + " (" + t.Label + ")"
}

// expand replaces {dir} / {version} in s.
func (t Tool) expand(s, dir string) string {
	s = strings.ReplaceAll(s, "{dir}", dir)
	return strings.ReplaceAll(s, "{version}", t.Version)
}
