package testkit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Spec: Given an open file and the AI chat, when the user clicks the
// "Explain this file" suggestion, then the "Attached … to the AI chat"
// toast shows and goes away on its own after a few seconds. The toast's
// expiry timer used to be dropped, so with a quiet screen (the answer
// came, no other toast) it stayed forever.
func TestSuggestionAttachToastExpires(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"fake-ai-answer\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)

	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{W: 160, H: 40, Nvim: true, Files: textFiles(),
		Setup: func(t testing.TB, _ string) { // after the sandbox clears the AI env
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srv.URL+"/v1")
		}})
	open(d, "a.txt")
	d.Key("alt+a")
	if !d.WaitForText("Explain this file", 3*time.Second) {
		t.Fatalf("no suggestions in the AI chat:\n%s", screen(d))
	}
	clickText(d, "Explain this file")
	if !d.WaitForText("Attached a.txt", 2*time.Second) {
		t.Fatalf("no attach toast:\n%s", screen(d))
	}
	if !d.WaitForText("fake-ai-answer", 5*time.Second) {
		t.Fatalf("no answer:\n%s", screen(d))
	}
	gone := d.WaitFor(func() bool { return !strings.Contains(screen(d), "Attached a.txt") }, 6*time.Second)
	if !gone {
		t.Errorf("attach toast still on screen after 6s:\n%s", screen(d))
	}
}

// Spec: Given auto_save and show_hidden are on in config.json, when
// termocode starts, then they are applied silently: no "Auto save" or
// "Hidden files" toast. These toasts used to show at startup with their
// expiry timer dropped, so they could stay on screen.
func TestStartupSettingsAreSilent(t *testing.T) {
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{W: 120, H: 34, Nvim: true, Files: textFiles(),
		Config: map[string]string{"config.json": `{"auto_save": true, "show_hidden": true}`}})
	d.Settle(500 * time.Millisecond)
	s := screen(d)
	for _, bad := range []string{"Auto save", "Auto-save", "Hidden files"} {
		if strings.Contains(s, bad) {
			t.Errorf("startup shows a %q toast:\n%s", bad, s)
		}
	}
}

// Spec: Given an AI chat answer has finished, when the user does nothing,
// then the app is idle: no timer keeps sending messages. A timer left
// running costs CPU all the time and makes held keys lag behind.
func TestIdleAfterAIAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"## fake-ai-answer\\n\\n```go\\nfunc x() {}\\n```\\n\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	if testing.Short() {
		t.Skip("starts nvim")
	}
	d := New(t, Options{W: 160, H: 40, Nvim: true, Files: textFiles(),
		Setup: func(t testing.TB, _ string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srv.URL+"/v1")
		}})
	open(d, "a.txt")
	d.Key("alt+a")
	clickText(d, "Explain this file")
	if !d.WaitForText("fake-ai-answer", 5*time.Second) {
		t.Fatalf("no answer:\n%s", screen(d))
	}
	never := func() bool { return false }
	d.WaitFor(never, 5*time.Second) // toasts and animations end
	n0, _ := d.Updates()
	d.WaitFor(never, 3*time.Second)
	n1, last := d.Updates()
	// The git status poll (one check every 1.5 s) is expected; anything
	// else in the window means a timer that never stops.
	busy := 0
	for _, msg := range last[max(0, len(last)-(n1-n0)):] {
		if msg != "app.gitAutoRefreshMsg" && msg != "app.GitMsg" {
			busy++
		}
	}
	if busy > 0 {
		t.Errorf("app not idle: %d messages in 3 s with nothing to do; last ones:\n%s", busy, strings.Join(last, "\n"))
	}
}
