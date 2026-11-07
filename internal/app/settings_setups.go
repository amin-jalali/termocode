package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
)

// Project setups (CMP-101): a named set of the shareable settings files
// (settings, keys, snippets, user commands, custom theme) saved in the
// project as .termocode/setups/<name>.json, so a team commits it and each
// member uses it with one command. It is the settings export format,
// limited to setupFiles; secrets are removed as in an export. A setup
// comes from the repository, so using it is a Cancel-first card that
// lists every file it replaces, and the files that hold commands.

// setupsDir is the setups folder, relative to the workspace root.
const setupsDir = ".termocode/setups"

// setupFiles are the config files a setup holds. Per-machine files
// (databases, connections, credentials, state) never go in.
var setupFiles = map[string]string{
	"config.json":     "settings",
	"keymap.json":     "keys",
	"snippets.json":   "snippets",
	"commands.json":   "user commands: they run shell commands when you pick them",
	"user_theme.json": "custom theme",
}

// setupNameRE is a setup name: it becomes a file name.
var setupNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,39}$`)

func init() {
	registerGitx(gitxFeature{
		palette: []picker.Item{
			{ID: "settings-setup-save", Title: "Preferences: Save Setup to This Project…"},
			{ID: "settings-setup-use", Title: "Preferences: Use a Project Setup…"},
		},
		policies: map[string]aiActionPolicy{
			"settings-setup-save": aiNever("settings import / export stays with the user"),
			"settings-setup-use":  aiNever("settings import / export stays with the user"),
		},
		dispatch: func(m *Model, id string) (tea.Cmd, bool) {
			switch id {
			case "settings-setup-save":
				return m.openSetupSave(), true
			case "settings-setup-use":
				return m.openSetupUse(), true
			}
			return nil, false
		},
	})
}

// setupsRoot is the folder that holds the project's setups.
func setupsRoot() (string, error) {
	root, err := procRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, filepath.FromSlash(setupsDir)), nil
}

// buildSetupBundle is the settings export of dir limited to setupFiles,
// and the names it holds.
func buildSetupBundle(dir string, now time.Time) ([]byte, []string, error) {
	data, _, err := buildSettingsBundle(dir, now)
	if err != nil {
		return nil, nil, err
	}
	var b settingsBundle
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, nil, err
	}
	var names []string
	for name := range b.Files {
		if _, ok := setupFiles[name]; !ok {
			delete(b.Files, name)
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return append(out, '\n'), names, nil
}

// openSetupSave asks for the setup's name.
func (m *Model) openSetupSave() tea.Cmd {
	if _, err := setupsRoot(); err != nil {
		return m.toastErr("No project folder", err)
	}
	m.gitxPrompt("Save Setup to This Project", "Setup name:", "team", func(m *Model, name string) tea.Cmd {
		return m.askSetupSave(strings.TrimSpace(name))
	})
	return nil
}

// askSetupSave shows what the setup will hold on a Cancel-first card.
func (m *Model) askSetupSave(name string) tea.Cmd {
	if name == "" {
		return nil
	}
	if !setupNameRE.MatchString(name) {
		return m.gitxInfo("Use letters, digits, - and _ for the name (at most 40)")
	}
	dir, err := configDir()
	if err != nil {
		return m.toastErr("Save failed", err)
	}
	data, names, err := buildSetupBundle(dir, time.Now())
	if err != nil {
		return m.toastErr("Save failed", err)
	}
	if len(names) == 0 {
		return m.gitxInfo("No shareable settings yet: settings, keys, snippets, commands or a custom theme")
	}
	root, err := setupsRoot()
	if err != nil {
		return m.toastErr("No project folder", err)
	}
	p := filepath.Join(root, name+".json")
	var b strings.Builder
	fmt.Fprintf(&b, "Writes %s/%s.json with:\n\n", setupsDir, name)
	for _, n := range names {
		fmt.Fprintf(&b, "  %s (%s)\n", n, setupFiles[n])
	}
	b.WriteString("\nAPI keys, tokens and passwords are removed. Check the file before you commit it.")
	if _, err := os.Lstat(p); err == nil {
		b.WriteString("\nA setup with this name exists: it is replaced.")
	}
	m.gitxCard("Save setup \""+name+"\"?", b.String(), func(m *Model, id string) tea.Cmd {
		if id != "save" {
			return nil
		}
		return m.writeSetup(root, p, data, len(names))
	}, confirm.Button{ID: "save", Title: "Save Setup"})
	return nil
}

// writeSetup writes the setup file. A link in its place is refused.
func (m *Model) writeSetup(root, p string, data []byte, n int) tea.Cmd {
	if st, err := os.Lstat(p); err == nil && !st.Mode().IsRegular() {
		return m.toastErr("Save failed", errors.New(filepath.Base(p)+" is a link or not a file"))
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return m.toastErr("Save failed", err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return m.toastErr("Save failed", err)
	}
	return m.gitxInfo(fmt.Sprintf("Saved a setup with %d files: commit %s/%s to share it", n, setupsDir, filepath.Base(p)))
}

// projectSetups lists the setup names of the project, sorted.
func projectSetups() []string {
	root, err := setupsRoot()
	if err != nil {
		return nil
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !e.Type().IsRegular() || !setupNameRE.MatchString(name) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// openSetupUse picks one of the project's setups.
func (m *Model) openSetupUse() tea.Cmd {
	names := projectSetups()
	if len(names) == 0 {
		return m.gitxInfo("No setups in this project: run Preferences: Save Setup to This Project… first")
	}
	items := make([]picker.Item, 0, len(names))
	for _, n := range names {
		items = append(items, picker.Item{ID: n, Title: n, Hint: setupsDir + "/" + n + ".json"})
	}
	m.gitxPick("Use a Project Setup", items, func(m *Model, id string) tea.Cmd {
		return m.askSetupUse(id)
	})
	return nil
}

// askSetupUse reads a setup (untrusted: only setupFiles are taken) and
// opens the import card with Cancel first. Its answers go to
// onSettingsImportConfirm (a backup is saved before anything is written).
func (m *Model) askSetupUse(name string) tea.Cmd {
	if !setupNameRE.MatchString(name) {
		return nil
	}
	root, err := setupsRoot()
	if err != nil {
		return m.toastErr("Import failed", err)
	}
	p := filepath.Join(root, name+".json")
	st, err := os.Lstat(p)
	if err != nil {
		return m.toastErr("Import failed", err)
	}
	if !st.Mode().IsRegular() || st.Size() > 4<<20 {
		return m.toastErr("Import failed", errors.New(name+".json is a link, not a file or too big"))
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return m.toastErr("Import failed", err)
	}
	files, err := parseSettingsBundle(data)
	if err != nil {
		return m.toastErr("Import failed", err)
	}
	for n := range files {
		if _, ok := setupFiles[n]; !ok {
			delete(files, n)
		}
	}
	if len(files) == 0 {
		return m.gitxInfo("The setup has no settings files to use")
	}
	m.extras.importFiles = files
	var b strings.Builder
	fmt.Fprintf(&b, "From %s/%s.json. These files in ~/.config/termocode are replaced:\n\n", setupsDir, name)
	for _, n := range sortedKeys(files) {
		fmt.Fprintf(&b, "  %s (%s)\n", n, setupFiles[n])
	}
	b.WriteString("\nThe setup comes from the project: use it only if you trust its authors.")
	b.WriteString("\nA backup of your current settings is saved first.")
	m.confirm = confirm.Risky("Use setup \""+name+"\"?", b.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "import", Title: "Use Setup"},
		confirm.Button{ID: "import-reload", Title: "Use Setup & Reload"})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindSettingsImport
	return nil
}
