package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/voice"
)

// CMP-48: voice input. A program the user set up in voice.json listens
// and prints the words; they go into the chat input, never sent alone.

// fakeDictate stands in for the voice program.
type fakeDictate struct {
	started []voice.Settings
	res     chan voice.Result
	stopped bool
	cut     bool
}

func (f *fakeDictate) Stop()              { f.stopped = true; f.res <- voice.Result{Text: "fix the login test"} }
func (f *fakeDictate) Cancel()            { f.cut = true; f.res <- voice.Result{Canceled: true} }
func (f *fakeDictate) Wait() voice.Result { return <-f.res }
func (f *fakeDictate) start(_ context.Context, s voice.Settings) (aiDictateSession, error) {
	f.started = append(f.started, s)
	return f, nil
}

func dictateModel(t *testing.T, s voice.Settings) (*Model, *fakeDictate) {
	t.Helper()
	m := aiTestModel(t)
	f := &fakeDictate{res: make(chan voice.Result, 1)}
	oldLoad, oldStart := aiDictateLoad, aiDictateStart
	aiDictateLoad = func() (voice.Settings, error) { return s, nil }
	aiDictateStart = f.start
	t.Cleanup(func() { aiDictateLoad, aiDictateStart = oldLoad, oldStart })
	return &m, f
}

var dictateSetUp = voice.Settings{Command: []string{"my-dictate", "--max", "{seconds}"}, MaxSeconds: 30}

func TestAIDictateNotSetUpOffersTheConfig(t *testing.T) {
	m, f := dictateModel(t, voice.Settings{})
	m.dispatchAIPalette("ai-dictate")
	if !m.confirmOpen || m.confirmKind != confirmKindAIDictateSetup {
		t.Fatal("no set-up card")
	}
	if b := m.confirm.Buttons(); len(b) != 2 || b[0].ID != "close" || b[1].ID != "config" {
		t.Fatalf("buttons %+v", b)
	}
	if len(f.started) != 0 {
		t.Fatal("a program started without set up")
	}
}

func TestAIDictateConfigWritesTheTemplate(t *testing.T) {
	m, _ := dictateModel(t, voice.Settings{})
	m.dispatchAIPalette("ai-dictate")
	m.confirmOpen = false
	m.handleAIConfirm("config")
	dir, err := configDir()
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, voice.FileName))
	if err != nil || !strings.Contains(string(b), "\"command\"") {
		t.Fatalf("voice.json %q %v", b, err)
	}
}

func TestAIDictateAsksFirstAndCancelIsSafe(t *testing.T) {
	m, f := dictateModel(t, dictateSetUp)
	m.dispatchAIPalette("ai-dictate")
	if !m.confirmOpen || m.confirmKind != confirmKindAIDictate {
		t.Fatal("no mic card")
	}
	if m.confirm.SafeID() != "cancel" {
		t.Fatalf("safe answer %q", m.confirm.SafeID())
	}
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc
	if len(f.started) != 0 {
		t.Fatal("Esc started the microphone")
	}
	m.dispatchAIPalette("ai-dictate")
	m.confirmOpen = false
	m.handleAIConfirm("cancel")
	if len(f.started) != 0 {
		t.Fatal("Cancel started the microphone")
	}
}

func TestAIDictateStopPutsWordsInTheInputAndSendsNothing(t *testing.T) {
	m, f := dictateModel(t, dictateSetUp)
	m.aiSetInput("please")
	m.dispatchAIPalette("ai-dictate")
	m.confirmOpen = false
	wait, _ := m.handleAIConfirm("record")
	if len(f.started) != 1 || wait == nil {
		t.Fatal("not recording")
	}
	if !m.confirmOpen || m.confirmKind != confirmKindAIDictateRec {
		t.Fatal("no listening card")
	}
	m.confirmOpen = false
	m.handleAIConfirm("stop")
	if !f.stopped {
		t.Fatal("Stop did not ask the program to finish")
	}
	msg := wait()
	if _, ok := m.handleAIMsg(msg); !ok {
		t.Fatalf("message %T not routed", msg)
	}
	if got := string(m.ai.input); got != "please fix the login test" {
		t.Fatalf("input %q", got)
	}
	if len(m.ai.conv.Messages) != 0 || m.ai.streaming {
		t.Fatal("dictation sent the message")
	}
	if m.ai.dictate != nil {
		t.Fatal("the session stays")
	}
}

func TestAIDictateDiscardKeepsTheInput(t *testing.T) {
	m, f := dictateModel(t, dictateSetUp)
	m.aiSetInput("draft")
	m.dispatchAIPalette("ai-dictate")
	m.confirmOpen = false
	wait, _ := m.handleAIConfirm("record")
	m.confirmOpen = false
	m.onAIConfirmClosed() // Esc on the listening card discards
	if !f.cut {
		t.Fatal("Esc did not stop the program")
	}
	m.handleAIMsg(wait())
	if got := string(m.ai.input); got != "draft" {
		t.Fatalf("input %q", got)
	}
}

func TestAIDictateErrorKeepsTheInput(t *testing.T) {
	m, _ := dictateModel(t, dictateSetUp)
	m.aiSetInput("draft")
	m.ai.dictate = &aiDictate{id: 7}
	m.handleAIMsg(aiDictateDoneMsg{id: 7, res: voice.Result{Err: errors.New("no microphone")}})
	if got := string(m.ai.input); got != "draft" {
		t.Fatalf("input %q", got)
	}
	if m.ai.dictate != nil {
		t.Fatal("the failed session stays")
	}
}

func TestAIDictateOldResultIsIgnored(t *testing.T) {
	m, _ := dictateModel(t, dictateSetUp)
	m.ai.dictate = &aiDictate{id: 2}
	m.handleAIMsg(aiDictateDoneMsg{id: 1, res: voice.Result{Text: "old words"}})
	if len(m.ai.input) != 0 || m.ai.dictate == nil {
		t.Fatal("an old result was used")
	}
}

func TestAIDictateIsNeverAnAIAction(t *testing.T) {
	p, ok := aiActionPolicies["ai-dictate"]
	if !ok || p.Gate != aiGateNever {
		t.Fatalf("policy %+v", p)
	}
}

func TestAIDictateInsertSpacing(t *testing.T) {
	cases := []struct{ before, words, want string }{
		{"", "hello", "hello"},
		{"fix", "it", "fix it"},
		{"fix ", "it", "fix it"},
		{"line\n", "next", "line\nnext"},
	}
	for _, c := range cases {
		if got := c.before + aiDictateJoin([]rune(c.before), len([]rune(c.before)), c.words); got != c.want {
			t.Errorf("%q + %q = %q", c.before, c.words, got)
		}
	}
}
