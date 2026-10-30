package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Terminals (WEB-19, web-ui-design §2.5). A terminal is an nvim terminal
// buffer; the web reads it through the same attach as a file (rows as
// lines deltas, a window of the last MaxTermRows rows) and types into it
// through chansend, like the AI's terminal_send. Keys of the key bar
// (Esc, Tab, Ctrl+x, arrows) come as the bytes a terminal sends; they go
// to the shell as they are. The browser never resizes anything: rows are
// wrapped at the laptop's width, and the page says so.

func init() {
	webTopics["terms"] = webTopicDef{snapshot: webTermsSnapshot, apply: webTermsApply}
	webTopics["term"] = webTopicDef{snapshot: webTermSnapshot, apply: webTermApply}
	webOps[webproto.OpTermHistory] = webOpTermHistory
	webOps[webproto.OpTermSend] = webOpTermSend
	webOps[webproto.OpTermInterrupt] = webOpTermInterrupt
	webOps[webproto.OpTermNew] = webOpTermNew
	webOps[webproto.OpTermShowOnLaptop] = webOpTermShow
	webTermReset = func(m *Model, b *webBuf) {
		m.webPublish(b.topic(), webproto.TermResetDelta{Kind: "reset", First: b.first, Rows: webNonNil(b.rows), More: b.first > 0, Tick: b.tick})
	}
	webBufLinesSeen = func(m *Model, b *webBuf, d webproto.LinesDelta) {
		if b.term && b.scr.feed(webNow(), d.First, d.Last, len(d.Lines), b.first+len(b.rows), b.height) {
			m.webPublish(b.topic(), webproto.TermModeDelta{Kind: "mode", Screen: b.scr.on, Cols: b.cols, Height: b.height})
		}
	}
}

// webTermsNow is every shell, tab by tab (the shown tabs, then the
// hidden ones), with the panes of each tab after it.
func (m *Model) webTermsNow() webproto.TermsSnap {
	out := webproto.TermsSnap{Terms: []webproto.TermItem{}}
	add := func(tabs []terminalTab, active int, shown bool) {
		for i, t := range tabs {
			title := t.Name
			if t.Task != "" {
				title = t.Task
			}
			out.Terms = append(out.Terms, webproto.TermItem{Buf: t.BufID, Title: webName(title), Cwd: m.webCwd(t.Cwd),
				Busy: t.Busy, LastCmd: webName(t.LastCmd), Exited: t.Exited0, Task: webName(t.Task),
				Active: shown && i == active && t.focusedPane() == 0})
			for j, p := range t.Splits {
				out.Terms = append(out.Terms, webproto.TermItem{Buf: p.BufID, Title: webName(t.Name), Cwd: m.webCwd(p.Cwd),
					Busy: p.Busy, LastCmd: webName(p.LastCmd), Exited: p.Exited, PaneOf: t.BufID,
					Active: shown && i == active && t.focusedPane() == j+1})
			}
		}
	}
	add(m.terminalTabs, m.terminalActiveTab, m.termOpen)
	add(m.hiddenTerminalTabs, m.hiddenTerminalActive, false)
	return out
}

// webCwd is a shell's folder for the page: workspace-relative, or only
// its name when it is outside the workspace (never an absolute path).
func (m *Model) webCwd(dir string) string {
	if rel, ok := m.webRel(dir); ok {
		return rel
	}
	if dir == "" {
		return ""
	}
	return webName(filepath.Base(dir))
}

// webTermTab finds the tab of a shell buffer (a pane names its tab).
func (m *Model) webTermTab(buf int) (tab int, ok bool) {
	for _, tabs := range [][]terminalTab{m.terminalTabs, m.hiddenTerminalTabs} {
		for _, t := range tabs {
			for i := 0; i < t.paneCount(); i++ {
				if t.paneBuf(i) == buf {
					return t.BufID, true
				}
			}
		}
	}
	return 0, false
}

func webTermsSnapshot(m *Model, _ string) (any, error) {
	s := m.webTermsNow()
	if m.web != nil {
		m.web.filesState().termsList = &s
	}
	return s, nil
}

// webTermsApply: the delta is the whole list.
func webTermsApply(_, delta json.RawMessage) (json.RawMessage, error) {
	var s webproto.TermsSnap
	if err := json.Unmarshal(delta, &s); err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

// webTrackTerms publishes the list when it changed and someone follows it.
func (m *Model) webTrackTerms(h *webHub, f *webFiles) {
	if !h.webListens("terms") {
		f.termsList = nil
		return
	}
	now := m.webTermsNow()
	if f.termsList != nil && reflect.DeepEqual(*f.termsList, now) {
		return
	}
	f.termsList = &now
	m.webPublish("terms", now)
}

func webErrNoTerm() error { return webErr(webproto.CodeNotFound, "That terminal is closed.") }

func webTermSnapshot(m *Model, key string) (any, error) {
	n, _ := strconv.Atoi(key)
	if _, ok := m.webTermTab(n); !ok {
		return nil, webErrNoTerm()
	}
	b, err := m.webBufAttached(n, true)
	if err != nil {
		return nil, err
	}
	if b.height <= 0 { // not on show: the panel's size
		b.height = max(m.integratedTerminalRows()-1, 0)
	}
	return webproto.TermSnap{Buf: b.buf, First: b.first, Rows: webNonNil(b.rows), More: b.first > 0,
		Tick: b.tick, Screen: b.scr.on, Cols: b.cols, Height: b.height}, nil
}

// webTermApply folds one term delta into a snapshot (the browser store
// does the same): lines keep the window's last MaxTermRows rows.
func webTermApply(snap, delta json.RawMessage) (json.RawMessage, error) {
	var s webproto.TermSnap
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
		first, rows, ok := webLinesApply(s.First, s.Rows, d, webproto.MaxTermRows)
		if !ok {
			return nil, fmt.Errorf("term: lines [%d,%d) do not fit the window at %d", d.First, d.Last, s.First)
		}
		s.First, s.Rows, s.Tick = first, rows, d.Tick
	case "tick":
		var d webproto.TickDelta
		if err := json.Unmarshal(delta, &d); err != nil {
			return nil, err
		}
		s.Tick = d.Tick
	case "reset":
		var d webproto.TermResetDelta
		if err := json.Unmarshal(delta, &d); err != nil {
			return nil, err
		}
		s.First, s.Rows, s.Tick = d.First, d.Rows, d.Tick
	case "mode":
		var d webproto.TermModeDelta
		if err := json.Unmarshal(delta, &d); err != nil {
			return nil, err
		}
		s.Screen, s.Cols, s.Height = d.Screen, d.Cols, d.Height
	case "gone":
	default:
		return nil, fmt.Errorf("term: unknown delta kind %q", k.Kind)
	}
	s.Rows = webNonNil(s.Rows)
	s.More = s.First > 0
	return json.Marshal(s)
}

// webOpTermHistory pages older rows: [before-MaxTermRows, before).
func webOpTermHistory(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.TermHistoryArgs)
	if _, ok := m.webTermTab(a.Buf); !ok {
		webFail(call, webErrNoTerm())
		return nil
	}
	if m.nvim == nil {
		call.fail(webproto.CodeGone, "The editor is not running.")
		return nil
	}
	client, from := m.nvim, max(a.Before-webproto.MaxTermRows, 0)
	call.later()
	return func() tea.Msg {
		var res webLuaBuf
		if err := client.ExecLuaResult(webLuaRows, &res, a.Buf, from, a.Before); err != nil || res.Err != "" {
			call.fail(webproto.CodeGone, "That terminal is closed.")
			return nil
		}
		rows := webNonNil(res.Lines)
		for i := range rows {
			rows[i] = webClean(rows[i])
		}
		call.reply(webproto.TermHistoryRes{First: res.First, Rows: rows, More: res.First > 0})
		return nil
	}
}

// webTermType types text into a shell with chansend (aiTermSendLua).
func (m *Model) webTermType(call *webCall, buf int, text string) {
	if _, ok := m.webTermTab(buf); !ok {
		webFail(call, webErrNoTerm())
		return
	}
	if m.nvim == nil {
		call.fail(webproto.CodeGone, "The editor is not running.")
		return
	}
	var tick int
	if err := m.nvim.ExecLuaResult(aiTermSendLua, &tick, buf, text); err != nil {
		webFail(call, err)
		return
	}
	if tick < 0 {
		call.fail(webproto.CodeGone, "This shell has ended.")
		return
	}
	m.web.filesState().typedAt = webNow() // the "typing from the phone" mark
	call.reply(nil)
}

func webOpTermSend(m *Model, call *webCall) tea.Cmd {
	a := call.args.(*webproto.TermSendArgs)
	m.webTermType(call, a.Buf, a.Text)
	return nil
}

func webOpTermInterrupt(m *Model, call *webCall) tea.Cmd {
	m.webTermType(call, call.args.(*webproto.BufArgs).Buf, "\x03")
	return nil
}

// webOpTermNew starts a shell tab; the laptop's focus stays where it was.
func webOpTermNew(m *Model, call *webCall) tea.Cmd {
	if m.nvim == nil {
		call.fail(webproto.CodeGone, "The editor is not running.")
		return nil
	}
	restore := m.aiTermKeepFocus()
	idx, err := m.aiTermNewTab()
	restore()
	if err != nil {
		call.fail(webproto.CodeInternal, "A new shell could not start.")
		return nil
	}
	tabs, _, _ := m.aiTermTabs()
	call.reply(webproto.TermNewRes{Buf: tabs[idx].BufID})
	return nil
}

// webOpTermShow shows a shell's tab on the laptop (the user asked for it).
func webOpTermShow(m *Model, call *webCall) tea.Cmd {
	tab, ok := m.webTermTab(call.args.(*webproto.BufArgs).Buf)
	if !ok || m.nvim == nil {
		webFail(call, webErrNoTerm())
		return nil
	}
	if _, err := m.aiTermShow(tab); err != nil {
		webFail(call, webErrNoTerm())
		return nil
	}
	call.reply(nil)
	return nil
}
