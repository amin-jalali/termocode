package app

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Live editing together (G-COL): each side sees where the other is. The
// laptop's cursor in a file a page follows rides on the buf topic; a
// page's cursor shows in the laptop's editor as a mark with its name.

// webCursorDeltas pumps nvim until a cursor delta of topic is queued on
// the rig's connection and returns the cursor deltas.
func webCursorDeltas(r *webFilesRig, topic string) []map[string]any {
	r.t.Helper()
	var got []map[string]any
	r.pumpUntil("a cursor delta", func() bool {
		for _, d := range r.deltas(topic) {
			if d["kind"] == "cursor" {
				got = append(got, d)
			}
		}
		return len(got) > 0
	})
	return got
}

// Spec (G-COL): the laptop's cursor in a file the page follows reaches
// the page (a cursor delta and the snapshot); when the laptop goes to
// another buffer the page hears that it left.
func TestLaptopCursorReachesThePage(t *testing.T) {
	r, b := webEditRig(t, "one\ntwo\nthree\n")
	topic := "buf:" + strconv.Itoa(b.Buf)
	r.sub(topic)
	r.lua(`local b = ...
vim.api.nvim_set_current_buf(b)
vim.api.nvim_win_set_cursor(0, { 2, 1 })
vim.api.nvim_exec_autocmds('CursorMoved', {})`, nil, b.Buf)
	ds := webCursorDeltas(r, topic)
	at, _ := ds[len(ds)-1]["at"].(map[string]any)
	if at == nil || at["row"] != float64(1) || at["col"] != float64(1) {
		t.Fatalf("cursor delta: %v", ds)
	}
	c2 := r.conn("c-2", webproto.RoleController)
	snap := r.subOn(c2, topic)
	if l, _ := snap["laptop"].(map[string]any); l == nil || l["row"] != float64(1) || l["col"] != float64(1) {
		t.Fatalf("snapshot: %v", snap["laptop"])
	}
	r.lua(`vim.cmd('enew') vim.api.nvim_exec_autocmds('CursorMoved', {})`, nil)
	ds = webCursorDeltas(r, topic)
	if d := ds[len(ds)-1]; d["at"] != nil {
		t.Fatalf("the laptop left the buffer: %v", d)
	}
}

// Spec (G-COL): the cursor delta folds into the snapshot the same way
// on the hub and in the page.
func TestBufApplyCursor(t *testing.T) {
	snap, _ := json.Marshal(webproto.BufSnap{Buf: 3, Tick: 2, Lines: []string{"a"}})
	d, _ := json.Marshal(webproto.CursorDelta{Kind: "cursor", At: &webproto.Cursor{Row: 0, Col: 1}})
	out, err := webBufApply(snap, d)
	if err != nil {
		t.Fatal(err)
	}
	var s webproto.BufSnap
	_ = json.Unmarshal(out, &s)
	if s.Laptop == nil || s.Laptop.Col != 1 || s.Tick != 2 {
		t.Fatalf("applied: %s", out)
	}
	d, _ = json.Marshal(webproto.CursorDelta{Kind: "cursor"})
	if out, err = webBufApply(out, d); err != nil {
		t.Fatal(err)
	}
	s = webproto.BufSnap{}
	_ = json.Unmarshal(out, &s)
	if s.Laptop != nil {
		t.Fatalf("left: %s", out)
	}
}

// webPeerMarks lists the page cursors the laptop's editor shows in b:
// row, col and the name.
func webPeerMarks(r *webFilesRig, b int) []string {
	var out []string
	r.lua(`local b = ...
local ns = vim.api.nvim_create_namespace('termocode_web_peer')
local out = {}
for _, m in ipairs(vim.api.nvim_buf_get_extmarks(b, ns, 0, -1, { details = true })) do
  local name = ''
  for _, c in ipairs(m[4].virt_text or {}) do name = name .. c[1] end
  out[#out + 1] = m[2] .. ':' .. m[3] .. ':' .. name
end
return out`, &out, b)
	return out
}

// Spec (G-COL): a page's cursor shows in the laptop's editor at its row
// and column with the device's name; it moves with the page and goes
// when the page leaves.
func TestPageCursorShowsOnTheLaptop(t *testing.T) {
	r, b := webEditRig(t, "one\ntwo\nthree\n")
	f := r.call(webproto.OpBufCursor, &webproto.BufCursorArgs{Buf: b.Buf, Row: 1, Col: 2})
	if f["t"] != "res" {
		t.Fatalf("buf.cursor: %v", f)
	}
	if got := webPeerMarks(r, b.Buf); len(got) != 1 || got[0] != "1:2: web" {
		t.Fatalf("marks: %q", got)
	}
	r.call(webproto.OpBufCursor, &webproto.BufCursorArgs{Buf: b.Buf, Row: 2, Col: 0})
	if got := webPeerMarks(r, b.Buf); len(got) != 1 || got[0] != "2:0: web" {
		t.Fatalf("moved: %q", got)
	}
	// A column past the end of its row is kept on the row.
	r.call(webproto.OpBufCursor, &webproto.BufCursorArgs{Buf: b.Buf, Row: 0, Col: 99})
	if got := webPeerMarks(r, b.Buf); len(got) != 1 || got[0] != "0:3: web" {
		t.Fatalf("past the end: %q", got)
	}
	// The page goes: its mark goes too, while another page keeps the
	// buffer.
	c2 := r.conn("c-2", webproto.RoleController)
	r.subOn(c2, "buf:"+strconv.Itoa(b.Buf))
	r.c.cancel()
	delete(r.h.live, r.c)
	r.track()
	if !webAttached(r, b.Buf) {
		t.Fatal("the other page's buffer was let go")
	}
	if got := webPeerMarks(r, b.Buf); len(got) != 0 {
		t.Fatalf("after the page left: %q", got)
	}
}

// Spec (G-COL): a cursor op is no read: a viewer may not move a mark on
// the laptop.
func TestViewerMayNotSendACursor(t *testing.T) {
	if webproto.Allowed(webproto.RoleViewer, webproto.OpBufCursor) {
		t.Fatal("a viewer may send buf.cursor")
	}
	if !webproto.Allowed(webproto.RoleController, webproto.OpBufCursor) {
		t.Fatal("a controller may not send buf.cursor")
	}
}
