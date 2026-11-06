package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/menu"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The GITX hub (docs-local/plans/git-gap.md): each GITX feature lives in
// its own file and registers its palette rows, palette ids, GRAPH menu
// rows, Source Control keys and AI policies here from an init. The
// shared files (palette.go, update.go, confirm.go, file_ops.go, menu.go,
// git_actions.go) keep one small hook each.
//
// The features share one picker, one card and one prompt kind: whoever
// opens one sets the answer function, so no feature needs a kind number
// of its own.

const (
	pickerKindGitx  pickerKindEnum  = 3690
	confirmKindGitx confirmKindEnum = 3690
	promptKindGitx  promptKindEnum  = 3690
)

// gitxFeature is what one GITX feature adds.
type gitxFeature struct {
	palette    []picker.Item
	policies   map[string]aiActionPolicy
	dispatch   func(m *Model, id string) (tea.Cmd, bool)
	menu       []menu.Item // rows of the GRAPH commit menu
	menuSelect func(m *Model, id string) (tea.Cmd, bool)
	key        func(m *Model, k tea.KeyMsg) (tea.Cmd, bool) // Source Control keys
}

var gitxFeatures []gitxFeature

// registerGitx adds a feature; call it from an init. Its policies go into
// aiActionPolicies, so every new palette id is classified for the AI.
func registerGitx(f gitxFeature) {
	gitxFeatures = append(gitxFeatures, f)
	for id, p := range f.policies {
		aiActionPolicies[id] = p
	}
}

// gitxState is the answer of the open GITX picker, card or prompt.
type gitxState struct {
	onPick    func(m *Model, id string) tea.Cmd
	onConfirm func(m *Model, id string) tea.Cmd
	onPrompt  func(m *Model, value string) tea.Cmd
	items     []picker.Item // the rows of the last GITX picker (tests read them)
	checks    gitxChecks    // the before-commit checks (gitx_commit_checks.go)
}

// gitxDoneMsg carries the end of work done off the UI loop; apply runs
// on the loop.
type gitxDoneMsg struct{ apply func(m *Model) tea.Cmd }

func gitxPaletteItems() []picker.Item {
	var out []picker.Item
	for _, f := range gitxFeatures {
		out = append(out, f.palette...)
	}
	return out
}

func gitxMenuItems() []menu.Item {
	var out []menu.Item
	for _, f := range gitxFeatures {
		out = append(out, f.menu...)
	}
	return out
}

func (m *Model) dispatchGitxPalette(id string) (tea.Cmd, bool) {
	for _, f := range gitxFeatures {
		if f.dispatch != nil {
			if c, ok := f.dispatch(m, id); ok {
				return c, true
			}
		}
	}
	return nil, false
}

func (m *Model) handleGitxMenuSelect(id string) (tea.Cmd, bool) {
	for _, f := range gitxFeatures {
		if f.menuSelect != nil {
			if c, ok := f.menuSelect(m, id); ok {
				return c, true
			}
		}
	}
	return nil, false
}

func (m *Model) gitxSidebarKey(k tea.KeyMsg) (tea.Cmd, bool) {
	for _, f := range gitxFeatures {
		if f.key != nil {
			if c, ok := f.key(m, k); ok {
				return c, true
			}
		}
	}
	return nil, false
}

func (m *Model) handleGitxPickerSelect(id string) (tea.Cmd, bool) {
	if m.pickerKind != pickerKindGitx || m.gitx.onPick == nil {
		return nil, false
	}
	on := m.gitx.onPick
	m.gitx.onPick = nil
	return on(m, id), true
}

// handleGitxConfirm answers the GITX card. Esc never gets here (it only
// closes the card), so Esc is always the safe answer.
func (m *Model) handleGitxConfirm(id string) (tea.Cmd, bool) {
	if m.confirmKind != confirmKindGitx || m.gitx.onConfirm == nil {
		return nil, false
	}
	on := m.gitx.onConfirm
	m.gitx.onConfirm = nil
	return on(m, id), true
}

func (m *Model) handleGitxPromptSubmit(value string) (tea.Cmd, bool) {
	if m.promptKind != promptKindGitx || m.gitx.onPrompt == nil {
		return nil, false
	}
	on := m.gitx.onPrompt
	m.gitx.onPrompt = nil
	return on(m, value), true
}

// gitxPick opens a picker; on gets the picked row's id.
func (m *Model) gitxPick(title string, items []picker.Item, on func(m *Model, id string) tea.Cmd) {
	m.picker = picker.NewItems(title, items)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindGitx
	m.gitx.onPick, m.gitx.items = on, items
}

// gitxCard opens a Cancel-first card: Cancel has the focus, Enter and Esc
// never run an action. on gets the pressed button's id ("cancel" too).
func (m *Model) gitxCard(title, text string, on func(m *Model, id string) tea.Cmd, actions ...confirm.Button) {
	m.confirm = confirm.Risky(title, text, confirm.Button{ID: "cancel", Title: "Cancel"}, actions...)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindGitx
	m.gitx.onConfirm = on
}

// gitxPrompt asks one line; on gets the typed text.
func (m *Model) gitxPrompt(title, label, value string, on func(m *Model, value string) tea.Cmd) {
	m.prompt = prompt.New(title, label, value)
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindGitx
	m.gitx.onPrompt = on
}

// gitxInfo shows a short note.
func (m *Model) gitxInfo(title string) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.Push(toast.Info, title)
	return c
}

// gitxFail shows what failed and git's reason.
func (m *Model) gitxFail(title string, err error) tea.Cmd {
	var c tea.Cmd
	m.toast, c = m.toast.PushDetail(toast.Errr, title, err.Error())
	return c
}
