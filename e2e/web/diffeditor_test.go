//go:build browser

package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webhl"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop's diff editor: a changed file opens as an editor tab with
// the older text on the left (read only) and the newer on the right,
// changed rows and words marked, unchanged regions folded, Previous and
// Next Change, side by side or inline. The working tree's side is the
// laptop's live buffer and takes typing.

// Wide is a big desktop screen: the diff editor goes side by side only
// with 900 px or more for itself (as the laptop's editor does).
var Wide = Viewport{Name: "desktop", W: 1920, H: 1080, Scale: 1}

const diffView = `document.querySelector('[data-view="diff"]')`
const diffDoc = diffView + `?.querySelector(".desk-editor")?.contentDocument`

// answerGitText answers the page's git.text requests (and a buf.open of
// path as buffer buf, when lines is not nil) until every revision of
// texts was asked once; other messages go by.
func answerGitText(t *testing.T, c *HubConn, path string, texts map[string]string, buf int, lines []string) {
	t.Helper()
	want := len(texts)
	if lines != nil {
		want++
	}
	end := time.Now().Add(wait)
	for want > 0 {
		if time.Now().After(end) {
			t.Fatalf("still waiting for %d requests", want)
		}
		r, ok := c.Next(t).(*webproto.Req)
		if !ok {
			continue
		}
		switch r.Op {
		case "git.text":
			var a webproto.GitTextArgs
			if err := json.Unmarshal(r.Args, &a); err != nil || a.Path != path {
				t.Fatalf("git.text args %s", r.Args)
			}
			text, ok := texts[a.Rev]
			if !ok {
				t.Fatalf("git.text asked for rev %q", a.Rev)
			}
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(webproto.GitText{Path: path, Rev: a.Rev, Text: text})})
			want--
		case "buf.open":
			if lines == nil {
				t.Fatalf("a buf.open for a read-only diff: %s", r.Args)
			}
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"buf": buf, "path": path, "tick": 3, "lines": len(lines), "dirty": false, "ft": "go", "readonly": false})})
			topic := "buf:" + itoa(buf)
			waitSubSkipping(t, c, topic)
			c.Send(t, Snap(topic, 1, bufSnap(buf, path, lines...)))
			want--
		case "buf.close":
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID})
		case "buf.hl":
			answerHl(t, c, r, path, lines)
		}
	}
}

// serveHlUntil answers buf.hl requests until js is true.
func serveHlUntil(t *testing.T, p *Page, c *HubConn, path string, lines []string, js string) {
	t.Helper()
	end := time.Now().Add(wait)
	for !Eval[bool](p, "!!("+js+")") {
		if time.Now().After(end) {
			t.Fatalf("waited %s for %s", wait, js)
		}
		select {
		case m := <-c.in:
			if r, ok := m.(*webproto.Req); ok && r.Op == "buf.hl" {
				answerHl(t, c, r, path, lines)
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// answerHl colours lines as the hub's own lexer does.
func answerHl(t *testing.T, c *HubConn, r *webproto.Req, path string, lines []string) {
	t.Helper()
	var a webproto.HlArgs
	_ = json.Unmarshal(r.Args, &a)
	res, _ := webhl.Chroma(path, "go", lines, 3, a.First, a.Last, webproto.MaxHlSpans)
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(res)})
}

// diffSide is the text one side of the diff editor shows.
func diffSide(p *Page, side string) string {
	return Eval[string](p, `[...`+diffDoc+`.querySelectorAll(".editor.`+side+` .view-lines .view-line")].sort((a, b) => a.offsetTop - b.offsetTop).map((l) => l.textContent.replace(/\u00a0/g, " ")).join("\n")`)
}

// From source control: a changed file opens as "name (Working Tree)",
// the index on the left, the live buffer on the right; typing on the
// right edits the laptop's buffer; F7 goes to the change; Inline View
// makes one column.
func TestDiffEditorWorkingTree(t *testing.T) {
	p, c := openDeskSCMAt(t, Wide, nil)
	p.TapJS(scmRow("changes", "src/a.go"))
	old := []string{"package a", "", "func A() int {", "\treturn 1", "}", "", "// one", "// two", "// three", "// four", "// five", "// six", "// seven", "// eight", "// end"}
	cur := append([]string(nil), old...)
	cur[3] = "\treturn 2"
	answerGitText(t, c, "src/a.go", map[string]string{"index": strings.Join(old, "\n") + "\n"}, 2, cur)
	p.WaitJS(diffDoc + `?.querySelector(".monaco-diff-editor.side-by-side .editor.modified .view-line")`)
	if got := Eval[string](p, `document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent ?? ""`); got != "a.go (Working Tree)" {
		t.Errorf("the tab is %q", got)
	}
	p.WaitJS(diffDoc + `.querySelector(".editor.modified .char-insert, .editor.modified .line-insert") && ` + diffDoc + `.querySelector(".editor.original .char-delete, .editor.original .line-delete")`)
	if got := diffSide(p, "original"); !strings.Contains(got, "return 1") || strings.Contains(got, "return 2") {
		t.Errorf("the left side shows %q", got)
	}
	// Both sides have the code's colours (the left one borrows them for
	// the rows the two sides share).
	serveHlUntil(t, p, c, "src/a.go", cur, diffDoc+`.querySelector(".editor.modified .view-line .hl-k") && `+diffDoc+`.querySelector(".editor.original .view-line .hl-k")`)
	// Unchanged regions fold: "// six" is far from the change.
	if strings.Contains(diffSide(p, "modified"), "// six") {
		t.Error("the unchanged rows are not folded")
	}
	// Typing on the right edits the laptop's buffer.
	Eval[bool](p, `(`+diffView+`.querySelector(".desk-editor").focus(), `+diffDoc+`.querySelector(".editor.modified .native-edit-context, .editor.modified textarea").focus(), true)`)
	p.Do(chromedp.KeyEvent(kb.Home, modKey()))
	p.Do(chromedp.KeyEvent("X"))
	r, a := expectSetText(t, c)
	if a.Buf != 2 || a.Tick != 3 || strings.Join(a.Lines, "|") != "X" {
		t.Fatalf("set_text %+v", a)
	}
	okEdit(t, c, r, 4)
	// Next Change (F7) puts the caret on the changed row.
	p.Do(chromedp.KeyEvent(kb.F7))
	p.WaitJS(`document.querySelector(".statusbar .sb-pos")?.textContent.startsWith("Ln 4,")`)
	// Inline View: one column.
	p.TapJS(`document.querySelector('.ed-actions [data-act="inline"]')`)
	p.WaitJS(`!` + diffDoc + `.querySelector(".monaco-diff-editor.side-by-side")`)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

// A staged change opens as "name (Index)": HEAD on the left, the index
// on the right, both from git and read only; no buffer is opened.
func TestDiffEditorIndexReadOnly(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	p.TapJS(scmRow("staged", "b.go"))
	answerGitText(t, c, "b.go", map[string]string{"HEAD": "package b\nvar x = 1\n", "index": "package b\nvar x = 2\n"}, 0, nil)
	p.WaitJS(diffDoc + `?.querySelector(".editor.modified .view-line")`)
	if got := Eval[string](p, `document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent ?? ""`); got != "b.go (Index)" {
		t.Errorf("the tab is %q", got)
	}
	p.WaitJS(diffDoc + `.querySelector(".editor.modified .char-insert, .editor.modified .line-insert")`)
	if !Eval[bool](p, `document.querySelector(".ed-actions").innerText.includes("View only")`) {
		t.Error("a staged diff does not say it is read only")
	}
	noReq(t, c, 300*time.Millisecond)
}

// A review card on the desktop offers Open Changes: the AI's change opens
// as "name (AI Change)", the file as it is now on the left and the change
// on the right, read only; Review… opens the card again (Reject first).
func TestDiffEditorReviewCard(t *testing.T) {
	p, c := openCardView(t, Wide, "review")
	p.TapJS(ButtonByText("Open Changes"))
	p.WaitJS("!" + dialog)
	answerBufOpen(t, c, 2, "a.go", []string{"package a", "x := 1", "y := 3"})
	p.WaitJS(diffDoc + `?.querySelector(".editor.modified .line-insert, .editor.modified .char-insert")`)
	if got := Eval[string](p, `document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent ?? ""`); got != "a.go (AI Change)" {
		t.Errorf("the tab is %q", got)
	}
	if got := diffSide(p, "modified"); got != "package a\nx := 2\ny := 3" {
		t.Errorf("the change shows %q", got)
	}
	if got := diffSide(p, "original"); got != "package a\nx := 1\ny := 3" {
		t.Errorf("the file shows %q", got)
	}
	p.TapJS(`document.querySelector('.ed-actions [data-act="review"]')`)
	p.WaitJS(dialog + ` && ` + dialog + `.contains(document.activeElement) && document.activeElement.textContent === "Reject"`)
}

// answerBufOpen answers the page's buf.open of path as buffer buf with lines.
func answerBufOpen(t *testing.T, c *HubConn, buf int, path string, lines []string) {
	t.Helper()
	answerGitText(t, c, path, map[string]string{}, buf, lines)
}
