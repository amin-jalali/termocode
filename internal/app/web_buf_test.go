package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

func webBufOpen(t *testing.T, r *webFilesRig, c *webConn, rel string) webproto.BufOpenRes {
	t.Helper()
	var res webproto.BufOpenRes
	webResData(t, r.callOn(c, webproto.OpBufOpen, &webproto.PathArgs{Path: rel}), &res)
	return res
}

// webLaptopBuf is what the laptop's nvim shows: the current buffer's
// name and the listed buffers.
func webLaptopBuf(r *webFilesRig) string {
	var s string
	r.lua(`local out = { vim.fn.fnamemodify(vim.api.nvim_buf_get_name(0), ':t') }
for _, b in ipairs(vim.api.nvim_list_bufs()) do
  if vim.bo[b].buflisted then table.insert(out, vim.fn.fnamemodify(vim.api.nvim_buf_get_name(b), ':t')) end
end
return table.concat(out, ' ')`, &s)
	return s
}

func webAttached(r *webFilesRig, buf int) bool {
	var on bool
	r.lua(`local b = ... return _G.termocode_web ~= nil and _G.termocode_web.bufs[b] ~= nil`, &on, buf)
	return on
}

// Spec (WEB-17): buf.open loads the file hidden: the laptop's current
// buffer and its tab list do not change.
func TestOpenLoadsHiddenBuffer(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	a := r.write("a.txt", "laptop\n")
	r.write("b.txt", "one\ntwo\n")
	if err := r.m.nvim.OpenFile("edit", a); err != nil {
		t.Fatal(err)
	}
	before := webLaptopBuf(r)
	res := webBufOpen(t, r, r.c, "b.txt")
	if res.Path != "b.txt" || res.Lines != 2 || res.Dirty || res.Buf <= 0 {
		t.Fatalf("open: %+v", res)
	}
	if after := webLaptopBuf(r); after != before || before != "a.txt a.txt" {
		t.Fatalf("laptop changed: %q → %q", before, after)
	}
	snap := r.sub("buf:" + strconv.Itoa(res.Buf))
	if b, _ := json.Marshal(snap["lines"]); string(b) != `["one","two"]` {
		t.Fatalf("snap: %v", snap)
	}
}

// Spec (WEB-17): an edit on the laptop reaches the browser as lines
// deltas that replay to the buffer; the replay golden pins them.
func TestLinesEventsReachBrowser(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.write("notes.txt", "alpha\nbeta\ngamma\n")
	res := webBufOpen(t, r, r.c, "notes.txt")
	topic := "buf:" + strconv.Itoa(res.Buf)
	var want int
	c := webReplayRecord(t, r.m, r.h, topic, func() {
		r.lua(`local b = ...
vim.api.nvim_buf_set_lines(b, 1, 2, false, { "BETA", "beta 2" })
vim.api.nvim_buf_set_text(b, 0, 0, 0, 5, { "ALPHA" })
vim.api.nvim_buf_set_lines(b, 3, 4, false, {})
return vim.api.nvim_buf_get_changedtick(b)`, &want, res.Buf)
		r.pumpUntil("the last edit", func() bool { b := r.h.files.bufs[res.Buf]; return b.tick == want && b.dirty })
	})
	if len(c.Deltas) < 3 {
		t.Fatalf("deltas: %s", c.Deltas)
	}
	webReplayCheck(t, webTopics["buf"], c)
	var snap webproto.BufSnap
	_ = json.Unmarshal(c.SnapN, &snap)
	if strings.Join(snap.Lines, "|") != "ALPHA|BETA|beta 2" || !snap.Dirty {
		t.Fatalf("final: %+v", snap)
	}
	webReplayGolden(t, webReplayPath(topic, "laptop_edits"), c)
}

// Spec (WEB-17): a file over 4 MiB is refused with reason "big" and
// buf.head shows its first 2000 lines, read only.
func TestBigFileHeadOnly(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	line := strings.Repeat("x", 99) + "\n"
	r.write("big.log", strings.Repeat(line, webproto.MaxOpenBytes/len(line)+10))
	f := r.call(webproto.OpBufOpen, &webproto.PathArgs{Path: "big.log"})
	if webErrCode(f) != "limit" || f["data"].(map[string]any)["reason"] != "big" {
		t.Fatalf("open: %v", f)
	}
	var head webproto.BufHeadRes
	webResData(t, r.call(webproto.OpBufHead, &webproto.PathArgs{Path: "big.log"}), &head)
	if len(head.Lines) != webproto.MaxHeadLines || !head.Truncated || head.Lines[0] != strings.TrimSuffix(line, "\n") {
		t.Fatalf("head: %d lines, truncated %v", len(head.Lines), head.Truncated)
	}
	if webLaptopBuf(r) != "" && strings.Contains(webLaptopBuf(r), "big.log") {
		t.Error("big.log was loaded")
	}
	r.write("many.txt", strings.Repeat("a\n", webproto.MaxOpenLines+1))
	if f := r.call(webproto.OpBufOpen, &webproto.PathArgs{Path: "many.txt"}); webErrCode(f) != "limit" {
		t.Fatalf("too many lines: %v", f)
	}
}

// Spec (WEB-17, T40): a binary file and a file that may hold secrets
// are never sent, not by buf.open and not by buf.head.
func TestBinaryRefused(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.write("img.png", "\x89PNG\x00\x00data")
	r.write(".env", "TOKEN="+"sk-"+"test-123\n")
	for _, op := range []string{webproto.OpBufOpen, webproto.OpBufHead} {
		f := r.call(op, &webproto.PathArgs{Path: "img.png"})
		if webErrCode(f) != "limit" || f["data"].(map[string]any)["reason"] != "binary" {
			t.Errorf("%s img.png: %v", op, f)
		}
		f = r.call(op, &webproto.PathArgs{Path: ".env"})
		if webErrCode(f) != "forbidden" {
			t.Errorf("%s .env: %v", op, f)
		}
	}
	// The laptop has .env open: its buffer number does not open it either.
	if err := r.m.nvim.OpenFile("edit", filepath.Join(r.root, ".env")); err != nil {
		t.Fatal(err)
	}
	var n int
	r.lua(`return vim.api.nvim_get_current_buf()`, &n)
	r.m.onWebSub(webSubMsg{hub: r.h, conn: r.c, topics: []string{"buf:" + strconv.Itoa(n)}})
	fs := r.take()
	if len(fs) != 1 || webErrCode(fs[0]) != "forbidden" {
		t.Fatalf("sub buf of .env: %v", fs)
	}
}

// Spec (WEB-17): two browsers on one file share one attach; it ends when
// the last one lets go, and a buffer the web loaded is unloaded again.
func TestAttachRefCounted(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.write("x.txt", "x\n")
	c2 := r.conn("c-2", webproto.RoleViewer)
	b1 := webBufOpen(t, r, r.c, "x.txt")
	b2 := webBufOpen(t, r, c2, "x.txt")
	if b1.Buf != b2.Buf || len(r.h.files.bufs) != 1 {
		t.Fatalf("two buffers: %d %d", b1.Buf, b2.Buf)
	}
	r.call(webproto.OpBufClose, &webproto.BufArgs{Buf: b1.Buf})
	r.track()
	if !webAttached(r, b1.Buf) {
		t.Fatal("detached while the second browser holds it")
	}
	delete(r.h.live, c2) // the second browser went away
	r.track()
	if webAttached(r, b1.Buf) || len(r.h.files.bufs) != 0 {
		t.Fatal("still attached")
	}
	var loaded bool
	r.lua(`local b = ... return vim.api.nvim_buf_is_valid(b) and vim.api.nvim_buf_is_loaded(b)`, &loaded, b1.Buf)
	if loaded {
		t.Error("the web loaded x.txt and left it loaded")
	}
}

// Spec (WEB-17): buf.close never closes the laptop's buffer.
func TestCloseKeepsLaptopBuffer(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	a := r.write("a.txt", "a\n")
	if err := r.m.nvim.OpenFile("edit", a); err != nil {
		t.Fatal(err)
	}
	res := webBufOpen(t, r, r.c, "a.txt")
	topic := "buf:" + strconv.Itoa(res.Buf)
	r.sub(topic)
	r.call(webproto.OpBufClose, &webproto.BufArgs{Buf: res.Buf})
	r.m.onWebSub(webSubMsg{hub: r.h, conn: r.c, topics: []string{topic}, unsub: true})
	r.track()
	if webAttached(r, res.Buf) {
		t.Fatal("still attached")
	}
	if got := webLaptopBuf(r); got != "a.txt a.txt" {
		t.Fatalf("laptop: %q", got)
	}
}

// Spec (WEB-17): a buffer the laptop closes sends gone.
func TestBufGoneWhenLaptopCloses(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	a := r.write("a.txt", "a\n")
	if err := r.m.nvim.OpenFile("edit", a); err != nil {
		t.Fatal(err)
	}
	res := webBufOpen(t, r, r.c, "a.txt")
	topic := "buf:" + strconv.Itoa(res.Buf)
	r.sub(topic)
	r.lua(`local b = ... vim.api.nvim_buf_delete(b, { force = true })`, nil, res.Buf)
	r.pumpUntil("gone", func() bool { return r.h.files.bufs[res.Buf] == nil })
	if ds := r.deltas(topic); len(ds) == 0 || ds[len(ds)-1]["kind"] != "gone" {
		t.Fatalf("deltas: %v", ds)
	}
}

// Spec (WEB-17): a lost callback (a gap in the sequence) reads the
// buffer again and sends a reset instead of drifting.
func TestBufGapResets(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	r.write("a.txt", "a\nb\n")
	res := webBufOpen(t, r, r.c, "a.txt")
	topic := "buf:" + strconv.Itoa(res.Buf)
	r.sub(topic)
	b := r.h.files.bufs[res.Buf]
	r.lua(`local b = ... vim.api.nvim_buf_set_lines(b, 0, 1, false, { "A" })`, nil, res.Buf)
	b.seq-- // as if one callback was lost before
	r.pumpUntil("the edit", func() bool { return len(b.rows) > 0 && b.rows[0] == "A" })
	ds := r.deltas(topic)
	if len(ds) == 0 || ds[len(ds)-1]["kind"] != "reset" {
		t.Fatalf("deltas: %v", ds)
	}
	_ = os.Remove(filepath.Join(r.root, "a.txt"))
}
