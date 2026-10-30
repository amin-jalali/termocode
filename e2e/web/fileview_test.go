//go:build browser

package web

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The file view (WEB-18, web UX 5.5): the laptop's buffer, live, LTR and
// isolated rows with numbers; files the web never shows say what they are.

// goFile moves the page to a file's view and reads its buf.open.
func goFile(t *testing.T, p *Page, c *HubConn, path string) *webproto.Req {
	t.Helper()
	Eval[bool](p, `(location.hash = `+jsq("#/files/"+path)+`, true)`)
	r := expectReq(t, c, "buf.open")
	var a webproto.PathArgs
	if err := json.Unmarshal(r.Args, &a); err != nil || a.Path != path {
		t.Fatalf("buf.open args %s, want %q", r.Args, path)
	}
	return r
}

func bufSnap(buf int, path string, lines ...string) map[string]any {
	return map[string]any{"buf": buf, "path": path, "tick": 3, "lines": lines, "dirty": false, "ft": "go", "readonly": false}
}

// openBuf opens path as buffer buf with lines and waits for the rows.
func openBuf(t *testing.T, p *Page, c *HubConn, buf int, path string, lines ...string) {
	t.Helper()
	r := goFile(t, p, c, path)
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"buf": buf, "path": path, "tick": 3, "lines": len(lines), "dirty": false, "ft": "go", "readonly": false})})
	topic := fmt.Sprint("buf:", buf)
	c.WaitSub(t, topic)
	c.Send(t, Snap(topic, 1, bufSnap(buf, path, lines...)))
	p.WaitJS(filesView + `.querySelector(".crow")`)
}

func codeRow(i int) string {
	return filesView + fmt.Sprintf(`.querySelector('.crow[data-i="%d"]')`, i)
}

func TestFileViewShowsLinesLive(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openFiles(t, vp)
			openBuf(t, p, c, 2, "src/a.go", "package main", `// <script>window.__x=1</script>`, "s := \"سلام دنیا\" // greet", "func main() {}")
			if got := Eval[string](p, filesView+`.querySelector(".view-title").textContent`); got != "a.go" {
				t.Errorf("title %q, want the file name", got)
			}
			if got := Eval[string](p, codeRow(3)+`.querySelector(".crow-n").textContent`); got != "4" {
				t.Errorf("line number %q, want 4", got)
			}
			if Eval[bool](p, filesView+`.querySelector("script") !== null || window.__x === 1`) {
				t.Fatal("a script tag in the file became an element")
			}
			cs := Eval[[]string](p, `(() => { document.documentElement.dir = "rtl"; const s = getComputedStyle(`+codeRow(2)+`); const r = [s.direction, s.unicodeBidi]; document.documentElement.dir = "ltr"; return r; })()`)
			if cs[0] != "ltr" || (cs[1] != "isolate" && cs[1] != "isolate-override") {
				t.Errorf("a code row runs %v, want ltr isolated", cs)
			}

			// The laptop changes line 2: it flashes and a line says so.
			c.Send(t, Delta("buf:2", 2, map[string]any{"kind": "lines", "first": 1, "last": 2, "lines": []string{"// changed on the laptop"}, "tick": 4}))
			p.WaitJS(codeRow(1) + `?.classList.contains("is-flash") && ` + codeRow(1) + `.innerText.includes("changed on the laptop")`)
			p.WaitJS(filesView + `.querySelector(".file-updated")?.textContent === "Updated from the laptop"`)
			if Eval[bool](p, codeRow(0)+`.classList.contains("is-flash")`) {
				t.Error("an unchanged row flashed")
			}
			c.Send(t, Delta("buf:2", 3, map[string]any{"kind": "dirty", "on": true}))
			p.WaitJS(filesView + `.querySelector(".view-actions .chip.tone-amber")?.innerText.includes("Not saved")`)

			// The laptop closes it: the rows stay, a line says so.
			c.Send(t, Delta("buf:2", 4, map[string]any{"kind": "gone"}))
			p.WaitJS(filesView + `.querySelector(".file-note")?.innerText.includes("This file was closed on the laptop.")`)

			// Back to the tree: the page stops following and drops its reference.
			p.TapJS(filesView + `.querySelector(".head-back")`)
			var unsub, closed bool
			for !unsub || !closed {
				switch m := c.Next(t).(type) {
				case *webproto.Unsub:
					unsub = unsub || slices.Contains(m.Topics, "buf:2")
				case *webproto.Req:
					if m.Op == "buf.close" {
						var a webproto.BufArgs
						_ = json.Unmarshal(m.Args, &a)
						closed = a.Buf == 2
						c.Send(t, &webproto.Res{T: webproto.KindRes, ID: m.ID})
					}
				}
			}
			p.WaitJS(`location.hash === "#/files"`)
		})
	}
}

// A file too big to open shows its first lines read-only (buf.head); a
// binary file and a file that may hold secrets are never sent and say so.
func TestFileViewBigBinarySecret(t *testing.T) {
	p, c := openFiles(t, Phone)
	r := goFile(t, p, c, "big.log")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeLimit, Msg: "too big", Data: raw(map[string]any{"reason": "big", "size": 9 << 20})})
	h := expectReq(t, c, "buf.head")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: h.ID, Data: raw(map[string]any{"path": "big.log", "lines": []string{"first", "second"}, "truncated": true, "size": 9 << 20})})
	p.WaitJS(filesView + `.querySelector(".file-note")?.innerText.includes("Showing the first 2 000 lines, read-only.")`)
	p.WaitJS(codeRow(1) + `?.innerText.includes("second")`)
	if !Eval[bool](p, filesView+`.querySelector(".file-note").innerText.includes("9.0 MB")`) {
		t.Error("the big file's size is not shown")
	}

	r = goFile(t, p, c, "app.bin")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeLimit, Msg: "binary", Data: raw(map[string]any{"reason": "binary", "size": 2048})})
	p.WaitJS(filesView + `.innerText.includes("This is not a text file.") && ` + filesView + `.innerText.includes("2 KB")`)

	r = goFile(t, p, c, ".env")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeForbidden, Msg: "secret"})
	p.WaitJS(filesView + `.innerText.includes("This file may hold secrets. Open it on the laptop.")`)
	if Eval[bool](p, filesView+`.querySelector(".crow") !== null`) {
		t.Error("rows show for a refused file")
	}
}

// A long file draws only the rows near the screen; scrolling to the end
// draws the last line and a line chip tells where the reader is.
func TestFileViewVirtualizesLongFiles(t *testing.T) {
	p, c := openFiles(t, Desktop)
	lines := make([]string, 5000)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d of the long file", i+1)
	}
	openBuf(t, p, c, 4, "long.txt", lines...)
	if n := Eval[int](p, filesView+`.querySelectorAll(".crow").length`); n >= 1000 {
		t.Errorf("drew %d rows of 5000; want only the ones near the screen", n)
	}
	// The chip shows while scrolling: keep scrolling until it is seen.
	p.WaitJS(`(() => { const b = ` + filesView + `.querySelector(".view-body"); b.scrollTop += 400; return ` + filesView + `.querySelector(".line-chip")?.textContent.includes("of 5000") === true; })()`)
	p.WaitJS(`(() => { const b = ` + filesView + `.querySelector(".view-body"); b.scrollTop = b.scrollHeight; return ` + codeRow(4999) + ` !== null; })()`)
}
