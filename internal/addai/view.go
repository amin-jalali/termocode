package addai

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/theme"
)

// maxChips is how many model ids the done card names.
const maxChips = 12

var (
	btnCancel  = connectcard.Button{ID: "cancel", Title: "Cancel"}
	btnBack    = connectcard.Button{ID: "back", Title: "Back"}
	btnConnect = connectcard.Button{ID: "connect", Title: "Connect", Primary: true}
	btnRetry   = connectcard.Button{ID: "retry", Title: "Try again", Primary: true}
)

// build is the card of the current step.
func (m Model) build() connectcard.Card {
	e := m.entry
	busy := func(text string) connectcard.Row {
		return connectcard.Row{Kind: connectcard.Status, Text: theme.ASCIIText("◐ ") + text}
	}
	fail := func(rows []connectcard.Row) []connectcard.Row {
		if m.fail != "" {
			rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: m.fail})
		}
		return rows
	}
	actions := func(primary connectcard.Button) []connectcard.Button {
		if m.busy {
			return []connectcard.Button{btnCancel}
		}
		return []connectcard.Button{btnBack, btnCancel, primary}
	}
	switch m.step {
	case StepMethod:
		return connectcard.Card{Title: "Connect " + e.Name, Rows: []connectcard.Row{
			{Kind: connectcard.Choice, ID: "method", Vertical: true,
				Options: []string{"Sign in with " + e.Plan, "Use an API key"}},
			{Kind: connectcard.Blank},
			{Kind: connectcard.Dim, Text: "Sign-in uses your plan: no extra cost per request."},
			{Kind: connectcard.Dim, Text: "An API key is pay per use on your " + e.Name + " account."},
		}, Buttons: []connectcard.Button{btnBack, btnCancel}}

	case StepConfirm:
		cur := m.opts.Connected[e.Kind]
		return connectcard.Card{Title: "Replace how " + e.Name + " connects?", Rows: []connectcard.Row{
			{Kind: connectcard.Text, Text: e.Name + " is connected with " + m.wayNow(cur) + ". An AI connects one way, so " +
				m.wayNext(m.pending) + " replaces it."},
		}, Buttons: []connectcard.Button{{ID: "keep", Title: "Keep", Primary: true}, {ID: "replace", Title: "Replace"}}}

	case StepKey:
		rows := []connectcard.Row{
			{Kind: connectcard.Input, ID: "key", Masked: true, Placeholder: keyPlaceholder(e.Kind),
				Text: "Paste your key. It is saved in your secret store, never in a config file."},
		}
		if m.opts.KeyNote != "" {
			rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: m.opts.KeyNote})
		}
		if m.busy {
			rows = append(rows, connectcard.Row{Kind: connectcard.Blank}, busy("Checking the key and loading models…"))
		}
		return connectcard.Card{Title: e.Name + " API key", Rows: fail(rows), Buttons: actions(btnConnect)}

	case StepURL:
		rows := []connectcard.Row{
			{Kind: connectcard.Input, ID: "url", Text: "Server URL (OpenAI-style)", Value: "http://localhost:8000/v1"},
			{Kind: connectcard.Input, ID: "key", Masked: true, Text: "API key (leave empty if the server has none)"},
		}
		if m.busy {
			rows = append(rows, connectcard.Row{Kind: connectcard.Blank}, busy("Connecting and loading models…"))
		}
		return connectcard.Card{Title: "Other server", Rows: fail(rows), Buttons: actions(btnConnect), Form: true}

	case StepLocal:
		if m.busy || m.fail == "" {
			return connectcard.Card{Title: e.Name, Rows: []connectcard.Row{busy("Looking for " + e.Name + " on this machine…")},
				Buttons: []connectcard.Button{btnCancel}}
		}
		return connectcard.Card{Title: e.Name, Rows: append(fail(nil),
			connectcard.Row{Kind: connectcard.Dim, Text: "Install " + e.Name + " from " + e.Home + ", start it, then press Try again."},
		), Buttons: actions(btnRetry)}

	case StepSignIn:
		title := "Sign in with " + e.Plan
		cli := ai.CLITitle(e.CLI)
		if m.busy || m.fail == "" {
			return connectcard.Card{Title: title, Rows: []connectcard.Row{busy("Checking " + cli + " on this machine…")},
				Buttons: []connectcard.Button{btnCancel}}
		}
		return connectcard.Card{Title: title, Rows: append(fail(nil),
			connectcard.Row{Kind: connectcard.Dim, Text: "Sign in to " + cli + " in a terminal, then press Try again. Or use an API key instead."},
		), Buttons: []connectcard.Button{btnCancel, {ID: "apikey", Title: "Use an API key"}, btnRetry}}

	case StepDone:
		n := len(m.models)
		rows := []connectcard.Row{{Kind: connectcard.OK, Text: "Connected" + wayDone(m.method, e) + " · " + plural(n, "model")}}
		if n == 0 {
			rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "No models yet. Add one to " + e.Name + ", then refresh."})
		} else {
			shown := m.models
			more := ""
			if n > maxChips {
				shown, more = shown[:maxChips], fmt.Sprintf(" and %d more", n-maxChips)
			}
			rows = append(rows, connectcard.Row{Kind: connectcard.Blank},
				connectcard.Row{Kind: connectcard.Text, Text: strings.Join(shown, theme.ASCIIText("  ·  ")) + more})
		}
		return connectcard.Card{Title: e.Name + " is ready", Rows: rows,
			Buttons: []connectcard.Button{{ID: "done", Title: "Done", Primary: true}}}
	}

	// StepPick
	var opts []string
	for _, c := range catalog {
		hint := c.About
		if p, ok := m.opts.Connected[c.Kind]; ok {
			hint = theme.ASCIIText("✓ added · ") + ai.MethodLabel(p)
		}
		opts = append(opts, runewidth.FillRight(c.Name, 14)+hint)
	}
	return connectcard.Card{Title: "Add an AI", Rows: []connectcard.Row{
		{Kind: connectcard.Choice, ID: "provider", Vertical: true, Options: opts},
	}, Buttons: []connectcard.Button{btnCancel}}
}

// wayNow names how a connected AI connects, in a sentence.
func (m Model) wayNow(p ai.ProviderEntry) string {
	switch p.Method {
	case ai.MethodKey:
		return "an API key"
	case ai.MethodCLI:
		return "a sign-in via " + ai.CLITitle(p.CLI)
	case ai.MethodLocal:
		return "a server on this machine"
	}
	return "a server URL"
}

// wayNext names the way Replace starts, as a sentence's subject.
func (m Model) wayNext(method string) string {
	if method == ai.MethodCLI {
		return "signing in with " + m.entry.Plan
	}
	return "the API key"
}

// wayDone is the done line's " with …" part.
func wayDone(method string, e Entry) string {
	switch method {
	case ai.MethodKey:
		return " with an API key"
	case ai.MethodCLI:
		return " with " + e.Plan
	case ai.MethodLocal:
		return " on this machine"
	}
	return ""
}

func keyPlaceholder(kind string) string {
	switch kind {
	case ai.KindAnthropic:
		return "sk-ant-…"
	case ai.KindOpenRouter:
		return "sk-or-…"
	}
	return "sk-…"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return strconv.Itoa(n) + " " + word + "s"
}

// Box renders the card (not placed).
func (m Model) Box() string { return m.card.Box() }

// Origin is the card's top-left cell when centred.
func (m Model) Origin() (x, y int) { return m.card.Origin() }

// ButtonRects lists the buttons with their screen cells.
func (m Model) ButtonRects() []connectcard.ButtonRect { return m.card.ButtonRects() }

// OptionRects lists the AIs (or the ways) with their screen cells; ID is
// the kind (or the method).
func (m Model) OptionRects() []connectcard.ButtonRect {
	var ids, titles []string
	switch m.step {
	case StepPick:
		for _, c := range catalog {
			ids, titles = append(ids, c.Kind), append(titles, c.Name)
		}
	case StepMethod:
		ids = m.entry.Methods
		titles = m.Card().Rows[0].Options
	default:
		return nil
	}
	var out []connectcard.ButtonRect
	for _, r := range m.card.OptionRects() {
		if r.Option < len(ids) {
			out = append(out, connectcard.ButtonRect{ID: ids[r.Option], Title: titles[r.Option], X: r.X, Y: r.Y, W: r.W})
		}
	}
	return out
}
