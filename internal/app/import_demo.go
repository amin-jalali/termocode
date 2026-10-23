package app

// importdemo (devhooks.go): sample editor folders for the screenshot and
// golden scenes of Import from Another Editor. They are written into the
// sandbox config dir only (the shots run with a private XDG_CONFIG_HOME).

import (
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/migrate"
)

// importDemoFiles are the sample files, relative to XDG_CONFIG_HOME.
var importDemoFiles = map[string]string{
	"Code/User/settings.json": `// user settings
{
  "editor.tabSize": 2,
  "editor.wordWrap": "on",
  "editor.fontSize": 14,
  "files.autoSave": "afterDelay",
  "workbench.colorTheme": "One Dark Pro",
}
`,
	"Code/User/keybindings.json": `[
  { "key": "ctrl+alt+o", "command": "workbench.action.quickOpen" },
  { "key": "cmd+shift+k", "command": "workbench.action.showCommands" },
  { "key": "ctrl+k ctrl+s", "command": "workbench.action.openGlobalKeybindings" },
  { "key": "ctrl+alt+d", "command": "editor.action.deleteLines" },
]
`,
	"Code/User/extensions/golang.go-0.47.0/package.json":          `{"publisher":"golang","name":"go"}`,
	"Code/User/extensions/humao.rest-client-0.25.1/package.json":  `{"publisher":"humao","name":"rest-client"}`,
	"JetBrains/GoLand2025.1/options/editor.xml":                   `<application><component name="EditorSettings"><option name="USE_SOFT_WRAPS" value="MAIN_EDITOR"/></component></application>`,
	"JetBrains/GoLand2025.1/options/laf.xml":                      `<application><component name="LafManager"><laf themeId="ExperimentalDark"/></component></application>`,
	"sublime-text/Packages/User/Preferences.sublime-settings":     `{"tab_size": 4, "save_on_focus_lost": true}`,
	"sublime-text/Packages/User/Package Control.sublime-settings": `{"installed_packages": ["LSP", "LSP-gopls"]}`,
}

// importDemoSnippets are the sample VS Code snippets of the "snippets"
// variant, relative to XDG_CONFIG_HOME.
var importDemoSnippets = map[string]string{
	"Code/User/snippets/go.json": `{
  "if err": {"prefix": "iferr", "body": ["if err != nil {", "\treturn err", "}"], "description": "return the error"},
  "table test": {"prefix": "tt", "body": ["tests := []struct{", "\tname string", "}{", "\t{name: \"${1:case}\"},", "}"]},
  "log line": {"prefix": "logf", "body": "log.Printf(\"${1:msg}: %v\", ${2:err})$0"},
}`,
	// The user already has a Go snippet with the trigger iferr.
	"termocode/snippets.json": `{"go": {"iferr": "if err != nil { panic(err) }"}}` + "\n",
}

// importDemo writes the sample editors and opens the picker ("picker")
// or the review panel of the VS Code sample ("panel"); "confirm" goes on
// to the final card, "undo" imports and opens the Undo card, "setup" lets
// the setup card's Import row find the samples, "snippets" adds sample
// snippets (one of them clashes with the user's own). "empty"
// writes nothing (picker without editors); "file" opens the path prompt.
func (m *Model) importDemo(variant string) tea.Cmd {
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		return nil // never outside a sandbox
	}
	switch variant {
	case "empty": // no editors at all: the picker offers only From a file…
		return m.openImportPicker()
	case "file": // the path prompt behind From a file…
		return m.openImportFilePrompt()
	}
	for rel, body := range importDemoFiles {
		p := filepath.Join(root, rel)
		if os.MkdirAll(filepath.Dir(p), 0o755) != nil || os.WriteFile(p, []byte(body), 0o644) != nil {
			return nil
		}
	}
	if variant == "snippets" {
		for rel, body := range importDemoSnippets {
			p := filepath.Join(root, rel)
			if os.MkdirAll(filepath.Dir(p), 0o755) != nil || os.WriteFile(p, []byte(body), 0o644) != nil {
				return nil
			}
		}
		return m.importDemoPanel()
	}
	switch variant {
	case "setup": // the setup card finds the sample editors (its probe ran before they existed)
		if m.setup == nil {
			return nil
		}
		s := m.setupS()
		s.importProbed, s.importProbing = false, false
		return m.setupImportProbeCmd()
	case "panel":
		return m.importDemoPanel()
	case "confirm":
		cmd := m.importDemoPanel()
		return tea.Batch(cmd, m.confirmImport())
	case "undo":
		cmd := m.importDemoPanel()
		return tea.Batch(cmd, m.applyImport(), m.undoLastImport())
	}
	return m.openImportPicker()
}

// importDemoPanel opens the review panel of the VS Code sample. It names
// the sample folder itself rather than detecting it: on macOS detection
// looks under ~/Library, and the demo never writes outside the sandbox.
func (m *Model) importDemoPanel() tea.Cmd {
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		return nil
	}
	src := migrate.Source{ID: "vscode", Title: "VS Code", Dir: filepath.Join(root, "Code", "User"), Files: []string{"settings.json", "keybindings.json"}}
	doc, err := migrate.Read(src)
	if err != nil {
		return nil
	}
	cur, err := importCurrent(m.themeID)
	if err != nil {
		return nil
	}
	return m.onImportPlan(importPlanMsg{plan: migrate.Build(src, doc, cur)})
}
