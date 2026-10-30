package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

func webSetText(r *webFilesRig, c *webConn, buf, tick, sr, sc, er, ec int, lines ...string) map[string]any {
	r.t.Helper()
	return r.callOn(c, webproto.OpBufSetText, &webproto.SetTextArgs{Buf: buf, Tick: tick,
		Start: webproto.Pos{Row: sr, Col: sc}, End: webproto.Pos{Row: er, Col: ec}, Lines: lines})
}

func webBufLines(r *webFilesRig, buf int) string {
	var s string
	r.lua(`local b = ... return table.concat(vim.api.nvim_buf_get_lines(b, 0, -1, false), '|')`, &s, buf)
	return s
}

// webEditRig opens a file with the given text and returns its buffer.
func webEditRig(t *testing.T, body string) (*webFilesRig, webproto.BufOpenRes) {
	t.Helper()
	r := newWebFilesRig(t)
	r.withNvim()
	r.write("f.txt", body)
	return r, webBufOpen(t, r, r.c, "f.txt")
}

// Spec (WEB-21): set_text with the current tick changes the range and
// answers the new tick; with an old tick (the laptop typed in between)
// it is a conflict carrying the tick now, and the buffer is untouched.
func TestSetTextTickConflict(t *testing.T) {
	r, b := webEditRig(t, "hello world\n")
	f := webSetText(r, r.c, b.Buf, b.Tick, 0, 6, 0, 11, "phone")
	var res webproto.EditRes
	webResData(t, f, &res)
	if webBufLines(r, b.Buf) != "hello phone" || res.Tick <= b.Tick || !res.Dirty {
		t.Fatalf("edit: %q %+v", webBufLines(r, b.Buf), res)
	}
	r.lua(`local b = ... vim.api.nvim_buf_set_lines(b, 0, 1, false, { "laptop typed" })`, nil, b.Buf)
	f = webSetText(r, r.c, b.Buf, res.Tick, 0, 0, 0, 5, "HELLO")
	if webErrCode(f) != "conflict" {
		t.Fatalf("stale tick: %v", f)
	}
	if now := f["data"].(map[string]any)["tick"].(float64); int(now) <= res.Tick {
		t.Errorf("conflict tick %v", now)
	}
	if webBufLines(r, b.Buf) != "laptop typed" {
		t.Fatalf("buffer changed: %q", webBufLines(r, b.Buf))
	}
}

// Spec (WEB-21): two browsers send an edit on the same tick: one wins,
// the other gets conflict.
func TestSetTextAtomicUnderRace(t *testing.T) {
	r, b := webEditRig(t, "abc\n")
	c2 := r.conn("c-2", webproto.RoleController)
	webBufOpen(t, r, c2, "f.txt")
	f1 := webSetText(r, r.c, b.Buf, b.Tick, 0, 0, 0, 1, "X")
	f2 := webSetText(r, c2, b.Buf, b.Tick, 0, 0, 0, 1, "Y")
	if f1["t"] != "res" || webErrCode(f2) != "conflict" || webBufLines(r, b.Buf) != "Xbc" {
		t.Fatalf("race: %v / %v / %q", f1, f2, webBufLines(r, b.Buf))
	}
}

// Spec (WEB-21): bad positions are bad_request and never change the
// buffer: past the end, past a line, inside a multi-byte character; a
// buffer the browser did not open is not_found; a terminal is refused.
func TestSetTextBadArgsNeverChange(t *testing.T) {
	r, b := webEditRig(t, "äb\ncd\n")
	for _, p := range [][4]int{{5, 0, 5, 0}, {0, 0, 2, 0}, {0, 9, 0, 9}, {0, 1, 0, 1}, {1, 0, 1, 3}} {
		f := webSetText(r, r.c, b.Buf, b.Tick, p[0], p[1], p[2], p[3], "Z")
		if webErrCode(f) != "bad_request" {
			t.Errorf("%v: %v", p, f)
		}
	}
	if webBufLines(r, b.Buf) != "äb|cd" {
		t.Fatalf("changed: %q", webBufLines(r, b.Buf))
	}
	if f := webSetText(r, r.c, 999, 1, 0, 0, 0, 0, "Z"); webErrCode(f) != "not_found" {
		t.Errorf("unknown buffer: %v", f)
	}
	f := webSetText(r, r.c, b.Buf, b.Tick, 1, 2, 1, 2) // no lines: an insert of nothing
	if f["t"] != "res" || webBufLines(r, b.Buf) != "äb|cd" {
		t.Errorf("empty insert: %v %q", f, webBufLines(r, b.Buf))
	}
}

// Spec (WEB-21): save runs the laptop's own on-save hooks and writes the
// file; dirty turns off.
func TestSaveRunsOnSaveHooks(t *testing.T) {
	r, b := webEditRig(t, "x\n")
	r.lua(`vim.api.nvim_create_autocmd('BufWritePre', { callback = function(ev)
  vim.api.nvim_buf_set_lines(ev.buf, 0, 0, false, { "// formatted" })
end })`, nil)
	f := webSetText(r, r.c, b.Buf, b.Tick, 0, 1, 0, 1, "y")
	if f["t"] != "res" {
		t.Fatal(f)
	}
	var res webproto.EditRes
	webResData(t, r.call(webproto.OpBufSave, &webproto.BufArgs{Buf: b.Buf}), &res)
	got, _ := os.ReadFile(filepath.Join(r.root, "f.txt"))
	if string(got) != "// formatted\nxy\n" || res.Dirty {
		t.Fatalf("saved %q, %+v", got, res)
	}
}

// Spec (WEB-21): a write error comes back with nvim's message, without
// the workspace path (here: the file's folder was deleted).
func TestSaveErrorText(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.write("sub/f.txt", "x\n")
	b := webBufOpen(t, r, r.c, "sub/f.txt")
	webSetText(r, r.c, b.Buf, b.Tick, 0, 0, 0, 0, "a")
	if err := os.RemoveAll(filepath.Join(r.root, "sub")); err != nil {
		t.Fatal(err)
	}
	f := r.call(webproto.OpBufSave, &webproto.BufArgs{Buf: b.Buf})
	msg, _ := f["msg"].(string)
	t.Logf("nvim says: %s", msg)
	if f["t"] != "err" || !strings.HasPrefix(msg, "E") || strings.Contains(msg, r.root) {
		t.Fatalf("save error: %v", f)
	}
}

// Spec (WEB-21): undo undoes one step: the last web edit, not the one
// before it.
func TestUndoOnlyOneStep(t *testing.T) {
	r, b := webEditRig(t, "abc\n")
	var res webproto.EditRes
	webResData(t, webSetText(r, r.c, b.Buf, b.Tick, 0, 0, 0, 1, "1"), &res)
	webResData(t, webSetText(r, r.c, b.Buf, res.Tick, 0, 1, 0, 2, "2"), &res)
	if webBufLines(r, b.Buf) != "12c" {
		t.Fatal(webBufLines(r, b.Buf))
	}
	webResData(t, r.call(webproto.OpBufUndo, &webproto.BufArgs{Buf: b.Buf}), &res)
	if webBufLines(r, b.Buf) != "1bc" || !res.Dirty {
		t.Fatalf("undo: %q %+v", webBufLines(r, b.Buf), res)
	}
	r.pumpUntil("the undo in the mirror", func() bool { return r.h.files.bufs[b.Buf].tick == res.Tick })
	if got := strings.Join(r.h.files.bufs[b.Buf].rows, "|"); got != "1bc" {
		t.Fatalf("mirror: %q", got)
	}
}

// Spec (WEB-21): nvim's message reaches the page without the Lua/Vim
// prefix and without the workspace path.
func TestWebNvimText(t *testing.T) {
	for in, want := range map[string]string{
		"Lua: x.lua:0: Vim(write):E212: Can't open file for writing": "E212: Can't open file for writing",
		"Vim:E13: File exists /w/ws/a.go":                            "E13: File exists a.go",
		"":                                                           "fallback",
		"E1: one\ntwo":                                               "E1: one",
	} {
		if got := webNvimText("/w/ws", in, "fallback"); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}
