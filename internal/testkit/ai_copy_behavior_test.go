package testkit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/theme"
)

// copyAnswer is the fake AI answer of the copy tests.
const copyAnswer = "Alpha beta gamma delta.\n\n```go\nfunc hello() {}\n```"

// fakeClipboard catches copies (the real clipboard is never touched).
type fakeClipboard struct {
	mu   sync.Mutex
	last string
	n    int
}

func (c *fakeClipboard) get() (string, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last, c.n
}

// copyChat starts the app with a fake AI (answer is streamed as one
// SSE chunk), sends a question and waits for the answer.
func copyChat(t *testing.T, answer string) (*Driver, *fakeClipboard) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		q := strings.ReplaceAll(strings.ReplaceAll(answer, "\n", `\n`), `"`, `\"`)
		fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":\"%s\"}}]}\n\n", q)
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	clip := &fakeClipboard{}
	t.Cleanup(app.SetClipboardSink(func(s string) {
		clip.mu.Lock()
		clip.last, clip.n = s, clip.n+1
		clip.mu.Unlock()
	}))
	d := New(t, Options{W: 160, H: 40, Files: textFiles(),
		Setup: func(t testing.TB, _ string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srv.URL+"/v1")
		}})
	d.Key("alt+a")
	d.Type("question please")
	d.Key("enter")
	if !d.WaitForText("Alpha beta", 5*time.Second) {
		t.Fatalf("no answer:\n%s", screen(d))
	}
	if !d.WaitFor(func() bool { return strings.Contains(screen(d), "Copy") && !strings.Contains(screen(d), "generating") }, 5*time.Second) {
		t.Fatalf("answer not finished:\n%s", screen(d))
	}
	return d, clip
}

// Spec: dragging over part of an answer selects it (shown with the
// selection colour); on release the plain text is copied and a toast
// says how much.
func TestAIChatDragSelectCopies(t *testing.T) {
	d, clip := copyChat(t, copyAnswer)
	x, y, ok := find(d, "beta gamma")
	if !ok {
		t.Fatalf("answer text not shown:\n%s", screen(d))
	}
	d.Drag(x, y, x+len("beta gamma")-1, y)
	if got, _ := clip.get(); got != "beta gamma" {
		t.Errorf("clipboard = %q, want %q", got, "beta gamma")
	}
	if !d.WaitForText("Copied 10 chars", time.Second) {
		t.Errorf("no copy toast:\n%s", screen(d))
	}
	sel := lipgloss.NewStyle().Background(theme.LG(theme.BgSelection)).Foreground(theme.LG(theme.TextWhite)).Render("beta gamma")
	if !strings.Contains(d.Frame(), sel) {
		t.Errorf("selection not highlighted on screen")
	}
	// Esc drops the selection (and its highlight).
	d.Key("esc")
	if strings.Contains(d.Frame(), sel) {
		t.Errorf("selection still highlighted after Esc")
	}
}

// Spec: a drag over several rows copies the rows joined as the source
// text, without the code card's border or padding.
func TestAIChatDragAcrossCode(t *testing.T) {
	d, clip := copyChat(t, copyAnswer)
	x0, y0, _ := find(d, "Alpha")
	x1, y1, ok := find(d, "func hello() {}")
	if !ok {
		t.Fatalf("code not shown:\n%s", screen(d))
	}
	d.Drag(x0, y0, x1+len("func hello() {}")-1, y1)
	if got, want := func() string { s, _ := clip.get(); return s }(), "Alpha beta gamma delta.\n\nfunc hello() {}"; got != want {
		t.Errorf("clipboard = %q, want %q", got, want)
	}
}

// Spec: double-click selects and copies the word under the pointer.
func TestAIChatDoubleClickWord(t *testing.T) {
	d, clip := copyChat(t, copyAnswer)
	x, y, _ := find(d, "gamma")
	d.Click(x+2, y)
	d.Click(x+2, y)
	if got, _ := clip.get(); got != "gamma" {
		t.Errorf("clipboard = %q, want gamma", got)
	}
}

// Spec: the Copy button on the answer header copies the whole answer
// as Markdown; the one next to the question copies the question.
func TestAIChatCopyMessageButton(t *testing.T) {
	d, clip := copyChat(t, copyAnswer)
	_, hy, ok := find(d, "AI · fake")
	if !ok {
		t.Fatalf("no answer header:\n%s", screen(d))
	}
	line := strings.Split(screen(d), "\n")[hy]
	j := strings.LastIndex(line, "Copy")
	if j < 0 {
		t.Fatalf("no Copy button on the header: %q", line)
	}
	d.Click(len([]rune(line[:j])), hy)
	if got, _ := clip.get(); got != copyAnswer {
		t.Errorf("clipboard = %q, want the answer", got)
	}
	if !d.WaitForText("Copied message", time.Second) {
		t.Errorf("no toast:\n%s", screen(d))
	}
}

// Spec: right-click on a code row offers Copy Message and Copy Code
// Block; Copy Code Block copies the code only.
func TestAIChatRightClickMenu(t *testing.T) {
	d, clip := copyChat(t, copyAnswer)
	x, y, _ := find(d, "func hello")
	d.RightClick(x+1, y)
	if !d.WaitForText("Copy Code Block", time.Second) || !strings.Contains(screen(d), "Copy Message") {
		t.Fatalf("no chat menu:\n%s", screen(d))
	}
	clickText(d, "Copy Code Block")
	if got, _ := clip.get(); got != "func hello() {}" {
		t.Errorf("clipboard = %q", got)
	}
	// With a selection, the menu also offers Copy Selection.
	x, y, _ = find(d, "beta")
	d.Drag(x, y, x+3, y)
	d.RightClick(x, y)
	if !d.WaitForText("Copy Selection", time.Second) {
		t.Fatalf("no Copy Selection:\n%s", screen(d))
	}
	clickText(d, "Copy Message")
	if got, _ := clip.get(); got != copyAnswer {
		t.Errorf("clipboard = %q, want the answer", got)
	}
}

// Spec: Ctrl+C in the chat copies the selection when there is one;
// with no selection it still stops a running answer.
func TestAIChatCtrlC(t *testing.T) {
	d, clip := copyChat(t, copyAnswer)
	x, y, _ := find(d, "delta")
	d.Drag(x, y, x+4, y)
	_, n := clip.get()
	d.Key("ctrl+c")
	if got, n2 := clip.get(); got != "delta" || n2 != n+1 {
		t.Errorf("Ctrl+C with a selection: clipboard %q (%d copies, want %d)", got, n2, n+1)
	}
}

// Spec: with no selection, Ctrl+C in the chat stops the answer that is
// streaming (as before) and copies nothing.
func TestAIChatCtrlCStopsWithoutSelection(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"slow answer\"}}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release); srv.Close() })
	clip := &fakeClipboard{}
	t.Cleanup(app.SetClipboardSink(func(s string) { clip.mu.Lock(); clip.n++; clip.mu.Unlock() }))
	d := New(t, Options{W: 160, H: 40, Files: textFiles(),
		Setup: func(t testing.TB, _ string) {
			t.Setenv("TERMOCODE_AI_PROVIDER", "ollama")
			t.Setenv("TERMOCODE_AI_MODEL", "fake")
			t.Setenv("TERMOCODE_AI_BASE_URL", srv.URL+"/v1")
		}})
	d.Key("alt+a")
	d.Type("question please")
	d.Key("enter")
	if !d.WaitForText("slow answer", 5*time.Second) {
		t.Fatalf("no answer:\n%s", screen(d))
	}
	d.Key("ctrl+c")
	if !d.WaitForText("AI stopped", 2*time.Second) {
		t.Errorf("Ctrl+C did not stop the answer:\n%s", screen(d))
	}
	if _, n := clip.get(); n != 0 {
		t.Errorf("Ctrl+C without a selection copied %d times", n)
	}
}
