package addai

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clilogin"
)

// testLink is a fake sign-in link with a one-time state in it.
const testLink = "https://auth.example.test/oauth/authorize?state=one-time-0000"

// signInCard is the OpenAI sign-in card after its first request (Codex
// installed or not).
func signInCard(t *testing.T, installed bool) (Model, SignInMsg) {
	t.Helper()
	m := New(Options{Installed: map[string]bool{ai.CLICodex: installed}})
	m.SetSize(120, 40)
	pick(t, &m, ai.KindOpenAI)
	out := do(t, &m, key(tea.KeyEnter)) // Sign in with ChatGPT Plus / Pro
	if !installed {
		if out != nil {
			t.Fatalf("the ask card sent %T before a click", out)
		}
		out = press(t, &m, "signin")
	}
	msg, ok := out.(SignInMsg)
	if !ok || msg.Kind != ai.KindOpenAI || msg.CLI != ai.CLICodex || msg.Gen != m.Gen() {
		t.Fatalf("sign-in sent %#v", out)
	}
	return m, msg
}

// Spec: a CLI that is not on this machine gets one card that says what
// happens (install, browser, models) and one button, Install and sign
// in; nothing runs before the click.
func TestSignInAskCard(t *testing.T) {
	m := New(Options{})
	m.SetSize(120, 40)
	pick(t, &m, ai.KindOpenAI)
	if out := do(t, &m, key(tea.KeyEnter)); out != nil {
		t.Fatalf("choosing the sign-in sent %T", out)
	}
	s := text(m)
	for _, w := range []string{"Sign in with ChatGPT Plus / Pro", "Codex", "not on this machine yet",
		"Install Codex", "Sign in with your browser", "Load your models", "Install and sign in"} {
		if !strings.Contains(s, w) {
			t.Errorf("ask card misses %q:\n%s", w, s)
		}
	}
	if got := buttonIDs(m); !reflect.DeepEqual(got, []string{"back", "cancel", "signin"}) {
		t.Errorf("buttons %v", got)
	}
	if m.Busy() {
		t.Error("busy before the click")
	}
	press(t, &m, "back")
	if m.Step() != StepMethod {
		t.Errorf("Back: step %v", m.Step())
	}
}

// Spec: a CLI that is installed starts at once (it may be signed in
// already: then the flow goes straight to the models).
func TestSignInStartsAtOnceWhenInstalled(t *testing.T) {
	m, _ := signInCard(t, true)
	if !m.Busy() || m.Step() != StepSignIn {
		t.Fatalf("busy %v step %v", m.Busy(), m.Step())
	}
	if s := text(m); !strings.Contains(s, "Checking Codex") {
		t.Errorf("card:\n%s", s)
	}
	if got := buttonIDs(m); !reflect.DeepEqual(got, []string{"cancel"}) {
		t.Errorf("buttons %v", got)
	}
}

// Spec: the card follows the flow: Installing with its timer, the
// browser step with Copy the link (the link itself is not drawn when a
// browser opens), the paste-code field, the device code with its link
// over SSH, then Loading your models; Cancel is there all along.
func TestSignInProgress(t *testing.T) {
	m, msg := signInCard(t, false)
	if !m.Progress(msg.Gen, clilogin.State{Phase: clilogin.PhaseInstalling, Elapsed: 12 * time.Second}) {
		t.Fatal("Installing dropped")
	}
	s := text(m)
	if !strings.Contains(s, "Installing Codex") || !strings.Contains(s, "0:12") {
		t.Errorf("install card:\n%s", s)
	}
	if got := buttonIDs(m); !reflect.DeepEqual(got, []string{"cancel"}) {
		t.Errorf("install buttons %v", got)
	}

	m.Progress(msg.Gen, clilogin.State{Phase: clilogin.PhaseWaitingForBrowser})
	if got := buttonIDs(m); !reflect.DeepEqual(got, []string{"cancel"}) {
		t.Errorf("no link yet: buttons %v", got)
	}
	m.Progress(msg.Gen, clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: testLink})
	s = text(m)
	for _, w := range []string{"Installed Codex", "Sign in with your browser", "Your browser opened", "Browser did not open"} {
		if !strings.Contains(s, w) {
			t.Errorf("browser card misses %q:\n%s", w, s)
		}
	}
	if strings.Contains(s, "one-time") {
		t.Errorf("the link is drawn while a browser opens it:\n%s", s)
	}
	if got := buttonIDs(m); !reflect.DeepEqual(got, []string{"cancel", "copy"}) {
		t.Errorf("browser buttons %v", got)
	}
	if out, ok := press(t, &m, "copy").(CopyLinkMsg); !ok || out.Gen != msg.Gen {
		t.Errorf("Copy the link sent %#v", out)
	}
	// Enter copies the link; it never cancels the sign-in by accident.
	if out, ok := do(t, &m, key(tea.KeyEnter)).(CopyLinkMsg); !ok {
		t.Errorf("Enter on the browser step sent %#v", out)
	}
	if m.SignInURL() != testLink {
		t.Errorf("SignInURL %q", m.SignInURL())
	}

	m.Progress(msg.Gen, clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: testLink, NeedsCode: true})
	if s := text(m); !strings.Contains(s, "Paste the code") {
		t.Errorf("code card:\n%s", s)
	}
	if got := buttonIDs(m); !reflect.DeepEqual(got, []string{"cancel", "copy", "sendcode"}) {
		t.Errorf("code buttons %v", got)
	}
	if out := press(t, &m, "sendcode"); out != nil {
		t.Errorf("an empty code sent %T", out)
	}
	m.SetValue("code", " pasted-code-1 ")
	out, ok := do(t, &m, key(tea.KeyEnter)).(SendCodeMsg)
	if !ok || out.Code != "pasted-code-1" || out.Gen != msg.Gen {
		t.Fatalf("Enter in the code field sent %#v", out)
	}
	if m.Value("code") != "" || !m.Busy() {
		t.Errorf("after send: field %q busy %v", m.Value("code"), m.Busy())
	}

	m.Progress(msg.Gen, clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: testLink, Code: "ABCD-1234"})
	s = text(m)
	if !strings.Contains(s, "ABCD-1234") || !strings.Contains(s, "auth.example.test") || !strings.Contains(s, "any device") {
		t.Errorf("device card (SSH):\n%s", s)
	}

	m.Progress(msg.Gen, clilogin.State{Phase: clilogin.PhaseLoadingModels})
	s = text(m)
	if !strings.Contains(s, "Loading your models") || !strings.Contains(s, "Signed in") {
		t.Errorf("models card:\n%s", s)
	}
	if !m.Finish(msg.Gen, []string{"gpt-5-codex"}) || m.Step() != StepDone {
		t.Fatalf("Finish: step %v", m.Step())
	}
	if s := text(m); !strings.Contains(s, "OpenAI is ready") || !strings.Contains(s, "ChatGPT Plus / Pro") {
		t.Errorf("done card:\n%s", s)
	}
}

// Spec: an installed CLI never shows an install timer: its line is a
// plain check mark.
func TestSignInSkipsInstall(t *testing.T) {
	m, msg := signInCard(t, true)
	m.Progress(msg.Gen, clilogin.State{Phase: clilogin.PhaseLoadingModels})
	s := text(m)
	if strings.Contains(s, "Installing") || !strings.Contains(s, "Codex is on this machine") {
		t.Errorf("card:\n%s", s)
	}
}

// Spec: no Node.js is its own card (Codex needs Node.js) with Use an API
// key and Try again; another failure is one sentence with the same way
// out. Try again starts a new flow.
func TestSignInFailures(t *testing.T) {
	m, msg := signInCard(t, false)
	m.Progress(msg.Gen, clilogin.State{Phase: clilogin.PhaseInstalling})
	m.Progress(msg.Gen, clilogin.State{Phase: clilogin.PhaseFailed,
		Err: &clilogin.Error{Kind: clilogin.FailNoNode, Msg: "Codex needs Node.js. Install Node.js, then press Try again."}})
	s := text(m)
	if !strings.Contains(s, "Codex needs Node.js") || !strings.Contains(s, "Node.js is not on this machine") {
		t.Errorf("no-node card:\n%s", s)
	}
	if got := buttonIDs(m); !reflect.DeepEqual(got, []string{"cancel", "apikey", "retry"}) {
		t.Errorf("buttons %v", got)
	}
	if m.Busy() {
		t.Error("still busy after the failure")
	}
	out, ok := press(t, &m, "retry").(SignInMsg)
	if !ok || out.Gen == msg.Gen || !m.Busy() {
		t.Fatalf("Try again sent %#v busy %v", out, m.Busy())
	}
	m.Progress(out.Gen, clilogin.State{Phase: clilogin.PhaseFailed, Err: &clilogin.Error{Kind: clilogin.FailTimeout, Msg: "Sign-in took too long. Try again."}})
	if s := text(m); !strings.Contains(s, "Sign-in took too long.") || strings.Contains(s, "needs Node.js") {
		t.Errorf("timeout card:\n%s", s)
	}
	press(t, &m, "apikey")
	if m.Step() != StepKey {
		t.Errorf("Use an API key: step %v", m.Step())
	}
}

// Spec: after Esc, Back or Try again the old flow's states are dropped.
func TestSignInStaleStates(t *testing.T) {
	m, msg := signInCard(t, true)
	if _, ok := do(t, &m, key(tea.KeyEsc)).(CancelMsg); !ok {
		t.Fatal("Esc does not cancel")
	}
	if m.Progress(msg.Gen, clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: testLink}) {
		t.Error("a state after Esc was taken")
	}
	if m.Finish(msg.Gen, []string{"x"}) {
		t.Error("a Ready after Esc was taken")
	}
}
