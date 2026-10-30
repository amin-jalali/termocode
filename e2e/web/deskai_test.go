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
// at. The fake hub plays the laptop.

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
