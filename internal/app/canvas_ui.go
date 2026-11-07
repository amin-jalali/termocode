package app

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/canvas"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/confirm"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/prompt"
	"github.com/amin-jalali/termocode/internal/toast"
)

// ── The shared plan (CMP-92) ─────────────────────────────────────────────
//
// One plan per workspace (.termocode/canvas.json, internal/canvas) that
// the user and the AI both edit. Canvas: Open Shared Plan lists the
// cards in order (status glyph, title, who wrote it, whether it waits
// for the user's OK); Enter on a card opens its actions: mark to do /
// doing / done, approve, move up / down / to the top, edit the title or
// the note, delete (a card asks first, Cancel first and focused). The
// AI reads the plan with canvas_read and changes it with canvas_update;
// it never approves a card and never removes one the user wrote or
// approved (canvas.Board.Apply). After an AI change a toast says what
// changed.

const (
	pickerKindCanvas        pickerKindEnum  = 9201
	pickerKindCanvasCard    pickerKindEnum  = 9202
	promptKindCanvasAdd     promptKindEnum  = 9203
	promptKindCanvasTitle   promptKindEnum  = 9204
	promptKindCanvasNote    promptKindEnum  = 9206
	confirmKindCanvasDelete confirmKindEnum = 9205

	canvasOpenPaletteID = "canvas-open"
	canvasAddPaletteID  = "canvas-add"
	canvasCopyPaletteID = "canvas-copy"

	canvasCardPfx = "card:"
	canvasAddID   = "add"
	canvasActPfx  = "act:"
)

// canvasPaletteItems are the "Canvas: …" palette rows.
func canvasPaletteItems() []picker.Item {
	return []picker.Item{
		{ID: canvasOpenPaletteID, Title: "Canvas: Open Shared Plan", Lead: "plan"},
		{ID: canvasAddPaletteID, Title: "Canvas: Add Card…"},
		{ID: canvasCopyPaletteID, Title: "Canvas: Copy Plan as Markdown"},
	}
}

// dispatchCanvasPalette runs the "Canvas: …" palette commands.
func (m *Model) dispatchCanvasPalette(id string) (tea.Cmd, bool) {
	switch id {
	case canvasOpenPaletteID:
		return m.openCanvas(""), true
	case canvasAddPaletteID:
		return m.openCanvasAdd(), true
	case canvasCopyPaletteID:
		return m.canvasCopy(), true
	}
	return nil, false
}

// canvasRoot is the workspace the plan belongs to.
func (m *Model) canvasRoot() string {
	if r := m.workspace().Root; r != "" {
		return r
	}
	return "."
}

// canvasRedact hides secrets before a title or note is stored.
func canvasRedact(s string) string { return ai.Redact(ai.RedactSecrets(s)) }

// canvasGlyph is a card's status glyph.
func canvasGlyph(c canvas.Card) string {
	switch c.Status {
	case canvas.StatusDoing:
		return "◐"
	case canvas.StatusDone:
		return "☑"
	}
	return "☐"
}

// canvasHint is a card's right column: who wrote it and its OK.
func canvasHint(c canvas.Card) string {
	var parts []string
	if c.By == canvas.ByAI {
		parts = append(parts, "by AI")
	}
	if c.NeedsOK() {
		parts = append(parts, "needs your OK")
	}
	if n := len(c.Files); n > 0 {
		parts = append(parts, strconv.Itoa(n)+" file"+plural(n))
	}
	return strings.Join(parts, " · ")
}

// openCanvas lists the plan; sel is the card the cursor starts on.
func (m *Model) openCanvas(sel string) tea.Cmd {
	b, err := canvas.Load(m.canvasRoot())
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Could not read the shared plan", err.Error())
	}
	items := []picker.Item{{ID: canvasAddID, Title: "+ Add a card…", Hint: "you"}}
	for n, c := range b.Cards {
		items = append(items, picker.Item{ID: canvasCardPfx + c.ID,
			Title: strconv.Itoa(n+1) + ". " + canvasGlyph(c) + " " + aiCleanText(c.Title), Hint: canvasHint(c)})
	}
	todo, doing, done, need := b.Counts()
	note := []string{strconv.Itoa(todo) + " to do · " + strconv.Itoa(doing) + " doing · " + strconv.Itoa(done) + " done"}
	if need > 0 {
		note[0] += " · " + strconv.Itoa(need) + " need your OK"
	}
	note = append(note, "Enter: the card's actions. The AI edits this plan too (canvas_update); only you approve or remove your cards.")
	m.picker = picker.NewItems(" Shared plan ", items)
	m.picker.SetNote(note)
	m.picker.SetSize(m.w, m.h)
	if sel != "" {
		m.picker.SetCursorID(canvasCardPfx + sel)
	}
	m.pickerOpen = true
	m.pickerKind = pickerKindCanvas
	return nil
}

// onCanvasPicked is Enter in the list.
func (m *Model) onCanvasPicked(id string) tea.Cmd {
	if id == canvasAddID {
		return m.openCanvasAdd()
	}
	cardID, ok := strings.CutPrefix(id, canvasCardPfx)
	if !ok {
		return nil
	}
	return m.openCanvasCard(cardID)
}

// openCanvasCard shows the actions of card id.
func (m *Model) openCanvasCard(id string) tea.Cmd {
	b, err := canvas.Load(m.canvasRoot())
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Could not read the shared plan", err.Error())
	}
	i := b.Find(id)
	if i < 0 {
		return tea.Batch(m.pushToast(toast.Warn, "That card is gone (the AI or another window changed the plan)"), m.openCanvas(""))
	}
	c := b.Cards[i]
	m.canvasSel = id
	var items []picker.Item
	for _, st := range canvas.Statuses {
		if st != c.Status {
			items = append(items, picker.Item{ID: canvasActPfx + "status:" + st, Title: "Mark " + canvas.StatusWord(st)})
		}
	}
	if c.NeedsOK() {
		items = append(items, picker.Item{ID: canvasActPfx + "approve", Title: "Approve", Hint: "the AI may work on it"})
	} else {
		items = append(items, picker.Item{ID: canvasActPfx + "unapprove", Title: "Take the OK back"})
	}
	if i > 0 {
		items = append(items, picker.Item{ID: canvasActPfx + "up", Title: "Move up"},
			picker.Item{ID: canvasActPfx + "top", Title: "Move to the top"})
	}
	if i < len(b.Cards)-1 {
		items = append(items, picker.Item{ID: canvasActPfx + "down", Title: "Move down"})
	}
	items = append(items,
		picker.Item{ID: canvasActPfx + "title", Title: "Edit the title…"},
		picker.Item{ID: canvasActPfx + "note", Title: "Edit the note…"},
		picker.Item{ID: canvasActPfx + "copy", Title: "Copy the title"},
		picker.Item{ID: canvasActPfx + "delete", Title: "Delete…", Hint: "asks first"},
		picker.Item{ID: canvasActPfx + "back", Title: "← Back to the plan"})
	note := []string{canvas.StatusWord(c.Status) + " · " + canvasHint(c)}
	if c.Note != "" {
		for _, l := range strings.Split(aiCleanText(c.Note), "\n") {
			note = append(note, "  "+l)
		}
	}
	if len(c.Files) > 0 {
		note = append(note, "files: "+aiCleanText(strings.Join(c.Files, ", ")))
	}
	m.picker = picker.NewItems(" "+aiCardText(c.Title, 40)+" ", items)
	m.picker.SetNote(note)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindCanvasCard
	return nil
}

// canvasApply loads the plan, runs op as the user, saves it and shows
// the list again on the card.
func (m *Model) canvasApply(op canvas.Op) tea.Cmd {
	root := m.canvasRoot()
	b, err := canvas.Load(root)
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Could not read the shared plan", err.Error())
	}
	id, what, err := b.Apply(op, canvas.ByYou, clock.Now(), canvasRedact)
	if err != nil {
		return m.pushToast(toast.Warn, "Plan: "+err.Error())
	}
	if err := canvas.Save(root, &b); err != nil {
		return m.pushToastDetail(toast.Errr, "Could not save the shared plan", err.Error())
	}
	sel := id
	if op.Kind == canvas.OpRemove {
		sel = ""
	}
	return tea.Batch(m.openCanvas(sel), m.pushToast(toast.Info, "Plan: "+what))
}

// onCanvasActionPicked runs a card action.
func (m *Model) onCanvasActionPicked(id string) tea.Cmd {
	act, ok := strings.CutPrefix(id, canvasActPfx)
	cardID := m.canvasSel
	if !ok || cardID == "" {
		return nil
	}
	b, err := canvas.Load(m.canvasRoot())
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Could not read the shared plan", err.Error())
	}
	i := b.Find(cardID)
	if i < 0 && act != "back" {
		return tea.Batch(m.pushToast(toast.Warn, "That card is gone (the AI or another window changed the plan)"), m.openCanvas(""))
	}
	if st, ok := strings.CutPrefix(act, "status:"); ok {
		return m.canvasApply(canvas.Op{Kind: canvas.OpStatus, ID: cardID, Status: st})
	}
	switch act {
	case "approve":
		return m.canvasApply(canvas.Op{Kind: canvas.OpApprove, ID: cardID})
	case "unapprove":
		return m.canvasApply(canvas.Op{Kind: canvas.OpUnapprove, ID: cardID})
	case "up":
		return m.canvasApply(canvas.Op{Kind: canvas.OpMove, ID: cardID, To: i})
	case "top":
		return m.canvasApply(canvas.Op{Kind: canvas.OpMove, ID: cardID, To: 1})
	case "down":
		return m.canvasApply(canvas.Op{Kind: canvas.OpMove, ID: cardID, To: i + 2})
	case "title":
		m.prompt = prompt.New(" Edit the card ", "Title:", b.Cards[i].Title)
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindCanvasTitle
		return nil
	case "note":
		m.prompt = prompt.New(" Edit the card ", "Note (empty clears it):", strings.ReplaceAll(b.Cards[i].Note, "\n", " "))
		m.prompt.SetSize(m.w, m.h)
		m.promptOpen = true
		m.promptKind = promptKindCanvasNote
		return nil
	case "copy":
		return m.copyWithToast(b.Cards[i].Title, "Copied the card's title")
	case "delete":
		c := b.Cards[i]
		body := "Delete the card " + strconv.Quote(aiCardText(c.Title, 60)) + " from the shared plan?\n\nThe AI no longer sees it. There is no undo."
		m.confirm = confirm.Risky("Delete this card?", body,
			confirm.Button{ID: "cancel", Title: "Cancel"},
			confirm.Button{ID: "delete", Title: "Delete", Style: confirm.StyleDestructive})
		m.confirm.SetSize(m.w, m.h)
		m.confirmOpen = true
		m.confirmKind = confirmKindCanvasDelete
		return nil
	case "back":
		return m.openCanvas(cardID)
	}
	return nil
}

// answerCanvasDelete answers the delete card ("" = Esc = Cancel).
func (m *Model) answerCanvasDelete(id string) tea.Cmd {
	cardID := m.canvasSel
	if id != "delete" || cardID == "" {
		return m.openCanvas(cardID)
	}
	m.canvasSel = ""
	return m.canvasApply(canvas.Op{Kind: canvas.OpRemove, ID: cardID})
}

// openCanvasAdd asks for a new card's title.
func (m *Model) openCanvasAdd() tea.Cmd {
	m.prompt = prompt.New(" Add a card ", "What should be done:", "")
	m.prompt.SetNote("It goes to the end of the shared plan, approved (you wrote it).")
	m.prompt.SetSize(m.w, m.h)
	m.promptOpen = true
	m.promptKind = promptKindCanvasAdd
	return nil
}

// onCanvasPrompt is Enter on one of the plan's prompts.
func (m *Model) onCanvasPrompt(kind promptKindEnum, value string) tea.Cmd {
	v := strings.TrimSpace(value)
	switch kind {
	case promptKindCanvasAdd:
		if v == "" {
			return m.openCanvas("")
		}
		return m.canvasApply(canvas.Op{Kind: canvas.OpAdd, Title: v})
	case promptKindCanvasTitle:
		if v == "" {
			return m.openCanvas(m.canvasSel)
		}
		return m.canvasApply(canvas.Op{Kind: canvas.OpEdit, ID: m.canvasSel, Title: v})
	case promptKindCanvasNote:
		return m.canvasApply(canvas.Op{Kind: canvas.OpEdit, ID: m.canvasSel, Note: v, SetNote: true})
	}
	return nil
}

// canvasCopy copies the plan as a Markdown checklist.
func (m *Model) canvasCopy() tea.Cmd {
	b, err := canvas.Load(m.canvasRoot())
	if err != nil {
		return m.pushToastDetail(toast.Errr, "Could not read the shared plan", err.Error())
	}
	if len(b.Cards) == 0 {
		return m.pushToast(toast.Info, "The shared plan is empty: Canvas: Add Card… starts it")
	}
	return m.copyWithToast(b.Markdown(), "Copied the shared plan ("+strconv.Itoa(len(b.Cards))+" card"+plural(len(b.Cards))+")")
}

func init() {
	aiActionPolicies[canvasOpenPaletteID] = aiNever(aiNoPicker + "; read the plan with canvas_read")
	aiActionPolicies[canvasAddPaletteID] = aiNever(aiNoPicker + "; add cards with canvas_update")
	aiActionPolicies[canvasCopyPaletteID] = aiNever("writes the user's clipboard; read the plan with canvas_read")
}
