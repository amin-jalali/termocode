package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestShell starts a terminal buffer running argv in nvim (hidden,
// like a tab that is not on show) and lists it as the rig's only tab.
func webTestShell(t *testing.T, r *webFilesRig, argv ...string) int {
	t.Helper()
	var buf int
	r.lua(`local argv, dir = ...
local b = vim.api.nvim_create_buf(false, true)
vim.api.nvim_buf_call(b, function()
  vim.fn.jobstart(argv, { term = true, cwd = dir, env = { PS1 = '$ ', ENV = '' } })
end)
return b`, &buf, argv, r.root)
	r.m.terminalTabs = append(r.m.terminalTabs, terminalTab{BufID: buf, Name: "sh", Cwd: r.root, LastExit: -1})
	r.m.termOpen = true
	return buf
}

// webTermText is the mirror's rows of a terminal, joined.
func webTermText(r *webFilesRig, buf int) string {
	b := r.h.files.bufs[buf]
	if b == nil {
		return ""
	}
	return strings.Join(b.rows, "\n")
}

// Spec (WEB-19): term.send types through chansend (the AI tool's door):
// the shell runs what the phone typed and the rows follow the output as
// lines deltas that replay; key bar bytes (Ctrl+C) reach the shell too.
func TestTermLinesFollowOutput(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	buf := webTestShell(t, r, "sh")
	topic := "term:" + strconv.Itoa(buf)
	snap := r.sub(topic)
	if snap["buf"] != float64(buf) {
		t.Fatalf("snap: %v", snap)
	}
	if f := r.call(webproto.OpTermSend, &webproto.TermSendArgs{Buf: buf, Text: "echo web-$((40+2))\r"}); f["t"] != "res" {
		t.Fatalf("send: %v", f)
	}
	r.pumpUntil("the echo", func() bool { return strings.Contains(webTermText(r, buf), "web-42") })
	if f := r.call(webproto.OpTermSend, &webproto.TermSendArgs{Buf: buf, Text: "cat\r"}); f["t"] != "res" {
		t.Fatal(f)
	}
	if f := r.call(webproto.OpTermInterrupt, &webproto.BufArgs{Buf: buf}); f["t"] != "res" {
		t.Fatal(f)
	}
	r.call(webproto.OpTermSend, &webproto.TermSendArgs{Buf: buf, Text: "echo after-$((1+1))\r"})
	r.pumpUntil("after the interrupt", func() bool { return strings.Contains(webTermText(r, buf), "after-2") })
	// The deltas the browser got replay to what the hub holds.
	state, _ := json.Marshal(snap)
	for _, d := range r.deltas(topic) {
		b, _ := json.Marshal(d)
		next, err := webTermApply(state, b)
		if err != nil {
			t.Fatal(err)
		}
		state = next
	}
	var got webproto.TermSnap
	_ = json.Unmarshal(state, &got)
	if strings.Join(got.Rows, "\n") != webTermText(r, buf) {
		t.Fatalf("replay:\n%q\nhub:\n%q", got.Rows, webTermText(r, buf))
	}
}

// Spec (WEB-19, T20): a viewer may read terminals but never type; only
// listed shells can be typed into or read.
func TestTermSendViewerForbidden(t *testing.T) {
	r := newWebFilesRig(t)
	v := r.conn("v-1", webproto.RoleViewer)
	v.request(&webproto.Req{T: webproto.KindReq, ID: 1, Op: webproto.OpTermSend, Args: json.RawMessage(`{"buf":3,"text":"ls\r"}`)})
	v.request(&webproto.Req{T: webproto.KindReq, ID: 2, Op: webproto.OpTermInterrupt, Args: json.RawMessage(`{"buf":3}`)})
	fs := r.takeOn(v)
	if len(fs) != 2 || webErrCode(fs[0]) != "forbidden" || webErrCode(fs[1]) != "forbidden" {
		t.Fatalf("viewer: %v", fs)
	}
	if f := r.call(webproto.OpTermSend, &webproto.TermSendArgs{Buf: 3, Text: "ls\r"}); webErrCode(f) != "not_found" {
		t.Fatalf("unlisted shell: %v", f)
	}
	r.m.onWebSub(webSubMsg{hub: r.h, conn: r.c, topics: []string{"term:3"}})
	if fs := r.take(); len(fs) != 1 || webErrCode(fs[0]) != "not_found" {
		t.Fatalf("sub unlisted: %v", fs)
	}
}

// Spec (WEB-19): the snap holds the last 2000 rows; term.history pages
// the older ones and they join up with the snap.
func TestTermHistoryPaged(t *testing.T) {
	r := newWebFilesRig(t)
	r.withNvim()
	buf := webTestShell(t, r, "sh", "-c", `i=1; while [ $i -le 2600 ]; do echo row$i; i=$((i+1)); done; sleep 30`)
	r.pumpUntil("2600 rows", func() bool {
		var n int
		r.lua(`local b = ... return #vim.tbl_filter(function(l) return l ~= '' end, vim.api.nvim_buf_get_lines(b, 0, -1, false))`, &n, buf)
		return n >= 2600
	})
	snap := r.sub("term:" + strconv.Itoa(buf))
	first := int(snap["first"].(float64))
	rows := snap["rows"].([]any)
	if len(rows) != webproto.MaxTermRows || first <= 0 || snap["more"] != true {
		t.Fatalf("snap: first %d, %d rows, more %v", first, len(rows), snap["more"])
	}
	var h webproto.TermHistoryRes
	webResData(t, r.call(webproto.OpTermHistory, &webproto.TermHistoryArgs{Buf: buf, Before: first}), &h)
	if h.First != max(first-webproto.MaxTermRows, 0) || len(h.Rows) != first-h.First || h.More != (h.First > 0) {
		t.Fatalf("history: first %d, %d rows, more %v", h.First, len(h.Rows), h.More)
	}
	n0, _ := strconv.Atoi(strings.TrimPrefix(rows[0].(string), "row"))
	nl, _ := strconv.Atoi(strings.TrimPrefix(h.Rows[len(h.Rows)-1], "row"))
	if n0 == 0 || nl != n0-1 {
		t.Fatalf("history ends at %q, snap starts at %q", h.Rows[len(h.Rows)-1], rows[0])
	}
}

// Spec (WEB-19, WEB-7): the terms list follows the tabs and panes and
// replays; folders outside the workspace show by name only.
func TestTermsListReplayGolden(t *testing.T) {
	r := newWebFilesRig(t)
	r.m.terminalTabs = []terminalTab{{BufID: 3, Name: "zsh", Cwd: r.root}}
	r.m.termOpen = true
	c := webReplayRecord(t, r.m, r.h, "terms", func() {
		r.track()
		r.m.terminalTabs[0].Busy, r.m.terminalTabs[0].LastCmd = true, "go test ./..."
		r.track()
		r.m.terminalTabs = append(r.m.terminalTabs, terminalTab{BufID: 9, Name: "build", Task: "build", Cwd: "/opt/elsewhere",
			Splits: []termPane{{BufID: 11, Cwd: r.root + "/src"}}})
		r.m.terminalActiveTab = 1
		r.track()
	})
	if len(c.Deltas) != 2 {
		t.Fatalf("deltas: %s", c.Deltas)
	}
	webReplayCheck(t, webTopics["terms"], c)
	if s := string(c.SnapN); strings.Contains(s, "/opt") || !strings.Contains(s, `"cwd":"elsewhere"`) || !strings.Contains(s, `"pane_of":9`) {
		t.Fatalf("snap: %s", s)
	}
	webReplayGolden(t, webReplayPath("terms", "tabs_and_panes"), c)
}

// Spec (WEB-19, WEB-7): a terminal view replays: output, a redraw of a
// row, screen mode, a reset; the window keeps the last rows.
func TestTermReplayGolden(t *testing.T) {
	r := newWebFilesRig(t)
	r.m.terminalTabs = []terminalTab{{BufID: 5, Name: "sh", Cwd: r.root}}
	f := r.h.filesState()
	b := &webBuf{buf: 5, term: true, cap: webproto.MaxTermRows, rows: []string{"$ "}, tick: 3, height: 4, cols: 80, refs: map[string]bool{}}
	b.scr.total = 1
	f.bufs[5] = b
	seq := 0
	ev := func(first, last int, lines ...string) {
		seq++
		r.m.webBufCallback(webBufEvent{kind: "lines", buf: 5, seq: seq, tick: 3 + seq, first: first, last: last, lines: lines})
	}
	defer func(old func() time.Time) { webNow = old }(webNow)
	webNow = func() time.Time { return time.Unix(0, 0) }
	c := webReplayRecord(t, r.m, r.h, "term:5", func() {
		ev(0, 1, "$ top")
		ev(1, 1, "", "", "")
		ev(0, 4, "load 1", "cpu 3%", "mem 2G", "q quit")
		webNow = func() time.Time { return time.Unix(0, 0).Add(500 * time.Millisecond) }
		ev(0, 4, "load 2", "cpu 4%", "mem 2G", "q quit")
		webTermReset(r.m, b)
	})
	if !b.scr.on {
		t.Fatal("screen mode stayed off")
	}
	webReplayCheck(t, webTopics["term"], c)
	webReplayGolden(t, webReplayPath("term:5", "output_screen_reset"), c)
}
