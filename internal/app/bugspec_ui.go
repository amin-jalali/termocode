package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/bugspec"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── Bugfix specs (CMP-79) ────────────────────────────────────────────────
//
// AI: New Bugfix Spec… asks for the bug's title and writes
// .termocode/specs/<slug>/bugfix.md (current / expected / unchanged
// behavior and steps), then opens it in the editor. AI: Fix Bug from
// Spec… lists the specs; a ready one opens the chat with a test-first fix
// prompt (a failing test per expected line, a guard test per unchanged
// line, then the smallest fix) and the spec attached. Nothing is sent
// until the user presses Enter. The AI runs neither command.

const (
	pickerKindBugspec      pickerKindEnum = 4791
	promptKindBugspecTitle promptKindEnum = 4792
	bugspecNewPaletteID                   = "bugfix-spec-new"
	bugspecFixPaletteID                   = "bugfix-spec-fix"
	bugspecPickPfx                        = "bugspec:"
)

func bugspecPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: bugspecNewPaletteID, Title: "AI: New Bugfix Spec…", Hint: "current / expected / unchanged behavior"},
		{ID: bugspecFixPaletteID, Title: "AI: Fix Bug from Spec…", Hint: "test-first fix prompt in the chat"},
	}
}

func (m *Model) dispatchBugspecPalette(id string) (tea.Cmd, bool) {
	switch id {
	case bugspecNewPaletteID:
		return m.openBugspecNew(), true
	case bugspecFixPaletteID:
		return m.openBugspecPicker(), true
	}
	return nil, false
}

func (m *Model) openBugspecNew() tea.Cmd {
	m.prompt = prompt.New(" New bugfix spec ", "The bug in a few words:", "")
	m.prompt.SetNote("termocode writes " + bugspec.Dir + "/<name>/" + bugspec.FileName + " for you to fill in; nothing goes to a model.")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindBugspecTitle
	return nil
}

// onBugspecTitle writes the spec of title (an existing one is only
// opened) and opens it at its first empty section.
func (m *Model) onBugspecTitle(title string) tea.Cmd {
	title = strings.Join(strings.Fields(title), " ")
	if title == "" {
		return nil
	}
	path := bugspec.PathFor(aiWorkspaceRoot(), title)
	body, err := os.ReadFile(path)
	existed := err == nil
	if !existed {
		body = []byte(bugspec.Template(title))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return m.toastErr("Could not make the spec folder", err)
		}
		if err := writeProjectFile(path, body); err != nil {
			return m.toastErr("Could not write the bugfix spec", err)
		}
	}
	md := string(body)
	m.openFileAtLine(path, max(bugspec.FirstEmptyLine(md, bugspec.Parse(md)), 1))
	if existed {
		return m.aiInfo("This bugfix spec exists already: opened it")
	}
	return m.aiInfo("Fill in current, expected and unchanged behavior, then AI: Fix Bug from Spec…")
}

func (m *Model) openBugspecPicker() tea.Cmd {
	list, err := bugspec.List(aiWorkspaceRoot())
	if err != nil {
		return m.toastErr("Could not read the bugfix specs", err)
	}
	if len(list) == 0 {
		return m.aiInfo("No bugfix spec yet: AI: New Bugfix Spec… makes one")
	}
	items := make([]picker.Item, 0, len(list))
	for _, e := range list {
		hint := "ready"
		if n := len(e.Spec.Problems()); n > 0 {
			hint = fmt.Sprintf("%d empty section%s", n, plural(n))
		}
		items = append(items, picker.Item{ID: bugspecPickPfx + e.Rel, Title: e.Title, Hint: hint})
	}
	m.picker = picker.NewItems(" Fix bug from spec ", items)
	m.picker.SetNote([]string{"Enter puts a test-first fix prompt in the chat; you read it and send it."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindBugspec
	return nil
}

// onBugspecPicked fills the chat with the fix prompt of the picked spec,
// or opens the spec at its first empty section.
func (m *Model) onBugspecPicked(id string) tea.Cmd {
	rel, ok := strings.CutPrefix(id, bugspecPickPfx)
	if !ok {
		return nil
	}
	root := aiWorkspaceRoot()
	var e *bugspec.Entry
	list, _ := bugspec.List(root)
	for i := range list {
		if list[i].Rel == rel { // only a listed spec: never a path from elsewhere
			e = &list[i]
		}
	}
	if e == nil {
		return m.aiInfo("That bugfix spec is gone")
	}
	if p := e.Spec.Problems(); len(p) > 0 {
		body, _ := os.ReadFile(e.Path)
		m.openFileAtLine(e.Path, max(bugspec.FirstEmptyLine(string(body), e.Spec), 1))
		var c tea.Cmd
		m.toast, c = m.toast.PushDetail(toast.Warn, "Fill in the spec first; nothing went to the chat", strings.Join(p, "\n"))
		return c
	}
	s := m.aiS()
	if s.streaming || s.chatCancel != nil {
		return m.aiInfo("The AI is still answering: try again when it is done")
	}
	var open tea.Cmd
	if len(s.conv.Messages) > 0 {
		open = m.newAIChat() // the old chat stays in Chat History
	} else {
		open = m.openAIPanel(true)
	}
	s.input = []rune(bugspec.FixPrompt(e.Rel, e.Spec))
	s.caret = len(s.input)
	attach := m.aiAttachPaths([]string{e.Path})
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, "Read the fix prompt and press Enter to send it")
	return tea.Batch(open, attach, c)
}

func init() {
	aiActionPolicies[bugspecNewPaletteID] = aiNever(aiNoPicker + "; the user names the bug and fills in the spec")
	aiActionPolicies[bugspecFixPaletteID] = aiNever("starts a chat with the user; the user reads the fix prompt and sends it")
}
