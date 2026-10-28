// Package addai is the Add AI card (AIP-3): one floating card that
// connects an AI without a terminal, JSON or config words.
//
//	pick → method (Anthropic, OpenAI) → key | sign-in check
//	     → key (OpenRouter)                    ↘
//	     → local (Ollama, LM Studio): looked for at once → done
//	     → url (Other server): URL + optional key        ↗
//
// An AI connects one way: picking the other way of a connected AI asks
// first (Keep first and lit). The card is a pure state machine on
// connectcard rows: it never touches disk, network or a process. It
// sends the app a request (ConnectKeyMsg, ConnectURLMsg, DetectLocalMsg,
// CheckSignInMsg) and the app answers with Fail or Finish; CancelMsg and
// DoneMsg close it. Each request carries a generation, so the answer to
// an older one (after Back, Esc or Try again) is dropped.
package addai

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/connectcard"
)

// Step is the card shown.
type Step int

const (
	StepPick    Step = iota // the list of AIs
	StepMethod              // sign in or an API key
	StepConfirm             // replace the way a connected AI uses?
	StepKey                 // the masked key field
	StepURL                 // Other server: URL + optional key
	StepLocal               // looking for a local server
	StepSignIn              // checking the vendor CLI's sign-in
	StepDone                // ✓ ready + the models
)

// Entry is one AI of the list.
type Entry struct {
	Kind    string
	Name    string
	About   string
	Methods []string // ai.MethodKey, MethodCLI, MethodLocal, MethodURL; the first is offered first
	CLI     string   // MethodCLI: the CLI that holds the sign-in
	Plan    string   // MethodCLI: the plan the sign-in uses
	Home    string   // MethodLocal: where to get it
}

var catalog = []Entry{
	{Kind: ai.KindAnthropic, Name: "Anthropic", About: "Claude models", Methods: []string{ai.MethodCLI, ai.MethodKey},
		CLI: ai.CLIClaude, Plan: "Claude Pro / Max"},
	{Kind: ai.KindOpenAI, Name: "OpenAI", About: "GPT models", Methods: []string{ai.MethodCLI, ai.MethodKey},
		CLI: ai.CLICodex, Plan: "ChatGPT Plus / Pro"},
	{Kind: ai.KindOllama, Name: "Ollama", About: "Llama, Qwen… on your machine", Methods: []string{ai.MethodLocal}, Home: "ollama.com"},
	{Kind: ai.KindOpenRouter, Name: "OpenRouter", About: "Many models, one key", Methods: []string{ai.MethodKey}},
	{Kind: ai.KindLMStudio, Name: "LM Studio", About: "Local models", Methods: []string{ai.MethodLocal}, Home: "lmstudio.ai"},
	{Kind: ai.KindCompatible, Name: "Other server", About: "Any OpenAI-style URL", Methods: []string{ai.MethodURL}},
}

// Catalog lists the AIs of the first card in order.
func Catalog() []Entry { return append([]Entry(nil), catalog...) }

// EntryOf is the list entry of kind.
func EntryOf(kind string) (Entry, bool) {
	for _, e := range catalog {
		if e.Kind == kind {
			return e, true
		}
	}
	return Entry{}, false
}

// Options open a card.
type Options struct {
	// Connected are the AIs connected now (by kind): the list marks them
	// and the other way of one asks first.
	Connected map[string]ai.ProviderEntry
	// KeyNote is a quiet line under the key field (what it accepts).
	KeyNote string
}

// Requests to the app. Gen is the card's generation at the request.
type (
	// ConnectKeyMsg: check Key for Kind (list its models) and save it.
	ConnectKeyMsg struct {
		Gen       int
		Kind, Key string
	}
	// ConnectURLMsg: list the models of an OpenAI-style server at URL
	// (Key may be empty) and save it.
	ConnectURLMsg struct {
		Gen      int
		URL, Key string
	}
	// DetectLocalMsg: look for Kind's server on this machine.
	DetectLocalMsg struct {
		Gen  int
		Kind string
	}
	// CheckSignInMsg: is CLI signed in (Kind's plan)?
	CheckSignInMsg struct {
		Gen       int
		Kind, CLI string
	}
	// CancelMsg: the card closed before an AI was connected.
	CancelMsg struct{}
	// DoneMsg: the card closed after Kind was connected.
	DoneMsg struct{ Kind string }
)

// Model is the open card.
type Model struct {
	card    connectcard.Model
	opts    Options
	step    Step
	entry   Entry
	method  string // the way being connected
	pending string // StepConfirm: the way Replace starts
	from    Step   // StepConfirm: where Keep goes back to
	busy    bool
	fail    string // one sentence under the fields
	gen     int
	models  []string
}

// New opens the card on the list.
func New(o Options) Model {
	m := Model{opts: o}
	m.card = connectcard.New(m.build())
	return m
}

// Step is the card shown.
func (m Model) Step() Step { return m.step }

// Kind is the AI being connected ("" on the list).
func (m Model) Kind() string { return m.entry.Kind }

// Method is the way being connected.
func (m Model) Method() string { return m.method }

// Gen is the generation of the last request.
func (m Model) Gen() int { return m.gen }

// Busy reports a request in flight.
func (m Model) Busy() bool { return m.busy }

// Card is the card drawn now.
func (m Model) Card() connectcard.Card { return m.card.Card() }

// Value is the text of the field id ("key", "url").
func (m Model) Value(id string) string { return m.card.InputValue(id) }

// SetValue sets the text of the field id (tests, the demo).
func (m *Model) SetValue(id, v string) { m.card.SetInputValue(id, v) }

// SetSize sets the window size.
func (m *Model) SetSize(w, h int) { m.card.SetSize(w, h) }

// Fail ends the request gen with one sentence: the card stays on its
// step (the typed key stays) and offers its way out. A stale gen is
// dropped (false).
func (m *Model) Fail(gen int, text string) bool {
	if gen != m.gen || !m.busy {
		return false
	}
	m.busy, m.fail = false, text
	m.refresh()
	return true
}

// Finish ends the request gen with the AI connected and its models.
func (m *Model) Finish(gen int, models []string) bool {
	if gen != m.gen || !m.busy {
		return false
	}
	m.busy, m.fail, m.step = false, "", StepDone
	m.models = append([]string(nil), models...)
	m.refresh()
	return true
}

func emit(v tea.Msg) tea.Cmd { return func() tea.Msg { return v } }

// Update handles a key or a button.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case connectcard.ButtonMsg:
		return m.press(msg.ID)
	case tea.KeyMsg:
		switch {
		case msg.Type == tea.KeyEsc || msg.Type == tea.KeyCtrlC:
			return m.close()
		case msg.Type == tea.KeyEnter && m.onChoice():
			return m.choose()
		case msg.Type == tea.KeyDown && m.onChoice() && m.card.Choice(m.card.FocusedID()) < m.options()-1:
			msg = tea.KeyMsg{Type: tea.KeyRight} // ↓ moves in the list; past its end, on to the buttons
		case msg.Type == tea.KeyUp && m.onChoice() && m.card.Choice(m.card.FocusedID()) > 0:
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		}
		var cmd tea.Cmd
		m.card, cmd = m.card.Update(msg)
		return m.inner(cmd)
	}
	return m, nil
}

// HandleMouse: a click on an AI or a way chooses it; the rest is the
// card's (buttons, fields).
func (m Model) HandleMouse(x, y int, action tea.MouseAction, button tea.MouseButton) (Model, tea.Cmd) {
	var cmd tea.Cmd
	_, _, onOption := m.card.OptionAt(x, y)
	m.card, cmd = m.card.HandleMouse(x, y, action, button)
	if onOption && action == tea.MouseActionPress && button == tea.MouseButtonLeft && (m.step == StepPick || m.step == StepMethod) {
		return m.choose()
	}
	return m.inner(cmd)
}

// onChoice reports the focus on the list (or the ways).
func (m Model) onChoice() bool {
	id := m.card.FocusedID()
	return (m.step == StepPick && id == "provider") || (m.step == StepMethod && id == "method")
}

// options is the number of choices on the list (or the ways).
func (m Model) options() int {
	if m.step == StepPick {
		return len(catalog)
	}
	return len(m.entry.Methods)
}

// inner runs the card's own message at once (it only wraps a value).
func (m Model) inner(cmd tea.Cmd) (Model, tea.Cmd) {
	if cmd == nil {
		return m, nil
	}
	switch v := cmd().(type) {
	case connectcard.ButtonMsg:
		return m.press(v.ID)
	case connectcard.SubmitMsg:
		return m.press("connect")
	case connectcard.CloseMsg:
		return m.close()
	case nil:
		return m, nil
	default:
		return m, emit(v)
	}
}

// close is Esc / Cancel: after Done it is Done, else nothing is kept.
func (m Model) close() (Model, tea.Cmd) {
	m.gen++ // an answer still on its way is dropped
	m.busy = false
	if m.step == StepDone {
		return m, emit(DoneMsg{Kind: m.entry.Kind})
	}
	return m, emit(CancelMsg{})
}

func (m Model) press(id string) (Model, tea.Cmd) {
	switch id {
	case "cancel":
		return m.close()
	case "done":
		return m, emit(DoneMsg{Kind: m.entry.Kind})
	case "back":
		return m.back(), nil
	case "connect":
		return m.connect()
	case "retry":
		return m.start(m.method)
	case "apikey":
		return m.want(ai.MethodKey)
	case "keep":
		m.step, m.pending = m.from, ""
		m.refresh()
		return m, nil
	case "replace":
		return m.start(m.pending)
	}
	return m, nil
}

// choose takes the selected AI (the list) or way (the method card).
func (m Model) choose() (Model, tea.Cmd) {
	if m.step == StepPick {
		m.entry = catalog[clamp(m.card.Choice("provider"), len(catalog))]
		if len(m.entry.Methods) > 1 {
			m.step = StepMethod
			m.refresh()
			return m, nil
		}
		return m.want(m.entry.Methods[0])
	}
	return m.want(m.entry.Methods[clamp(m.card.Choice("method"), len(m.entry.Methods))])
}

// want goes to a way: a connected AI with another way asks first.
func (m Model) want(method string) (Model, tea.Cmd) {
	if c, ok := m.opts.Connected[m.entry.Kind]; ok && c.Method != method {
		m.from, m.pending, m.step = m.step, method, StepConfirm
		if m.from == StepSignIn || m.from == StepKey {
			m.from = StepMethod
		}
		m.refresh()
		return m, nil
	}
	return m.start(method)
}

// start opens a way's step; the local and sign-in ways ask at once.
func (m Model) start(method string) (Model, tea.Cmd) {
	m.method, m.fail, m.busy = method, "", false
	switch method {
	case ai.MethodKey:
		m.step = StepKey
	case ai.MethodURL:
		m.step = StepURL
	case ai.MethodLocal:
		m.step, m.busy = StepLocal, true
		m.gen++
		m.refresh()
		return m, emit(DetectLocalMsg{Gen: m.gen, Kind: m.entry.Kind})
	case ai.MethodCLI:
		m.step, m.busy = StepSignIn, true
		m.gen++
		m.refresh()
		return m, emit(CheckSignInMsg{Gen: m.gen, Kind: m.entry.Kind, CLI: m.entry.CLI})
	}
	m.refresh()
	return m, nil
}

// back goes one card back; a request in flight is dropped.
func (m Model) back() Model {
	m.gen++
	m.busy, m.fail = false, ""
	switch {
	case m.step == StepMethod:
		m.step = StepPick
	case m.step == StepConfirm:
		m.step = m.from
	case len(m.entry.Methods) > 1:
		m.step = StepMethod
	default:
		m.step = StepPick
	}
	m.refresh()
	return m
}

// connect checks the fields and sends the request.
func (m Model) connect() (Model, tea.Cmd) {
	if m.busy || (m.step != StepKey && m.step != StepURL) {
		return m, nil
	}
	k := strings.TrimSpace(m.card.InputValue("key"))
	failNow := func(text string) (Model, tea.Cmd) {
		m.fail = text
		m.refresh()
		return m, nil
	}
	if m.step == StepKey {
		if k == "" {
			return failNow("Paste your key first.")
		}
		m.busy, m.fail = true, ""
		m.gen++
		m.refresh()
		return m, emit(ConnectKeyMsg{Gen: m.gen, Kind: m.entry.Kind, Key: k})
	}
	u := strings.TrimSpace(m.card.InputValue("url"))
	if text := urlProblem(u); text != "" {
		return failNow(text)
	}
	m.busy, m.fail = true, ""
	m.gen++
	m.refresh()
	return m, emit(ConnectURLMsg{Gen: m.gen, URL: u, Key: k})
}

// urlProblem is why a server URL is not taken, in the card's words (it
// never repeats the URL); "" = fine.
func urlProblem(u string) string {
	if u == "" {
		return "Type the server URL."
	}
	if err := ai.CheckSlotBaseURL(u); err != nil {
		if strings.Contains(err.Error(), "user or password") {
			return "Take the user or password out of the URL. Put a key in the key field."
		}
		return "The URL must start with http:// or https:// and name a host."
	}
	return ""
}

// refresh draws the current step (the typed text stays).
func (m *Model) refresh() { m.card.SetCard(m.build()) }

func clamp(v, n int) int {
	if v < 0 || n <= 0 {
		return 0
	}
	if v >= n {
		return n - 1
	}
	return v
}
