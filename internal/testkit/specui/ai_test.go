package specui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/testkit"
)

const aiAnswer = "```go\npackage main\n\nfunc Add(a, b int) int { return a + b }\n```\n\nDone."

// aiDriver starts the app against the fake AI server with main.go open
// and the chat panel open and focused.
func aiDriver(t *testing.T, f *fakeAI) *testkit.Driver {
	t.Helper()
	slow(t)
	d := testkit.New(t, testkit.Options{W: 140, H: 40, Nvim: true, Config: map[string]string{"config.json": f.config()}})
	d.Action("open:main.go")
	mustSee(t, d, "fmt.Println")
	d.Key("alt+a")
	mustSee(t, d, "Ask AI anything")
	return d
}

// ask types a question into the chat and sends it.
func ask(d *testkit.Driver, q string) {
	d.Type(q)
	d.Key("enter")
}

// Spec: Given a provider that streams a Markdown answer,
// when the user asks a question in the AI chat,
// then the first part shows while the answer still streams ("generating"),
// and the finished answer renders Markdown (no ** or ## marks) with a
// code card that has Copy / Apply buttons; the request carried the
// question and the configured model.
func TestAIChatStreamsMarkdown(t *testing.T) {
	f := newFakeAI(t)
	f.reply("## Plan\n\nUse **bold** and `code`.\n\n", "<wait>", aiAnswer)
	d := aiDriver(t, f)
	ask(d, "how to add?")
	mustSee(t, d, "Use bold and")
	mustSee(t, d, "generating")
	mustNotSee(t, d, "Copy")
	close(f.release)
	mustSee(t, d, "Done.")
	s := screen(d)
	for _, bad := range []string{"**", "## Plan", "```"} {
		if strings.Contains(s, bad) {
			t.Errorf("raw Markdown %q on screen:\n%s", bad, s)
		}
	}
	if !strings.Contains(s, "Copy") || !strings.Contains(s, "Apply") || !strings.Contains(s, "func Add(a, b int)") {
		t.Errorf("no code card with Copy / Apply:\n%s", s)
	}
	reqs := f.requests()
	if len(reqs) != 1 || !strings.Contains(reqs[0], "how to add?") || !strings.Contains(reqs[0], `"model":"fake-model"`) {
		t.Errorf("request body: %.400q", reqs)
	}
}

// Spec: Given a finished answer with a code block,
// when the user clicks Apply on the code card and then "Accept file" on
// the review card,
// then the open file is replaced by the code (saved with Ctrl+S).
func TestAIChatApplyCodeBlock(t *testing.T) {
	f := newFakeAI(t)
	f.reply(aiAnswer)
	d := aiDriver(t, f)
	ask(d, "rewrite it")
	mustSee(t, d, "Done.")
	clickText(t, d, "Apply") // on the code card
	mustSee(t, d, "Review AI changes")
	clickText(t, d, "Accept file")
	mustSee(t, d, "func Add(a, b int)")
	d.Key("ctrl+s")
	if got := waitFile(t, d, "main.go", func(s string) bool { return strings.Contains(s, "func Add") }); !strings.Contains(got, "func Add(a, b int) int") {
		t.Fatalf("main.go = %q", got)
	}
}

// Spec: Given a finished answer with a code block,
// when the user clicks Copy on the code card, then clicks into the editor
// and pastes (Ctrl+V),
// then a toast confirms the copy and the code lands in the file.
func TestAIChatCopyCodeBlock(t *testing.T) {
	f := newFakeAI(t)
	f.reply("```go\n// pasted from chat\n// it's line two\n```")
	var clip string
	t.Cleanup(app.SetClipboardSink(func(s string) { clip = s })) // never the real clipboard
	d := aiDriver(t, f)
	ask(d, "a comment please")
	mustSee(t, d, "Apply")
	// The card's Copy sits left of its Apply (the answer header has a
	// Copy of its own, for the whole answer).
	_, y, _ := locate(d, "Apply")
	row := strings.Split(screen(d), "\n")[y]
	i := strings.LastIndex(row[:strings.Index(row, "Apply")], "Copy")
	if i < 0 {
		t.Fatalf("no Copy next to Apply: %q", row)
	}
	d.Click(ansi.StringWidth(row[:i]), y)
	mustSee(t, d, "Copied code block")
	if clip != "// pasted from chat\n// it's line two" {
		t.Errorf("clipboard = %q", clip)
	}
	clickText(t, d, "fmt.Println") // back into the editor
	d.Key("ctrl+v", "ctrl+s")
	if got := waitFile(t, d, "main.go", func(s string) bool { return strings.Contains(s, "pasted from chat") }); !strings.Contains(got, "// pasted from chat") || !strings.Contains(got, "// it's line two") {
		t.Fatalf("main.go after paste = %q", got)
	}
}

// Spec: Given a provider that answers 401,
// when the user asks a question,
// then the chat shows an error message with the status (no crash, no
// half answer), and the user can ask again.
func TestAIChatShowsHTTPError(t *testing.T) {
	f := newFakeAI(t)
	f.status = 401
	d := aiDriver(t, f)
	ask(d, "hello")
	mustSee(t, d, "401")
	f.mu.Lock()
	f.status = 0
	f.mu.Unlock()
	f.reply("All good now.")
	ask(d, "again")
	mustSee(t, d, "All good now.")
}

// Spec: Given a provider URL where nothing listens,
// when the user asks a question,
// then the chat shows a connection error and stops generating.
func TestAIChatServerDown(t *testing.T) {
	f := newFakeAI(t)
	f.srv.Close()
	d := aiDriver(t, f)
	ask(d, "hello")
	mustSee(t, d, "refused")
	mustNotSee(t, d, "generating")
}
