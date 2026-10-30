//go:build browser

package web

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// File ops from the Files tab (WEB-41): each row's ⋯ opens its actions;
// the + in the head acts on the workspace folder.

// openTree shows a root with src/ and a.go.
func openTree(t *testing.T, vp Viewport) (*Page, *HubConn) {
	t.Helper()
	p, c := openFiles(t, vp)
	c.Send(t, Snap("tree", 1, treeSnap(false)))
	expectList(t, c, "tree.list", "")
	c.Send(t, Delta("tree", 2, dirDelta("", false, entry("src", true), entry("a.go", false))))
	p.WaitJS(treeRow("a.go"))
	return p, c
}

func moreOf(path string) string {
	return treeRow(path) + `.parentElement.querySelector(".tree-more")`
}

// New file in a folder: the name sheet (Cancel first) sends tree.new_file
// with the folder's path; a name with a slash cannot be sent.
func TestFilesNewFileInFolder(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openTree(t, vp)
			p.TapJS(moreOf("src"))
			p.WaitJS(`document.querySelector(".file-menu")`)
			if first := p.Text(".file-menu .btn"); first != "Cancel" {
				t.Errorf("first choice %q", first)
			}
			p.TapJS(ButtonByText("New file"))
			p.WaitJS(`document.activeElement === document.querySelector(".name-input")`)
			setInput(p, `document.querySelector(".name-input")`, "x/y.go")
			p.WaitJS(`document.querySelector(".name-bad")`)
			p.Key(kb.Enter)
			c.Quiet(t, 200*time.Millisecond)
			setInput(p, `document.querySelector(".name-input")`, "new.go")
			p.TapJS(ButtonByText("Create"))
			r := expectOp(t, c, webproto.OpTreeNewFile, `{"path":"src/new.go"}`)
			reply(t, c, r, webproto.TreeChanged{Path: "src/new.go"})
			p.WaitJS(`!document.querySelector(".file-actions") && document.querySelector(".toasts")?.textContent.includes("Created src/new.go.")`)

			p.TapJS(`document.querySelector(".head-new")`)
			p.TapJS(ButtonByText("New folder"))
			setInput(p, `document.querySelector(".name-input")`, "pkg")
			p.TapJS(ButtonByText("Create"))
			reply(t, c, expectOp(t, c, webproto.OpTreeNewDir, `{"path":"pkg"}`), webproto.TreeChanged{Path: "pkg"})
			p.noOverflow("files")
		})
	}
}

// Rename starts with the old name; the same name cannot be sent; the
// hub's refusal shows in its own words.
func TestFilesRename(t *testing.T) {
	p, c := openTree(t, Phone)
	p.TapJS(moreOf("a.go"))
	if n := Eval[int](p, `[...document.querySelectorAll(".file-menu .btn")].filter((b) => b.textContent.startsWith("New")).length`); n != 0 {
		t.Errorf("a file offers %d new buttons", n)
	}
	p.TapJS(ButtonByText("Rename"))
	p.WaitJS(`document.querySelector(".name-input")?.value === "a.go"`)
	p.TapJS(`[...document.querySelectorAll(".name-form .btn")].find((b) => b.textContent === "Rename")`)
	c.Quiet(t, 200*time.Millisecond)
	setInput(p, `document.querySelector(".name-input")`, "b.go")
	p.TapJS(`[...document.querySelectorAll(".name-form .btn")].find((b) => b.textContent === "Rename")`)
	r := expectOp(t, c, webproto.OpTreeRename, `{"path":"a.go","name":"b.go"}`)
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeConflict, Msg: "That name exists already."})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("That name exists already.")`)
}

// Delete asks in the page first (Enter cancels), then waits for the
// laptop; confirmed there, it says so.
func TestFilesDeleteWaitsForLaptop(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			p, c := openTree(t, vp)
			p.TapJS(moreOf("src"))
			p.TapJS(ButtonByText("Delete…"))
			p.WaitJS(`document.querySelector(".ask") && document.activeElement === document.querySelector(".ask .choices button")`)
			if txt := p.Text(".ask-text"); txt != "Delete the folder src and everything in it? This cannot be undone." {
				t.Errorf("question %q", txt)
			}
			p.Key(kb.Enter)
			p.WaitJS(`!document.querySelector(".ask")`)
			c.Quiet(t, 200*time.Millisecond)

			p.TapJS(moreOf("a.go"))
			p.TapJS(ButtonByText("Delete…"))
			p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
			p.TapJS(ButtonByText("Ask the laptop"))
			reply(t, c, expectOp(t, c, webproto.OpTreeDelete, `{"path":"a.go"}`), webproto.AskRef{Card: "card-9"})
			p.WaitJS(`document.querySelector(".waiting")`)
			c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-9", By: "laptop", Choice: "confirm"})})
			p.WaitJS(`!document.querySelector(".waiting") && document.querySelector(".toasts")?.textContent.includes("Done on the laptop.")`)
		})
	}
}

// A viewer gets no actions.
func TestFilesViewerHasNoActions(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	m := Meta("c-v")
	m.Role = webproto.RoleViewer
	c := p.LoginMeta(m)
	p.GoTab("files")
	c.Subs(t, "tree", "bufs")
	c.Send(t, Snap("tree", 1, treeSnap(false)))
	expectList(t, c, "tree.list", "")
	c.Send(t, Delta("tree", 2, dirDelta("", false, entry("a.go", false))))
	p.WaitJS(treeRow("a.go"))
	if n := Eval[int](p, `document.querySelectorAll(".tree-more, .head-new").length`); n != 0 {
		t.Fatalf("a viewer sees %d action buttons", n)
	}
}

// Download reads the file in chunks and hands the browser one blob with
// the bytes; a secret file's refusal shows in the hub's words.
func TestFilesDownload(t *testing.T) {
	p, c := openTree(t, Desktop)
	Eval[bool](p, `(() => { window.__txt = null; const o = URL.createObjectURL.bind(URL); URL.createObjectURL = (b) => { b.text().then((x) => (window.__txt = x)); return o(b); }; return true; })()`)
	p.TapJS(moreOf("a.go"))
	p.TapJS(ButtonByText("Download"))
	r := expectOp(t, c, webproto.OpFileDownload, `{"path":"a.go"}`)
	reply(t, c, r, webproto.FileChunk{Path: "a.go", Size: 11, From: 0, Data: "cGFja2FnZSA=", Next: 8, Mtime: 7})
	r = expectOp(t, c, webproto.OpFileDownload, `{"path":"a.go","from":8}`)
	reply(t, c, r, webproto.FileChunk{Path: "a.go", Size: 11, From: 8, Data: "YWJj", Mtime: 7})
	p.WaitJS(`window.__txt === "package abc"`)
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Downloaded a.go.")`)

	p.TapJS(moreOf("a.go"))
	p.TapJS(ButtonByText("Download"))
	r = expectOp(t, c, webproto.OpFileDownload, `{"path":"a.go"}`)
	c.Send(t, &webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: webproto.CodeForbidden, Msg: "This file may hold secrets. It stays on the laptop."})
	p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("It stays on the laptop.")`)
}

// Upload sends the chosen file in chunks to the folder; a name that
// exists asks the laptop to replace it.
func TestFilesUpload(t *testing.T) {
	for _, exists := range []bool{false, true} {
		p, c := openTree(t, Desktop)
		f := filepath.Join(t.TempDir(), "note.txt")
		if err := os.WriteFile(f, []byte("hello upload"), 0o644); err != nil {
			t.Fatal(err)
		}
		p.TapJS(moreOf("src"))
		p.TapJS(ButtonByText("Upload here"))
		p.WaitJS(`document.querySelector(".xfer-input")`)
		p.Do(chromedp.SetUploadFiles(".xfer-input", []string{f}))
		reply(t, c, expectOp(t, c, webproto.OpFileUploadBegin, `{"path":"src/note.txt","size":12}`), webproto.UploadStarted{ID: "0a1b2c3d", Chunk: 524288, Exists: exists})
		reply(t, c, expectOp(t, c, webproto.OpFileUploadChunk, `{"id":"0a1b2c3d","off":0,"data":"aGVsbG8gdXBsb2Fk"}`), webproto.UploadProgress{Got: 12})
		if !exists {
			reply(t, c, expectOp(t, c, webproto.OpFileUploadSave, `{"id":"0a1b2c3d"}`), webproto.TreeChanged{Path: "src/note.txt"})
			p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Uploaded src/note.txt.")`)
			continue
		}
		p.WaitJS(`document.querySelector(".ask-text")?.textContent === "src/note.txt exists. Replace it with the upload?"`)
		p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
		p.TapJS(ButtonByText("Ask the laptop"))
		reply(t, c, expectOp(t, c, webproto.OpFileUploadReplace, `{"id":"0a1b2c3d"}`), webproto.AskRef{Card: "card-31"})
		c.Send(t, &webproto.Event{T: webproto.KindEv, Kind: webproto.EventAnswered, Data: raw(webproto.Answered{ID: "card-31", By: "laptop", Choice: "confirm"})})
		p.WaitJS(`document.querySelector(".toasts")?.textContent.includes("Done on the laptop.")`)
	}
}
