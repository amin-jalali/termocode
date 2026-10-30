package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/addai"
	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/clilogin"
)

// Add AI (ai_add.go, ai_add_run.go): requests that arrive after the card
// closed, a malformed key reference, a key from the environment, a moved
// local server, a save that fails and a failed sign-in.

func TestCovAIAddRequestsAfterClose(t *testing.T) {
	m := addAITestModel(t)
	for _, msg := range []any{addai.ConnectKeyMsg{Kind: ai.KindOpenAI, Key: "x"}, addai.ConnectURLMsg{URL: "http://x"},
		addai.DetectLocalMsg{Kind: ai.KindOllama}, addai.SignInMsg{Kind: ai.KindOpenAI, CLI: ai.CLICodex}, addai.CopyLinkMsg{}, addai.SendCodeMsg{Code: "c"}} {
		cmd, ok := m.handleAddAIMsg(msg)
		if !ok || cmd != nil {
			t.Errorf("%T after close: %v %v", msg, ok, cmd != nil)
		}
	}
	if m.onAddAIResult(addAIResultMsg{gen: 1}) != nil || m.onAddAISignIn(addAISignInStateMsg{gen: 1}) != nil {
		t.Error("answers after close did something")
	}
	if _, ok := m.handleAddAIMsg("foreign"); ok {
		t.Error("a foreign message was taken")
	}
}

// A malformed reference in the key or URL key is refused on the card,
// with nothing sent.
func TestCovAIAddMalformedReference(t *testing.T) {
	m := addAITestModel(t)
	var f fakeAddAIList
	f.install(t, addAIServer(t))
	addAIPick(t, m, ai.KindAnthropic)
	for _, msg := range []any{addai.ConnectKeyMsg{Gen: m.addAI.Gen(), Kind: ai.KindAnthropic, Key: "${nope:x}"},
		addai.ConnectURLMsg{Gen: m.addAI.Gen(), URL: "http://127.0.0.1:1/v1", Key: "${nope:x}"}} {
		if cmd, _ := m.handleAddAIMsg(msg); cmd != nil {
			t.Errorf("%T with a bad reference sent a request", msg)
		}
	}
	if len(f.keys) != 0 {
		t.Errorf("keys sent: %q", f.keys)
	}
}

// A key typed as a reference is read first; one that gives nothing says
// so; a closed card ends the request silently; a server error is one
// sentence.
func TestCovAIAddListCmd(t *testing.T) {
	addAITestModel(t)
	var got []string
	old := aiListModels
	aiListModels = func(ctx context.Context, cfg ai.Config) ([]ai.ModelInfo, error) {
		got = append(got, cfg.APIKey+"@"+cfg.BaseURL)
		if cfg.Kind == ai.KindCompatible {
			return nil, errors.New("dial tcp: refused")
		}
		return []ai.ModelInfo{{ID: "m1"}}, nil
	}
	t.Cleanup(func() { aiListModels = old })
	t.Setenv("COV_ADD_KEY", "sk-cov-add-123456789")
	msg := addAIListCmd(context.Background(), 3, ai.ProviderEntry{Kind: ai.KindOpenAI}, "${env:COV_ADD_KEY}", addAIKeyFail("OpenAI"))()
	r := msg.(addAIResultMsg)
	if r.fail != "" || r.key != "${env:COV_ADD_KEY}" || got[0] != "sk-cov-add-123456789@" {
		t.Errorf("reference key: %+v sent %q", r, got)
	}
	r = addAIListCmd(context.Background(), 3, ai.ProviderEntry{Kind: ai.KindOpenAI}, "${env:COV_ADD_MISSING}", addAIKeyFail("OpenAI"))().(addAIResultMsg)
	if r.fail != "That reference gave no key." {
		t.Errorf("empty reference: %+v", r)
	}
	r = addAIListCmd(context.Background(), 3, ai.ProviderEntry{Kind: ai.KindCompatible, BaseURL: "http://127.0.0.1:1/v1"}, "", addAIKeyFail("Box"))().(addAIResultMsg)
	if r.fail != "Could not reach Box. Check your connection." {
		t.Errorf("server error: %+v", r)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if out := addAIListCmd(ctx, 3, ai.ProviderEntry{Kind: ai.KindCompatible}, "", addAIKeyFail("Box"))(); out != nil {
		t.Errorf("a closed card got %T", out)
	}
}

// A local server the user moved stays where it is when detected again.
func TestCovAIAddDetectLocalKeepsMovedServer(t *testing.T) {
	m := addAITestModel(t)
	var f fakeAddAIList
	f.install(t, addAIServer(t))
	addAIPick(t, m, ai.KindOllama)
	m.aiS().providers = map[string]ai.ProviderEntry{ai.KindOllama: {Kind: ai.KindOllama, Method: ai.MethodLocal, BaseURL: "http://box:11434"}}
	var base string
	old := aiListModels
	aiListModels = func(_ context.Context, cfg ai.Config) ([]ai.ModelInfo, error) { base = cfg.BaseURL; return nil, nil }
	t.Cleanup(func() { aiListModels = old })
	cmd, _ := m.handleAddAIMsg(addai.DetectLocalMsg{Gen: m.addAI.Gen(), Kind: ai.KindOllama})
	if cmd == nil {
		t.Fatal("no detect request")
	}
	cmd()
	if base != "http://box:11434" {
		t.Errorf("detect used %q", base)
	}
}

// A provider that can not be saved: the card says so, errors.log has why.
func TestCovAIAddSaveFails(t *testing.T) {
	m := addAITestModel(t)
	var f fakeAddAIList
	f.install(t, addAIServer(t))
	p, err := ai.ProvidersPath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(p)
	if err := os.MkdirAll(p, 0o755); err != nil { // providers.json is a folder
		t.Fatal(err)
	}
	addAIPick(t, m, ai.KindOllama) // picking a local AI detects it at once
	if !strings.Contains(addAICardText(m), "Could not save it") {
		t.Errorf("card:\n%s", addAICardText(m))
	}
	cfg, _ := configDir()
	if b, _ := os.ReadFile(filepath.Join(cfg, "errors.log")); !strings.Contains(string(b), "Add AI ollama:") {
		t.Errorf("errors.log:\n%s", b)
	}
}

// A sign-in that fails logs the installer's detail (never the link) and
// the card shows the failure; Copy the link copies it.
func TestCovAIAddSignInFails(t *testing.T) {
	m := addAITestModel(t)
	fk := installFakeSignIn(t, true)
	wait := signInStart(t, m, false)
	f := fk.last(t)
	wait = signInFeed(t, m, f, wait, clilogin.State{Phase: clilogin.PhaseWaitingForBrowser, URL: "https://auth.example/x"})
	var copied []string
	sink := func(x string) { copied = append(copied, x) }
	clipboardSink.Store(&sink)
	t.Cleanup(func() { clipboardSink.Store(nil) })
	m.handleAddAIMsg(addai.CopyLinkMsg{Gen: m.addAI.Gen()})
	if len(copied) != 1 || copied[0] != "https://auth.example/x" {
		t.Errorf("copied %q", copied)
	}
	if next := signInFeed(t, m, f, wait, clilogin.State{Phase: clilogin.PhaseFailed, Err: &clilogin.Error{Msg: "npm failed", Detail: "npm ERR! code E401"}}); next != nil {
		t.Error("the app still waits after a failure")
	}
	cfg, _ := configDir()
	b, _ := os.ReadFile(filepath.Join(cfg, "errors.log"))
	if !strings.Contains(string(b), "Add AI sign-in codex: npm ERR! code E401") || strings.Contains(string(b), "auth.example") {
		t.Errorf("errors.log:\n%s", b)
	}
}
