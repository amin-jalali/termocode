//go:build browser

package web

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Syntax colours (WEB-38): the page asks buf.hl for the rows it draws and
// paints nvim's groups as theme classes; an answer for an old tick is
// dropped, a changed row is plain until the next answer.

func expectHl(t *testing.T, c *HubConn) (*webproto.Req, webproto.HlArgs) {
	t.Helper()
	r := expectReq(t, c, "buf.hl")
	var a webproto.HlArgs
	if err := json.Unmarshal(r.Args, &a); err != nil {
		t.Fatal(err)
	}
	return r, a
}

func hlRes(id uint64, tick int, first, last int, spans ...[]int) *webproto.Res {
	return &webproto.Res{T: webproto.KindRes, ID: id, Data: raw(map[string]any{
		"tick": tick, "first": first, "last": last, "groups": []string{"keyword", "string", "variable", "comment"}, "spans": spans})}
}

func TestFileSyntaxColours(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openFiles(t, vp)
			openBuf(t, p, c, 2, "a.lua", `local s = "hi"`, "-- note", "return s")
			r, a := expectHl(t, c)
			if a.Buf != 2 || a.First != 0 || a.Last != 3 {
				t.Fatalf("buf.hl args %+v", a)
			}
			c.Send(t, hlRes(r.ID, 3, 0, 3, []int{0, 0, 5, 0}, []int{0, 6, 7, 2}, []int{0, 10, 14, 1}, []int{1, 0, 7, 3}, []int{2, 0, 6, 0}))
			p.WaitJS(codeRow(0) + `.querySelector(".hl-k")?.textContent === "local"`)
			if got := Eval[string](p, codeRow(0)+`.querySelector(".hl-s").textContent`); got != `"hi"` {
				t.Errorf("string run %q", got)
			}
			if Eval[bool](p, codeRow(0)+`.querySelector('[class="hl-"]') !== null`) {
				t.Error("a variable got a colour class")
			}
			if got := Eval[string](p, codeRow(0)+`.querySelector(".crow-text").textContent`); got != `local s = "hi"` {
				t.Errorf("row text %q after painting", got)
			}
			if Eval[string](p, `getComputedStyle(`+codeRow(1)+`.querySelector(".hl-c")).fontStyle`) != "italic" {
				t.Error("a comment is not italic")
			}
			// The laptop changes row 1: it is plain now, the others keep
			// their colours, and the page asks again for the new tick.
			c.Send(t, Delta("buf:2", 2, map[string]any{"kind": "lines", "first": 1, "last": 2, "lines": []string{"x = 1"}, "tick": 4}))
			p.WaitJS(codeRow(1) + `.innerText.includes("x = 1") && !` + codeRow(1) + `.querySelector("[class^=hl-]")`)
			if !Eval[bool](p, codeRow(2)+`.querySelector(".hl-k") !== null`) {
				t.Error("an unchanged row lost its colours")
			}
			r, a = expectHl(t, c)
			// An answer for an old tick paints nothing.
			c.Send(t, hlRes(r.ID, 3, 0, 3, []int{1, 0, 1, 0}))
			p.Holds(`!`+codeRow(1)+`.querySelector(".hl-k")`, 300*time.Millisecond)
			// Then the right one comes.
			c.Send(t, Delta("buf:2", 3, map[string]any{"kind": "lines", "first": 2, "last": 3, "lines": []string{"return x"}, "tick": 5}))
			r, _ = expectHl(t, c)
			c.Send(t, hlRes(r.ID, 5, 0, 3, []int{1, 0, 1, 2}, []int{2, 0, 6, 0}))
			p.WaitJS(codeRow(2) + `.querySelector(".hl-k")?.textContent === "return"`)
			if len(p.Problems()) > 0 {
				t.Errorf("problems: %v", p.Problems())
			}
		})
	}
}

// A file with no parser asks once per tick and stays plain.
func TestFileSyntaxNoneStaysPlain(t *testing.T) {
	p, c := openFiles(t, Phone)
	openBuf(t, p, c, 2, "n.txt", "words", "more")
	r, _ := expectHl(t, c)
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"tick": 3, "first": 0, "last": 2, "groups": []string{}, "spans": [][]int{}, "none": true})})
	noPaint(t, c, "buf.hl", 400*time.Millisecond)
	if Eval[bool](p, codeRow(0)+`.querySelector("[class^=hl-]") !== null`) {
		t.Error("plain text got colours")
	}
}

// noPaint fails when the page sends op within d.
func noPaint(t *testing.T, c *HubConn, op string, d time.Duration) {
	t.Helper()
	end := time.After(d)
	for {
		select {
		case m := <-c.in:
			if r, ok := m.(*webproto.Req); ok && r.Op == op {
				t.Fatalf("page sent %s again", show(m))
			}
		case <-end:
			return
		}
	}
}
