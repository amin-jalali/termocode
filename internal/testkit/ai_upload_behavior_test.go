package testkit

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// aiRecorder is a fake OpenAI-compatible server that keeps every request
// body and answers each with reply.
type aiRecorder struct {
	mu     sync.Mutex
	bodies []string
}

func (r *aiRecorder) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.bodies) == 0 {
		return ""
	}
	return r.bodies[len(r.bodies)-1]
}

func newAIRecorder(t *testing.T, reply string) (*aiRecorder, *httptest.Server) {
	rec := &aiRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.bodies = append(rec.bodies, string(b))
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", reply)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	return rec, srv
}

// openAIChat opens and focuses the chat panel.
func openAIChat(d *Driver) {
	d.T.Helper()
	d.Key("alt+a")
	if !d.WaitForText("AI CHAT", 2*time.Second) {
		d.T.Fatalf("AI panel did not open:\n%s", screen(d))
	}
}

// Spec: Given the focused AI chat input, when the user drops a file on
// the terminal (the terminal pastes its quoted path), then a chip for
// the file shows instead of the text, and the next message sends the
// file's content to the model.
func TestAIDroppedFileBecomesChip(t *testing.T) {
	rec, srv := newAIRecorder(t, "fake-ai-answer")
	d := aiDriver(t, srv.URL, nil, false)
	openAIChat(d)
	path := filepath.Join(d.Dir, "b.txt")
	d.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("'" + path + "' "), Paste: true})
	if !d.WaitForText("b.txt ×", 2*time.Second) {
		t.Fatalf("no b.txt chip after the drop:\n%s", screen(d))
	}
	if strings.Contains(screen(d), path) {
		t.Fatalf("the path was typed as text:\n%s", screen(d))
	}
	d.Type("what is in it")
	d.Key("enter")
	if !d.WaitForText("fake-ai-answer", 5*time.Second) {
		t.Fatalf("no answer:\n%s", screen(d))
	}
	if body := rec.last(); !strings.Contains(body, `one\ntwo\nthree`) || !strings.Contains(body, "Attached b.txt") {
		t.Errorf("request does not carry b.txt's content:\n%s", body)
	}
}

// Spec: Given an AI answer with a code block that names its file, when
// the user clicks "Save as…" and accepts the suggested path, then the
// file is written, shows in the explorer and opens in the editor. A
// second save to the same path asks before it overwrites.
func TestAICodeBlockSaveAs(t *testing.T) {
	_, srv := newAIRecorder(t, "Put this in `notes/hello.go`:\n\n```go\npackage notes\n```\n")
	d := aiDriver(t, srv.URL, nil, false)
	open(d, "a.txt")
	askAI(d, "write it")
	if !d.WaitForText("Save as", 5*time.Second) {
		t.Fatalf("no Save as button on the code block:\n%s", screen(d))
	}
	clickText(d, "Save as")
	if !d.WaitForText("Save code block as", 2*time.Second) || !strings.Contains(screen(d), "notes/hello.go") {
		t.Fatalf("no save prompt with the named path:\n%s", screen(d))
	}
	d.Key("enter")
	if !eventually(d, func() bool { return diskFile(d, "notes/hello.go") == "package notes\n" }) {
		t.Fatalf("file not written: %q\n%s", diskFile(d, "notes/hello.go"), screen(d))
	}
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == "notes/hello.go" }, 5*time.Second) {
		got, a := tabs(d)
		t.Fatalf("saved file not opened: active %q, tabs %v\n%s", a, got, screen(d))
	}
	if !d.WaitFor(func() bool { return explorerShowsExact(d, "hello.go") }, 3*time.Second) {
		t.Fatalf("explorer does not show hello.go:\n%s", screen(d))
	}

	// Again: the file exists now, so it asks first.
	d.Key("alt+a") // back to the chat (the editor has the focus)
	if !d.WaitForText("Save as", 2*time.Second) {
		d.Key("alt+a")
	}
	clickText(d, "Save as")
	if !d.WaitForText("Save code block as", 2*time.Second) {
		t.Fatalf("no save prompt:\n%s", screen(d))
	}
	d.Key("enter")
	if !d.WaitForText("Overwrite?", 2*time.Second) {
		t.Fatalf("no overwrite confirm:\n%s", screen(d))
	}
	clickButton(d, "Cancel")
	if diskFile(d, "notes/hello.go") != "package notes\n" {
		t.Errorf("cancel changed the file")
	}
}

// Spec: Given a chat with a question and an answer, when the user runs
// "AI: Export Chat as Markdown", then ai-chat-<date>.md is written in
// the workspace with the conversation and opens in the editor.
func TestAIExportChatMarkdown(t *testing.T) {
	_, srv := newAIRecorder(t, "fake-ai-answer")
	d := aiDriver(t, srv.URL, nil, false)
	askAI(d, "hello there")
	if !d.WaitForText("fake-ai-answer", 5*time.Second) {
		t.Fatalf("no answer:\n%s", screen(d))
	}
	d.Palette("ai-export-chat")
	name := "ai-chat-" + time.Now().Format("2006-01-02") + ".md"
	if !eventually(d, func() bool { return diskFile(d, name) != "" }) {
		t.Fatalf("%s not written:\n%s", name, screen(d))
	}
	md := diskFile(d, name)
	for _, want := range []string{"## You", "hello there", "## AI", "fake-ai-answer"} {
		if !strings.Contains(md, want) {
			t.Errorf("export misses %q:\n%s", want, md)
		}
	}
	if !d.WaitFor(func() bool { _, a := tabs(d); return a == name }, 5*time.Second) {
		got, a := tabs(d)
		t.Errorf("export not opened: active %q, tabs %v", a, got)
	}
}
