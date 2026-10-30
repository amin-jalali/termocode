//go:build browser

package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The edit sheet (WEB-22, web UX 3.4): the selected rows and 20 around
// them, Cancel first, one buf.set_text with the tick the rows came with;
// a laptop change in between is a conflict that keeps the user's text.

const editSheet = `document.querySelector(".sheet .edit")`

func tapEdit(p *Page) {
	p.T.Helper()
	p.TapJS(`[...` + filesView + `.querySelectorAll(".view-actions .btn")].find((b) => b.textContent === "Edit lines")`)
	p.WaitJS(editSheet)
}

func expectSetText(t *testing.T, c *HubConn) (*webproto.Req, webproto.SetTextArgs) {
	t.Helper()
	r := expectReq(t, c, "buf.set_text")
	var a webproto.SetTextArgs
	if err := json.Unmarshal(r.Args, &a); err != nil {
		t.Fatal(err)
	}
	return r, a
}

// longPress holds a row for 600 ms with pointer events, as a finger does.
func longPress(p *Page, el string) {
	p.T.Helper()
	Eval[bool](p, `(() => { const el = `+el+`.querySelector(".crow-text"); const r = el.getBoundingClientRect();
		el.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true, clientX: r.left + 2, clientY: r.top + 2 }));
		setTimeout(() => el.dispatchEvent(new PointerEvent("pointerup", { bubbles: true, clientX: r.left + 2, clientY: r.top + 2 })), 600);
		return true; })()`)
}

func TestFileEditSheetSetText(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openFiles(t, vp)
			openBuf(t, p, c, 2, "notes.txt", "alpha", "beta", "gamma é")
			p.TapJS(codeRow(1))
			p.WaitJS(codeRow(1) + `.classList.contains("is-sel")`)
			tapEdit(p)
			if got := Eval[string](p, editSheet+`.querySelector(".edit-title").textContent`); got != "notes.txt · lines 1–3" {
				t.Errorf("sheet title %q", got)
			}
			if got := Eval[string](p, editSheet+`.querySelector("textarea").value`); got != "alpha\nbeta\ngamma é" {
				t.Errorf("sheet text %q, want the rows with their context", got)
			}
			order := Eval[[]string](p, `[...`+editSheet+`.querySelectorAll(".edit-actions .btn")].map((b) => b.textContent)`)
			if strings.Join(order, ",") != "Cancel,Send" {
				t.Errorf("buttons %v, want Cancel first", order)
			}
			setInput(p, editSheet+`.querySelector("textarea")`, "alpha\nBETA\ngamma é")
			p.TapJS(editSheet + `.querySelector(".edit-send .btn")`)
			r, a := expectSetText(t, c)
			// The last row is 8 bytes long (é is two): the end column counts bytes.
			if a.Buf != 2 || a.Tick != 3 || a.Start != (webproto.Pos{Row: 0, Col: 0}) || a.End != (webproto.Pos{Row: 2, Col: 8}) || strings.Join(a.Lines, "|") != "alpha|BETA|gamma é" {
				t.Fatalf("set_text args %+v", a)
			}
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"tick": 4, "dirty": true})})
			p.WaitJS(`(` + toastsJS + `).includes("Sent. Line 2 changed.") && !` + editSheet)
			c.Send(t, Delta("buf:2", 2, map[string]any{"kind": "lines", "first": 1, "last": 2, "lines": []string{"BETA"}, "tick": 4}))
			c.Send(t, Delta("buf:2", 3, map[string]any{"kind": "dirty", "on": true}))
			p.WaitJS(codeRow(1) + `.innerText.includes("BETA")`)
			if Eval[bool](p, filesView+`.querySelector(".file-updated") !== null`) {
				t.Error("this page's own edit says it came from the laptop")
			}

			p.TapJS(`[...` + filesView + `.querySelectorAll(".view-actions .btn")].find((b) => b.textContent === "Save")`)
			r = expectReq(t, c, "buf.save")
			c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(map[string]any{"tick": 5, "dirty": false})})
			p.WaitJS(`(` + toastsJS + `).includes("Saved. The laptop's on-save hooks ran.")`)

			p.TapJS(`[...` + filesView + `.querySelectorAll(".view-actions .btn")].find((b) => b.textContent === "Undo")`)
			r = expectReq(t, c, "buf.undo")
			c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeBadRequest, Msg: "Nothing to undo."})
			p.WaitJS(`(` + toastsJS + `).includes("Nothing to undo.")`)

			// A long press opens the sheet on that row too.
			longPress(p, codeRow(2))
			p.WaitJS(editSheet)
			p.TapJS(`[...` + editSheet + `.querySelectorAll(".btn")].find((b) => b.textContent === "Cancel")`)
			p.WaitJS(`!` + editSheet)
		})
	}
}

// The laptop changed the rows after the sheet opened: Send does not
// overwrite them. The text stays, a line says why, Show what changed
// shows the laptop's change, Send again sends on the new tick. A conflict
// the hub finds is shown the same way.
func TestFileEditConflictKeepsText(t *testing.T) {
	p, c := openFiles(t, Phone)
	openBuf(t, p, c, 2, "notes.txt", "alpha", "beta")
	p.TapJS(codeRow(0))
	tapEdit(p)
	setInput(p, editSheet+`.querySelector("textarea")`, "ALPHA mine\nbeta")
	c.Send(t, Delta("buf:2", 2, map[string]any{"kind": "lines", "first": 1, "last": 2, "lines": []string{"beta from laptop"}, "tick": 4}))
	p.WaitJS(codeRow(1) + `.innerText.includes("beta from laptop")`)
	p.TapJS(editSheet + `.querySelector(".edit-send .btn")`)
	p.WaitJS(editSheet + `.querySelector(".edit-conflict")?.innerText.includes("The file changed on the laptop. Check the new text and send again.")`)
	noReq(t, c, 300*time.Millisecond)
	if got := Eval[string](p, editSheet+`.querySelector("textarea").value`); got != "ALPHA mine\nbeta" {
		t.Fatalf("the user's text is %q after the conflict", got)
	}
	p.TapJS(editSheet + `.querySelector(".edit-diff-btn")`)
	p.WaitJS(editSheet + `.querySelector(".drow-del")?.innerText.includes("beta") && ` + editSheet + `.querySelector(".drow-add")?.innerText.includes("beta from laptop")`)
	if got := Eval[string](p, editSheet+`.querySelector(".edit-send .btn").textContent`); got != "Send again" {
		t.Errorf("send button %q, want Send again", got)
	}
	p.TapJS(editSheet + `.querySelector(".edit-send .btn")`)
	r, a := expectSetText(t, c)
	if a.Tick != 4 || strings.Join(a.Lines, "|") != "ALPHA mine|beta" || a.End.Row != 1 {
		t.Fatalf("send again args %+v", a)
	}
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeConflict, Msg: "changed", Data: raw(map[string]any{"tick": 6})})
	p.WaitJS(editSheet + `.querySelector(".edit-conflict")`)
	if got := Eval[string](p, editSheet+`.querySelector("textarea").value`); got != "ALPHA mine\nbeta" {
		t.Fatalf("the user's text is %q after the hub's conflict", got)
	}
}

// A failed save shows the editor's own words under the header, with Try again.
func TestFileSaveShowsEditorError(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openBuf(t, p, c, 2, "notes.txt", "alpha")
	c.Send(t, Delta("buf:2", 2, map[string]any{"kind": "dirty", "on": true}))
	p.TapJS(`[...` + filesView + `.querySelectorAll(".view-actions .btn")].find((b) => b.textContent === "Save")`)
	r := expectReq(t, c, "buf.save")
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeInternal, Msg: "E212: Can't open file for writing"})
	p.WaitJS(filesView + `.querySelector(".save-err")?.innerText.includes("Could not save: E212: Can't open file for writing")`)
	p.TapJS(filesView + `.querySelector(".save-err .btn")`)
	expectReq(t, c, "buf.save")
}
