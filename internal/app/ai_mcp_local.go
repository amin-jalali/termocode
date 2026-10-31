package app

import (
	"encoding/json"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/secrets"
)

// ── Connect → Local server… (MCPX-15: DI-7, DI-12, DI-17) ──────────────
//
//	form (runs with, command or package, name, secret env names, scope)
//	  → preview (the entry and its file) → one hidden field per secret
//	  → written: the entry to mcp.json, the values to the secret store
//
// Nothing is written before Add; the values never reach mcp.json.

const mcpConnectLocalID = "+local"

// The steps of the local form (after the wizard's own steps).
const (
	wizLocal        mcpWizStep = 100 + iota // the form
	wizLocalPreview                         // the entry before it is written
	wizLocalSecret                          // a secret value (hidden)
)

// mcpLocalKinds are the "Runs with" options, in mcp.Kind order.
var mcpLocalKinds = []struct{ kind, title string }{
	{mcp.KindCommand, "Command"}, {mcp.KindNPM, "npm package"}, {mcp.KindPip, "pip package"}, {mcp.KindImage, "Container image"},
}

// mcpLocalForm is the open local form.
type mcpLocalForm struct {
	kind, scope    int // option indexes
	cmd, name, env string

	spec  mcp.LocalSpec
	entry map[string]any
	path  string
	vals  map[string]string // store name → value
	next  int               // the secret asked now
	err   string
}

func (m *Model) openMCPLocal() tea.Cmd {
	w := m.newWizard(mcp.Preset{Name: "local", Title: "a local server"}, "")
	w.local = &mcpLocalForm{}
	w.step = wizLocal
	return m.showWizard()
}

// localCard is the card of a local step (ok=false: not one).
func (m *Model) localCard() (connectcard.Card, bool) {
	w := m.wiz()
	f := w.local
	if f == nil {
		return connectcard.Card{}, false
	}
	cancel := connectcard.Button{ID: "cancel", Title: "Cancel"}
	fail := func(rows []connectcard.Row) []connectcard.Row {
		if f.err != "" {
			rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: f.err})
		}
		return rows
	}
	switch w.step {
	case wizLocal:
		var kinds []string
		for _, k := range mcpLocalKinds {
			kinds = append(kinds, k.title)
		}
		return connectcard.Card{Title: "Add a local server", Rows: fail([]connectcard.Row{
			{Kind: connectcard.Text, Text: "A server that runs on this machine."},
			{Kind: connectcard.Blank},
			{Kind: connectcard.Choice, ID: "lkind", Text: "Runs with", Options: kinds, Selected: f.kind},
			{Kind: connectcard.Input, ID: "lcmd", Text: "Command or package, then arguments:", Value: f.cmd},
			{Kind: connectcard.Input, ID: "lname", Text: "Name (empty: from the package):", Value: f.name},
			{Kind: connectcard.Input, ID: "lenv", Text: "Secret env names (optional, e.g. API_KEY):", Value: f.env},
			{Kind: connectcard.Choice, ID: "lscope", Text: "Save in", Options: []string{"Your mcp.json", "This project"}, Selected: f.scope},
			{Kind: connectcard.Blank},
			{Kind: connectcard.Dim, Text: "You type the secret values next. They go to the secret store, never into mcp.json."},
		}), Buttons: []connectcard.Button{{ID: "local-next", Title: "Next", Primary: true}, cancel}}, true
	case wizLocalPreview:
		b, _ := json.MarshalIndent(map[string]any{f.spec.Name: f.entry}, "", "  ")
		rows := []connectcard.Row{{Kind: connectcard.Text, Text: "Add this to " + f.path + ":"}, {Kind: connectcard.Blank}}
		for _, l := range strings.Split(string(b), "\n") {
			rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: l})
		}
		rows = append(rows, connectcard.Row{Kind: connectcard.Blank})
		if f.scope == 1 {
			rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "termocode asks you to trust a project server before it first starts."})
		}
		if n := len(f.spec.Secrets); n > 0 {
			rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: fmt.Sprintf("Next: the value of %s.", strings.Join(f.spec.Secrets, ", "))})
		}
		return connectcard.Card{Title: "Add " + f.spec.Name, Rows: fail(rows),
			Buttons: []connectcard.Button{{ID: "local-add", Title: "Add", Primary: true}, {ID: "local-back", Title: "Back"}, cancel}}, true
	case wizLocalSecret:
		env := f.spec.Secrets[f.next]
		return connectcard.Card{Title: fmt.Sprintf("Add %s — secret %d of %d", f.spec.Name, f.next+1, len(f.spec.Secrets)), Rows: fail([]connectcard.Row{
			{Kind: connectcard.Input, ID: "lsecret", Text: "Value of " + env + ":", Masked: true},
			{Kind: connectcard.Blank},
			{Kind: connectcard.Dim, Text: "Accepts: " + secrets.PromptHint + "."},
			{Kind: connectcard.Dim, Text: "Kept in the secret store, never in mcp.json."},
		}), Buttons: []connectcard.Button{{ID: "submit", Title: "Save", Primary: true}, cancel}}, true
	}
	return connectcard.Card{}, false
}

// onLocalButton runs a button of the local steps (ok=false: not one).
func (m *Model) onLocalButton(id string) (tea.Cmd, bool) {
	w := m.wiz()
	if w == nil || w.local == nil {
		return nil, false
	}
	switch id {
	case "local-next":
		return m.localNext(), true
	case "local-back":
		w.step, w.local.err = wizLocal, ""
		return m.showWizard(), true
	case "local-add":
		if len(w.local.spec.Secrets) > 0 {
			w.step, w.local.next, w.local.vals, w.local.err = wizLocalSecret, 0, map[string]string{}, ""
			return m.showWizard(), true
		}
		return m.localWrite(), true
	}
	return nil, false
}

// onLocalSubmit takes Enter in a local input (ok=false: not one).
func (m *Model) onLocalSubmit(id, v string) (tea.Cmd, bool) {
	w := m.wiz()
	if w == nil || w.local == nil {
		return nil, false
	}
	switch id {
	case "lcmd", "lname", "lenv":
		return m.localNext(), true
	case "lsecret":
		return m.localSecret(v), true
	}
	return nil, false
}

func (m *Model) localFail(msg string) tea.Cmd {
	m.wiz().local.err = msg
	return m.showWizard()
}

// localNext checks the form and shows the preview.
func (m *Model) localNext() tea.Cmd {
	f := m.wiz().local
	f.kind = clampIdx(m.connect.Choice("lkind"), len(mcpLocalKinds))
	f.scope = clampIdx(m.connect.Choice("lscope"), 2)
	f.cmd = strings.TrimSpace(m.connect.InputValue("lcmd"))
	f.name = strings.TrimSpace(m.connect.InputValue("lname"))
	f.env = strings.TrimSpace(m.connect.InputValue("lenv"))
	words, err := mcp.SplitCommandLine(f.cmd)
	if err != nil {
		return m.localFail(err.Error())
	}
	spec := mcp.LocalSpec{Name: f.name, Kind: mcpLocalKinds[f.kind].kind, Command: words[0], Args: words[1:]}
	if spec.Name == "" {
		spec.Name = mcp.SuggestName(words[0])
	}
	for _, n := range strings.FieldsFunc(f.env, func(r rune) bool { return r == ',' || r == ' ' }) {
		spec.Secrets = append(spec.Secrets, n)
	}
	entry, err := spec.Entry()
	if err != nil {
		return m.localFail(err.Error())
	}
	path := mcpUserPath()
	if f.scope == 1 {
		path = mcp.ProjectPath(aiWorkspaceRoot())
	}
	user, project := path, ""
	if f.scope == 1 {
		user, project = "", path
	}
	list, _ := mcp.LoadAll(user, project)
	for _, s := range list {
		if s.Name == spec.Name {
			return m.localFail(spec.Name + " is already in " + path + ": choose another name.")
		}
	}
	f.spec, f.entry, f.path, f.err = spec, entry, path, ""
	m.wiz().step = wizLocalPreview
	return m.showWizard()
}

// localSecret keeps one typed value; after the last one it writes.
func (m *Model) localSecret(v string) tea.Cmd {
	f := m.wiz().local
	if strings.TrimSpace(v) == "" {
		return m.localFail("Type the value (it is hidden).")
	}
	if msg := secrets.TypedProblem(v); msg != "" {
		return m.localFail(msg)
	}
	ai.RegisterSecret(v)
	f.vals[mcp.LocalSecretName(f.spec.Name, f.spec.Secrets[f.next])] = v
	f.next++
	f.err = ""
	m.connect.SetInputValue("lsecret", "")
	if f.next < len(f.spec.Secrets) {
		return m.showWizard()
	}
	return m.localWrite()
}

// localWrite stores the values, then writes the entry.
func (m *Model) localWrite() tea.Cmd {
	w := m.wiz()
	f := w.local
	cs := mcp.CredStore{Path: mcpCredPath()}
	for k, v := range f.vals {
		if err := cs.SetSecret(k, v); err != nil {
			return m.wizFail(err)
		}
	}
	if err := mcp.AddServer(f.path, f.spec.Name, f.entry); err != nil {
		return m.wizFail(fmt.Errorf("%s", ai.Redact(err.Error())))
	}
	f.vals = nil
	m.aiMCP().Reload()
	w.name = f.spec.Name
	w.step, w.errText = wizDone, "Added "+f.spec.Name+" to "+f.path+"."
	return m.showWizard()
}
