package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	runewidth "github.com/amin-jalali/termocode/internal/cellwidth"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/promptactions"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Prompt actions (CMP-74) ─────────────────────────────────────────────
//
// Saved prompts the user runs with one pick: "Prompt Actions: Run…" lists
// them by group; Enter sends the prompt to the AI chat, or types it into
// the active terminal tab (an agent waiting there) and presses Enter.
// New asks the name ("Group/Name"), the prompt (seeded with the chat
// input) and where it runs. Import and export use a JSON file; import
// shows what it adds and replaces on a Cancel-first card, export asks
// before it writes over a file. The file is
// <config>/termocode/prompt-actions.json (promptactions).

const (
	paRunPaletteID    = "prompt-actions"
	paNewPaletteID    = "prompt-actions-new"
	paImportPaletteID = "prompt-actions-import"
	paExportPaletteID = "prompt-actions-export"
	paEditPaletteID   = "prompt-actions-edit"

	paRunPrefix = "pa-run:"
	paRowNew    = "pa-new"
	paRowImport = "pa-import"
	paRowExport = "pa-export"
	paRowEdit   = "pa-edit"

	paDefaultExportPath = "~/termocode-prompt-actions.json"
)

const (
	pickerKindPromptActions        pickerKindEnum  = 7474
	pickerKindPromptActionTarget   pickerKindEnum  = 7475
	promptKindPromptActionName     promptKindEnum  = 7474
	promptKindPromptActionBody     promptKindEnum  = 7475
	promptKindPromptActionImport   promptKindEnum  = 7476
	promptKindPromptActionExport   promptKindEnum  = 7477
	confirmKindPromptActionsImport confirmKindEnum = 7474
	confirmKindPromptActionsExport confirmKindEnum = 7475
)

const defaultPromptActionsJSON = `{
  "version": 1,
  "actions": [
    {
      "name": "Review my changes",
      "group": "Review",
      "prompt": "Review the uncommitted changes in this repository. List real bugs first, then small fixes."
    },
    {
      "name": "Write tests",
      "prompt": "Write tests for the code you just changed, then run them.",
      "target": "terminal"
    }
  ]
}
`

func init() {
	for _, id := range []string{paRunPaletteID, paNewPaletteID, paImportPaletteID, paExportPaletteID, paEditPaletteID} {
		aiActionPolicies[id] = aiNever(aiNoPicker + "; the user picks which saved prompt runs")
	}
}

// promptActionSendChat sends a prompt to the AI chat (tests replace it).
var promptActionSendChat = func(m *Model, text string) tea.Cmd {
	return tea.Batch(m.openAIPanel(true), m.sendAIChat(text))
}

// paState is the open New / Import / Export flow.
type paState struct {
	group, name, body string
	imports           []promptactions.Action
	exportPath        string
}

func (m *Model) paS() *paState {
	s := m.aiS()
	if s.promptActs == nil {
		s.promptActs = &paState{}
	}
	return s.promptActs
}

func promptActionsPath() (string, error) {
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, promptactions.FileName), nil
}

func loadPromptActions() ([]promptactions.Action, error) {
	p, err := promptActionsPath()
	if err != nil {
		return nil, err
	}
	list, _, err := promptactions.Load(p)
	return list, err
}

// promptActionsPaletteItems are the "Prompt Actions: …" palette rows.
func promptActionsPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: paRunPaletteID, Title: "Prompt Actions: Run…", Hint: "saved prompts"},
		{ID: paNewPaletteID, Title: "Prompt Actions: New…"},
		{ID: paImportPaletteID, Title: "Prompt Actions: Import…"},
		{ID: paExportPaletteID, Title: "Prompt Actions: Export…"},
		{ID: paEditPaletteID, Title: "Prompt Actions: Edit File"},
	}
}

// dispatchPromptActionsPalette runs the "Prompt Actions: …" rows.
func (m *Model) dispatchPromptActionsPalette(id string) (tea.Cmd, bool) {
	switch id {
	case paRunPaletteID:
		return m.openPromptActions(), true
	case paNewPaletteID:
		return m.openPromptActionNew(), true
	case paImportPaletteID:
		return m.openPromptActionPath(promptKindPromptActionImport, "Import Prompt Actions", "JSON file to import:", ""), true
	case paExportPaletteID:
		return m.openPromptActionPath(promptKindPromptActionExport, "Export Prompt Actions", "Write the prompt actions to:", paDefaultExportPath), true
	case paEditPaletteID:
		return m.editConfigFile(promptactions.FileName, defaultPromptActionsJSON), true
	}
	return nil, false
}

// promptActionPickerItems are the picker rows: the actions, then the
// rows that make, import, export and edit them.
func promptActionPickerItems(list []promptactions.Action) []picker.Item {
	var items []picker.Item
	for _, a := range promptactions.Sorted(list) {
		hint := a.TargetOrChat() + " · " + runewidth.Truncate(strings.Join(strings.Fields(firstLine(a.Prompt)), " "), 48, "…")
		items = append(items, picker.Item{ID: paRunPrefix + a.Key(), Title: a.Title(), Hint: hint})
	}
	return append(items,
		picker.Item{ID: paRowNew, Title: "+ New Prompt Action…", Hint: "Group/Name"},
		picker.Item{ID: paRowImport, Title: "Import…", Hint: "from a JSON file"},
		picker.Item{ID: paRowExport, Title: "Export…", Hint: "to a JSON file"},
		picker.Item{ID: paRowEdit, Title: "Open prompt-actions.json", Hint: "edit or delete by hand"},
	)
}

// openPromptActions shows the picker.
func (m *Model) openPromptActions() tea.Cmd {
	list, err := loadPromptActions()
	if err != nil {
		return m.toastErr("prompt-actions.json could not be read", err)
	}
	m.picker = picker.NewItems(" Prompt Actions ", promptActionPickerItems(list))
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindPromptActions
	return m.picker.Init()
}

// onPromptActionPicked runs an action or a row of the picker.
func (m *Model) onPromptActionPicked(id string) tea.Cmd {
	switch id {
	case paRowNew:
		return m.openPromptActionNew()
	case paRowImport:
		cmd, _ := m.dispatchPromptActionsPalette(paImportPaletteID)
		return cmd
	case paRowExport:
		cmd, _ := m.dispatchPromptActionsPalette(paExportPaletteID)
		return cmd
	case paRowEdit:
		return m.editConfigFile(promptactions.FileName, defaultPromptActionsJSON)
	}
	key, ok := strings.CutPrefix(id, paRunPrefix)
	if !ok {
		return nil
	}
	list, err := loadPromptActions()
	if err != nil {
		return m.toastErr("prompt-actions.json could not be read", err)
	}
	a, ok := promptactions.Find(list, key)
	if !ok {
		return m.pushToast(toast.Warn, "That prompt action is not there any more")
	}
	return m.runPromptAction(a)
}

// runPromptAction sends a's prompt where it runs.
func (m *Model) runPromptAction(a promptactions.Action) tea.Cmd {
	if a.TargetOrChat() == promptactions.TargetChat {
		return promptActionSendChat(m, a.Prompt)
	}
	t := m.activeTerminalTab()
	if t == nil {
		return m.pushToastDetail(toast.Info, "Open a terminal tab first", "“"+a.Title()+"” types into the active terminal tab.")
	}
	if t.Hibernated { // AGX-15: the shell is bare; resume first
		m.agentWake(t)
		return m.pushToastDetail(toast.Info, "The agent in this tab is starting again", "Pick the prompt action again in a few seconds.")
	}
	if m.nvim == nil {
		return m.pushToast(toast.Warn, "The editor is not running")
	}
	text := strings.TrimRight(a.Prompt, "\r\n") + "\r"
	var tick int
	if err := m.nvim.ExecLuaResult(aiTermSendLua, &tick, t.BufID, text); err != nil || tick < 0 {
		return m.pushToast(toast.Warn, "Could not type into the terminal tab")
	}
	name := t.agentTitle()
	if name == "" {
		name = t.Name
	}
	return m.pushToast(toast.Info, "Sent “"+a.Title()+"” to "+name)
}

// ── New ─────────────────────────────────────────────────────────────────

func (m *Model) openPromptActionNew() tea.Cmd {
	*m.paS() = paState{}
	m.prompt = prompt.New("New Prompt Action", "Name (Group/Name; the group is optional):", "")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindPromptActionName
	return nil
}

// onPromptActionName is step 1: the name.
func (m *Model) onPromptActionName(v string) tea.Cmd {
	g, n, err := promptactions.ParseRef(v)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Bad prompt action name", err.Error())
	}
	st := m.paS()
	st.group, st.name = g, n
	initial := strings.TrimSpace(string(m.aiS().input))
	if list, err := loadPromptActions(); err == nil {
		if a, ok := promptactions.Find(list, promptactions.Action{Group: g, Name: n}.Key()); ok {
			initial = a.Prompt // the same name: change that one
		}
	}
	initial = strings.NewReplacer("\\", "\\\\", "\n", "\\n").Replace(initial)
	title := n
	if g != "" {
		title = g + " · " + n
	}
	m.prompt = prompt.New("Prompt Action "+title, `Prompt (\n = new line):`, initial)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindPromptActionBody
	return nil
}

// onPromptActionBody is step 2: the prompt; step 3 picks the target.
func (m *Model) onPromptActionBody(v string) tea.Cmd {
	body := strings.TrimSpace(unescapeBodyInput(v))
	if body == "" {
		*m.paS() = paState{}
		return nil
	}
	if len(body) > promptactions.MaxPrompt {
		return m.pushToast(toast.Warn, "The prompt is over 16 KB")
	}
	m.paS().body = body
	m.picker = picker.NewItems(" Where does it run? ", []picker.Item{
		{ID: promptactions.TargetChat, Title: "AI chat", Hint: "sends the prompt to the chat"},
		{ID: promptactions.TargetTerminal, Title: "Active terminal tab", Hint: "types it into the agent there, then Enter"},
	})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindPromptActionTarget
	return m.picker.Init()
}

// onPromptActionTarget is the last step: save.
func (m *Model) onPromptActionTarget(target string) tea.Cmd {
	st := m.paS()
	a := promptactions.Action{Name: st.name, Group: st.group, Prompt: st.body}
	*st = paState{}
	if a.Name == "" || a.Prompt == "" {
		return nil
	}
	if target == promptactions.TargetTerminal {
		a.Target = target
	}
	p, err := promptActionsPath()
	if err != nil {
		return m.toastErr("Prompt action not saved", err)
	}
	list, _, err := promptactions.Load(p)
	if err != nil {
		return m.toastErr("prompt-actions.json could not be read; nothing was changed", err)
	}
	list, _, replaced := promptactions.Merge(list, []promptactions.Action{a})
	if err := promptactions.Save(p, list); err != nil {
		return m.toastErr("Prompt action not saved", err)
	}
	if replaced > 0 {
		return m.pushToastDetail(toast.Info, "Prompt action changed", a.Title())
	}
	return m.pushToastDetail(toast.Info, "Prompt action saved", a.Title()+" · Prompt Actions: Run…")
}

// ── Import / export ─────────────────────────────────────────────────────

func (m *Model) openPromptActionPath(kind promptKindEnum, title, label, initial string) tea.Cmd {
	m.prompt = prompt.New(title, label, initial)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = kind
	return nil
}

// onPromptActionImportPath reads the file and shows what it would add.
func (m *Model) onPromptActionImportPath(v string) tea.Cmd {
	path := expandHome(v)
	if path == "" {
		return nil
	}
	in, notes, err := promptactions.Load(path)
	if err == nil && len(in) == 0 {
		err = fmt.Errorf("no prompt actions in %s", filepath.Base(path))
	}
	if err != nil {
		return m.toastErr("Nothing imported", err)
	}
	have, err := loadPromptActions()
	if err != nil {
		return m.toastErr("prompt-actions.json could not be read; nothing was changed", err)
	}
	_, added, replaced := promptactions.Merge(have, in)
	var b strings.Builder
	fmt.Fprintf(&b, "From %s: %d new, %d replace one with the same name.\n", path, added, replaced)
	for i, a := range promptactions.Sorted(in) {
		if i == 12 {
			fmt.Fprintf(&b, "\n  … and %d more", len(in)-i)
			break
		}
		fmt.Fprintf(&b, "\n  %s (%s): %s", a.Title(), a.TargetOrChat(),
			runewidth.Truncate(strings.Join(strings.Fields(ai.Redact(a.Prompt)), " "), 60, "…"))
	}
	if len(notes) > 0 {
		fmt.Fprintf(&b, "\n\nLeft out: %s", strings.Join(notes, "; "))
	}
	b.WriteString("\n\nNothing runs now: you pick each action later.")
	m.paS().imports = in
	m.confirm = confirm.Risky("Import prompt actions?", b.String(),
		confirm.Button{ID: "cancel", Title: "Cancel"},
		confirm.Button{ID: "import", Title: "Import"})
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindPromptActionsImport
	return nil
}

// answerPromptActionsImport answers the import card ("" = Esc = Cancel).
func (m *Model) answerPromptActionsImport(id string) tea.Cmd {
	st := m.paS()
	in := st.imports
	st.imports = nil
	if id != "import" || len(in) == 0 {
		return nil
	}
	p, err := promptActionsPath()
	if err != nil {
		return m.toastErr("Nothing imported", err)
	}
	have, _, err := promptactions.Load(p)
	if err != nil {
		return m.toastErr("prompt-actions.json could not be read; nothing was changed", err)
	}
	out, added, replaced := promptactions.Merge(have, in)
	if err := promptactions.Save(p, out); err != nil {
		return m.toastErr("Nothing imported", err)
	}
	return m.pushToastDetail(toast.Info, "Prompt actions imported", fmt.Sprintf("%d new, %d replaced", added, replaced))
}

// onPromptActionExportPath writes the file, after a card when one is
// there.
func (m *Model) onPromptActionExportPath(v string) tea.Cmd {
	path := expandHome(v)
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err == nil {
		m.paS().exportPath = path
		m.confirm = confirm.Risky("Replace this file?", path+" is there already. Export writes over it.",
			confirm.Button{ID: "cancel", Title: "Cancel"},
			confirm.Button{ID: "replace", Title: "Replace"})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindPromptActionsExport
		return nil
	}
	return m.exportPromptActions(path)
}

// answerPromptActionsExport answers the replace card ("" = Esc = Cancel).
func (m *Model) answerPromptActionsExport(id string) tea.Cmd {
	st := m.paS()
	path := st.exportPath
	st.exportPath = ""
	if id != "replace" || path == "" {
		return nil
	}
	return m.exportPromptActions(path)
}

func (m *Model) exportPromptActions(path string) tea.Cmd {
	list, err := loadPromptActions()
	if err != nil {
		return m.toastErr("prompt-actions.json could not be read; nothing was written", err)
	}
	if len(list) == 0 {
		return m.pushToast(toast.Info, "There are no prompt actions to export")
	}
	if err := promptactions.Save(path, list); err != nil {
		return m.toastErr("Prompt actions not exported", err)
	}
	return m.pushToastDetail(toast.Info, fmt.Sprintf("%d prompt actions exported", len(list)), path)
}
