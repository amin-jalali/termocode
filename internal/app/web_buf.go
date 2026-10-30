package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The file view (WEB-17, web-ui-design §2.4): a file opens through nvim,
// on the same buffer the laptop uses. buf.open loads it hidden (the
// laptop's windows do not change) and attaches it once; the hub keeps a
// mirror of its lines, fed by the attach callbacks (web_lua.go), and the
// "buf:<n>" topic sends the mirror as its snap and every change as a
// delta. Files that may hold secrets, binary files and huge files are
// never sent (buf.head shows the start of a big text file, read only).

// webBuf is one attached buffer and its mirror (Update only).
type webBuf struct {
	buf      int
	term     bool // a terminal buffer (web_term.go)
	path     string
	first    int      // the buffer row of rows[0] (a terminal keeps a tail window)
	rows     []string // the mirror
	cap      int      // rows kept (0 = the whole buffer)
	tick     int
	seq      int // the last callback seen
	dirty    bool
	ft       string
	readonly bool
	created  bool            // the web loaded it: unload it when let go, if untouched
	refs     map[string]bool // connection ids that opened it
}

// webLuaBuf is what webLuaAttach / webLuaOpen return.
type webLuaBuf struct {
	Err      string   `msgpack:"err"`
	Msg      string   `msgpack:"msg"`
	Buf      int      `msgpack:"buf"`
	Tick     int      `msgpack:"tick"`
	Dirty    bool     `msgpack:"dirty"`
	Ft       string   `msgpack:"ft"`
	Readonly bool     `msgpack:"readonly"`
	Name     string   `msgpack:"name"`
	Bt       string   `msgpack:"bt"`
	Count    int      `msgpack:"count"`
	Seq      int      `msgpack:"seq"`
	First    int      `msgpack:"first"`
	Lines    []string `msgpack:"lines"`
	Created  bool     `msgpack:"created"`
}

// webBufEvent is one attach callback.
type webBufEvent struct {
	kind           string // "lines", "tick", "reset"
	buf, seq, tick int
	first, last    int
	lines          []string
}

// webEarlyMax caps the callbacks kept while a buf.open is on its way.
const webEarlyMax = 4096

func init() {
	webTopics["buf"] = webTopicDef{snapshot: webBufSnapshot, apply: webBufApply}
	webOps[webproto.OpBufOpen] = webOpBufOpen
	webOps[webproto.OpBufHead] = webOpBufHead
	webOps[webproto.OpBufClose] = webOpBufClose
	registerNotifyHandler("termocode_web_lines", func(m *Model, a []any) tea.Cmd {
		m.webBufCallback(webBufEvent{kind: "lines", buf: notifyArgInt(a, 0), seq: notifyArgInt(a, 1),
			tick: notifyArgInt(a, 2), first: notifyArgInt(a, 3), last: notifyArgInt(a, 4), lines: notifyArgLines(a, 5)})
		m.webBufDirty(notifyArgInt(a, 0), notifyArgBool(a, 6))
		return nil
	})
	registerNotifyHandler("termocode_web_tick", func(m *Model, a []any) tea.Cmd {
		m.webBufCallback(webBufEvent{kind: "tick", buf: notifyArgInt(a, 0), seq: notifyArgInt(a, 1), tick: notifyArgInt(a, 2)})
		m.webBufDirty(notifyArgInt(a, 0), notifyArgBool(a, 3))
		return nil
	})
	registerNotifyHandler("termocode_web_reset", func(m *Model, a []any) tea.Cmd {
		m.webBufCallback(webBufEvent{kind: "reset", buf: notifyArgInt(a, 0), seq: notifyArgInt(a, 1),
			tick: notifyArgInt(a, 2), lines: notifyArgLines(a, 3)})
		return nil
	})
	registerNotifyHandler("termocode_web_dirty", func(m *Model, a []any) tea.Cmd {
		m.webBufDirty(notifyArgInt(a, 0), notifyArgBool(a, 1))
		return nil
	})
	registerNotifyHandler("termocode_web_gone", func(m *Model, a []any) tea.Cmd {
		m.webBufGone(notifyArgInt(a, 0))
		return nil
	})
}

// notifyArgLines returns args[i] as a list of strings (an empty Lua
// table may arrive as a map: no lines).
func notifyArgLines(args []any, i int) []string {
	if i < 0 || i >= len(args) {
		return []string{}
	}
	list, _ := args[i].([]any)
	out := make([]string, 0, len(list))
	for j := range list {
		out = append(out, notifyArgString(list, j))
	}
	return out
}

// webTopicFor is the topic of a buffer's view.
func (b *webBuf) topic() string {
	if b.term {
		return "term:" + strconv.Itoa(b.buf)
	}
	return "buf:" + strconv.Itoa(b.buf)
}

// webLinesApply replaces rows [d.First, d.Last) (buffer rows) in the
// window that starts at row first, then keeps the last max rows (0 = all).
// ok is false when the window cannot know the result (it would need rows
// it does not have): the caller then reads the buffer again.
func webLinesApply(first int, rows []string, d webproto.LinesDelta, max int) (int, []string, bool) {
	if d.First < 0 || d.Last < d.First {
		return first, rows, false
	}
	end := first + len(rows)
	if d.Last > end {
		return first, rows, false
	}
	var out []string
	switch {
	case d.Last <= first: // all above the window: it moves
		first += len(d.Lines) - (d.Last - d.First)
		out = rows
	case d.First < first: // reaches into the window from above
		out = make([]string, 0, len(d.Lines)+end-d.Last)
		out = append(append(out, d.Lines...), rows[d.Last-first:]...)
		first = d.First
	default:
		lf, ll := d.First-first, d.Last-first
		out = make([]string, 0, len(rows)-(ll-lf)+len(d.Lines))
		out = append(append(append(out, rows[:lf]...), d.Lines...), rows[ll:]...)
	}
	if first < 0 {
		return first, rows, false
	}
	if max > 0 && len(out) > max {
		first += len(out) - max
		out = out[len(out)-max:]
	}
	if max > 0 && len(out) < max && first > 0 { // older rows would show: unknown here
		return first, rows, false
	}
	return first, out, true
}

// webLinesOut is a lines delta as published: two of them merge while
// queued when the second rewrites exactly what the first wrote.
type webLinesOut struct {
	webproto.LinesDelta
	max int
}

func (d webLinesOut) coalesce(next any) (any, bool) {
	n, ok := next.(webLinesOut)
	if !ok || n.First != d.First || n.Last != d.First+len(d.Lines) {
		return nil, false
	}
	if d.max > 0 && len(d.Lines)+len(n.Lines) > d.max/2 { // a window trim could tell them apart
		return nil, false
	}
	merged := d
	merged.Lines, merged.Tick = n.Lines, n.Tick
	return merged, true
}

// webBufSnapshot is the "buf:<n>" snap: the mirror. A buffer no browser
// opened yet (a resumed view, a tap on the laptop's tab list) is
// attached here.
func webBufSnapshot(m *Model, key string) (any, error) {
	n, _ := strconv.Atoi(key)
	b, err := m.webBufAttached(n, false)
	if err != nil {
		return nil, err
	}
	return webproto.BufSnap{Buf: b.buf, Path: b.path, Tick: b.tick, Lines: webNonNil(b.rows),
		Dirty: b.dirty, Ft: b.ft, Readonly: b.readonly}, nil
}

func webNonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// webBufAttached returns the mirror of buffer n, attaching it when
// needed (a file inside the workspace, or a terminal when term is true).
func (m *Model) webBufAttached(n int, term bool) (*webBuf, error) {
	if m.web == nil {
		return nil, webErr(webproto.CodeGone, "The web side is off.")
	}
	f := m.web.filesState()
	if b := f.bufs[n]; b != nil {
		if b.term != term {
			return nil, webErr(webproto.CodeBadRequest, "That is not a file.")
		}
		return b, nil
	}
	if m.nvim == nil {
		return nil, webErr(webproto.CodeGone, "The editor is not running.")
	}
	tail := 0
	if term {
		tail = webTermRows
	}
	var info webLuaBuf
	if err := m.nvim.ExecLuaResult(webLuaAttach, &info, n, tail); err != nil {
		return nil, err
	}
	if info.Err != "" {
		return nil, webErr(webproto.CodeNotFound, "That buffer is not open.")
	}
	if term != (info.Bt == "terminal") {
		m.webDetach([]*webBuf{{buf: n}})
		return nil, webErr(webproto.CodeBadRequest, "That is not a file.")
	}
	b := &webBuf{buf: n, term: term, refs: map[string]bool{}}
	if !term {
		rel, err := m.webBufFileOK(info)
		if err != nil {
			m.webDetach([]*webBuf{b})
			return nil, err
		}
		b.path = rel
	} else {
		b.cap = webTermRows
	}
	b.load(info)
	f.bufs[n] = b
	return b, nil
}

// webBufFileOK checks an attached buffer may be shown: a plain file
// inside the workspace, not one that may hold secrets.
func (m *Model) webBufFileOK(info webLuaBuf) (string, error) {
	if info.Bt != "" {
		return "", webErr(webproto.CodeBadRequest, "That is not a file.")
	}
	rel, ok := m.webRel(info.Name)
	if !ok {
		return "", webPathOutside()
	}
	if ai.IsSecretFileName(info.Name) {
		return "", webErrSecret()
	}
	return rel, nil
}

func webErrSecret() error {
	return webErr(webproto.CodeForbidden, "This file may hold secrets. Open it on the laptop.")
}

// load sets the mirror from an attach result.
func (b *webBuf) load(info webLuaBuf) {
	b.first, b.rows, b.tick, b.seq = info.First, webNonNil(info.Lines), info.Tick, info.Seq
	b.dirty, b.ft, b.readonly = info.Dirty, info.Ft, info.Readonly
}

// webBufApply folds one buf delta into a snapshot (the browser store
// does the same).
func webBufApply(snap, delta json.RawMessage) (json.RawMessage, error) {
	var s webproto.BufSnap
	if err := json.Unmarshal(snap, &s); err != nil {
		return nil, err
	}
	var k struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(delta, &k); err != nil {
		return nil, err
	}
	switch k.Kind {
	case "lines":
		var d webproto.LinesDelta
		if err := json.Unmarshal(delta, &d); err != nil {
			return nil, err
		}
		_, rows, ok := webLinesApply(0, s.Lines, d, 0)
		if !ok {
			return nil, fmt.Errorf("buf: lines [%d,%d) outside %d lines", d.First, d.Last, len(s.Lines))
		}
		s.Lines, s.Tick = rows, d.Tick
	case "tick":
		var d webproto.TickDelta
		if err := json.Unmarshal(delta, &d); err != nil {
			return nil, err
		}
		s.Tick = d.Tick
	case "dirty":
		var d webproto.DirtyDelta
		if err := json.Unmarshal(delta, &d); err != nil {
			return nil, err
		}
		s.Dirty = d.On
	case "reset":
		var d webproto.ResetDelta
		if err := json.Unmarshal(delta, &d); err != nil {
			return nil, err
		}
		s.Tick, s.Lines, s.Dirty = d.Tick, d.Lines, d.Dirty
	case "gone":
	default:
		return nil, fmt.Errorf("buf: unknown delta kind %q", k.Kind)
	}
	s.Lines = webNonNil(s.Lines)
	return json.Marshal(s)
}

// webBufCallback applies one attach callback to the mirror and publishes
// it. A callback for a buffer whose buf.open is on its way waits; a gap
// in the sequence reads the buffer again (a reset).
func (m *Model) webBufCallback(ev webBufEvent) {
	if m.web == nil || m.web.files == nil {
		return
	}
	f := m.web.files
	b := f.bufs[ev.buf]
	if b == nil {
		if f.loading > 0 && f.earlyN < webEarlyMax {
			f.early[ev.buf] = append(f.early[ev.buf], ev)
			f.earlyN++
		}
		return
	}
	m.webBufApplyEvent(b, ev)
}

func (m *Model) webBufApplyEvent(b *webBuf, ev webBufEvent) {
	if ev.seq <= b.seq { // already in the mirror (it came before the attach answered)
		return
	}
	if ev.seq != b.seq+1 {
		m.webBufResync(b)
		return
	}
	b.seq = ev.seq
	switch ev.kind {
	case "lines":
		d := webproto.LinesDelta{Kind: "lines", First: ev.first, Last: ev.last, Lines: ev.lines, Tick: ev.tick}
		first, rows, ok := webLinesApply(b.first, b.rows, d, b.cap)
		if !ok {
			m.webBufResync(b)
			return
		}
		b.first, b.rows, b.tick = first, rows, ev.tick
		m.webPublish(b.topic(), webLinesOut{LinesDelta: d, max: b.cap})
		m.webBufLinesSeen(b, d)
	case "tick":
		b.tick = ev.tick
		m.webPublish(b.topic(), webproto.TickDelta{Kind: "tick", Tick: ev.tick})
	case "reset":
		if b.term {
			m.webBufResync(b)
			return
		}
		b.first, b.rows, b.tick = 0, ev.lines, ev.tick
		m.webPublish(b.topic(), webproto.ResetDelta{Kind: "reset", Tick: b.tick, Lines: webNonNil(b.rows), Dirty: b.dirty})
	}
}

// webBufLinesSeen is a hook for the terminal view (screen mode).
var webBufLinesSeen = func(*Model, *webBuf, webproto.LinesDelta) {}

func (m *Model) webBufLinesSeen(b *webBuf, d webproto.LinesDelta) { webBufLinesSeen(m, b, d) }

// webBufResync reads the whole buffer (window) again and publishes it as
// a reset. Rare: a lost callback or a window that cannot follow.
func (m *Model) webBufResync(b *webBuf) {
	if m.nvim == nil {
		return
	}
	tail := 0
	if b.term {
		tail = webTermRows
	}
	var info webLuaBuf
	if err := m.nvim.ExecLuaResult(webLuaAttach, &info, b.buf, tail); err != nil || info.Err != "" {
		m.webBufGone(b.buf)
		return
	}
	b.load(info)
	m.webPublishReset(b)
}

// webPublishReset sends the mirror as a whole (terminals: web_term.go).
var webTermReset = func(*Model, *webBuf) {}

func (m *Model) webPublishReset(b *webBuf) {
	if b.term {
		webTermReset(m, b)
		return
	}
	m.webPublish(b.topic(), webproto.ResetDelta{Kind: "reset", Tick: b.tick, Lines: webNonNil(b.rows), Dirty: b.dirty})
}

func (m *Model) webBufDirty(n int, on bool) {
	if m.web == nil || m.web.files == nil {
		return
	}
	b := m.web.files.bufs[n]
	if b == nil || b.dirty == on {
		return
	}
	b.dirty = on
	if !b.term {
		m.webPublish(b.topic(), webproto.DirtyDelta{Kind: "dirty", On: on})
	}
}

// webBufGone: the buffer was unloaded or deleted on the laptop.
func (m *Model) webBufGone(n int) {
	if m.web == nil || m.web.files == nil {
		return
	}
	b := m.web.files.bufs[n]
	if b == nil {
		return
	}
	delete(m.web.files.bufs, n)
	m.webPublish(b.topic(), webproto.GoneDelta{Kind: "gone"})
}

// webDetach lets go of buffers in nvim.
func (m *Model) webDetach(bs []*webBuf) {
	if m.nvim == nil || len(bs) == 0 {
		return
	}
	list := make([][]any, 0, len(bs))
	for _, b := range bs {
		list = append(list, []any{b.buf, b.created})
	}
	var ok bool
	if err := m.nvim.ExecLuaResult(webLuaDetach, &ok, list); err != nil {
		recordError("[web] detach: " + err.Error())
	}
}

// webReleaseBufs lets go of buffers no browser follows or holds open.
func (m *Model) webReleaseBufs(h *webHub, f *webFiles) {
	var drop []*webBuf
	for n, b := range f.bufs {
		for id := range b.refs {
			if !h.webConnKnown(id) {
				delete(b.refs, id)
			}
		}
		if len(b.refs) == 0 && !h.webListens(b.topic()) {
			drop = append(drop, b)
			delete(f.bufs, n)
		}
	}
	sort.Slice(drop, func(i, j int) bool { return drop[i].buf < drop[j].buf })
	m.webDetach(drop)
}

// webOpenedBy counts the buffers connection id holds open.
func (f *webFiles) openedBy(id string) int {
	n := 0
	for _, b := range f.bufs {
		if b.refs[id] {
			n++
		}
	}
	return n
}

// webFailData answers call with an err that carries data.
func webFailData(call *webCall, code webproto.ErrCode, msg string, data any) {
	raw, err := webData(data)
	if err != nil {
		raw = nil
	}
	call.answer(&webproto.ErrMsg{T: webproto.KindErr, ID: call.id, Code: code, Msg: webText(msg), Data: raw})
}

// webFileCheck is what buf.open learns off Update before it loads.
type webFileCheck struct {
	code   webproto.ErrCode
	msg    string
	reason string // "big", "binary" (err data, so the page offers buf.head)
	size   int64
}

// webCheckFile refuses what must not be loaded: not a regular file,
// over MaxOpenBytes, or a NUL in the first BinarySniff bytes.
func webCheckFile(abs string) webFileCheck {
	info, err := os.Stat(abs)
	if err != nil {
		return webFileCheck{code: webproto.CodeNotFound, msg: "That file does not exist."}
	}
	if !info.Mode().IsRegular() {
		return webFileCheck{code: webproto.CodeBadRequest, msg: "That is not a file."}
	}
	head, err := webReadHead(abs, webproto.BinarySniff)
	if err != nil {
		return webFileCheck{code: webproto.CodeNotFound, msg: "That file cannot be read."}
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return webFileCheck{code: webproto.CodeLimit, msg: "This is a binary file. It is not shown here.", reason: "binary", size: info.Size()}
	}
	if info.Size() > webproto.MaxOpenBytes {
		return webFileCheck{code: webproto.CodeLimit, msg: "This file is too big to open here. You can see its start.", reason: "big", size: info.Size()}
	}
	return webFileCheck{size: info.Size()}
}

func webReadHead(abs string, n int) ([]byte, error) {
	fh, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	buf := make([]byte, n)
	k, err := io.ReadFull(fh, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:k], nil
}

func webOpBufOpen(m *Model, call *webCall) tea.Cmd {
	raw := call.args.(*webproto.PathArgs).Path
	abs, err := m.webResolve(raw)
	if err != nil {
		webFail(call, err)
		return nil
	}
	if _, remote := m.remoteFS(); remote {
		call.fail(webproto.CodeNotFound, "Files of a container workspace open on the laptop only.")
		return nil
	}
	if ai.IsSecretFileName(abs) {
		webFail(call, webErrSecret())
		return nil
	}
	if m.nvim == nil {
		call.fail(webproto.CodeGone, "The editor is not running.")
		return nil
	}
	f := m.web.filesState()
	if f.openedBy(call.conn.id) >= webMaxBufTopics {
		call.fail(webproto.CodeLimit, "Too many open files. Close some first.")
		return nil
	}
	f.loading++
	if f.early == nil {
		f.early = map[int][]webBufEvent{}
	}
	client := m.nvim
	call.later()
	return func() tea.Msg {
		chk := webCheckFile(abs)
		var info webLuaBuf
		var lerr error
		if chk.code == "" {
			lerr = client.ExecLuaResult(webLuaOpen, &info, abs, webproto.MaxOpenLines)
		}
		return applyMsg(func(m *Model) tea.Cmd {
			m.webBufOpened(call, chk, info, lerr)
			return nil
		})
	}
}

// webBufOpened finishes buf.open on Update.
func (m *Model) webBufOpened(call *webCall, chk webFileCheck, info webLuaBuf, lerr error) {
	if m.web == nil {
		call.fail(webproto.CodeGone, "The web side is off.")
		return
	}
	f := m.web.filesState()
	f.loading--
	defer func() {
		if f.loading == 0 {
			f.early, f.earlyN = map[int][]webBufEvent{}, 0
		}
	}()
	switch {
	case chk.code != "":
		if chk.reason != "" {
			webFailData(call, chk.code, chk.msg, map[string]any{"reason": chk.reason, "size": chk.size})
		} else {
			call.fail(chk.code, chk.msg)
		}
		return
	case lerr != nil:
		recordError("[web] buf.open: " + lerr.Error())
		call.fail(webproto.CodeInternal, "The editor could not open that file.")
		return
	case info.Err == "lines":
		webFailData(call, webproto.CodeLimit, "This file has too many lines to open here. You can see its start.",
			map[string]any{"reason": "big", "size": chk.size})
		return
	case info.Err != "":
		call.fail(webproto.CodeBadRequest, "The editor could not open that file.")
		return
	}
	b := f.bufs[info.Buf]
	if b == nil {
		rel, err := m.webBufFileOK(info)
		if err != nil {
			m.webDetach([]*webBuf{{buf: info.Buf, created: info.Created}})
			webFail(call, err)
			return
		}
		b = &webBuf{buf: info.Buf, path: rel, created: info.Created, refs: map[string]bool{}}
		b.load(info)
		f.bufs[b.buf] = b
		for _, ev := range f.early[b.buf] { // callbacks that came before this answer
			m.webBufApplyEvent(b, ev)
		}
		delete(f.early, b.buf)
	}
	b.refs[call.conn.id] = true
	call.reply(webproto.BufOpenRes{Buf: b.buf, Path: b.path, Tick: b.tick, Lines: len(b.rows),
		Dirty: b.dirty, Ft: b.ft, Readonly: b.readonly})
}

// webOpBufClose drops this connection's hold on a buffer; the laptop's
// buffer stays as it is.
func webOpBufClose(m *Model, call *webCall) tea.Cmd {
	n := call.args.(*webproto.BufArgs).Buf
	if b := m.web.filesState().bufs[n]; b != nil {
		delete(b.refs, call.conn.id)
	}
	call.reply(nil)
	return nil
}

func webOpBufHead(m *Model, call *webCall) tea.Cmd {
	raw := call.args.(*webproto.PathArgs).Path
	abs, err := m.webResolve(raw)
	if err != nil {
		webFail(call, err)
		return nil
	}
	if ai.IsSecretFileName(abs) {
		webFail(call, webErrSecret())
		return nil
	}
	rel := webCleanRel(raw)
	fsys, remote := m.remoteFS()
	call.later()
	return func() tea.Msg {
		var data []byte
		var size int64
		var err error
		if remote {
			var info os.FileInfo
			if info, err = fsys.Stat(context.Background(), abs); err == nil {
				size = info.Size()
				if !info.Mode().IsRegular() {
					err = errors.New("not a file")
				} else if size > webHeadMaxRead*16 {
					err = errors.New("too big to read")
				} else {
					data, err = fsys.ReadFile(context.Background(), abs)
				}
			}
		} else {
			var info os.FileInfo
			if info, err = os.Stat(abs); err == nil {
				size = info.Size()
				if !info.Mode().IsRegular() {
					err = errors.New("not a file")
				} else {
					data, err = webReadHead(abs, webHeadMaxRead)
				}
			}
		}
		if err != nil {
			call.fail(webproto.CodeNotFound, "That file cannot be read.")
			return nil
		}
		sniff := data
		if len(sniff) > webproto.BinarySniff {
			sniff = sniff[:webproto.BinarySniff]
		}
		if bytes.IndexByte(sniff, 0) >= 0 {
			webFailData(call, webproto.CodeLimit, "This is a binary file. It is not shown here.", map[string]any{"reason": "binary", "size": size})
			return nil
		}
		lines, cut := webHeadLines(data, webproto.MaxHeadLines)
		call.reply(webproto.BufHeadRes{Path: rel, Lines: lines, Truncated: cut || int64(len(data)) < size, Size: size})
		return nil
	}
}

// webHeadMaxRead caps what buf.head reads of a file.
const webHeadMaxRead = 1 << 20

// webHeadLines splits data into at most max lines (a cut last line is
// dropped when more data follows).
func webHeadLines(data []byte, max int) ([]string, bool) {
	out := []string{}
	for len(data) > 0 && len(out) < max {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			out = append(out, string(bytes.TrimSuffix(data, []byte("\r"))))
			data = nil
			break
		}
		out = append(out, string(bytes.TrimSuffix(data[:i], []byte("\r"))))
		data = data[i+1:]
	}
	return out, len(data) > 0
}

// webTermRows is the tail of a terminal the web keeps and sends.
const webTermRows = 2000
