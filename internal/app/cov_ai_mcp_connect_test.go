package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/connectcard"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/auth"
)

// The Connect a Service wizard (ai_mcp_connect.go): picker hints, each
// step's card, the error screens with one fix each, the typed inputs
// and the pasted configs. Nothing here talks to a network.

func covWizModel(t *testing.T) *Model {
	t.Helper()
	m := aiTestModel(t)
	t.Setenv("SSH_CONNECTION", "")
	t.Setenv("SSH_TTY", "")
	return &m
}

// covStubOpen records browser opens (and can fail them).
func covStubOpen(t *testing.T, fail bool) *[]string {
	t.Helper()
	var mu sync.Mutex
	var opened []string
	old := osOpen
	osOpen = func(u string) error {
		mu.Lock()
		defer mu.Unlock()
		opened = append(opened, u)
		if fail {
			return errors.New("no browser")
		}
		return nil
	}
	t.Cleanup(func() { osOpen = old })
	return &opened
}

func covButtonIDs(c connectcard.Card) string {
	var ids []string
	for _, b := range c.Buttons {
		ids = append(ids, b.ID)
	}
	return strings.Join(ids, ",")
}

// The picker hints say what the next step will be, and "connected as"
// once a service is set up.
func TestCovAIMCPConnectHints(t *testing.T) {
	m := covWizModel(t)
	hint := func(name string) string {
		p, _ := mcp.PresetByName(name)
		return m.mcpConnectHint(p)
	}
	if got := hint("linear"); got != "sign in in the browser" {
		t.Errorf("linear = %q", got)
	}
	if got := hint("slack"); got != "needs a Slack app (2 min)" {
		t.Errorf("slack = %q", got)
	}
	m.aiS().mcpGHAccounts = []mcp.GHAccount{{Login: "amin", Active: true}}
	if got := hint("github"); got != "gh is signed in" {
		t.Errorf("github with gh = %q", got)
	}
	m.aiS().mcpGHAccounts = nil
	t.Setenv("SSH_TTY", "/dev/pts/1")
	if got := hint("linear"); got != "paste the address (SSH)" {
		t.Errorf("linear over ssh = %q", got)
	}
	t.Setenv("SSH_TTY", "")
	writeUserMCP(t, `{"mcpServers":{"gitlab":{"type":"http","url":"https://gitlab.com/api/v4/mcp","oauth":{"clientId":"app-123"}},`+
		`"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/","auth":{"gh":"octo"}},`+
		`"linear":{"type":"http","url":"https://mcp.linear.app/mcp"},`+
		`"sentry":{"type":"http","url":"https://mcp.sentry.dev/mcp"}}}`)
	m.aiMCP().Reload()
	if got := hint("github"); got != "connected as octo" {
		t.Errorf("github with an entry = %q", got)
	}
	if got := hint("gitlab"); got != "sign in in the browser" {
		t.Errorf("gitlab with an app id = %q", got)
	}
	cs := mcp.CredStore{Path: mcpCredPath()}
	if err := cs.SetAccount("linear", "amin@x"); err != nil {
		t.Fatal(err)
	}
	if got := hint("linear"); got != "connected as amin@x" {
		t.Errorf("linear with an account = %q", got)
	}
	if err := cs.Save("sentry", ai.MCPOAuth{Issuer: "https://sentry.example", ClientID: "c"}); err != nil {
		t.Fatal(err)
	}
	if got := hint("sentry"); got != "connected" {
		t.Errorf("sentry with a sign-in = %q", got)
	}
	if got := hostOf(nil, "::bad url"); got != "::bad url" {
		t.Errorf("hostOf fallback = %q", got)
	}
	if got := hostOf(&mcp.DeviceApp{DeviceEndpoint: "https://github.com/login/device/code"}, ""); got != "github.com" {
		t.Errorf("hostOf device = %q", got)
	}
}

// The palette entry asks gh first, then opens the picker; a preset that
// no longer exists starts nothing; a locked store waits for the unlock.
func TestCovAIMCPConnectEnvAndPicker(t *testing.T) {
	m := covWizModel(t)
	if cmd := m.onMCPWizEnv(mcpWizEnvMsg{then: "no-such-service"}); cmd != nil || m.wiz() != nil {
		t.Error("an unknown preset started a wizard")
	}
	m.onMCPWizEnv(mcpWizEnvMsg{accounts: []mcp.GHAccount{{Login: "amin"}}})
	if !m.pickerOpen || m.pickerKind != pickerKindMCPConnect {
		t.Fatal("the services picker did not open")
	}
	if len(m.aiS().mcpGHAccounts) != 1 {
		t.Error("gh accounts are not kept")
	}
	if m.onMCPConnectPicked("no-such-service") != nil || m.wiz() != nil {
		t.Error("an unknown row started a wizard")
	}
	m.onMCPConnectPicked(mcpConnectURLID)
	if w := m.wiz(); w == nil || w.step != wizURL || !m.connectOpen {
		t.Fatal("Custom URL… did not open its step")
	}
	if got := m.wizCard().Title; got != "Connect a custom server" {
		t.Errorf("url card = %q", got)
	}
	m.onMCPWizEnv(mcpWizEnvMsg{then: "linear", name: "linear-2", another: true})
	if w := m.wiz(); w == nil || !w.another || w.name != "linear-2" {
		t.Errorf("another account: %+v", m.wiz())
	}
	m.closeWizard()

	m.secretsS().busy = true
	if cmd := m.mcpConnectServer("linear"); cmd != nil || m.wiz() != nil {
		t.Error("a sign-in started while the store unlocks")
	}
	m.secretsS().busy = false
	if cmd := m.mcpConnectServer("github"); cmd == nil {
		t.Error("github sign-in asks gh first")
	}
	writeUserMCP(t, `{"mcpServers":{"mine":{"type":"http","url":"https://mcp.mine.example/mcp"}}}`)
	m.aiMCP().Reload()
	covStubOpen(t, false)
	m.mcpConnectServer("mine")
	if w := m.wiz(); w == nil || w.preset.URL != "https://mcp.mine.example/mcp" || w.step != wizBusy {
		t.Errorf("own server sign-in: %+v", m.wiz())
	}
	m.closeWizard()
}

// A service with no way on this machine says so with one fix.
func TestCovAIMCPConnectNoWay(t *testing.T) {
	m := covWizModel(t)
	m.newWizard(mcp.Preset{Name: "github", Title: "Locked", NoBrowser: true, Device: &mcp.DeviceApp{}}, "locked")
	m.wizStart()
	w := m.wiz()
	if w.step != wizError || !strings.Contains(w.errText, "no way to connect") {
		t.Errorf("no way: step %v %q", w.step, w.errText)
	}
	if !strings.Contains(w.note, "not set up in this build") {
		t.Errorf("note = %q", w.note)
	}
	if got := covButtonIDs(m.wizCard()); got != "retry,cancel" {
		t.Errorf("buttons = %q", got)
	}
	// wizFail with no wizard is a toast, not a crash.
	m.closeWizard()
	if m.wizFail(errors.New("x")) == nil {
		t.Error("no toast without a wizard")
	}
}

// Each error gets its own text and buttons; Cancel is always there.
func TestCovAIMCPConnectErrorScreens(t *testing.T) {
	m := covWizModel(t)
	gh, _ := mcp.PresetByName("github")
	for _, c := range []struct {
		method mcp.Method
		err    error
		text   string
		btns   string
	}{
		{mcp.MethodDevice, auth.ErrDeviceExpired, "The code expired", "retry,token,cancel"},
		{mcp.MethodDevice, auth.ErrDeviceDenied, "cancelled on the web page", "retry,cancel"},
		{mcp.MethodCLI, mcp.ErrGHNotLoggedIn, "gh is installed but not signed in", "device,token,cancel"},
		{mcp.MethodBrowser, errors.New("this server allows only approved clients"), "only approved apps", "token,cancel"},
		{mcp.MethodBrowser, errors.New("oauth: state does not match"), "another sign-in", "retry,cancel"},
		{mcp.MethodBrowser, errors.New("sign-in timed out"), "No answer from the browser", "retry,cancel"},
		{mcp.MethodToken, errors.New("401 Unauthorized"), "refused the token (401). Check that it has repo, read:org.", "edit-token,cancel"},
		{mcp.MethodCLI, errors.New("not authorized"), "refused the gh token", "token,cancel"},
		{mcp.MethodBrowser, errors.New("dial tcp: refused"), "Could not connect GitHub: dial tcp: refused", "retry,token,cancel"},
	} {
		w := m.newWizard(gh, "github")
		w.method = c.method
		m.wizFail(c.err)
		card := m.wizCard()
		if !strings.Contains(wizText(card), c.text) || covButtonIDs(card) != c.btns {
			t.Errorf("%v: %q buttons %s", c.err, wizText(card), covButtonIDs(card))
		}
	}
	// A token error for a service without scopes says only the 401.
	w := m.newWizard(mcp.Preset{Name: "x", Title: "X"}, "x")
	w.method = mcp.MethodToken
	m.wizFail(errors.New("not authorized"))
	if got := w.errText; got != "X refused the token (401)." {
		t.Errorf("no scopes = %q", got)
	}
}

// The step cards: the device code over SSH and with no browser, the SSH
// paste with an error, the e-mail step, the custom URL and paste errors.
func TestCovAIMCPConnectStepCards(t *testing.T) {
	m := covWizModel(t)
	gh, _ := mcp.PresetByName("github")
	w := m.newWizard(gh, "github")
	w.plan = []mcp.Method{mcp.MethodDevice, mcp.MethodToken}
	w.step, w.method, w.openErr, w.link, w.userCode = wizDevice, mcp.MethodDevice, true, "https://github.com/login/device", "ABCD-1234"
	covMustCard(t, m, "(o: open it)", "ABCD-1234")
	if got := covButtonIDs(m.wizCard()); got != "token,cancel" {
		t.Errorf("device buttons = %q", got)
	}
	w.ssh = true
	covMustCard(t, m, "(on your laptop or phone)", "Over SSH")

	w.step, w.method, w.pasteErr, w.port, w.sshHost = wizSSH, mcp.MethodPaste, "That is not the whole address", 47391, "box"
	covMustCard(t, m, "That is not the whole address", "ssh -L 47391:127.0.0.1:47391 box")

	w.step, w.openErr = wizBrowser, true
	covMustCard(t, m, "Could not open a browser")

	at, _ := mcp.PresetByName("atlassian")
	w = m.newWizard(at, "atlassian")
	m.wizGo(mcp.MethodToken)
	if w.step != wizEmail {
		t.Fatalf("atlassian token starts at %v, want the e-mail", w.step)
	}
	covMustCard(t, m, "go with the e-mail of your account")
	w.step = wizToken
	if strings.Contains(wizText(m.wizCard()), "Accepts:") {
		t.Error("an e-mail token card offers references")
	}

	w = m.newWizard(mcp.Preset{Name: "custom"}, "")
	w.step, w.pasteErr = wizURL, "Use an https:// address."
	covMustCard(t, m, "Use an https:// address.")
	w.step, w.pasteErr = wizPaste, "Nothing added."
	covMustCard(t, m, "Nothing added.")
	w.step = mcpWizStep(99)
	if c := m.wizCard(); len(c.Rows) != 0 || c.Title == "" {
		t.Errorf("unknown step card = %+v", c)
	}
}

func covMustCard(t *testing.T, m *Model, wants ...string) {
	t.Helper()
	covAIMustContain(t, "card", wizText(m.wizCard()), wants...)
}

// Buttons: nil wizard closes the card; gh without accounts fails with
// the gh fix; retry with no method plans again; submit sends the input.
func TestCovAIMCPConnectButtons(t *testing.T) {
	m := covWizModel(t)
	m.connectOpen = true
	if m.onMCPWizButton("done") != nil || m.connectOpen {
		t.Error("a button without a wizard keeps the card")
	}
	gh, _ := mcp.PresetByName("github")
	w := m.newWizard(gh, "github")
	m.onMCPWizButton("gh")
	if w.step != wizError || !strings.Contains(w.errText, "gh is installed but not signed in") {
		t.Errorf("gh without accounts: %v %q", w.step, w.errText)
	}
	w = m.newWizard(mcp.Preset{Name: "github", Title: "Locked", NoBrowser: true}, "github")
	m.onMCPWizButton("retry")
	if w.step != wizError {
		t.Errorf("retry with no method did not plan again: %v", w.step)
	}
	if m.onMCPWizButton("nope") != nil {
		t.Error("unknown button did something")
	}

	// Submit takes the input of the card: an https address.
	covStubOpen(t, false)
	w = m.newWizard(mcp.Preset{Name: "custom", Title: "a custom server"}, "")
	w.step = wizURL
	m.showWizard()
	m.connect.SetValue("https://MCP.Example.com/mcp")
	m.onMCPWizButton("submit")
	if w.preset.Name != "mcp-example-com" || w.preset.URL != "https://MCP.Example.com/mcp" || w.step != wizBusy {
		t.Errorf("url submit: %+v step %v", w.preset, w.step)
	}
	m.closeWizard()

	// Show servers, Open mcp.json: the card closes first.
	for _, id := range []string{"servers", "edit-json"} {
		m.newWizard(mcp.Preset{Name: "pasted"}, "")
		m.connectOpen = true
		m.onMCPWizButton(id)
		if m.wiz() != nil || m.connectOpen {
			t.Errorf("%s left the wizard open", id)
		}
	}
}

// Typed inputs: empty and bad values are ignored or explained; a good
// e-mail moves on; a Slack client id plans the browser sign-in.
func TestCovAIMCPConnectSubmit(t *testing.T) {
	m := covWizModel(t)
	if m.onMCPWizSubmit("url", "x") != nil {
		t.Error("submit without a wizard")
	}
	at, _ := mcp.PresetByName("atlassian")
	w := m.newWizard(at, "atlassian")
	w.step = wizEmail
	m.onMCPWizSubmit("email", "   ")
	m.onMCPWizSubmit("email", "not-an-email")
	if w.step != wizEmail {
		t.Fatal("a bad e-mail moved on")
	}
	m.onMCPWizSubmit("email", "amin@example.com")
	if w.step != wizToken || w.email != "amin@example.com" {
		t.Errorf("good e-mail: step %v %q", w.step, w.email)
	}
	m.onMCPWizSubmit("token", "tok-cov-atl-123456789")
	if w.step != wizBusy {
		t.Errorf("token did not start the test call: %v", w.step)
	}
	if b, _ := os.ReadFile(mcpUserPath()); !strings.Contains(string(b), "${secret:atlassian_token}") || strings.Contains(string(b), "tok-cov-atl") {
		t.Errorf("mcp.json after the token:\n%s", b)
	}

	// The SSH paste with no flow does nothing.
	w.flow = nil
	if m.onMCPWizSubmit("addr", "http://127.0.0.1:1/callback?code=x") != nil {
		t.Error("addr without a flow")
	}

	covStubOpen(t, false)
	sl, _ := mcp.PresetByName("slack")
	w = m.newWizard(sl, "slack")
	m.wizStart()
	if w.step != wizApp {
		t.Fatalf("slack starts at %v", w.step)
	}
	m.onMCPWizSubmit("client", "bad id with spaces")
	if w.step != wizApp {
		t.Error("a bad client id moved on")
	}
	m.onMCPWizSubmit("client", "1234.5678")
	if w.env.AppClientID != "1234.5678" || w.method != mcp.MethodBrowser || w.step != wizBusy {
		t.Errorf("client id: %+v method %v step %v", w.env, w.method, w.step)
	}
	m.closeWizard()

	w = m.newWizard(mcp.Preset{Name: "custom"}, "")
	w.step = wizURL
	for _, bad := range []string{"http://example.com/mcp", "ftp://x", "https://"} {
		m.onMCPWizSubmit("url", bad)
		if w.pasteErr != "Use an https:// address." {
			t.Errorf("%s: %q", bad, w.pasteErr)
		}
	}
	m.onMCPWizSubmit("url", "http://127.0.0.1:9/mcp")
	if w.preset.Name != "127-0-0-1" {
		t.Errorf("loopback http: %+v", w.preset)
	}
	m.closeWizard()
	if m.onMCPWizSubmit("other", "x") != nil {
		t.Error("unknown input")
	}
}

// Custom server names: lower case, only letters and digits, at most 30
// and unique in mcp.json.
func TestCovAIMCPCustomName(t *testing.T) {
	covWizModel(t)
	if got := mcpCustomName("..."); got != "server" {
		t.Errorf("empty = %q", got)
	}
	if got := mcpCustomName(strings.Repeat("a", 40) + ".com"); len(got) != 30 {
		t.Errorf("long = %q", got)
	}
	writeUserMCP(t, `{"mcpServers":{"mcp-x-io":{"type":"http","url":"https://mcp.x.io"},"mcp-x-io-2":{"type":"http","url":"https://mcp.x.io/2"}}}`)
	if got := mcpCustomName("mcp.x.io"); got != "mcp-x-io-3" {
		t.Errorf("taken = %q", got)
	}
	if !isLoopbackHost("localhost") || !isLoopbackHost("::1") || isLoopbackHost("example.com") {
		t.Error("isLoopbackHost")
	}
}

// Letter keys: c copies the code / manifest / link, o opens the page;
// a browser that does not open says so on the card.
func TestCovAIMCPConnectKeys(t *testing.T) {
	m := covWizModel(t)
	var copied []string
	sink := func(s string) { copied = append(copied, s) }
	clipboardSink.Store(&sink)
	t.Cleanup(func() { clipboardSink.Store(nil) })
	if m.onMCPWizKey("c") != nil {
		t.Error("a key without a wizard")
	}
	gh, _ := mcp.PresetByName("github")
	w := m.newWizard(gh, "github")
	w.step, w.userCode, w.link = wizDevice, "ABCD-1234", "https://github.com/login/device"
	if m.onMCPWizKey("c") == nil || copied[len(copied)-1] != "ABCD-1234" {
		t.Errorf("c on the device step copied %q", copied)
	}
	w.step = wizBrowser
	m.onMCPWizKey("c")
	if copied[len(copied)-1] != "https://github.com/login/device" {
		t.Errorf("c on the browser step copied %q", copied)
	}
	sl, _ := mcp.PresetByName("slack")
	w.preset, w.step = sl, wizApp
	m.onMCPWizKey("c")
	if !strings.Contains(copied[len(copied)-1], "display_information") && !strings.Contains(copied[len(copied)-1], "oauth") {
		t.Errorf("c on the Slack step copied %.80q", copied[len(copied)-1])
	}
	opened := covStubOpen(t, true)
	w.preset, w.step = gh, wizBrowser
	m.showWizard()
	if m.onMCPWizKey("o") == nil || !w.openErr || len(*opened) != 1 {
		t.Errorf("o with no browser: openErr %v opened %v", w.openErr, *opened)
	}
	if !strings.Contains(wizText(m.connect.Card()), "Could not open a browser") {
		t.Error("the card does not say the browser did not open")
	}
	if m.onMCPWizKey("x") != nil {
		t.Error("unknown key")
	}
	if cmd, ok := m.handleMCPConnectMsg(connectcard.KeyMsg{Key: "x"}); !ok || cmd != nil {
		t.Error("the card's key message is not routed")
	}
}

// Mouse on the card: the probe names the button under the pointer; the
// hit map lists the buttons.
func TestCovAIMCPConnectMouseProbe(t *testing.T) {
	m := covWizModel(t)
	gh, _ := mcp.PresetByName("github")
	w := m.newWizard(gh, "github")
	w.step, w.errText, w.errBtns = wizError, "x", []connectcard.Button{{ID: "retry", Title: "Try again"}, {ID: "cancel", Title: "Cancel"}}
	m.showWizard()
	m.View() // lays the card out
	targets := m.connectTargets()
	if len(targets) != 2 || targets[0].ID != "connect:retry" {
		t.Fatalf("targets = %+v", targets)
	}
	var probe string
	m.mouseProbe = &probe
	b := targets[1]
	m.routeToConnect(tea.MouseMsg{X: b.X, Y: b.Y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if probe != "connect:cancel" {
		t.Errorf("probe on Cancel = %q", probe)
	}
	m.routeToConnect(tea.MouseMsg{X: 0, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if probe != "connect" {
		t.Errorf("probe off the buttons = %q", probe)
	}
}

// Stale messages of an old wizard are dropped; a device error fails the
// step; a sign-in error fails it; a tick after Esc does nothing.
func TestCovAIMCPConnectStaleMessages(t *testing.T) {
	m := covWizModel(t)
	if m.onMCPWizDevice(mcpWizDeviceMsg{gen: 1}) != nil || m.onMCPWizSignIn(mcpWizSignInMsg{gen: 1}) != nil {
		t.Error("messages without a wizard")
	}
	gh, _ := mcp.PresetByName("github")
	w := m.newWizard(gh, "github")
	if m.onMCPWizDevice(mcpWizDeviceMsg{gen: w.gen + 1}) != nil {
		t.Error("stale device message")
	}
	m.onMCPWizDevice(mcpWizDeviceMsg{gen: w.gen, err: errors.New("device endpoint down")})
	if w.step != wizError || !strings.Contains(w.errText, "device endpoint down") {
		t.Errorf("device error: %v %q", w.step, w.errText)
	}
	m.onMCPWizSignIn(mcpWizSignInMsg{gen: w.gen, err: errors.New("server says no")})
	if !strings.Contains(w.errText, "server says no") {
		t.Errorf("sign-in error: %q", w.errText)
	}
	m.onMCPWizSignIn(mcpWizSignInMsg{gen: w.gen, connected: true})
	if w.step != wizBusy || !strings.Contains(w.busy, "Testing the connection") {
		t.Errorf("connected without sign-in: %v %q", w.step, w.busy)
	}
	m.connectOpen = false
	w.step = wizDevice
	if m.onMCPWizTick(mcpWizTickMsg{gen: w.gen}) != nil {
		t.Error("a tick after the card closed")
	}
	m.connectOpen = true
	if m.onMCPWizTick(mcpWizTickMsg{gen: w.gen}) == nil {
		t.Error("a tick while waiting does not tick again")
	}
	w.step = wizDone
	if m.onMCPWizTick(mcpWizTickMsg{gen: w.gen}) != nil {
		t.Error("a tick on Done")
	}
	// Esc while waiting: the closed sign-in says nothing.
	w.step = wizBusy
	if m.onMCPWizDone(mcpWizDoneMsg{gen: w.gen, err: context.Canceled}) != nil || w.step != wizBusy {
		t.Error("a cancelled sign-in showed an error")
	}
}

// Another account of a service: the entry is renamed after the account
// once the test call knows it.
func TestCovAIMCPConnectAnotherAccountRename(t *testing.T) {
	m := covWizModel(t)
	writeUserMCP(t, `{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/"},"github-2":{"type":"http","url":"https://api.githubcopilot.com/mcp/"}}}`)
	m.aiMCP().Reload()
	gh, _ := mcp.PresetByName("github")
	w := m.newWizard(gh, "github-2")
	w.another = true
	m.setup = &setupState{}
	m.onMCPWizDone(mcpWizDoneMsg{gen: w.gen, res: mcp.IdentityResult{Account: "perkino", Tools: 4}})
	if w.name != "github-perkino" || w.step != wizDone {
		t.Errorf("renamed to %q step %v", w.name, w.step)
	}
	if len(mcpServerEntries("github-perkino")) != 1 || len(mcpServerEntries("github-2")) != 0 {
		t.Error("mcp.json was not renamed")
	}
	if m.setup.services != -1 {
		t.Error("the setup card does not count again")
	}
}

// Paste a config: a bad block explains itself; a name already in
// mcp.json is skipped; nothing new says so; new servers are added and
// their tokens go to credentials.json.
func TestCovAIMCPConnectImport(t *testing.T) {
	m := covWizModel(t)
	writeUserMCP(t, `{"mcpServers":{"have":{"type":"http","url":"https://have.example/mcp"}}}`)
	m.aiMCP().Reload()
	w := m.newWizard(mcp.Preset{Name: "pasted", Title: "pasted servers"}, "")
	w.step = wizPaste
	m.onMCPWizSubmit("config", "{not json")
	if w.pasteErr == "" || w.step != wizPaste {
		t.Fatalf("bad block: %q", w.pasteErr)
	}
	m.onMCPWizSubmit("config", `{"mcpServers":{"have":{"type":"http","url":"https://have.example/mcp"}}}`)
	if !strings.HasPrefix(w.pasteErr, "Nothing added.") || !strings.Contains(w.pasteErr, "have") {
		t.Errorf("only known servers: %q", w.pasteErr)
	}
	m.onMCPWizSubmit("config", `{"mcpServers":{"have":{"type":"http","url":"https://have.example/mcp"},`+
		`"fresh":{"type":"http","url":"https://fresh.example/mcp","headers":{"Authorization":"Bearer tok-cov-fresh-1234567890"}}}}`)
	if w.step != wizDone || !strings.Contains(w.errText, "fresh") {
		t.Fatalf("import: step %v %q", w.step, w.errText)
	}
	if got := covButtonIDs(m.wizCard()); got != "servers,done" {
		t.Errorf("added buttons = %q", got)
	}
	b, _ := os.ReadFile(mcpUserPath())
	if strings.Contains(string(b), "tok-cov-fresh") || !strings.Contains(string(b), "fresh") {
		t.Errorf("mcp.json:\n%s", b)
	}
	c, _ := os.ReadFile(mcpCredPath())
	if !strings.Contains(string(c), "tok-cov-fresh") {
		t.Error("the token did not move to credentials.json")
	}
}

// Writing the entry fails when mcp.json cannot be written: the device
// and browser steps show the error instead of waiting.
func TestCovAIMCPConnectEntryWriteFails(t *testing.T) {
	m := covWizModel(t)
	p := mcpUserPath()
	if err := os.MkdirAll(filepath.Join(p, "blocker"), 0o755); err != nil { // mcp.json is a folder
		t.Fatal(err)
	}
	ln, _ := mcp.PresetByName("linear")
	w := m.newWizard(ln, "linear")
	m.wizGo(mcp.MethodBrowser)
	if w.step != wizError {
		t.Errorf("browser step with a broken mcp.json: %v", w.step)
	}
	gl, _ := mcp.PresetByName("gitlab")
	w = m.newWizard(gl, "gitlab")
	m.wizGo(mcp.MethodDevice)
	if w.step != wizError {
		t.Errorf("device step with a broken mcp.json: %v", w.step)
	}
	w.step = wizToken
	m.onMCPWizSubmit("token", "tok-cov-gl-1234567890")
	if w.step != wizError {
		t.Errorf("token with a broken mcp.json: %v", w.step)
	}
}
