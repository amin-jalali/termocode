package app

import (
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/specs"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// ── Specs: requirements → design → tasks → build → review (CMP-75) ─────
//
// Spec: New… asks for the goal and makes .termocode/specs/<name>/
// (internal/specs). The spec card shows each phase and where it stands;
// Ask AI puts the current phase's prompt into the chat input (the user
// reads it and sends it: nothing runs by itself), Open shows the phase's
// file, and Approve… (a Cancel-first card) binds the phase to the file
// as it is now so the next one can start. Only the user approves: the
// AI can never run these commands.

const (
	specNewPaletteID    = "spec-new"
	specNewBugPaletteID = "spec-new-bugfix"
	specOpenPaletteID   = "spec-open"

	promptKindSpecNew    promptKindEnum  = 7531
	promptKindSpecNewBug promptKindEnum  = 7532
	pickerKindSpecOpen   pickerKindEnum  = 7533
	confirmKindSpecCard  confirmKindEnum = 7534
	// confirmKindSpecApprove is the Cancel-first approval of one phase.
	confirmKindSpecApprove confirmKindEnum = 7535

	specsGuidePage = "guides/specs.md"
)

func init() {
	aiActionPolicies[specNewPaletteID] = aiNever(aiNoPicker + "; write the spec files with the file tools")
	aiActionPolicies[specNewBugPaletteID] = aiNever(aiNoPicker + "; write the spec files with the file tools")
	aiActionPolicies[specOpenPaletteID] = aiNever(aiNoPicker + "; only the user approves a spec phase")
}

// specPaletteItems are the "Spec: …" palette rows.
func specPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: specNewPaletteID, Title: "Spec: New…"},
		{ID: specNewBugPaletteID, Title: "Spec: New Bugfix…"},
		{ID: specOpenPaletteID, Title: "Spec: Open…"},
	}
}

// dispatchSpecPalette runs the "Spec: …" palette commands.
func (m *Model) dispatchSpecPalette(id string) (tea.Cmd, bool) {
	switch id {
	case specNewPaletteID, specNewBugPaletteID:
		if _, c, ok := m.specRoot(); !ok {
			return c, true
		}
		if id == specNewBugPaletteID {
			return m.openHTTPPrompt(promptKindSpecNewBug, "New bugfix spec", "Bug:", ""), true
		}
		return m.openHTTPPrompt(promptKindSpecNew, "New spec", "Goal:", ""), true
	case specOpenPaletteID:
		return m.openSpecPicker(), true
	}
	return nil, false
}

// specRoot is the project folder of the specs. Specs are plain files in
// a local folder for now: a remote or container workspace says so.
func (m *Model) specRoot() (string, tea.Cmd, bool) {
	ws := m.workspace()
	root, err := m.rootDir()
	if err != nil {
		return "", m.pushToastDetail(toast.Info, "No folder for a spec", "Open a project folder first."), false
	}
	if ws.Kind != "" && ws.Kind != workspace.KindLocal {
		return "", m.pushToastDetail(toast.Info, "Specs work in a local folder for now", "This window is a "+string(ws.Kind)+" workspace."), false
	}
	return root, nil, true
}

// handleSpecPromptSubmit makes the spec from the typed goal. ok=false:
// not ours.
func (m *Model) handleSpecPromptSubmit(value string) (tea.Cmd, bool) {
	var kind specs.Kind
	switch m.promptKind {
	case promptKindSpecNew:
		kind = specs.Feature
	case promptKindSpecNewBug:
		kind = specs.Bugfix
	default:
		return nil, false
	}
	if strings.TrimSpace(value) == "" {
		return nil, true
	}
	root, c, ok := m.specRoot()
	if !ok {
		return c, true
	}
	s, err := specs.Create(root, value, kind, clock.Now())
	if err != nil {
		return m.pushToastDetail(toast.Warn, "No spec made", err.Error()), true
	}
	return tea.Batch(m.pushToastDetail(toast.Info, "Spec "+s.Name+" is ready", s.Rel(specs.Requirements)), m.openSpecCard(s.Name)), true
}

// specHint is a spec's phase in a few words ("Design · draft").
func specHint(s specs.Spec) string {
	p, done := s.Current()
	if done {
		return "done"
	}
	h := p.Title() + theme.ASCIIText(" · ") + specStatusWord(s.Status(p))
	if p == specs.Build {
		if b, err := os.ReadFile(s.FilePath(specs.Tasks)); err == nil {
			d, all := specs.Progress(specs.ParseTasks(b))
			h += theme.ASCIIText(" · ") + strconv.Itoa(d) + "/" + strconv.Itoa(all) + " tasks"
		}
	}
	return h
}

func specStatusWord(st specs.Status) string {
	switch st {
	case specs.Missing:
		return "not written"
	case specs.Template:
		return "empty template"
	case specs.Draft:
		return "draft"
	case specs.Approved:
		return "approved"
	}
	return "changed since approved"
}

// openSpecPicker lists the specs of the project.
func (m *Model) openSpecPicker() tea.Cmd {
	root, c, ok := m.specRoot()
	if !ok {
		return c
	}
	list, err := specs.List(root)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Specs could not be read", err.Error())
	}
	if len(list) == 0 {
		return m.pushToastDetail(toast.Info, "No specs yet", "Spec: New… starts one.")
	}
	items := make([]picker.Item, 0, len(list))
	for _, s := range list {
		items = append(items, picker.Item{ID: s.Name, Title: s.Goal, Hint: specHint(s)})
	}
	m.picker = picker.NewItems(" Specs ", items)
	m.picker.SetNote([]string{"Pick a spec to see its phases, ask the AI for the next one or approve it."})
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindSpecOpen
	return nil
}

// specCardText is the card's body: the goal and every phase.
func specCardText(s specs.Spec) string {
	cur, done := s.Current()
	var b strings.Builder
	b.WriteString(s.Goal + "\n" + specs.Dir + "/" + s.Name + "/  (" + string(s.Kind) + ")\n")
	for _, p := range specs.Phases {
		st := s.Status(p)
		mark := "  "
		switch {
		case st == specs.Approved:
			mark = theme.GlyphOK.String() + " "
		case !done && p == cur:
			mark = theme.ASCIIText("▸") + " "
		}
		line := "\n" + mark + p.Title() + "  " + s.File(p) + theme.ASCIIText(" · ") + specStatusWord(st)
		if p == specs.Build && st != specs.Missing {
			if bt, err := os.ReadFile(s.FilePath(specs.Tasks)); err == nil {
				d, all := specs.Progress(specs.ParseTasks(bt))
				line = "\n" + mark + p.Title() + "  " + strconv.Itoa(d) + "/" + strconv.Itoa(all) + " tasks ticked"
				if n := s.NextWave(); n != "" && !done && p == cur {
					line += theme.ASCIIText(" · ") + "next: " + n
				}
			}
		}
		b.WriteString(line)
	}
	b.WriteString("\n\n")
	if done {
		b.WriteString("Every phase is approved.")
	} else {
		b.WriteString("Ask AI puts the " + cur.Title() + " prompt into the chat; you read it and send it. " +
			"Approve when " + s.File(cur) + " is right: the next phase needs it.")
	}
	return b.String()
}

// openSpecCard shows the spec's card (nothing changes until a button).
func (m *Model) openSpecCard(name string) tea.Cmd {
	root, c, ok := m.specRoot()
	if !ok {
		return c
	}
	s, err := specs.Load(root, name)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Spec could not be read", err.Error())
	}
	m.specOpen = s.Name
	cur, done := s.Current()
	buttons := []confirm.Button{{ID: "open", Title: "Open " + s.File(cur)}}
	if !done {
		ask := "Ask AI"
		switch cur {
		case specs.Build:
			ask = "Build with AI"
		case specs.Review:
			ask = "Review with AI"
		}
		buttons = append(buttons, confirm.Button{ID: "draft", Title: ask},
			confirm.Button{ID: "approve", Title: "Approve " + cur.Title() + "…"})
	}
	m.confirm = confirm.Risky("Spec: "+s.Name, specCardText(s), confirm.Button{ID: "close", Title: "Close"}, buttons...)
	m.confirm.SetSize(m.w, m.h)
	m.confirmOpen = true
	m.confirmKind = confirmKindSpecCard
	return nil
}

// handleSpecConfirm runs the spec cards' buttons. ok=false: not ours.
func (m *Model) handleSpecConfirm(id string) (tea.Cmd, bool) {
	switch m.confirmKind {
	case confirmKindSpecCard:
		return m.answerSpecCard(id), true
	case confirmKindSpecApprove:
		return m.answerSpecApprove(id), true
	}
	return nil, false
}

func (m *Model) answerSpecCard(id string) tea.Cmd {
	if id == "close" || id == "" {
		return nil
	}
	root, c, ok := m.specRoot()
	if !ok {
		return c
	}
	s, err := specs.Load(root, m.specOpen)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Spec could not be read", err.Error())
	}
	cur, done := s.Current()
	switch id {
	case "open":
		p := cur
		if done {
			p = specs.Review
		}
		m.openFileAtLine(s.FilePath(p), 1)
		return nil
	case "draft":
		if done {
			return nil
		}
		m.aiSetInput(s.Prompt(cur, root))
		return tea.Batch(m.openAIPanel(true),
			m.pushToastDetail(toast.Info, "The "+cur.Title()+" prompt is in the AI chat", "Read it, change it if you like, then press Enter to send it."))
	case "approve":
		if done {
			return nil
		}
		if st := s.Status(cur); st == specs.Missing || st == specs.Template {
			return m.pushToastDetail(toast.Info, s.File(cur)+" is not written yet", "Ask AI to write it, or write it yourself, then approve it.")
		}
		text := "Approve " + s.Rel(cur) + " as it is now?\n\n"
		if cur == specs.Build {
			text = "Approve the build of " + s.Name + "?\n\nEvery task in tasks.md must be ticked.\n\n"
		}
		text += "The next phase starts from it. A later change to the file needs approval again."
		m.confirm = confirm.Risky("Approve "+cur.Title()+"?", text,
			confirm.Button{ID: "cancel", Title: "Cancel"},
			confirm.Button{ID: "approve", Title: "Approve", Style: confirm.StylePrimary})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindSpecApprove
		return nil
	}
	return nil
}

// answerSpecApprove stores the approval (anything but Approve: nothing
// changes and the spec card comes back).
func (m *Model) answerSpecApprove(id string) tea.Cmd {
	root, c, ok := m.specRoot()
	if !ok {
		return c
	}
	if id != "approve" {
		return m.openSpecCard(m.specOpen)
	}
	s, err := specs.Load(root, m.specOpen)
	if err != nil {
		return m.pushToastDetail(toast.Warn, "Spec could not be read", err.Error())
	}
	cur, done := s.Current()
	if done {
		return m.openSpecCard(s.Name)
	}
	if err := s.Approve(cur, clock.Now()); err != nil {
		return tea.Batch(m.pushToastDetail(toast.Warn, cur.Title()+" not approved", err.Error()), m.openSpecCard(s.Name))
	}
	detail := "Every phase is approved."
	if next, all := s.Current(); !all {
		detail = "Next: " + next.Title() + "."
	}
	return tea.Batch(m.pushToastDetail(toast.Info, cur.Title()+" approved", detail), m.openSpecCard(s.Name))
}
