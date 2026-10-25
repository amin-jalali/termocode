package app

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/auth"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/secrets"
	"github.com/amin-jalali/termocode/internal/theme"
)

// ── Connect a service (CX-5) ────────────────────────────────────────────
//
// AI: Connect a Service… is the one door to MCP services, shaped like
// the Claude sign-in: pick a service → one button → the browser opens
// (or a code over SSH) → termocode sees the answer → "connected as …".
//
//	pick (picker) → gh | device | browser | ssh paste | token | Slack app
//	             → test call (Identity) → Done
//
// internal/mcp.Plan picks the ways per service and machine; the first is
// shown, the next ones are the buttons ("Use a token instead" is always
// last). Each step is a connectcard.Card. Tokens only ever go to
// credentials.json (0600) and are registered with ai.RegisterSecret.

const pickerKindMCPConnect pickerKindEnum = 956

// Picker rows that are not presets.
const (
	mcpConnectURLID  = "+url"
	mcpConnectJSONID = "+json"
)

type mcpWizStep int

const (
	wizBusy    mcpWizStep = iota // connecting / testing
	wizGH                        // use gh: account + access
	wizDevice                    // the device code
	wizBrowser                   // laptop: the browser opened
	wizSSH                       // over SSH: paste the address back
	wizEmail                     // Atlassian token: the e-mail first
	wizToken                     // paste a token
	wizApp                       // Slack: create your app
	wizURL                       // Custom URL: type it
	wizPaste                     // Paste a config: an mcpServers block
	wizDone
	wizError
)

// mcpWizard is the open wizard.
type mcpWizard struct {
	gen      int
	preset   mcp.Preset
	name     string // the mcp.json entry
	env      mcp.PlanEnv
	plan     []mcp.Method
	step     mcpWizStep
	method   mcp.Method
	writes   bool
	another  bool // a second account: the entry is named after it
	ssh      bool // the browser is on another machine
	accounts []mcp.GHAccount
	ghUser   string

	flow     *auth.Flow
	device   *auth.DeviceFlow
	cancel   context.CancelFunc
	deadline time.Time
	link     string // the URL c copies / o opens
	userCode string // the device code shown
	port     int    // the loopback port (ssh -L) …
	sshHost  string // … and this machine as the laptop reaches it
	openErr  bool   // the browser did not open
	pasteErr string
	email    string
	busy     string
	note     string // a line on the next screen ("the code sign-in is not set up …")

	result  mcp.IdentityResult
	errText string
	errBtns []connectcard.Button
}

// Messages of the wizard's Cmd goroutines (gen drops stale ones).
type (
	mcpWizEnvMsg struct {
		accounts []mcp.GHAccount
		then     string // a preset to start at once ("" the picker) …
		name     string // … for this entry
		another  bool   // … as another account of the service
	}
	mcpWizDeviceMsg struct {
		gen  int
		flow *auth.DeviceFlow
		err  error
	}
	mcpWizSignInMsg struct {
		gen       int
		flow      *auth.Flow
		connected bool // no sign-in was needed
		err       error
	}
	mcpWizDoneMsg struct {
		gen int
		res mcp.IdentityResult
		err error
	}
	mcpWizTickMsg struct{ gen int }
)

func (m *Model) wiz() *mcpWizard { return m.aiS().mcpWiz }

// openMCPConnect is the palette entry: it asks gh for its accounts (on a
// Cmd goroutine, 5 s at most) and opens the services picker.
// A sign-in is saved in the secret store, so a locked store asks first.
func (m *Model) openMCPConnect() tea.Cmd {
	if c, wait := m.secretsOpenFirst((*Model).openMCPConnect); wait {
		return c
	}
	return m.mcpConnectEnv("", "")
}

func (m *Model) mcpConnectEnv(then, name string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		acc, _ := mcp.GHAccounts(ctx)
		return mcpWizEnvMsg{accounts: acc, then: then, name: name}
	}
}

// mcpConnectServer runs the wizard for a configured server (its Sign in
// action): a preset's ways when the entry is one ("github",
// "github-perkino"), else the browser / paste sign-in of its URL.
func (m *Model) mcpConnectServer(name string) tea.Cmd {
	if c, wait := m.secretsOpenFirst(func(m *Model) tea.Cmd { return m.mcpConnectServer(name) }); wait {
		return c
	}
	if p, ok := mcpPresetFor(name); ok {
		return m.mcpConnectEnv(p.Name, name)
	}
	u := ""
	for _, e := range mcpServerEntries(name) {
		u = e.URL
	}
	m.newWizard(mcp.Preset{Name: name, Title: name, URL: u}, name)
	return m.wizStart()
}

// mcpPresetFor is the preset of an entry name ("github-perkino" →
// github).
func mcpPresetFor(name string) (mcp.Preset, bool) {
	for _, p := range mcp.Presets() {
		if name == p.Name || strings.HasPrefix(name, p.Name+"-") {
			return p, true
		}
	}
	return mcp.Preset{}, false
}

func (m *Model) onMCPWizEnv(msg mcpWizEnvMsg) tea.Cmd {
	m.aiS().mcpGHAccounts = msg.accounts
	if msg.then != "" {
		p, ok := mcp.PresetByName(msg.then)
		if !ok {
			return nil
		}
		w := m.newWizard(p, msg.name)
		w.another = msg.another
		return m.wizStart()
	}
	m.picker = picker.NewItems(" Connect a service ", m.mcpConnectItems())
	m.picker.SetWidth(connectcard.MaxWidth)
	m.picker.SetSize(m.w, m.h)
	m.pickerOpen = true
	m.pickerKind = pickerKindMCPConnect
	return nil
}

// mcpConnectItems are the picker rows: services with a computed hint.
func (m *Model) mcpConnectItems() []picker.Item {
	var items []picker.Item
	for _, p := range mcp.Presets() {
		items = append(items, picker.Item{ID: p.Name, Title: fmt.Sprintf("%-15s %s", p.Title, p.Blurb), Hint: m.mcpConnectHint(p)})
	}
	items = append(items,
		picker.Item{ID: mcpConnectURLID, Title: fmt.Sprintf("%-15s %s", "Custom URL…", "any MCP server"), Hint: "type its address"},
		picker.Item{ID: mcpConnectJSONID, Title: fmt.Sprintf("%-15s %s", "Paste a config…", "an mcpServers block"), Hint: "tokens move to credentials"})
	return items
}

// mcpConnectHint says what the next step for a service will be.
func (m *Model) mcpConnectHint(p mcp.Preset) string {
	cs := mcp.CredStore{Path: mcpCredPath()}
	for _, e := range mcpServerEntries(p.Name) {
		if a := cs.Account(p.Name); a != "" {
			return "connected as " + a
		}
		if e.Auth != nil && e.Auth.GH != "" {
			return "connected as " + e.Auth.GH
		}
		if _, ok := cs.Load(p.Name); ok {
			return "connected"
		}
	}
	env := m.mcpPlanEnv(p)
	plan := mcp.Plan(p, env)
	if len(plan) == 0 {
		return ""
	}
	switch plan[0] {
	case mcp.MethodCLI:
		return "gh is signed in"
	case mcp.MethodDevice:
		return "code on " + hostOf(p.Device, p.URL)
	case mcp.MethodBrowser:
		return "sign in in the browser"
	case mcp.MethodPaste:
		return "paste the address (SSH)"
	case mcp.MethodApp:
		return "needs a Slack app (2 min)"
	case mcp.MethodToken:
		return "use a token"
	}
	return ""
}

func hostOf(d *mcp.DeviceApp, fallback string) string {
	u := fallback
	if d != nil && d.DeviceEndpoint != "" {
		u = d.DeviceEndpoint
	}
	if p, err := url.Parse(u); err == nil && p.Host != "" {
		return p.Host
	}
	return u
}

// mcpPlanEnv is this machine for the planner.
func (m *Model) mcpPlanEnv(p mcp.Preset) mcp.PlanEnv {
	env := mcp.PlanEnv{SSH: mcpOverSSH()}
	if p.CLI == "gh" {
		env.GHAccounts = m.aiS().mcpGHAccounts
	}
	if p.Device != nil && p.Device.ClientID != nil {
		env.GitHubClientID = p.Device.ClientID()
	}
	for _, e := range mcpServerEntries(p.Name) {
		if e.OAuth != nil && e.OAuth.ClientID != "" && !strings.Contains(e.OAuth.ClientID, "${") {
			env.AppClientID = e.OAuth.ClientID
		}
	}
	return env
}

// onMCPConnectPicked starts the wizard for a picker row.
func (m *Model) onMCPConnectPicked(id string) tea.Cmd {
	switch id {
	case mcpConnectJSONID:
		w := m.newWizard(mcp.Preset{Name: "pasted", Title: "pasted servers"}, "")
		w.step = wizPaste
		return m.showWizard()
	case mcpConnectURLID:
		w := m.newWizard(mcp.Preset{Name: "custom", Title: "a custom server"}, "")
		w.step = wizURL
		return m.showWizard()
	}
	p, ok := mcp.PresetByName(id)
	if !ok {
		return nil
	}
	m.newWizard(p, p.Name)
	return m.wizStart()
}

func (m *Model) newWizard(p mcp.Preset, name string) *mcpWizard {
	s := m.aiS()
	m.closeWizardFlows()
	s.mcpWizGen++
	w := &mcpWizard{gen: s.mcpWizGen, preset: p, name: name}
	s.mcpWiz = w
	return w
}

// wizStart plans the ways and goes to the first one.
func (m *Model) wizStart() tea.Cmd {
	w := m.wiz()
	w.env = m.mcpPlanEnv(w.preset)
	w.ssh = w.env.SSH
	for _, e := range mcpServerEntries(w.name) {
		w.writes = mcpIsPreset(w.preset.Name) && (w.preset.ReadOnlyURL != "" || w.preset.WriteScopes != nil) && !mcpEntryReadOnly(e, w.preset)
	}
	w.plan = mcp.Plan(w.preset, w.env)
	w.accounts = w.env.GHAccounts
	if w.preset.Device != nil && w.env.GitHubClientID == "" && w.preset.NoBrowser {
		w.note = "The GitHub sign-in with a code is not set up in this build yet (no OAuth app id). Use gh (run gh auth login) or a token."
	}
	if len(w.plan) == 0 {
		return m.wizFail(errors.New("there is no way to connect this service on this machine"))
	}
	return m.wizGo(w.plan[0])
}

// wizHas reports a method in the plan.
func (w *mcpWizard) has(meth mcp.Method) bool {
	for _, x := range w.plan {
		if x == meth {
			return true
		}
	}
	return false
}

// closeWizardFlows ends a pending sign-in or poll of the open wizard.
func (m *Model) closeWizardFlows() {
	if m.ai == nil || m.ai.mcpWiz == nil {
		return
	}
	w := m.ai.mcpWiz
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	if w.flow != nil {
		w.flow.Close()
		w.flow = nil
	}
	w.device = nil
}

// closeWizard closes the card and ends everything it started.
func (m *Model) closeWizard() {
	m.closeWizardFlows()
	if m.ai != nil {
		m.ai.mcpWiz = nil
		m.ai.mcpWizGen++
	}
	m.connectOpen = false
}

// wizEntry writes the mcp.json entry for a method (read-only first).
func (m *Model) wizEntry(meth mcp.Method) error {
	w := m.wiz()
	e := w.preset.EntryFor(meth, mcp.EntryOptions{Writes: w.writes, GHUser: w.ghUser, AppClientID: w.env.AppClientID, Secret: w.name + "_token"})
	if !mcpIsPreset(w.preset.Name) {
		if len(mcpServerEntries(w.name)) > 0 && meth != mcp.MethodToken {
			return nil // the user's own entry stays as it is
		}
		e = map[string]any{"type": mcp.TypeHTTP, "url": w.preset.URL}
		if meth == mcp.MethodToken {
			e["headers"] = map[string]string{"Authorization": "Bearer ${secret:" + w.name + "_token}"}
		}
	}
	if err := mcp.PutServer(mcpUserPath(), w.name, e); err != nil {
		return err
	}
	m.aiMCP().Reload()
	return nil
}

func mcpIsPreset(name string) bool {
	_, ok := mcp.PresetByName(name)
	return ok
}

// wizGo shows the screen of one method and starts its work.
func (m *Model) wizGo(meth mcp.Method) tea.Cmd {
	w := m.wiz()
	m.closeWizardFlows()
	w.method, w.pasteErr, w.openErr = meth, "", false
	mgr := m.aiMCP()
	gen, name := w.gen, w.name
	switch meth {
	case mcp.MethodCLI:
		w.step = wizGH
		return m.showWizard()
	case mcp.MethodApp:
		w.step = wizApp
		w.link = w.preset.App.Page
		m.copyToClipboard(w.preset.App.Manifest())
		return m.showWizard()
	case mcp.MethodToken:
		w.step = wizToken
		if w.preset.Token != nil && w.preset.Token.Email && w.email == "" {
			w.step = wizEmail
		}
		if w.preset.Token != nil {
			w.link = w.preset.Token.Page
			m.copyToClipboard(w.link)
		}
		return m.showWizard()
	case mcp.MethodDevice:
		if err := m.wizEntry(meth); err != nil {
			return m.wizFail(err)
		}
		w.step, w.busy = wizBusy, "Asking "+w.preset.Title+" for a code …"
		app := mcp.DeviceApp{}
		clientID := w.env.AppClientID
		if w.preset.Device != nil {
			app = *w.preset.Device
			clientID = w.env.GitHubClientID
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		w.cancel = cancel
		return tea.Batch(m.showWizard(), func() tea.Msg {
			defer cancel()
			d, err := mgr.DeviceSignIn(ctx, name, app, clientID)
			return mcpWizDeviceMsg{gen: gen, flow: d, err: err}
		})
	case mcp.MethodBrowser, mcp.MethodPaste:
		if err := m.wizEntry(meth); err != nil {
			return m.wizFail(err)
		}
		w.step, w.busy = wizBusy, "Connecting to "+w.preset.Title+" …"
		so := mcp.SignInOptions{}
		if r := w.preset.App; r != nil {
			so = mcp.SignInOptions{Port: r.Port, RedirectHost: r.RedirectHost}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		w.cancel = cancel
		return tea.Batch(m.showWizard(), func() tea.Msg {
			defer cancel()
			// A first connect: no sign-in needed, or the 401 tells where
			// the authorization server is.
			err := mgr.Connect(ctx, nil, name)
			if err == nil {
				return mcpWizSignInMsg{gen: gen, connected: true}
			}
			if !errors.Is(err, mcp.ErrSignInNeeded) {
				return mcpWizSignInMsg{gen: gen, err: err}
			}
			fl, err := mgr.BeginSignInWith(ctx, name, so)
			return mcpWizSignInMsg{gen: gen, flow: fl, err: err}
		})
	}
	return nil
}

// wizIdentity runs the test call and ends on Done or an error.
func (m *Model) wizIdentity() tea.Cmd {
	w := m.wiz()
	w.step, w.busy = wizBusy, "Testing the connection to "+w.preset.Title+" …"
	mgr := m.aiMCP()
	gen, name, tool := w.gen, w.name, w.preset.Identity
	mgr.Stop(name) // the next start uses the new entry / token
	return tea.Batch(m.showWizard(), func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := mgr.Identity(ctx, name, tool)
		return mcpWizDoneMsg{gen: gen, res: res, err: err}
	})
}

func (m *Model) onMCPWizDevice(msg mcpWizDeviceMsg) tea.Cmd {
	w := m.wiz()
	if w == nil || msg.gen != w.gen {
		return nil
	}
	if msg.err != nil {
		return m.wizFail(msg.err)
	}
	d := msg.flow
	w.device, w.step, w.deadline = d, wizDevice, d.ExpiresAt()
	w.link, w.userCode = d.VerificationURI(), d.UserCode()
	m.copyToClipboard(d.UserCode())
	if !w.ssh {
		open := d.VerificationURIComplete()
		if open == "" {
			open = d.VerificationURI()
		}
		w.openErr = osOpen(open) != nil
	}
	mgr := m.aiMCP()
	gen, name, tool := w.gen, w.name, w.preset.Identity
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	return tea.Batch(m.showWizard(), m.wizTick(), func() tea.Msg {
		defer cancel()
		tok, err := d.Poll(ctx)
		if err == nil {
			err = mgr.FinishSignIn(name, tok)
		}
		if err != nil {
			return mcpWizDoneMsg{gen: gen, err: err}
		}
		mgr.Stop(name)
		ictx, icancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer icancel()
		res, err := mgr.Identity(ictx, name, tool)
		return mcpWizDoneMsg{gen: gen, res: res, err: err}
	})
}

func (m *Model) onMCPWizSignIn(msg mcpWizSignInMsg) tea.Cmd {
	w := m.wiz()
	if w == nil || msg.gen != w.gen {
		if msg.flow != nil {
			msg.flow.Close()
		}
		return nil
	}
	switch {
	case msg.err != nil:
		return m.wizFail(msg.err)
	case msg.connected:
		return m.wizIdentity()
	}
	fl := msg.flow
	w.flow, w.deadline, w.link, w.port = fl, clock.Now().Add(auth.DefaultLifetime), fl.AuthURL(), fl.Port()
	m.copyToClipboard(fl.AuthURL())
	if w.method == mcp.MethodBrowser {
		w.step = wizBrowser
		w.openErr = osOpen(fl.AuthURL()) != nil
	} else {
		w.step, w.sshHost = wizSSH, mcpSSHHost()
	}
	mgr := m.aiMCP()
	gen, name, tool := w.gen, w.name, w.preset.Identity
	return tea.Batch(m.showWizard(), m.wizTick(), func() tea.Msg {
		tok, err := fl.Wait(context.Background())
		if err == nil {
			err = mgr.FinishSignIn(name, tok)
		}
		if err != nil {
			return mcpWizDoneMsg{gen: gen, err: err}
		}
		mgr.Stop(name)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := mgr.Identity(ctx, name, tool)
		return mcpWizDoneMsg{gen: gen, res: res, err: err}
	})
}

func (m *Model) onMCPWizDone(msg mcpWizDoneMsg) tea.Cmd {
	w := m.wiz()
	if w == nil || msg.gen != w.gen {
		return nil
	}
	w.flow, w.device = nil, nil
	if msg.err != nil {
		if strings.Contains(msg.err.Error(), "the sign-in was closed") || errors.Is(msg.err, context.Canceled) {
			return nil // Esc or a new step: nothing to say
		}
		return m.wizFail(msg.err)
	}
	w.result, w.step = msg.res, wizDone
	cs := mcp.CredStore{Path: mcpCredPath()}
	_ = cs.SetAccount(w.name, msg.res.Account)
	if w.another && msg.res.Account != "" && strings.HasPrefix(w.name, w.preset.Name+"-") {
		// "github-2" → "github-perkino-deals" now that the account is known.
		if n := mcp.EntryName(w.preset.Name, msg.res.Account); n != "" && n != w.name && len(mcpServerEntries(n)) == 0 {
			mgr := m.aiMCP()
			mgr.Stop(w.name)
			if err := mcp.RenameServer(mcpUserPath(), cs, w.name, n); err == nil {
				w.name = n
				mgr.Reload()
			}
		}
	}
	if m.setup != nil {
		m.setup.services = -1 // the setup card counts again
	}
	return m.showWizard()
}

// wizTick redraws the countdown once a second while a step waits.
func (m *Model) wizTick() tea.Cmd {
	gen := m.wiz().gen
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return mcpWizTickMsg{gen: gen} })
}

func (m *Model) onMCPWizTick(msg mcpWizTickMsg) tea.Cmd {
	w := m.wiz()
	if w == nil || msg.gen != w.gen || !m.connectOpen {
		return nil
	}
	switch w.step {
	case wizDevice, wizBrowser, wizSSH:
		m.connect.SetCard(m.wizCard())
		return m.wizTick()
	}
	return nil
}

// wizFail shows the error screen for err (one text, one fix).
func (m *Model) wizFail(err error) tea.Cmd {
	w := m.wiz()
	if w == nil {
		return m.aiToastErr("Connect", err)
	}
	m.closeWizardFlows()
	title := w.preset.Title
	tokenBtn := connectcard.Button{ID: "token", Title: "Use a token instead"}
	cancel := connectcard.Button{ID: "cancel", Title: "Cancel"}
	retry := connectcard.Button{ID: "retry", Title: "Try again", Primary: true}
	hasToken := w.preset.Token != nil && w.method != mcp.MethodToken
	btns := func(bs ...connectcard.Button) []connectcard.Button {
		var out []connectcard.Button
		for _, b := range bs {
			if b.ID == "token" && !hasToken {
				continue
			}
			out = append(out, b)
		}
		return out
	}
	msg := ai.Redact(err.Error())
	text := ""
	switch {
	case errors.Is(err, auth.ErrDeviceExpired):
		text = "The code expired before " + title + " saw it."
		retry.Title = "Try again (new code)"
		w.errBtns = btns(retry, tokenBtn, cancel)
	case errors.Is(err, auth.ErrDeviceDenied):
		text = "The sign-in was cancelled on the web page. Nothing was connected."
		w.errBtns = btns(retry, cancel)
	case errors.Is(err, mcp.ErrGHNotLoggedIn):
		text = "gh is installed but not signed in. Run gh auth login, or sign in on github.com."
		w.errBtns = btns(connectcard.Button{ID: "device", Title: "Sign in on github.com", Primary: true}, tokenBtn, cancel)
	case strings.Contains(msg, "allows only approved clients"), errors.Is(err, auth.ErrNeedsApp):
		text = "This service lets only approved apps sign in. Use a token instead, or ask your admin."
		tokenBtn.Primary = true
		w.errBtns = btns(tokenBtn, cancel)
	case strings.Contains(msg, "state does not match"):
		text = "That address belongs to another sign-in. Start again."
		w.errBtns = btns(retry, cancel)
	case strings.Contains(msg, "timed out"):
		text = "No answer from the browser in 5 minutes."
		w.errBtns = btns(retry, cancel)
	case w.method == mcp.MethodToken && (strings.Contains(msg, "not authorized") || strings.Contains(msg, "401")):
		scopes := ""
		if w.preset.Token != nil {
			scopes = strings.Join(w.preset.Token.Scopes, ", ")
		}
		text = title + " refused the token (401)."
		if scopes != "" {
			text += " Check that it has " + scopes + "."
		}
		w.errBtns = []connectcard.Button{{ID: "edit-token", Title: "Edit token", Primary: true}, cancel}
	case w.method == mcp.MethodCLI && strings.Contains(msg, "not authorized"):
		text = title + " refused the gh token. Run gh auth refresh, or sign in another way."
		w.errBtns = btns(tokenBtn, cancel)
	default:
		text = "Could not connect " + title + ": " + aiCleanText(msg)
		w.errBtns = btns(retry, tokenBtn, cancel)
	}
	w.step, w.errText = wizError, text
	return m.showWizard()
}

// showWizard opens (or refreshes) the card for the current step.
func (m *Model) showWizard() tea.Cmd {
	c := m.wizCard()
	if m.connectOpen {
		m.connect.SetCard(c)
	} else {
		m.connect = connectcard.New(c)
	}
	m.connect.SetSize(m.w, m.h)
	m.connectOpen = true
	return nil
}

// countdown is "4:51 left".
func countdown(until time.Time) string {
	d := until.Sub(clock.Now())
	if d < 0 {
		d = 0
	}
	s := int(d.Round(time.Second).Seconds())
	return fmt.Sprintf("%d:%02d left", s/60, s%60)
}

// wizCard builds the card of the current step.
func (m *Model) wizCard() connectcard.Card {
	w := m.wiz()
	p := w.preset
	title := "Connect " + p.Title
	cancel := connectcard.Button{ID: "cancel", Title: "Cancel"}
	var tokenBtns []connectcard.Button
	if w.has(mcp.MethodToken) && w.method != mcp.MethodToken {
		tokenBtns = append(tokenBtns, connectcard.Button{ID: "token", Title: "Use a token instead"})
	}
	wait := theme.GlyphWait.String() + " Waiting for " + p.Title + " …  " + countdown(w.deadline)
	note := func(rows []connectcard.Row) []connectcard.Row {
		if w.note == "" {
			return rows
		}
		return append([]connectcard.Row{{Kind: connectcard.Dim, Text: w.note}, {Kind: connectcard.Blank}}, rows...)
	}
	switch w.step {
	case wizBusy:
		return connectcard.Card{Title: title, Rows: []connectcard.Row{{Kind: connectcard.Status, Text: theme.GlyphWait.String() + " " + w.busy}},
			Buttons: []connectcard.Button{cancel}}
	case wizGH:
		var opts []string
		for _, a := range w.accounts {
			o := a.Login
			if a.Active {
				o += " (active)"
			}
			opts = append(opts, o)
		}
		btns := []connectcard.Button{{ID: "gh", Title: "Use gh", Primary: true}}
		if w.has(mcp.MethodDevice) {
			btns = append(btns, connectcard.Button{ID: "device", Title: "Sign in on github.com"})
		}
		if w.has(mcp.MethodToken) {
			btns = append(btns, connectcard.Button{ID: "token", Title: "Use a token"})
		}
		btns = append(btns, cancel)
		return connectcard.Card{Title: title, Rows: []connectcard.Row{
			{Kind: connectcard.Text, Text: "You are signed in to GitHub with the gh command."},
			{Kind: connectcard.Blank},
			{Kind: connectcard.Choice, ID: "account", Text: "Account", Options: opts, Vertical: true},
			{Kind: connectcard.Blank},
			{Kind: connectcard.Choice, ID: "access", Text: "Access", Options: []string{"Read only", "Read and write"}},
			{Kind: connectcard.Blank},
			{Kind: connectcard.Dim, Text: "termocode asks gh for the token when it connects; it keeps no copy."},
		}, Buttons: btns}
	case wizDevice:
		open := "(opened)"
		switch {
		case w.ssh:
			open = "(on your laptop or phone)"
		case w.openErr:
			open = "(o: open it)"
		}
		rows := []connectcard.Row{
			{Kind: connectcard.Text, Text: "1. Open  " + w.link + "  " + open},
			{Kind: connectcard.Text, Text: "2. Enter this code there (it is copied):"},
			{Kind: connectcard.Code, Text: w.userCode},
			{Kind: connectcard.Status, Text: wait},
		}
		if w.ssh {
			rows = append(rows, connectcard.Row{Kind: connectcard.Blank},
				connectcard.Row{Kind: connectcard.Dim, Text: "Over SSH: open the link on your laptop or phone; this screen finishes by itself when " + p.Title + " says yes."})
		}
		return connectcard.Card{Title: title, Rows: rows, Buttons: append(tokenBtns, cancel),
			Keys: []string{"c", "copy code", "o", "open page"}}
	case wizBrowser:
		rows := []connectcard.Row{{Kind: connectcard.Text, Text: "The browser opened. Allow termocode there; this closes by itself."}}
		if w.openErr {
			rows = []connectcard.Row{{Kind: connectcard.Fail, Text: "Could not open a browser. Press c to copy the link and open it anywhere."}}
		}
		rows = append(rows, connectcard.Row{Kind: connectcard.Blank}, connectcard.Row{Kind: connectcard.Status, Text: wait},
			connectcard.Row{Kind: connectcard.Blank},
			connectcard.Row{Kind: connectcard.Dim, Text: "Browser did not open?  c: copy the link  ·  o: open it again"})
		return connectcard.Card{Title: title, Rows: rows, Buttons: append(tokenBtns, cancel),
			Keys: []string{"c", "copy link", "o", "open"}}
	case wizSSH:
		port := w.port
		rows := []connectcard.Row{
			{Kind: connectcard.Text, Text: "1. Open this link in your laptop's browser (it is copied):"},
			{Kind: connectcard.Link, Text: w.link},
			{Kind: connectcard.Text, Text: "2. Allow termocode. The browser then says \"can't connect\": that is normal. Copy the whole address from its address bar."},
			{Kind: connectcard.Input, ID: "addr", Text: "3. Paste it here:", Masked: true},
		}
		if w.pasteErr != "" {
			rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: w.pasteErr})
		}
		rows = append(rows, connectcard.Row{Kind: connectcard.Status, Text: wait},
			connectcard.Row{Kind: connectcard.Blank},
			connectcard.Row{Kind: connectcard.Dim, Text: "Shortcut: run this on your laptop before step 2 (no paste then):"},
			connectcard.Row{Kind: connectcard.Link, Text: fmt.Sprintf("ssh -L %d:127.0.0.1:%d %s", port, port, w.sshHost)})
		return connectcard.Card{Title: title + " — over SSH", Rows: rows, Buttons: append(tokenBtns, cancel)}
	case wizEmail:
		return connectcard.Card{Title: title + " with a token", Rows: note([]connectcard.Row{
			{Kind: connectcard.Text, Text: p.Title + " API tokens go with the e-mail of your account."},
			{Kind: connectcard.Input, ID: "email", Text: "Your e-mail:"},
		}), Buttons: []connectcard.Button{{ID: "submit", Title: "Next", Primary: true}, cancel}}
	case wizToken:
		page, scopes := "", ""
		if t := p.Token; t != nil {
			page, scopes = t.Page, strings.Join(t.Scopes, ", ")
		}
		rows := []connectcard.Row{}
		if page != "" {
			rows = append(rows, connectcard.Row{Kind: connectcard.Text, Text: "1. Open this page (it is copied):"}, connectcard.Row{Kind: connectcard.Link, Text: page})
		}
		if scopes != "" {
			rows = append(rows, connectcard.Row{Kind: connectcard.Text, Text: "2. Create a token with: " + scopes})
		}
		rows = append(rows, connectcard.Row{Kind: connectcard.Input, ID: "token", Text: "3. Paste it here:", Masked: true})
		if w.pasteErr != "" {
			rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: w.pasteErr})
		}
		rows = append(rows, connectcard.Row{Kind: connectcard.Blank})
		if p.Token == nil || !p.Token.Email {
			rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "Accepts: " + secrets.PromptHint + "."})
		}
		rows = append(rows, connectcard.Row{Kind: connectcard.Dim, Text: "Kept in credentials.json (only you can read it), never in mcp.json."})
		return connectcard.Card{Title: title + " with a token", Rows: note(rows),
			Buttons: []connectcard.Button{{ID: "submit", Title: "Connect", Primary: true}, cancel}, Keys: []string{"o", "open page"}}
	case wizApp:
		r := p.App
		rows := []connectcard.Row{{Kind: connectcard.Text, Text: "Slack lets only registered apps in. Create yours once (2 min):"}}
		for i, s := range r.Steps {
			rows = append(rows, connectcard.Row{Kind: connectcard.Text, Text: fmt.Sprintf("%d. %s", i+1, s)})
		}
		rows = append(rows, connectcard.Row{Kind: connectcard.Blank}, connectcard.Row{Kind: connectcard.Input, ID: "client", Text: "Client ID:"},
			connectcard.Row{Kind: connectcard.Dim, Text: "The manifest has no secret. c: copy it again  ·  o: open api.slack.com"})
		return connectcard.Card{Title: title + " — create your Slack app", Rows: rows,
			Buttons: []connectcard.Button{{ID: "submit", Title: "Connect", Primary: true}, cancel}, Keys: []string{"c", "copy manifest", "o", "open"}}
	case wizURL:
		rows := []connectcard.Row{{Kind: connectcard.Text, Text: "Any remote MCP server (Streamable HTTP)."},
			{Kind: connectcard.Input, ID: "url", Text: "Its address (https://…/mcp):"}}
		if w.pasteErr != "" {
			rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: w.pasteErr})
		}
		return connectcard.Card{Title: "Connect a custom server", Rows: rows,
			Buttons: []connectcard.Button{{ID: "submit", Title: "Connect", Primary: true}, cancel}}
	case wizPaste:
		rows := []connectcard.Row{
			{Kind: connectcard.Text, Text: "Paste an mcpServers block from another tool, such as Claude Desktop or Claude Code (the whole file or some servers)."},
			{Kind: connectcard.Blank},
			{Kind: connectcard.Input, ID: "config", Text: "The block:", Masked: true},
			{Kind: connectcard.Blank},
			{Kind: connectcard.Dim, Text: "Tokens in it are moved to credentials.json (only you can read it); mcp.json gets ${secret:…} instead."},
		}
		if w.pasteErr != "" {
			rows = append(rows, connectcard.Row{Kind: connectcard.Fail, Text: w.pasteErr})
		}
		return connectcard.Card{Title: "Paste a config", Rows: rows,
			Buttons: []connectcard.Button{{ID: "submit", Title: "Add", Primary: true}, {ID: "edit-json", Title: "Open mcp.json"}, cancel}}
	case wizDone:
		if w.preset.Name == "pasted" {
			rows := []connectcard.Row{{Kind: connectcard.OK, Text: w.errText}}
			rows = append(rows, connectcard.Row{Kind: connectcard.Blank},
				connectcard.Row{Kind: connectcard.Dim, Text: "Local servers start on the first chat message that needs them. A remote one that needs a sign-in: AI: MCP Servers… → the server → Sign in."})
			return connectcard.Card{Title: "Added", Rows: rows, Buttons: []connectcard.Button{{ID: "servers", Title: "Show servers", Primary: true}, {ID: "done", Title: "Done"}}}
		}
		return m.wizDoneCard()
	case wizError:
		return connectcard.Card{Title: title, Rows: []connectcard.Row{{Kind: connectcard.Fail, Text: w.errText}}, Buttons: w.errBtns}
	}
	return connectcard.Card{Title: title}
}

// wizReadOnly reports an entry on the read-only endpoint / scopes.
func (w *mcpWizard) readOnly() bool {
	return !w.writes && (w.preset.ReadOnlyURL != "" || w.preset.WriteScopes != nil)
}

func (m *Model) wizDoneCard() connectcard.Card {
	w := m.wiz()
	p, r := w.preset, w.result
	parts := []string{p.Title + " — connected"}
	if r.Account != "" {
		parts[0] += " as " + r.Account
	} else if p.Identity != "" {
		parts = append(parts, "account unknown")
	} else if r.Server != "" {
		parts = append(parts, r.Server)
	}
	parts = append(parts, fmt.Sprintf("%d tools", r.Tools))
	if w.readOnly() {
		parts = append(parts, "read only")
	}
	rows := []connectcard.Row{{Kind: connectcard.OK, Text: strings.Join(parts, " · ")}}
	if p.Try != "" {
		rows = append(rows, connectcard.Row{Kind: connectcard.Blank}, connectcard.Row{Kind: connectcard.Text, Text: "Try:  \"" + p.Try + "\""})
	}
	if w.readOnly() {
		rows = append(rows, connectcard.Row{Kind: connectcard.Blank},
			connectcard.Row{Kind: connectcard.Dim, Text: "Writes (create, comment, change) are off. Turn them on any time: AI: MCP Servers… → " + w.name + " → Allow writes."})
	}
	btns := []connectcard.Button{{ID: "done", Title: "Done", Primary: true}}
	if p.Try != "" {
		btns = []connectcard.Button{{ID: "ask", Title: "Ask the AI", Primary: true}, {ID: "done", Title: "Done"}}
	}
	return connectcard.Card{Title: "Connected", Rows: rows, Buttons: btns}
}

// onMCPWizButton runs a button of the card.
func (m *Model) onMCPWizButton(id string) tea.Cmd {
	w := m.wiz()
	if w == nil {
		m.connectOpen = false
		return nil
	}
	switch id {
	case "cancel", "done":
		m.closeWizard()
		return nil
	case "ask":
		try := w.preset.Try
		m.closeWizard()
		c := m.openAIPanel(true)
		s := m.aiS()
		s.input = []rune(try)
		s.caret = len(s.input)
		return c
	case "gh":
		if len(w.accounts) == 0 {
			return m.wizFail(mcp.ErrGHNotLoggedIn)
		}
		w.ghUser = w.accounts[clampIdx(m.connect.Choice("account"), len(w.accounts))].Login
		w.writes = m.connect.Choice("access") == 1
		if w.another {
			if n := mcp.EntryName(w.preset.Name, w.ghUser); n != "" && len(mcpServerEntries(n)) == 0 {
				w.name = n
			}
		}
		if err := m.wizEntry(mcp.MethodCLI); err != nil {
			return m.wizFail(err)
		}
		return m.wizIdentity()
	case "device":
		return m.wizGo(mcp.MethodDevice)
	case "token":
		return m.wizGo(mcp.MethodToken)
	case "edit-token":
		w.method = ""
		return m.wizGo(mcp.MethodToken)
	case "edit-json":
		m.closeWizard()
		return m.editMCPConfig()
	case "servers":
		m.closeWizard()
		return m.openMCPServers()
	case "retry":
		if w.method == "" {
			return m.wizStart()
		}
		return m.wizGo(w.method)
	case "submit":
		for _, r := range m.connect.Card().Rows {
			if r.Kind == connectcard.Input {
				return m.onMCPWizSubmit(r.ID, m.connect.Value())
			}
		}
	}
	return nil
}

func clampIdx(i, n int) int {
	if i < 0 || i >= n {
		return 0
	}
	return i
}

var emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// onMCPWizSubmit takes the input of a step.
func (m *Model) onMCPWizSubmit(id, v string) tea.Cmd {
	w := m.wiz()
	if w == nil {
		return nil
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	switch id {
	case "addr":
		if w.flow == nil {
			return nil
		}
		err := w.flow.Paste(v)
		if err != nil && strings.Contains(err.Error(), "paste the whole address") {
			w.pasteErr = "That is not the whole address: copy it from the address bar (it has ?code=…&state=…)."
			m.connect = connectcard.New(m.wizCard())
			m.connect.SetSize(m.w, m.h)
			return nil
		}
		w.step, w.busy = wizBusy, "Signing in to "+w.preset.Title+" …"
		return m.showWizard() // the Wait goroutine ends the step
	case "email":
		if !emailRe.MatchString(v) {
			return nil
		}
		w.email = v
		w.step = wizToken
		return m.showWizard()
	case "token":
		emailToken := w.preset.Token != nil && w.preset.Token.Email
		msg := secrets.TypedProblem(v)
		if msg == "" && emailToken && secrets.IsRef(v) {
			// The e-mail is joined to the token before it is stored, so the
			// reference would be encoded instead of resolved.
			msg = "This service needs the token itself here, not a reference."
		}
		if msg != "" {
			w.pasteErr = msg
			m.connect = connectcard.New(m.wizCard())
			m.connect.SetSize(m.w, m.h)
			return nil
		}
		w.pasteErr = ""
		ai.RegisterSecret(v)
		val := v
		if emailToken {
			val = base64.StdEncoding.EncodeToString([]byte(w.email + ":" + v))
			ai.RegisterSecret(val)
		}
		secret := w.name + "_token"
		if err := (mcp.CredStore{Path: mcpCredPath()}).SetSecret(secret, val); err != nil {
			return m.wizFail(err)
		}
		w.method = mcp.MethodToken
		if err := m.wizEntry(mcp.MethodToken); err != nil {
			return m.wizFail(err)
		}
		return m.wizIdentity()
	case "client":
		if strings.ContainsAny(v, " \t\"'${}") || len(v) > 100 {
			return nil
		}
		w.env.AppClientID = v
		w.plan = mcp.Plan(w.preset, w.env)
		return m.wizGo(w.plan[0])
	case "config":
		return m.wizImport(v)
	case "url":
		u, err := url.Parse(v)
		if err != nil || u.Host == "" || (u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackHost(u.Hostname()))) {
			w.pasteErr = "Use an https:// address."
			return m.showWizard()
		}
		w.preset = mcp.Preset{Name: mcpCustomName(u.Hostname()), Title: u.Hostname(), URL: v}
		w.name = w.preset.Name
		w.env = mcp.PlanEnv{SSH: w.ssh}
		w.plan = mcp.Plan(w.preset, w.env)
		return m.wizGo(w.plan[0])
	}
	return nil
}

func isLoopbackHost(h string) bool {
	return h == "localhost" || h == "::1" || strings.HasPrefix(h, "127.")
}

// mcpCustomName is an entry name for a host ("mcp.example.com" →
// "mcp-example-com"), unique in mcp.json.
func mcpCustomName(host string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(host) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), "-")
	if len(name) > 30 {
		name = name[:30]
	}
	if name == "" {
		name = "server"
	}
	base := name
	for i := 2; len(mcpServerEntries(name)) > 0; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return name
}

// onMCPWizKey runs a letter key of the card.
func (m *Model) onMCPWizKey(k string) tea.Cmd {
	w := m.wiz()
	if w == nil {
		return nil
	}
	switch k {
	case "c":
		switch w.step {
		case wizDevice:
			if w.userCode != "" {
				m.copyToClipboard(w.userCode)
				return m.aiInfo("Copied the code")
			}
		case wizApp:
			m.copyToClipboard(w.preset.App.Manifest())
			return m.aiInfo("Copied the Slack app manifest")
		default:
			if w.link != "" {
				m.copyToClipboard(w.link)
				return m.aiInfo("Copied the link")
			}
		}
	case "o":
		if w.link != "" {
			if err := osOpen(w.link); err != nil {
				w.openErr = true
				m.connect.SetCard(m.wizCard())
				return m.aiInfo("Could not open a browser: press c to copy the link")
			}
		}
	}
	return nil
}

// routeToConnect sends keys, clicks and resizes to the open card.
func (m Model) routeToConnect(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.applyLayout()
		m.connect.SetSize(m.w, m.h)
		return m, nil
	case tea.MouseMsg:
		if m.mouseProbe != nil {
			for _, b := range m.connect.ButtonRects() {
				if msg.Y == b.Y && msg.X >= b.X && msg.X < b.X+b.W {
					m.probed("connect:" + b.ID)
					return m, nil
				}
			}
			m.probed("connect")
			return m, nil
		}
		var cmd tea.Cmd
		m.connect, cmd = m.connect.HandleMouse(msg.X, msg.Y, msg.Action, msg.Button)
		return m, cmd
	}
	var cmd tea.Cmd
	m.connect, cmd = m.connect.Update(msg)
	return m, cmd
}

// connectTargets are the card's buttons for the hit map.
func (m Model) connectTargets() []hitTarget {
	var out []hitTarget
	for _, b := range m.connect.ButtonRects() {
		out = append(out, hitTarget{ID: "connect:" + b.ID, Label: b.Title, X: b.X, Y: b.Y, W: b.W, H: 1})
	}
	return out
}

// handleMCPConnectMsg routes the wizard's messages. ok=false: not ours.
func (m *Model) handleMCPConnectMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case mcpWizEnvMsg:
		return m.onMCPWizEnv(msg), true
	case mcpWizDeviceMsg:
		return m.onMCPWizDevice(msg), true
	case mcpWizSignInMsg:
		return m.onMCPWizSignIn(msg), true
	case mcpWizDoneMsg:
		return m.onMCPWizDone(msg), true
	case mcpWizTickMsg:
		return m.onMCPWizTick(msg), true
	case connectcard.ButtonMsg:
		return m.onMCPWizButton(msg.ID), true
	case connectcard.SubmitMsg:
		return m.onMCPWizSubmit(msg.ID, msg.Value), true
	case connectcard.KeyMsg:
		return m.onMCPWizKey(msg.Key), true
	case connectcard.CloseMsg:
		m.closeWizard()
		return nil, true
	}
	return nil, false
}

// connectDemo opens one wizard step with sample data for the screenshot
// goldens (devhooks "connectdemo:<step>"): nothing connects, nothing is
// written.
func (m *Model) connectDemo(step string) tea.Cmd {
	s := m.aiS()
	s.mcpGHAccounts = []mcp.GHAccount{{Login: "amin-jalali", Active: true}, {Login: "perkino-deals"}, {Login: "huisstart"}}
	if step == "pick" {
		return m.onMCPWizEnv(mcpWizEnvMsg{accounts: s.mcpGHAccounts})
	}
	name := map[string]string{"gh": "github", "device": "github", "browser": "linear", "ssh": "linear",
		"done": "github", "error": "gitlab", "token": "sentry", "slack": "slack", "url": "linear", "paste": "linear"}[step]
	p, ok := mcp.PresetByName(name)
	if !ok {
		return nil
	}
	w := m.newWizard(p, p.Name)
	w.accounts = s.mcpGHAccounts
	w.plan = []mcp.Method{mcp.MethodCLI, mcp.MethodDevice, mcp.MethodBrowser, mcp.MethodToken}
	now := clock.Now()
	switch step {
	case "gh":
		w.step = wizGH
	case "device":
		w.step, w.method, w.ssh = wizDevice, mcp.MethodDevice, true
		w.link, w.userCode, w.deadline = "https://github.com/login/device", "ABCD-1234", now.Add(14*time.Minute+32*time.Second)
	case "browser":
		w.step, w.method, w.deadline = wizBrowser, mcp.MethodBrowser, now.Add(4*time.Minute+51*time.Second)
		w.link = "https://mcp.linear.app/authorize?client_id=termocode&state=demo"
	case "ssh":
		w.step, w.method, w.ssh, w.port, w.sshHost = wizSSH, mcp.MethodPaste, true, 47391, "myserver"
		w.deadline = now.Add(4*time.Minute + 51*time.Second)
		w.link = "https://mcp.linear.app/authorize?response_type=code&client_id=termocode&redirect_uri=http%3A%2F%2F127.0.0.1%3A47391%2Fcallback&state=demo"
	case "done":
		w.step, w.method = wizDone, mcp.MethodCLI
		w.result = mcp.IdentityResult{Account: "amin-jalali", Server: "github-mcp-server", Tools: 28}
	case "error":
		w.step, w.method = wizError, mcp.MethodBrowser
		w.errText = "This service lets only approved apps sign in. Use a token instead, or ask your admin."
		w.errBtns = []connectcard.Button{{ID: "token", Title: "Use a token instead", Primary: true}, {ID: "cancel", Title: "Cancel"}}
	case "token":
		w.step, w.method = wizToken, mcp.MethodToken
	case "slack":
		w.step, w.method, w.link = wizApp, mcp.MethodApp, p.App.Page
	case "paste":
		w.preset, w.step = mcp.Preset{Name: "pasted", Title: "pasted servers"}, wizPaste
	default:
		return nil
	}
	return m.showWizard()
}

// wizImport adds the servers of a pasted block (Paste a config…): the
// tokens go to credentials.json, the entries to the user mcp.json; a
// name that exists is skipped.
func (m *Model) wizImport(text string) tea.Cmd {
	w := m.wiz()
	list, problems, err := mcp.ParseImport(text)
	if err != nil {
		w.pasteErr = err.Error()
		m.connect = connectcard.New(m.wizCard())
		m.connect.SetSize(m.w, m.h)
		return nil
	}
	cs := mcp.CredStore{Path: mcpCredPath()}
	var added []string
	skipped := problems
	for _, im := range list {
		if len(mcpServerEntries(im.Name)) > 0 {
			skipped = append(skipped, im.Name+": already in mcp.json")
			continue
		}
		ok := true
		for k, v := range im.Secrets {
			ai.RegisterSecret(v)
			if err := cs.SetSecret(k, v); err != nil {
				skipped = append(skipped, im.Name+": "+err.Error())
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if err := mcp.AddServer(mcpUserPath(), im.Name, im.Entry); err != nil {
			skipped = append(skipped, im.Name+": "+ai.Redact(err.Error()))
			continue
		}
		added = append(added, im.Name)
	}
	m.aiMCP().Reload()
	if len(added) == 0 {
		w.pasteErr = "Nothing added. " + mcp.ImportSummary(nil, skipped)
		m.connect = connectcard.New(m.wizCard())
		m.connect.SetSize(m.w, m.h)
		return nil
	}
	w.step, w.errText = wizDone, mcp.ImportSummary(added, skipped)
	return m.showWizard()
}
