//go:build browser

package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The laptop's AI in the desktop editor (WEBP-3): ghost text and next
// edit suggestions asked of the hub at the tick the page's text is known
// at, the editor's Explain / Fix / Edit / Doc, the AI's suggestion as a
// diff tab to accept or reject, the agent worktrees side view and the
// chat's profile and model picker. The fake hub plays the laptop.

// aiFixture is the ai topic: AI set up with ghost text and next edits as
// given.
func aiFixture(inline, nes bool) webproto.AISnap {
	return webproto.AISnap{Configured: true, Provider: "Anthropic", Model: "claude-sonnet-4-5", Models: []string{"claude-sonnet-4-5", "claude-haiku-4-5"},
		Profile: "default", Profiles: []webproto.AIProfile{{Name: "default", Source: "builtin", Trust: "use", Hint: "one step"}, {Name: "review", Source: "builtin", Trust: "use"}, {Name: "repo", Source: "project", Trust: "ask"}},
		Inline: inline, NextEdit: nes, Worktrees: true}
}

// aiLaptop answers the page's requests as the laptop does: buf.set_text
// moves the tick on; the ai.* answers come from the test.
type aiLaptop struct {
	t        *testing.T
	c        *HubConn
	tick     int
	complete func(a webproto.AICompleteArgs) webproto.AICompletion
	next     func(a webproto.AINextEditArgs) webproto.AINextEdit
	sets     []webproto.SetTextArgs
	seen     map[string]int
}

func newAILaptop(t *testing.T, c *HubConn) *aiLaptop {
	none := func(webproto.AINextEditArgs) webproto.AINextEdit {
		return webproto.AINextEdit{None: true, Old: []string{}, Lines: []string{}}
	}
	return &aiLaptop{t: t, c: c, tick: 3, next: none, seen: map[string]int{},
		complete: func(webproto.AICompleteArgs) webproto.AICompletion { return webproto.AICompletion{None: true} }}
}

// serveUntil answers the page's requests until done says so.
func (l *aiLaptop) serveUntil(done func() bool) {
	l.t.Helper()
	end := time.Now().Add(wait)
	for !done() {
		if time.Now().After(end) {
			l.t.Fatalf("still waiting; seen %v, sets %+v", l.seen, l.sets)
		}
		var m webproto.Msg
		select {
		case m = <-l.c.in:
		case <-time.After(time.Until(end)):
			continue
		}
		r, ok := m.(*webproto.Req)
		if !ok {
			continue
		}
		if r.Op == "lsp.complete" || r.Op == "lsp.hover" { // no language server for this file
			l.seen[r.Op]++
			l.c.Send(l.t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeNotFound, Msg: "No language server runs for this file."})
			continue
		}
		if paintOps[r.Op] {
			continue
		}
		l.seen[r.Op]++
		switch r.Op {
		case "buf.set_text":
			var a webproto.SetTextArgs
			if err := json.Unmarshal(r.Args, &a); err != nil {
				l.t.Fatal(err)
			}
			if a.Tick != l.tick {
				l.t.Fatalf("set_text at tick %d, the laptop is at %d", a.Tick, l.tick)
			}
			l.sets = append(l.sets, a)
			l.tick++
			reply(l.t, l.c, r, webproto.EditRes{Tick: l.tick})
		case "ai.complete":
			var a webproto.AICompleteArgs
			if err := json.Unmarshal(r.Args, &a); err != nil {
				l.t.Fatal(err)
			}
			if a.Tick != l.tick {
				l.t.Fatalf("ai.complete at tick %d, the laptop is at %d", a.Tick, l.tick)
			}
			reply(l.t, l.c, r, l.complete(a))
		case "ai.next_edit":
			var a webproto.AINextEditArgs
			if err := json.Unmarshal(r.Args, &a); err != nil {
				l.t.Fatal(err)
			}
			if a.Seed {
				l.seen["seed"]++
			}
			reply(l.t, l.c, r, l.next(a))
		case "buf.close":
			l.c.Send(l.t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
		default:
			l.t.Fatalf("page sent %s", show(r))
		}
	}
}

// openAIDesk opens path on the desktop with the AI as given.
func openAIDesk(t *testing.T, inline, nes bool, path string, lines ...string) (*Page, *HubConn, *aiLaptop) {
	t.Helper()
	p, c := openFiles(t, Desktop)
	c.Send(t, Snap("ai", 1, aiFixture(inline, nes)))
	openDesk(t, p, c, 2, path, lines...)
	l := newAILaptop(t, c)
	if nes { // the editor opened: the laptop keeps its first copy
		l.serveUntil(func() bool { return l.seen["seed"] > 0 })
	}
	return p, c, l
}

// Typing pauses: the page asks the laptop for ghost text at the caret,
// after its own text reached the laptop; the answer shows dim after the
// caret and Tab takes it (one edit sent to the laptop).
func TestDeskAIGhostText(t *testing.T) {
	p, _, l := openAIDesk(t, true, false, "main.go", "package main", "", "func main() {", "    fmt.Pri", "}")
	var asked webproto.AICompleteArgs
	l.complete = func(a webproto.AICompleteArgs) webproto.AICompletion {
		asked = a
		if a.Line != 3 || a.Col != 12 {
			return webproto.AICompletion{None: true}
		}
		return webproto.AICompletion{Line: 3, From: 12, To: 12, Text: `tln("hi")`}
	}
	deskCaret(p, 3, 7)
	p.Do(chromedp.KeyEvent("n"))
	l.serveUntil(func() bool { return len(l.sets) == 1 && l.seen["ai.complete"] > 0 && asked.Col == 12 })
	if asked.Buf != 2 || asked.Manual {
		t.Errorf("ai.complete %+v", asked)
	}
	p.WaitJS(deskDoc + `.querySelector(".ghost-text-decoration, .ghost-text")?.textContent.includes('tln("hi")')`)
	p.Do(chromedp.KeyEvent(kb.Tab))
	l.complete = func(webproto.AICompleteArgs) webproto.AICompletion { return webproto.AICompletion{None: true} }
	l.serveUntil(func() bool { return len(l.sets) == 2 })
	if got := strings.Join(l.sets[1].Lines, "\n"); !strings.Contains(got, `tln("hi")`) || strings.Contains(got, "\t") {
		t.Errorf("Tab sent %+v", l.sets[1])
	}
	p.WaitJS(`(` + deskDoc + `.querySelector(".view-lines").textContent.includes('fmt.Println("hi")'))`)
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}

// After an edit and a pause the page asks the laptop for the next edit;
// the rows it would change are marked and the new text shows under them.
// Tab writes it; Esc drops it and nothing is sent.
func TestDeskAINextEdit(t *testing.T) {
	p, c, l := openAIDesk(t, false, true, "a.go", "func f() int {", "    oldName := 1", "    return oldName", "}")
	l.next = func(a webproto.AINextEditArgs) webproto.AINextEdit {
		if a.Seed {
			return webproto.AINextEdit{None: true, Old: []string{}, Lines: []string{}}
		}
		return webproto.AINextEdit{Start: 2, End: 2, Old: []string{"    return oldName"}, Lines: []string{"    return newName"}}
	}
	deskCaret(p, 1, 1)
	p.Do(chromedp.KeyEvent("x"))
	l.serveUntil(func() bool { return len(l.sets) == 1 && l.seen["ai.next_edit"] > 1 })
	p.WaitJS(deskDoc + `.querySelector(".tc-nes")?.textContent.includes("return newName") && ` + deskDoc + `.querySelector(".tc-nes-old")`)
	if got := Eval[string](p, deskDoc+`.querySelector(".tc-nes-hint").textContent`); got != "Tab to accept · Esc to dismiss" {
		t.Errorf("hint %q", got)
	}
	p.Do(chromedp.KeyEvent(kb.Escape))
	p.WaitJS(`!` + deskDoc + `.querySelector(".tc-nes")`)
	noReq(t, c, 600*time.Millisecond)

	p.Do(chromedp.KeyEvent("y"))
	l.serveUntil(func() bool { return len(l.sets) == 2 && l.seen["ai.next_edit"] > 2 })
	p.WaitJS(deskDoc + `.querySelector(".tc-nes")`)
	p.Do(chromedp.KeyEvent(kb.Tab))
	l.next = func(webproto.AINextEditArgs) webproto.AINextEdit {
		return webproto.AINextEdit{None: true, Old: []string{}, Lines: []string{}}
	}
	l.serveUntil(func() bool { return len(l.sets) == 3 })
	if a := l.sets[2]; a.Start.Row != 2 || strings.Join(a.Lines, "\n") != "new" { // "old" → "new" in "return oldName"
		t.Errorf("Tab sent %+v", a)
	}
	p.WaitJS(`!` + deskDoc + `.querySelector(".tc-nes") && ` + deskDoc + `.querySelector(".view-lines").textContent.replace(/\u00a0/g, " ").includes("return newName")`)
}

// aiNext reads the page's next request, which must be op: subs, unsubs
// and paint requests on the way are skipped, a buf.close answered.
func aiNext(t *testing.T, c *HubConn, op string) *webproto.Req {
	t.Helper()
	for {
		switch m := c.Next(t).(type) {
		case *webproto.Sub, *webproto.Unsub:
		case *webproto.Req:
			if paintOps[m.Op] {
				continue
			}
			if m.Op == "buf.close" {
				c.Send(t, &webproto.Res{T: webproto.KindRes, ID: m.ID})
				continue
			}
			if m.Op != op {
				t.Fatalf("page sent %s; want %s", show(m), op)
			}
			return m
		default:
			t.Fatalf("page sent %s; want %s", show(m), op)
		}
	}
}

// expectAIAction reads the page's next ai.action (other requests fail).
func expectAIAction(t *testing.T, c *HubConn) (*webproto.Req, webproto.AIActionArgs) {
	t.Helper()
	r := aiNext(t, c, "ai.action")
	var a webproto.AIActionArgs
	if err := json.Unmarshal(r.Args, &a); err != nil {
		t.Fatal(err)
	}
	return r, a
}

// Edit with AI (Ctrl/Cmd+I) asks what to change, then shows the AI's
// lines as a diff tab: the file on the left, the suggestion on the right
// with an arrow per change. Accept writes it in one edit at the tick it
// was asked at, and the file's tab comes back.
func TestDeskAIEditAccept(t *testing.T) {
	p, c, _ := openAIDesk(t, false, false, "calc.go", "package calc", "", "func Add(a, b int) int {", "    return a - b", "}")
	deskCaret(p, 3, 1)
	p.Do(chromedp.KeyEvent("i", modKey()))
	p.WaitJS(`document.querySelector(".quick-box")?.placeholder === "Edit with AI: what should change?"`)
	p.Do(chromedp.KeyEvent("use plus"))
	p.Do(chromedp.KeyEvent(kb.Enter))
	r, a := expectAIAction(t, c)
	if a.Buf != 2 || a.Tick != 3 || a.Kind != "edit" || a.Instruction != "use plus" || a.Start != 3 || a.End != 3 || a.Line != 3 {
		t.Fatalf("ai.action %+v", a)
	}
	reply(t, c, r, webproto.AIAction{Kind: "edit", Title: "AI edit: use plus", Start: 3, End: 4, Lines: []string{"    return a + b"}})
	p.WaitJS(`document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent === "calc.go (AI Suggestion)"`)
	view := `document.querySelector('[data-view="ai-diff"]')`
	doc := view + `.querySelector(".desk-editor").contentDocument`
	p.WaitJS(doc + `?.querySelector(".editor.modified .char-insert, .editor.modified .line-insert")`)
	if got := Eval[bool](p, doc+`.querySelector(".editor.original .view-lines").textContent.replace(/\u00a0/g, " ").includes("return a - b")`); !got {
		t.Error("the left side is not the file")
	}
	p.TapJS(view + `.querySelector('[data-act="accept"]')`)
	r2 := aiNext(t, c, "buf.set_text")
	var set webproto.SetTextArgs
	if err := json.Unmarshal(r2.Args, &set); err != nil {
		t.Fatal(err)
	}
	if set.Buf != 2 || set.Tick != 3 || strings.Join(set.Lines, "\n") != "+" {
		t.Errorf("accept sent %+v", set)
	}
	reply(t, c, r2, webproto.EditRes{Tick: 4})
	// The file's tab comes back (its editor opens the buffer again).
	answerOpen(t, p, c, 2, "calc.go", "package calc", "", "func Add(a, b int) int {", "    return a + b", "}")
	p.WaitJS(`document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent === "calc.go" && !document.querySelector('[data-view="ai-diff"]')`)
	if probs := p.Problems(); len(probs) > 0 {
		t.Errorf("problems (CSP, console): %v", probs)
	}
}

// The editor's menu has the AI's actions when the laptop has AI. Doc
// shows its suggestion; Reject drops it and sends nothing. Explain asks
// in the laptop's chat, which comes into view.
func TestDeskAIMenuDocRejectAndExplain(t *testing.T) {
	p, c, _ := openAIDesk(t, false, false, "calc.go", "package calc", "func Add(a, b int) int { return a + b }")
	deskCaret(p, 1, 0)
	editorRightClick(p)
	if got := menuLabels(p); !strings.Contains(got, "Paste|Explain with AI|Fix with AI|Edit with AI…|Generate Doc Comment with AI|Command Palette…") {
		t.Errorf("editor menu %q", got)
	}
	menuPick(p, "Generate Doc Comment with AI")
	r, a := expectAIAction(t, c)
	if a.Kind != "doc" || a.Line != 1 || a.Start != 1 || a.End != 1 || a.Instruction != "" {
		t.Fatalf("doc %+v", a)
	}
	reply(t, c, r, webproto.AIAction{Kind: "doc", Title: "AI doc comment", Start: 1, End: 2, Lines: []string{"// Add adds.", "func Add(a, b int) int { return a + b }"}})
	view := `document.querySelector('[data-view="ai-diff"]')`
	p.WaitJS(view + `?.querySelector(".desk-editor")`)
	p.TapJS(view + `.querySelector('[data-act="reject"]')`)
	answerOpen(t, p, c, 2, "calc.go", "package calc", "func Add(a, b int) int { return a + b }") // nothing else: no edit
	p.WaitJS(`!document.querySelector('[data-view="ai-diff"]') && document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent === "calc.go"`)
	noReq(t, c, 400*time.Millisecond)
	deskCaret(p, 1, 0)

	Eval[bool](p, `(document.dispatchEvent(new KeyboardEvent("keydown", {key: "b", ctrlKey: true, bubbles: true})), true)`) // the side bar away: room
	editorRightClick(p)
	menuPick(p, "Explain with AI")
	r, a = expectAIAction(t, c)
	if a.Kind != "explain" {
		t.Fatalf("explain %+v", a)
	}
	reply(t, c, r, webproto.AIAction{Kind: "explain", Chat: true, ChatID: "c-1", Lines: []string{}})
	p.WaitJS(`document.querySelector(".chatcol")`)
}

// The chat's header shows the laptop's profile and model; picking
// another asks here first (Cancel first), then the laptop.
func TestDeskAIPicker(t *testing.T) {
	p, c := openFiles(t, Desktop)
	c.Send(t, Snap("ai", 1, aiFixture(true, true)))
	pick := `document.querySelector('.ai-pick[data-pick="profile"]')`
	p.WaitJS(pick + `?.textContent.includes("default") && document.querySelector('.ai-pick[data-pick="model"]')?.textContent.includes("claude-sonnet-4-5")`)
	p.TapJS(pick)
	p.WaitJS(`document.querySelector(".ctx-menu .ctx-item")`)
	if got := menuLabels(p); got != "default|review|repo" {
		t.Errorf("profiles %q", got)
	}
	if got := Eval[string](p, `[...document.querySelectorAll(".ctx-menu .ctx-item")].filter((b) => b.disabled).map((b) => b.querySelector(".ctx-label").textContent).join("|")`); got != "default|repo" {
		t.Errorf("off: %q (the one in use and the untrusted one)", got)
	}
	menuPick(p, "review")
	p.WaitJS(`document.querySelector(".ask-text")?.textContent === "Use profile review for the next chat messages?"`)
	p.WaitJS(`document.activeElement?.textContent === "Cancel"`) // the safe choice first and focused
	noReq(t, c, 300*time.Millisecond)
	p.WaitJS(`document.querySelector(".ask .btn-danger.is-armed")`) // it arms after 600 ms (web UX 3.9)
	p.TapJS(`document.querySelector(".ask .btn-danger")`)
	r := expectReq(t, c, "ai.use_profile")
	if string(r.Args) != `{"name":"review"}` {
		t.Errorf("args %s", r.Args)
	}
	reply(t, c, r, webproto.AskRef{Card: "k1"})
	p.WaitJS(`document.querySelector(".waiting, .wait")`)
}

// The agent worktrees view lists each worktree with its branch, owner
// and changed files; a file opens as a read-only diff tab of the agent's
// change; Merge and Discard ask the laptop.
func TestDeskAgentWorktrees(t *testing.T) {
	p, c := openFiles(t, Desktop)
	p.TapJS(`document.querySelector('nav.act-bar .act-item[data-act="agents"]')`)
	r := expectReq(t, c, "ai.worktrees")
	reply(t, c, r, webproto.AIWorktrees{Items: []webproto.AIWorktree{
		{ID: "r1-left", Branch: "termocode/agent-r1-left", Owner: "Left", Files: []webproto.AIWorktreeChange{{Path: "src/a.go", Status: "M"}, {Path: "b.txt", Status: "A"}}},
		{ID: "r1-right", Branch: "termocode/agent-r1-right", Running: true, Files: []webproto.AIWorktreeChange{}},
	}})
	wt := `document.querySelector('.agent-wt[data-id="r1-left"]')`
	p.WaitJS(wt + `?.querySelectorAll(".agent-file").length === 2`)
	if got := Eval[string](p, wt+`.querySelector(".agent-head").textContent`); !strings.Contains(got, "termocode/agent-r1-left") || !strings.HasPrefix(got, "Left") {
		t.Errorf("head %q", got)
	}
	if Eval[bool](p, `!!document.querySelector('.agent-wt[data-id="r1-right"] [data-act="merge"]')`) {
		t.Error("a running worktree offers Merge")
	}
	p.TapJS(wt + `.querySelector('[data-act="discard"]')`)
	p.WaitJS(`document.querySelector(".ask-text")?.textContent.includes("termocode/agent-r1-left")`)
	noReq(t, c, 300*time.Millisecond)
	p.TapJS(`[...document.querySelectorAll(".ask .btn")].find((b) => b.textContent === "Cancel")`)
	p.WaitJS(`!document.querySelector(".ask")`)
	noReq(t, c, 300*time.Millisecond)
	p.TapJS(wt + `.querySelector('.agent-file[data-path="src/a.go"]')`)
	r = expectReq(t, c, "ai.worktree_text")
	if string(r.Args) != `{"id":"r1-left","path":"src/a.go"}` {
		t.Errorf("args %s", r.Args)
	}
	reply(t, c, r, webproto.AIWorktreeText{ID: "r1-left", Path: "src/a.go", Before: "package a\nvar x = 1\n", After: "package a\nvar x = 2\n"})
	p.WaitJS(`document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent === "a.go (Agent)"`)
	doc := `document.querySelector('[data-view="ai-diff"] .desk-editor').contentDocument`
	p.WaitJS(doc + `?.querySelector(".editor.modified .char-insert, .editor.modified .line-insert")`)
	// The file opens beside the view: the side bar keeps the agents.
	if !Eval[bool](p, `!!document.querySelector(".wb-side .agent-wt") && !!document.querySelector('nav.act-bar .act-item.is-active[data-act="agents"]')`) {
		t.Error("opening an agent's file left the agents view in the side bar")
	}
}
