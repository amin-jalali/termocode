package app

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webAIFake is a provider that records its requests and answers reply
// (block: it waits until the request is cancelled, then fails).
type webAIFake struct {
	mu    sync.Mutex
	reqs  []ai.Request
	reply string
	block bool
	got   chan struct{} // one value per request, when not nil
}

func (*webAIFake) Name() string        { return "fake" }
func (*webAIFake) Configured() bool    { return true }
func (*webAIFake) SupportsTools() bool { return false }
func (f *webAIFake) Stream(ctx context.Context, r ai.Request) <-chan ai.Chunk {
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	reply, block, got := f.reply, f.block, f.got
	f.mu.Unlock()
	if got != nil {
		got <- struct{}{}
	}
	ch := make(chan ai.Chunk, 2)
	go func() {
		defer close(ch)
		if block {
			<-ctx.Done()
			ch <- ai.Chunk{Done: true, Err: ctx.Err()}
			return
		}
		ch <- ai.Chunk{Delta: reply}
		ch <- ai.Chunk{Done: true}
	}()
	return ch
}

func (f *webAIFake) requests() []ai.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ai.Request(nil), f.reqs...)
}

// webAIRig is a files rig with the AI set up (fake provider, inline on)
// and buffer 2 open on path with rows.
func webAIRig(t *testing.T, path string, rows ...string) (*webFilesRig, *webAIFake) {
	t.Helper()
	r := newWebFilesRig(t)
	f := &webAIFake{}
	s := r.m.aiS()
	s.provider = f
	s.cfg = ai.Config{Kind: ai.KindOllama, Model: "big-1", InlineModel: "fast-1"}
	on := true
	s.settings.Inline = &on
	r.h.filesState().bufs[2] = &webBuf{buf: 2, path: path, rows: rows, tick: 7, ft: "go", refs: map[string]bool{"c-1": true}}
	return r, f
}

func webAICompletion(t *testing.T, f map[string]any) webproto.AICompletion {
	t.Helper()
	var c webproto.AICompletion
	webResData(t, f, &c)
	return c
}

// Spec (WEBP-3): ai.complete asks the laptop's inline model with the
// laptop's prompt (the file around the caret, <CURSOR> at the caret) and
// answers with the laptop's ghost text: a continuation adds only what is
// new after the caret; a rewrite replaces what was typed on the row (From
// < To, UTF-16 columns); the imports the code needs come as whole lines
// with the row to put them before.
func TestWebAICompleteGhostText(t *testing.T) {
	r, f := webAIRig(t, "main.go", "package main", "", "func main() {", "\tfmt.Pri", "}")
	f.reply = `fmt.Println("hi")`
	c := webAICompletion(t, r.call(webproto.OpAIComplete, &webproto.AICompleteArgs{Buf: 2, Tick: 7, Line: 3, Col: 8, Manual: true}))
	if c.None || c.Line != 3 || c.From != 8 || c.To != 8 || c.Text != `ntln("hi")` {
		t.Fatalf("continuation: %+v", c)
	}
	reqs := f.requests()
	if len(reqs) != 1 || reqs[0].Model != "fast-1" {
		t.Fatalf("requests %+v", reqs)
	}
	user := reqs[0].Messages[len(reqs[0].Messages)-1].Content
	if !strings.Contains(user, "\tfmt.Pri"+ai.CursorMarker+"\n}") || !strings.Contains(user, "main.go") {
		t.Errorf("prompt %q", user)
	}

	// A rewrite of the typed word, with an import; a wide rune before the
	// caret counts two UTF-16 units.
	r.h.filesState().bufs[2].rows[3] = "\t𝔵 := print"
	f.reply = "IMPORT: import \"fmt\"\nfmt.Println(\"salam\")"
	c = webAICompletion(t, r.call(webproto.OpAIComplete, &webproto.AICompleteArgs{Buf: 2, Tick: 7, Line: 3, Col: 12, Manual: true}))
	if c.None || c.From != 1 || c.To != 12 || c.Text != `fmt.Println("salam")` {
		t.Fatalf("rewrite: %+v", c)
	}
	if c.ImportAt != 1 || len(c.Imports) == 0 || !strings.Contains(strings.Join(c.Imports, "\n"), `"fmt"`) {
		t.Errorf("imports at %d: %q", c.ImportAt, c.Imports)
	}
}

// Spec (WEBP-3): ai.complete follows the laptop: nothing while AI is not
// set up (a manual ask says why), nothing automatic while ai_inline is
// off, never for a file that may hold secrets, and the laptop's policy
// skips an automatic ask in the middle of a word. A buffer the page did
// not open is not_found; one that moved on is a conflict with its tick.
func TestWebAICompleteFollowsTheLaptop(t *testing.T) {
	r, f := webAIRig(t, "main.go", "package main", "func main() {", "\tx := foo()", "}")
	f.reply = "bar"
	at := func(line, col int, manual bool) webproto.AICompletion {
		return webAICompletion(t, r.call(webproto.OpAIComplete, &webproto.AICompleteArgs{Buf: 2, Tick: 7, Line: line, Col: col, Manual: manual}))
	}
	if c := at(2, 7, false); !c.None || c.Why != "" {
		t.Errorf("mid-word automatic ask: %+v", c)
	}
	off := false
	r.m.aiS().settings.Inline = &off
	if c := at(3, 1, false); !c.None {
		t.Errorf("ai_inline off: %+v", c)
	}
	r.h.filesState().bufs[2].path = "config/.env"
	if c := at(3, 1, true); !c.None || !strings.Contains(c.Why, "secrets") {
		t.Errorf("secret file: %+v", c)
	}
	r.m.aiS().provider = nil
	if c := at(3, 1, true); !c.None || !strings.Contains(c.Why, "Set up AI") {
		t.Errorf("not set up: %+v", c)
	}
	if n := len(f.requests()); n != 0 {
		t.Fatalf("%d requests reached the model", n)
	}
	if code := webErrCode(r.call(webproto.OpAIComplete, &webproto.AICompleteArgs{Buf: 9, Tick: 7, Line: 0, Col: 0})); code != "not_found" {
		t.Errorf("unknown buffer: %s", code)
	}
	if code := webErrCode(r.call(webproto.OpAIComplete, &webproto.AICompleteArgs{Buf: 2, Tick: 6, Line: 0, Col: 0})); code != "conflict" {
		t.Errorf("old tick: %s", code)
	}
}

// Spec (WEBP-3): a newer ai.complete of the same page cancels the older
// one, which answers "nothing" at once instead of waiting for the model.
func TestWebAICompleteNewerCancelsOlder(t *testing.T) {
	r, f := webAIRig(t, "main.go", "package main", "func main() {", "\t", "}")
	f.block = true
	f.got = make(chan struct{}, 4)
	r.id++
	r.c.inflight.Add(1)
	first := &webCall{ctx: r.c.ctx, conn: r.c, id: r.id, op: webproto.OpAIComplete, args: &webproto.AICompleteArgs{Buf: 2, Tick: 7, Line: 2, Col: 1, Manual: true}}
	cmd, _ := r.m.webRunCall(r.h, first)
	msgs := make(chan tea.Msg, 1)
	go func() { msgs <- cmd() }()
	<-f.got // the older one waits for the model
	f.mu.Lock()
	f.block = false
	f.reply = "return"
	f.mu.Unlock()
	c := webAICompletion(t, r.call(webproto.OpAIComplete, &webproto.AICompleteArgs{Buf: 2, Tick: 7, Line: 2, Col: 1, Manual: true}))
	if am, ok := (<-msgs).(applyMsg); ok {
		webDrain(t, r.m, am(r.m))
	}
	if c.None || c.Text != "return" {
		t.Fatalf("newer: %+v", c)
	}
	var old map[string]any
	for _, fr := range r.takeOn(r.c) {
		if id, _ := fr["id"].(float64); uint64(id) == first.id {
			old = fr
		}
	}
	if old == nil || !webAICompletion(t, old).None {
		t.Fatalf("older answer: %v", old)
	}
	if len(r.h.ai.inline) != 0 {
		t.Errorf("calls left: %d", len(r.h.ai.inline))
	}
}

// Spec (WEBP-3): ai.next_edit keeps the buffer's text at each pause
// (seed: the editor opened) and, after an edit, asks the laptop's model
// with the laptop's prompt (the recent edits and the lines around the
// caret); the answer is the rows to replace (zero-based, End included),
// their old text and the new lines. No edit since the last pause asks
// nothing.
func TestWebAINextEdit(t *testing.T) {
	r, f := webAIRig(t, "main.go", "package main", "func f() int {", "\toldName := 1", "\treturn oldName", "}")
	ne := func(line int, seed bool) webproto.AINextEdit {
		var out webproto.AINextEdit
		webResData(t, r.call(webproto.OpAINextEdit, &webproto.AINextEditArgs{Buf: 2, Tick: 7, Line: line, Seed: seed}), &out)
		return out
	}
	if out := ne(0, true); !out.None {
		t.Fatalf("seed: %+v", out)
	}
	if out := ne(2, false); !out.None {
		t.Fatalf("no edit: %+v", out)
	}
	if n := len(f.requests()); n != 0 {
		t.Fatalf("%d requests without an edit", n)
	}
	r.h.filesState().bufs[2].rows[2] = "\tnewName := 1"
	f.reply = `{"start": 4, "end": 4, "lines": ["\treturn newName"]}`
	out := ne(2, false)
	if out.None || out.Start != 3 || out.End != 3 || len(out.Old) != 1 || out.Old[0] != "\treturn oldName" || len(out.Lines) != 1 || out.Lines[0] != "\treturn newName" {
		t.Fatalf("next edit: %+v", out)
	}
	reqs := f.requests()
	if len(reqs) != 1 || reqs[0].Model != "fast-1" {
		t.Fatalf("requests %+v", reqs)
	}
	if user := reqs[0].Messages[len(reqs[0].Messages)-1].Content; !strings.Contains(user, "+\tnewName := 1") || !strings.Contains(user, "-\toldName := 1") {
		t.Errorf("prompt has no edit: %q", user)
	}
}

// Spec (WEBP-3): next edit suggestions follow the laptop's ai_next_edit
// setting and never read a file that may hold secrets.
func TestWebAINextEditFollowsTheLaptop(t *testing.T) {
	r, f := webAIRig(t, "deploy/token.txt", "a", "b")
	f.reply = `{"start": 1, "end": 1, "lines": ["x"]}`
	call := func() webproto.AINextEdit {
		var out webproto.AINextEdit
		webResData(t, r.call(webproto.OpAINextEdit, &webproto.AINextEditArgs{Buf: 2, Tick: 7, Line: 0}), &out)
		return out
	}
	call()
	r.h.filesState().bufs[2].rows[0] = "c"
	if out := call(); !out.None {
		t.Errorf("secret file: %+v", out)
	}
	r.h.filesState().bufs[2].path = "main.go"
	off := false
	r.m.aiS().settings.NextEdit = &off
	call()
	r.h.filesState().bufs[2].rows[0] = "d"
	if out := call(); !out.None {
		t.Errorf("ai_next_edit off: %+v", out)
	}
	if n := len(f.requests()); n != 0 {
		t.Fatalf("%d requests reached the model", n)
	}
}

func webAIActionRes(t *testing.T, f map[string]any) webproto.AIAction {
	t.Helper()
	var out webproto.AIAction
	webResData(t, f, &out)
	return out
}

// Spec (WEBP-3): Fix, Edit and Doc ask the laptop's chat model with the
// laptop's edit prompt and answer with the rows to replace and the new
// lines; nothing in the buffer changes. Fix takes the laptop's problems
// on the caret's row (no problem: not_found) and the lines around it;
// Doc the caret's row; Edit the selection and the user's instruction. An
// answer equal to the text is "no change".
func TestWebAIActionProposes(t *testing.T) {
	rows := []string{"package main", "", "func add(a, b int) int {", "\treturn a - b", "}"}
	r, f := webAIRig(t, "calc/add.go", rows...)
	act := func(args webproto.AIActionArgs) map[string]any {
		args.Buf, args.Tick = 2, 7
		return r.call(webproto.OpAIAction, &args)
	}
	f.reply = "```go\n// add returns the sum of a and b.\nfunc add(a, b int) int {\n```"
	out := webAIActionRes(t, act(webproto.AIActionArgs{Kind: "doc", Start: 2, End: 2, Line: 2}))
	if out.None || out.Start != 2 || out.End != 3 || len(out.Lines) != 2 || out.Lines[0] != "// add returns the sum of a and b." || out.Title != "AI doc comment" {
		t.Fatalf("doc: %+v", out)
	}
	req := f.requests()[0]
	if req.Model != "big-1" || !strings.Contains(req.Messages[1].Content, "Range to replace (lines 3-3)") || !strings.Contains(req.Messages[1].Content, "doc comment") {
		t.Errorf("doc request: %s %q", req.Model, req.Messages[1].Content)
	}

	if code := webErrCode(act(webproto.AIActionArgs{Kind: "fix", Start: 3, End: 3, Line: 3})); code != "not_found" {
		t.Errorf("fix with no problem: %s", code)
	}
	r.m.problems = &problemsPanel{items: []nvim.Diagnostic{{Path: filepath.Join(r.root, "calc", "add.go"), Line: 4, Col: 2, Severity: 1, Message: "wrong operator"}}}
	f.reply = "```go\nfunc add(a, b int) int {\n\treturn a + b\n}\n```"
	out = webAIActionRes(t, act(webproto.AIActionArgs{Kind: "fix", Start: 3, End: 3, Line: 3}))
	if out.None || out.Start != 0 || out.End != 5 || strings.Join(out.Lines, "|") != "func add(a, b int) int {|\treturn a + b|}" {
		t.Fatalf("fix: %+v", out)
	}
	if c := f.requests()[1].Messages[1].Content; !strings.Contains(c, "line 4: [error] wrong operator") {
		t.Errorf("fix prompt: %q", c)
	}

	f.reply = "```go\n\treturn a - b\n```"
	out = webAIActionRes(t, act(webproto.AIActionArgs{Kind: "edit", Start: 3, End: 4, Line: 3, Instruction: "keep it"}))
	if !out.None || out.Title != "AI edit: keep it" {
		t.Fatalf("no change: %+v", out)
	}
	if c := f.requests()[2].Messages[1].Content; !strings.Contains(c, "Instruction: keep it") {
		t.Errorf("edit prompt: %q", c)
	}
	if got := r.h.filesState().bufs[2].rows; strings.Join(got, "\n") != strings.Join(rows, "\n") {
		t.Fatalf("the buffer changed: %q", got)
	}
}

// Spec (WEBP-3): Explain puts the selection (or the lines around the
// caret) into the laptop's open chat as an attachment and asks there; the
// page is told which chat to show. Not while the AI answers; never for a
// file that may hold secrets; AI not set up is a conflict.
func TestWebAIActionExplainAsksInTheChat(t *testing.T) {
	r, f := webAIRig(t, "calc/add.go", "package main", "func add(a, b int) int {", "\treturn a + b", "}")
	f.reply = "It adds."
	out := webAIActionRes(t, r.call(webproto.OpAIAction, &webproto.AIActionArgs{Buf: 2, Tick: 7, Kind: "explain", Start: 1, End: 3, Line: 1}))
	s := r.m.aiS()
	if !out.Chat || out.ChatID != s.conv.ID {
		t.Fatalf("explain: %+v (chat %q)", out, s.conv.ID)
	}
	if n := len(s.conv.Messages); n == 0 || s.conv.Messages[0].Text != "Explain this code." {
		t.Fatalf("chat: %+v", s.conv.Messages)
	}
	if !s.streaming {
		t.Fatal("the chat does not answer")
	}
	if code := webErrCode(r.call(webproto.OpAIAction, &webproto.AIActionArgs{Buf: 2, Tick: 7, Kind: "explain", Start: 1, End: 1, Line: 1})); code != "busy" {
		t.Errorf("while answering: %s", code)
	}
	s.streaming = false
	r.h.filesState().bufs[2].path = "id_rsa.pem"
	if code := webErrCode(r.call(webproto.OpAIAction, &webproto.AIActionArgs{Buf: 2, Tick: 7, Kind: "doc", Start: 1, End: 1, Line: 1})); code != "forbidden" {
		t.Errorf("secret file: %s", code)
	}
	s.provider = nil
	if code := webErrCode(r.call(webproto.OpAIAction, &webproto.AIActionArgs{Buf: 2, Tick: 7, Kind: "doc", Start: 1, End: 1, Line: 1})); code != "conflict" {
		t.Errorf("not set up: %s", code)
	}
}
