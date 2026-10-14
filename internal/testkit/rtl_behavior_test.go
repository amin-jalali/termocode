package testkit

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/rtl"
)

// rtlChat starts the app with a fake AI that answers answer, sends
// question and waits until want is on screen.
func rtlChat(t *testing.T, question, answer, want string) (*Driver, *fakeClipboard) {
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
			t.Setenv("KONSOLE_VERSION", "") // rtl = auto: termocode lays out
			t.Setenv("MLTERM", "")
		}})
	d.Key("alt+a")
	d.Type(question)
	d.Key("enter")
	if !d.WaitFor(func() bool {
		s := screen(d)
		return strings.Contains(s, want) && strings.Contains(s, "Copy") && !strings.Contains(s, "generating")
	}, 5*time.Second) {
		t.Fatalf("no answer %q:\n%s", want, screen(d))
	}
	return d, clip
}

// Spec: a Persian question and answer show on screen with joined letters
// in reading order (right to left), the frame keeps its size, and
// dragging over the answer copies the text as written.
func TestAIChatPersianRTL(t *testing.T) {
	const answer = "سلام دنیا، خوبی؟"
	d, clip := rtlChat(t, "سلام", answer, rtl.Visual(answer, rtl.RTL))
	CheckFrame(t, d.Frame(), 160, 40)
	scr := screen(d)
	if !strings.Contains(scr, rtl.Visual("سلام", rtl.RTL)) {
		t.Errorf("question not shaped:\n%s", scr)
	}
	if strings.Contains(scr, answer) {
		t.Errorf("answer drawn in logical order:\n%s", scr)
	}
	vis := rtl.Visual(answer, rtl.RTL)
	x, y, ok := find(d, vis)
	if !ok {
		t.Fatalf("answer not on screen")
	}
	d.Drag(x, y, x+len([]rune(vis))-1, y)
	if got, _ := clip.get(); got != answer {
		t.Errorf("clipboard = %q, want the logical text %q", got, answer)
	}
}

// Spec: the Markdown preview of a Persian file shows the heading and
// text shaped, in reading order.
func TestMarkdownPreviewPersianRTL(t *testing.T) {
	files := textFiles()
	files["notes/fa.md"] = "# راهنمای فارسی\n\nاین یک متن فارسی است با English و عدد 42.\n"
	d := New(t, Options{W: 120, H: 30, Nvim: true, Files: files,
		Setup: func(t testing.TB, _ string) {
			t.Setenv("KONSOLE_VERSION", "")
			t.Setenv("MLTERM", "")
		}})
	open(d, "notes/fa.md")
	d.Key("f7")
	head := rtl.Visual("راهنمای فارسی", rtl.RTL)
	if !d.WaitForText(head, 5*time.Second) {
		t.Fatalf("Persian heading not shaped in the preview:\n%s", screen(d))
	}
	CheckFrame(t, d.Frame(), 120, 30)
	if !strings.Contains(screen(d), rtl.Visual("این یک متن فارسی است با English و عدد 42.", rtl.RTL)) {
		t.Errorf("paragraph not laid out:\n%s", screen(d))
	}
}
