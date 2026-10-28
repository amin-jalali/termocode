package app

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/addai"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clilogin"
	"github.com/amin-jalali/termocode/internal/connectcard"
)

// addAILink is a fake sign-in link with a one-time state in it.
const addAILink = "https://auth.example.test/oauth/authorize?state=one-time-5555"

// fakeSignIn stands in for clilogin.Run: the test sends the states, and
// it records the config, the pasted codes and whether ctx ended.
type fakeSignIn struct {
	mu    sync.Mutex
	cfgs  []clilogin.Config
	flows []*fakeSignInFlow
}

type fakeSignInFlow struct {
	ch    chan clilogin.State
	ctx   context.Context
	mu    sync.Mutex
	codes []string
}

func (f *fakeSignInFlow) Updates() <-chan clilogin.State { return f.ch }
func (f *fakeSignInFlow) Write(code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes = append(f.codes, code)
	return nil
}
func (f *fakeSignInFlow) Cancel() {}

func (f *fakeSignInFlow) canceled() bool { return f.ctx.Err() != nil }

// installFakeSignIn replaces the sign-in path and the installed check.
func installFakeSignIn(t *testing.T, installed bool) *fakeSignIn {
	t.Helper()
	fk := &fakeSignIn{}
	oldRun, oldInst := aiCLISignIn, aiCLIInstalled
	aiCLISignIn = func(ctx context.Context, cfg clilogin.Config) addAISignInFlow {
		fk.mu.Lock()
		defer fk.mu.Unlock()
		f := &fakeSignInFlow{ch: make(chan clilogin.State, 8), ctx: ctx}
		fk.cfgs = append(fk.cfgs, cfg)
		fk.flows = append(fk.flows, f)
		return f
	}
	aiCLIInstalled = func(string) bool { return installed }
	t.Cleanup(func() { aiCLISignIn, aiCLIInstalled = oldRun, oldInst })
	return fk
}

func (fk *fakeSignIn) last(t *testing.T) *fakeSignInFlow {
	t.Helper()
	fk.mu.Lock()
	defer fk.mu.Unlock()
	if len(fk.flows) == 0 {
		t.Fatal("no sign-in started")
	}
	return fk.flows[len(fk.flows)-1]
}

// signInStart opens the card on OpenAI's sign-in and starts it; it
// returns the command that waits for the flow's next state.
func signInStart(t *testing.T, m *Model, ask bool) tea.Cmd {
	t.Helper()
	addAIPick(t, m, ai.KindOpenAI)
	nm, cmd := m.routeToAddAI(tea.KeyMsg{Type: tea.KeyEnter}) // Sign in with ChatGPT Plus / Pro
	*m = nm
	if ask {
		if cmd != nil {
			t.Fatalf("the ask card sent %T before a click", cmd())
		}
		if !strings.Contains(addAICardText(m), "Install and sign in") {
			t.Fatalf("ask card:\n%s", addAICardText(m))
		}
		nm, cmd = m.routeToAddAI(connectcard.ButtonMsg{ID: "signin"})
		*m = nm
	}
	if cmd == nil {
		t.Fatal("no sign-in request")
	}
	next, ok := m.handleAddAIMsg(cmd())
	if !ok || next == nil {
		t.Fatal("the sign-in request did not start a flow")
	}
	return next
}

// signInFeed sends one state and runs what the app does with it; it returns
// the next wait (nil: the flow is over for the app).
func signInFeed(t *testing.T, m *Model, f *fakeSignInFlow, wait tea.Cmd, st clilogin.State) tea.Cmd {
	t.Helper()
	if wait == nil {
		t.Fatal("the app stopped waiting")
	}
	f.ch <- st
	msg := wait()
	if msg == nil {
		return nil
	}
	next, ok := m.handleAddAIMsg(msg)
	if !ok {
		t.Fatalf("%T is not handled", msg)
	}
	return next
}

// Spec: Install and sign in runs the whole path in one card: the
// install timer, the browser step (Copy the link goes to the clipboard;
// a pasted code goes to the CLI), the models; on Ready the provider is
// saved (method cli, the CLI) with the CLI's built-in models.
func TestAddAISignInFlow(t *testing.T) {
	m := addAITestModel(t)
	fk := installFakeSignIn(t, false)
	var copied string
	defer SetClipboardSink(func(s string) { copied = s })()

	wait := signInStart(t, m, true)
	if c := fk.cfgs[0]; c.CLI != ai.CLICodex {
		t.Errorf("config %+v", c)
	}
	f := fk.last(t)
	wait = signInFeed(t, m, f, wait, clilogin.State{Phase: clilogin.PhaseInstalling, Elapsed: 12 * time.Second})
	if s := addAICardText(m); !strings.Contains(s, "Installing Codex") || !strings.Contains(s, "0:12") {
		t.Fatalf("install card:\n%s", s)
	}
	wait = signInFeed(t, m, f, wait, clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: addAILink})
	addAIOnce(t, m, connectcard.ButtonMsg{ID: "copy"})
	if copied != addAILink {
		t.Errorf("Copy the link copied %q", copied)
	}
	wait = signInFeed(t, m, f, wait, clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: addAILink, NeedsCode: true})
	m.addAI.SetValue("code", "pasted-code-7")
	addAIOnce(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if len(f.codes) != 1 || f.codes[0] != "pasted-code-7" {
		t.Errorf("codes to the CLI %q", f.codes)
	}
	if addAIProviders(t) != nil {
		t.Fatal("providers.json written before Ready")
	}
	wait = signInFeed(t, m, f, wait, clilogin.State{Phase: clilogin.PhaseLoadingModels})
	if !strings.Contains(addAICardText(m), "Loading your models") {
		t.Fatalf("models card:\n%s", addAICardText(m))
	}
	if next := signInFeed(t, m, f, wait, clilogin.State{Phase: clilogin.PhaseReady, Models: ai.CLIModels(ai.CLICodex)}); next != nil {
		t.Error("the app still waits after Ready")
	}
	if m.addAI.Step() != addai.StepDone || !strings.Contains(addAICardText(m), "OpenAI is ready") {
		t.Fatalf("card:\n%s", addAICardText(m))
	}
	if p := addAIProviders(t)[ai.KindOpenAI]; p.Method != ai.MethodCLI || p.CLI != ai.CLICodex || p.AddedAt == 0 {
		t.Errorf("providers.json entry %+v", p)
	}
	cache, _ := ai.LoadModelsCache()
	if got := cache[ai.KindOpenAI].Models; len(got) == 0 || len(got) != len(ai.CLIModels(ai.CLICodex)) {
		t.Errorf("models cache %+v", got)
	}
	if f.canceled() {
		t.Error("Ready killed the CLI (it gets its time to exit)")
	}
	// The link never reaches the error log.
	errorRingMu.Lock()
	logged := strings.Join(errorRing, "\n")
	errorRingMu.Unlock()
	if strings.Contains(logged, "one-time-5555") {
		t.Error("the sign-in link is in the error log")
	}
}

// addAIOnce sends one key or button to the card and handles the one
// request it sends; what that request gives back is returned unrun (a
// wait on the flow would block, a toast would tick).
func addAIOnce(t *testing.T, m *Model, msg tea.Msg) tea.Cmd {
	t.Helper()
	nm, cmd := m.routeToAddAI(msg)
	*m = nm
	if cmd == nil {
		return nil
	}
	out := cmd()
	if out == nil {
		return nil
	}
	next, ok := m.handleAddAIMsg(out)
	if !ok {
		t.Fatalf("%T is not handled", out)
	}
	return next
}

// Spec: an installed CLI starts at once; Esc during the browser step
// stops the flow (its context ends) and saves nothing; a late state is
// dropped.
func TestAddAISignInCancel(t *testing.T) {
	m := addAITestModel(t)
	fk := installFakeSignIn(t, true)
	wait := signInStart(t, m, false)
	f := fk.last(t)
	wait = signInFeed(t, m, f, wait, clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: addAILink})
	addAIOnce(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.addAIOpen {
		t.Fatal("Esc did not close the card")
	}
	if !f.canceled() {
		t.Error("Esc did not stop the sign-in")
	}
	f.ch <- clilogin.State{Phase: clilogin.PhaseReady, Models: ai.CLIModels(ai.CLICodex)}
	if msg := wait(); msg != nil {
		if next, _ := m.handleAddAIMsg(msg); next != nil {
			t.Error("a late state started a new wait")
		}
	}
	if addAIProviders(t) != nil {
		t.Error("a canceled sign-in wrote providers.json")
	}
}

// Spec: no Node.js shows "Codex needs Node.js" with Use an API key and
// Try again; Try again starts a new flow; nothing is saved.
func TestAddAISignInNoNode(t *testing.T) {
	m := addAITestModel(t)
	fk := installFakeSignIn(t, false)
	wait := signInStart(t, m, true)
	f := fk.last(t)
	if next := signInFeed(t, m, f, wait, clilogin.State{Phase: clilogin.PhaseFailed,
		Err: &clilogin.Error{Kind: clilogin.FailNoNode, Msg: "Codex needs Node.js."}}); next != nil {
		t.Error("the app still waits after a failure")
	}
	s := addAICardText(m)
	if !strings.Contains(s, "Codex needs Node.js") || !strings.Contains(s, "Use an API key") || !strings.Contains(s, "Try again") {
		t.Fatalf("card:\n%s", s)
	}
	if addAIProviders(t) != nil {
		t.Error("a failed sign-in wrote providers.json")
	}
	addAIOnce(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // Try again (lit)
	fk.mu.Lock()
	n := len(fk.flows)
	fk.mu.Unlock()
	if n != 2 {
		t.Errorf("Try again: %d flows, want 2", n)
	}
}
