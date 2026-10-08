package theme

import (
	"path/filepath"
	"strings"
)

// File-type icons for the explorer, tabs, Quick Open and find results.
//
// Every entry carries three glyph tiers (Nerd Font, plain Unicode, ASCII),
// picked by CurrentIconMode at call time, plus a pointer to a palette
// token. The color is read through the pointer on every call, so a theme
// switch (SetPalette) recolors icons without rebuilding the table. No hex
// lives here: icons are colored by the active theme only.
//
// Width contract: every glyph in every tier is exactly one terminal cell.
// Nerd Font glyphs are single BMP Private Use Area runes (nf-seti, nf-dev,
// nf-custom, nf-cod, nf-fa). Unicode glyphs are East-Asian-Width neutral,
// so they stay one cell even on terminals that treat "ambiguous" glyphs as
// wide. fileicons_test.go enforces both.

// fileIconDef is one row of the icon table.
type fileIconDef struct {
	nerd, uni, ascii string
	color            *Color256
}

func (d fileIconDef) glyph() string {
	switch CurrentIconMode {
	case IconModeNerdFont:
		return d.nerd
	case IconModeUnicode:
		return d.uni
	default:
		return d.ascii
	}
}

// Unicode and ASCII glyphs are per category, so files of the same kind
// share a shape in the plain tiers and differ only by color.
const (
	uniCode    = "▪" // U+25AA
	uniConfig  = "⋄" // U+22C4
	uniDoc     = "▭" // U+25AD
	uniImage   = "◍" // U+25CD
	uniArchive = "⊞" // U+229E
	uniShell   = "$"
	uniGit     = "⌥" // U+2325
	uniLock    = "▴" // U+25B4
	uniData    = "◊" // U+25CA
	uniFile    = "◦" // U+25E6
	uniDirC    = "▸" // U+25B8
	uniDirO    = "▾" // U+25BE

	asciiCode    = "#"
	asciiConfig  = "~"
	asciiDoc     = "="
	asciiImage   = "%"
	asciiArchive = "@"
	asciiShell   = "$"
	asciiGit     = "+"
	asciiLock    = "!"
	asciiData    = "&"
	asciiFile    = "-"
	asciiDirC    = ">"
	asciiDirO    = "v"
)

func fiCode(nerd string, c *Color256) fileIconDef { return fileIconDef{nerd, uniCode, asciiCode, c} }
func fiConfig(nerd string, c *Color256) fileIconDef {
	return fileIconDef{nerd, uniConfig, asciiConfig, c}
}
func fiDoc(nerd string, c *Color256) fileIconDef   { return fileIconDef{nerd, uniDoc, asciiDoc, c} }
func fiImage(nerd string, c *Color256) fileIconDef { return fileIconDef{nerd, uniImage, asciiImage, c} }
func fiArchive(nerd string, c *Color256) fileIconDef {
	return fileIconDef{nerd, uniArchive, asciiArchive, c}
}
func fiData(nerd string, c *Color256) fileIconDef { return fileIconDef{nerd, uniData, asciiData, c} }

var (
	fileIconDefault = fileIconDef{"", uniFile, asciiFile, &TextMuted}  // cod-file
	dirClosedIcon   = fileIconDef{"", uniDirC, asciiDirC, &AccentBlue} // custom-folder
	dirOpenIcon     = fileIconDef{"", uniDirO, asciiDirO, &AccentBlue} // custom-folder_open

	iconShell   = fileIconDef{"", uniShell, asciiShell, &AccentGreen} // seti-shell
	iconGitFile = fileIconDef{"", uniGit, asciiGit, &AccentRedCoral}  // dev-git
	iconLock    = fileIconDef{"", uniLock, asciiLock, &TextMuted}     // seti-lock
	iconLicense = fiDoc("", &AccentAmber)                             // seti-license
	iconReadme  = fiDoc("", &AccentBlue)                              // seti-info
	iconEnv     = fiConfig("", &AccentAmber)                          // cod-key
	iconDocker  = fiConfig("", &AccentBlue)                           // seti-docker
	iconMake    = fiConfig("", &AccentRedCoral)                       // seti-makefile
	iconGoMod   = fiConfig("", &AccentMagenta)                        // seti-go
	iconEditCfg = fiConfig("", &TextMuted)                            // seti-editorconfig
	iconNpm     = fiConfig("", &AccentRedCoral)                       // seti-npm
)

// fileIconsByName matches the full (lower-case) base name first.
var fileIconsByName = map[string]fileIconDef{
	"go.mod":            iconGoMod,
	"go.sum":            iconLock,
	"go.work":           iconGoMod,
	"package-lock.json": iconLock,
	"yarn.lock":         iconLock,
	"pnpm-lock.yaml":    iconLock,
	"cargo.lock":        iconLock,
	"package.json":      iconNpm,
	"dockerfile":        iconDocker,
	"containerfile":     iconDocker,
	"makefile":          iconMake,
	"gnumakefile":       iconMake,
	".gitignore":        iconGitFile,
	".gitattributes":    iconGitFile,
	".gitmodules":       iconGitFile,
	".gitkeep":          iconGitFile,
	".env":              iconEnv,
	".editorconfig":     iconEditCfg,
	"copying":           iconLicense,
}

// fileIconsByPrefix matches when the lower-case base name starts with the
// key (LICENSE.md, README.rst, .env.local, …). Checked after exact names.
var fileIconsByPrefix = []struct {
	prefix string
	def    fileIconDef
}{
	{"license", iconLicense},
	{"licence", iconLicense},
	{"readme", iconReadme},
	{".env.", iconEnv},
	{"dockerfile.", iconDocker},
}

// fileIconsByExt matches the lower-case extension without the dot.
var fileIconsByExt = map[string]fileIconDef{
	"go":      fiCode("", &AccentBlue),     // seti-go
	"rs":      fiCode("", &AccentAmber),    // dev-rust
	"py":      fiCode("", &AccentAmber),    // seti-python
	"js":      fiCode("", &AccentAmber),    // seti-javascript
	"mjs":     fiCode("", &AccentAmber),    // seti-javascript
	"cjs":     fiCode("", &AccentAmber),    // seti-javascript
	"ts":      fiCode("", &AccentBlue),     // seti-typescript
	"tsx":     fiCode("", &AccentBlue),     // seti-react
	"jsx":     fiCode("", &AccentBlue),     // seti-react
	"lua":     fiCode("", &AccentBlue),     // seti-lua
	"vim":     fiCode("", &AccentGreen),    // custom-vim
	"c":       fiCode("", &AccentBlue),     // seti-c
	"h":       fiCode("", &AccentLavender), // seti-c
	"cpp":     fiCode("", &AccentBlue),     // seti-cpp
	"cc":      fiCode("", &AccentBlue),     // seti-cpp
	"cxx":     fiCode("", &AccentBlue),     // seti-cpp
	"hpp":     fiCode("", &AccentLavender), // seti-cpp
	"cs":      fiCode("", &AccentMagenta),  // dev-csharp
	"java":    fiCode("", &AccentRedCoral), // seti-java
	"kt":      fiCode("", &AccentLavender), // seti-kotlin
	"kts":     fiCode("", &AccentLavender), // seti-kotlin
	"swift":   fiCode("", &AccentRedCoral), // seti-swift
	"rb":      fiCode("", &AccentRedCoral), // seti-ruby
	"php":     fiCode("", &AccentLavender), // seti-php
	"html":    fiCode("", &AccentRedCoral), // seti-html
	"htm":     fiCode("", &AccentRedCoral), // seti-html
	"css":     fiCode("", &AccentBlue),     // seti-css
	"scss":    fiCode("", &AccentMagenta),  // seti-sass
	"sass":    fiCode("", &AccentMagenta),  // seti-sass
	"vue":     fiCode("", &AccentGreen),    // seti-vue
	"svelte":  fiCode("", &AccentRedCoral), // seti-svelte
	"dart":    fiCode("", &AccentBlue),     // seti-dart
	"ex":      fiCode("", &AccentLavender), // seti-elixir
	"exs":     fiCode("", &AccentLavender), // seti-elixir
	"erl":     fiCode("", &AccentRedCoral), // dev-erlang
	"hrl":     fiCode("", &AccentRedCoral), // dev-erlang
	"hs":      fiCode("", &AccentLavender), // seti-haskell
	"ml":      fiCode("", &AccentAmber),    // seti-ocaml
	"mli":     fiCode("", &AccentAmber),    // seti-ocaml
	"nix":     fiCode("", &AccentBlue),     // dev-nixos
	"zig":     fiCode("", &AccentAmber),    // seti-zig
	"sql":     fiData("", &AccentAmber),    // dev-database
	"db":      fiData("", &AccentAmber),    // dev-database
	"proto":   fiData("", &AccentGreen),    // cod-file_code
	"graphql": fiData("", &AccentMagenta),  // seti-graphql
	"gql":     fiData("", &AccentMagenta),  // seti-graphql
	"csv":     fiData("", &AccentGreen),    // seti-csv
	"tsv":     fiData("", &AccentGreen),    // seti-csv

	"sh":   iconShell,
	"bash": iconShell,
	"zsh":  iconShell,
	"fish": iconShell,

	"json":   fiConfig("", &AccentAmber),    // seti-json
	"jsonc":  fiConfig("", &AccentAmber),    // seti-json
	"yaml":   fiConfig("", &AccentLavender), // seti-yml
	"yml":    fiConfig("", &AccentLavender), // seti-yml
	"toml":   fiConfig("", &AccentRedCoral), // custom-toml
	"xml":    fiConfig("", &AccentAmber),    // seti-xml
	"tf":     fiConfig("", &AccentLavender), // seti-terraform
	"tfvars": fiConfig("", &AccentLavender), // seti-terraform
	"ini":    fiConfig("", &TextMuted),      // seti-config
	"conf":   fiConfig("", &TextMuted),      // seti-config
	"cfg":    fiConfig("", &TextMuted),      // seti-config
	"env":    iconEnv,
	"lock":   iconLock,

	"md":  fiDoc("", &AccentBlue),     // seti-markdown
	"mdx": fiDoc("", &AccentAmber),    // seti-markdown
	"txt": fiDoc("", &TextMuted),      // seti-text
	"log": fiDoc("", &TextMuted),      // cod-output
	"pdf": fiDoc("", &AccentRedCoral), // seti-pdf
	"tex": fiDoc("", &AccentGreen),    // seti-tex

	"png":  fiImage("", &AccentMagenta), // seti-image
	"jpg":  fiImage("", &AccentMagenta),
	"jpeg": fiImage("", &AccentMagenta),
	"gif":  fiImage("", &AccentMagenta),
	"webp": fiImage("", &AccentMagenta),
	"bmp":  fiImage("", &AccentMagenta),
	"ico":  fiImage("", &AccentAmber), // seti-favicon
	"svg":  fiImage("", &AccentAmber), // seti-svg

	"zip": fiArchive("", &AccentAmber), // seti-zip
	"tar": fiArchive("", &AccentAmber),
	"gz":  fiArchive("", &AccentAmber),
	"tgz": fiArchive("", &AccentAmber),
	"xz":  fiArchive("", &AccentAmber),
	"bz2": fiArchive("", &AccentAmber),
	"zst": fiArchive("", &AccentAmber),
	"7z":  fiArchive("", &AccentAmber),
	"rar": fiArchive("", &AccentAmber),
}

// dirIconsByName gives a few well-known folders their own Nerd Font glyph.
// The Unicode / ASCII tiers keep the plain folder chevrons so the tree
// still reads as a tree.
var dirIconsByName = map[string]struct {
	nerd  string
	color *Color256
}{
	".git":         {"", &AccentRedCoral}, // custom-folder_git
	".github":      {"", &TextSecondary},  // custom-folder_github
	".vscode":      {"", &AccentBlue},     // custom-folder_config
	".termocode":   {"", &AccentLavender}, // custom-folder_config
	"node_modules": {"", &AccentRedCoral}, // seti-npm
}

// FileIconFor returns the icon glyph (for CurrentIconMode) and its palette
// color for a file or folder. name may be a full path; only the base name
// is used. For folders, open selects the expanded variant.
//
// Lookup order for files: exact base name, then name prefix (LICENSE*,
// README*, .env.*), then extension; extension matching is case-insensitive.
// Unknown files get the generic file icon.
func FileIconFor(name string, isDir, open bool) (glyph string, color Color256) {
	base := strings.ToLower(filepath.Base(name))
	if isDir {
		d := dirClosedIcon
		if open {
			d = dirOpenIcon
		}
		if sp, ok := dirIconsByName[base]; ok {
			d.nerd = sp.nerd
			d.color = sp.color
		}
		return d.glyph(), *d.color
	}
	d := lookupFileIcon(base)
	return d.glyph(), *d.color
}

func lookupFileIcon(base string) fileIconDef {
	if d, ok := fileIconsByName[base]; ok {
		return d
	}
	for _, p := range fileIconsByPrefix {
		if strings.HasPrefix(base, p.prefix) {
			return p.def
		}
	}
	if i := strings.LastIndexByte(base, '.'); i >= 0 && i < len(base)-1 {
		if d, ok := fileIconsByExt[base[i+1:]]; ok {
			return d
		}
	}
	return fileIconDefault
}

// gitStatusIcons maps a status code to its glyph tiers. "U" and "??" both
// mean untracked here; "C" is a merge conflict, "I" ignored.
var gitStatusIcons = map[string]fileIconDef{
	"M":  {"", "●", "M", &GitModified},  // cod-diff_modified
	"A":  {"", "+", "A", &GitAdded},     // cod-diff_added
	"D":  {"", "−", "D", &GitDeleted},   // cod-diff_removed
	"R":  {"", "→", "R", &AccentBlue},   // cod-diff_renamed
	"U":  {"", "?", "U", &GitUntracked}, // cod-question
	"??": {"", "?", "U", &GitUntracked}, // cod-question
	"C":  {"", "!", "!", &GitConflict},  // cod-warning
	"I":  {"", "◌", "I", &GitIgnored},   // cod-diff_ignored
}

// GitStatusGlyph returns the glyph (for CurrentIconMode) and palette color
// for a git status code: "M", "A", "D", "R", "U" / "??" (untracked), "C"
// (conflict), "I" (ignored). Unknown codes return the code itself in
// TextMuted, so callers never print an empty cell.
func GitStatusGlyph(code string) (glyph string, color Color256) {
	if d, ok := gitStatusIcons[strings.ToUpper(strings.TrimSpace(code))]; ok {
		return d.glyph(), *d.color
	}
	if code == "" {
		return " ", TextMuted
	}
	return code[:1], TextMuted
}

// panelIcons holds the bottom-panel tab glyphs. An empty ASCII glyph means
// "no icon": the tab shows its label only.
var panelIcons = map[string][3]string{
	"terminal": {"", "›", ">"}, // cod-terminal
	"output":   {"", "≡", ""},  // cod-output
	"problems": {"", "✗", "!"}, // cod-error
	"debug":    {"", "◉", ""},  // cod-debug
	"tests":    {"", "✓", ""},  // cod-beaker
	"ports":    {"", "⇄", ""},  // cod-plug
	"ai":       {"", "✦", "*"}, // cod-sparkle
}

// PanelIcon returns the bottom-panel tab icon for kind ("terminal",
// "output", "problems", "debug", "tests", "ports", "ai") in the current
// icon mode. It returns "" for unknown kinds and for ASCII tiers that have
// no good one-letter icon.
func PanelIcon(kind string) string {
	g, ok := panelIcons[strings.ToLower(kind)]
	if !ok {
		return ""
	}
	switch CurrentIconMode {
	case IconModeNerdFont:
		return g[0]
	case IconModeUnicode:
		return g[1]
	default:
		return g[2]
	}
}
