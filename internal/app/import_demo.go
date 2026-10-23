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

// importDemo writes the sample editors and opens the picker ("picker")
// or the review panel of the VS Code sample ("panel"); "confirm" goes on
// to the final card, "undo" imports and opens the Undo card, "setup" lets
// the setup card's Import row find the samples. "empty"
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

// importDemoPanel opens the review panel of the VS Code sample.
func (m *Model) importDemoPanel() tea.Cmd {
	for _, src := range importDetect() {
		if src.ID != "vscode" {
			continue
		}
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
	return nil
}
