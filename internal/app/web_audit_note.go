package app

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The gateway's audit of web ops (web design 3.5, WEB-F2). When a write
// op answers, the hub builds a short summary from the policy table and
// the op's args (a path, a label, a count: never chat, file or terminal
// text, or a secret) and sends it on every control connection; the
// gateway writes it to its 0600 JSONL log with by=web:<device>. An op
// audited "yes" gives one line per call; a "counted" op one line per
// minute per connection with the number of calls and bytes (never the
// text), flushed by the next call after the minute or when the
// connection ends: no timer runs in the UI process. The laptop's Web
// channel (web_audit.go) is kept as it was.

// webCountWindow is how long a counted op's calls are summed.
const webCountWindow = time.Minute

// webAuditCounts sums a connection's counted ops (any goroutine).
type webAuditCounts struct {
	mu   sync.Mutex
	byOp map[string]*webCount
}

type webCount struct {
	since         time.Time
	calls, failed int
	bytes         int
}

// auditNote sends n to every control connection (any goroutine).
func (h *webHub) auditNote(n webproto.AuditNote) {
	data, err := json.Marshal(n)
	if err != nil {
		return
	}
	for _, c := range h.ctlConns() {
		c.send(&webproto.Event{T: webproto.KindEv, Kind: webproto.EvAudit, Data: data})
	}
}

// ctlConns lists the control connections.
func (h *webHub) ctlConns() []*webConn {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*webConn
	for _, c := range h.all {
		if c != nil && c.role == webproto.RoleCtl {
			out = append(out, c)
		}
	}
	return out
}

// audited is called once when call answered with m (any goroutine).
func (c *webCall) audited(m webproto.Msg) {
	conn := c.conn
	if conn == nil || conn.hub == nil || conn.role == webproto.RoleCtl {
		return
	}
	p, ok := webproto.Lookup(c.op)
	if !ok {
		return
	}
	n := webproto.AuditNote{Conn: conn.id, Device: webAuditDevice(conn.device), Op: c.op, OK: true}
	if e, isErr := m.(*webproto.ErrMsg); isErr {
		n.OK, n.Why = false, string(e.Code)
	}
	switch p.Audit {
	case webproto.AuditYes:
		if n.OK && p.LaptopCard {
			n.Why = "laptop_card" // the outcome follows when the laptop answers
		}
		n.Summary = webAuditSummary(c.args)
		conn.hub.auditNote(n)
	case webproto.AuditCounted:
		conn.count(c.op, webAuditBytes(c.args), n.OK)
	}
}

// count adds one call of a counted op; a window older than a minute is
// flushed first.
func (c *webConn) count(op string, bytes int, ok bool) {
	now := webNow()
	c.counts.mu.Lock()
	if c.counts.byOp == nil {
		c.counts.byOp = map[string]*webCount{}
	}
	var flush []webproto.AuditNote
	w := c.counts.byOp[op]
	if w != nil && now.Sub(w.since) >= webCountWindow {
		flush = append(flush, c.countNote(op, w))
		w = nil
	}
	if w == nil {
		w = &webCount{since: now}
		c.counts.byOp[op] = w
	}
	w.calls++
	w.bytes += bytes
	if !ok {
		w.failed++
	}
	c.counts.mu.Unlock()
	for _, n := range flush {
		c.hub.auditNote(n)
	}
}

// flushCounts sends what the counted ops summed so far (the connection
// ends).
func (c *webConn) flushCounts() {
	c.counts.mu.Lock()
	var flush []webproto.AuditNote
	for op, w := range c.counts.byOp {
		flush = append(flush, c.countNote(op, w))
	}
	c.counts.byOp = nil
	c.counts.mu.Unlock()
	for _, n := range flush {
		c.hub.auditNote(n)
	}
}

func (c *webConn) countNote(op string, w *webCount) webproto.AuditNote {
	s := strconv.Itoa(w.calls) + " calls"
	if w.failed > 0 {
		s += " (" + strconv.Itoa(w.failed) + " failed)"
	}
	s += ", " + strconv.Itoa(w.bytes) + " bytes"
	return webproto.AuditNote{Conn: c.id, Device: webAuditDevice(c.device), Op: op, OK: w.failed == 0, Why: "counted", Summary: s}
}

func webAuditDevice(d string) string {
	if d = webName(d); d == "" {
		return "web"
	}
	return d
}

// webAuditSummary is what the audit keeps of an op's args: names and
// counts only. Chat text, commit messages and typed text become a byte
// count; every string passes redaction and is cut short.
func webAuditSummary(args any) string {
	var s string
	switch a := args.(type) {
	case *webproto.PathArgs:
		s = a.Path
	case *webproto.PathsArgs:
		s = webAuditPaths(a.Paths)
	case *webproto.GitHunkArgs:
		s = a.Path
	case *webproto.BufArgs:
		s = "buf " + strconv.Itoa(a.Buf)
	case *webproto.LabelArgs:
		s = a.Label
	case *webproto.KeyArgs:
		s = a.Key
	case *webproto.SearchRunArgs:
		s = a.Query
	case *webproto.CardsAnswerArgs:
		s = a.ID + " " + a.Choice
	case *webproto.ChatSendArgs:
		s = strconv.Itoa(len(a.Text)) + " bytes"
	case *webproto.GitCommitArgs:
		s = "message of " + strconv.Itoa(len(a.Message)) + " bytes"
	}
	s = webName(ai.Redact(s))
	if r := []rune(s); len(r) > webAuditMax {
		s = string(r[:webAuditMax-1]) + "…"
	}
	return s
}

// webAuditBytes is the size of what a counted op carried.
func webAuditBytes(args any) int {
	switch a := args.(type) {
	case *webproto.TermSendArgs:
		return len(a.Text)
	case *webproto.SetTextArgs:
		n := 0
		for _, l := range a.Lines {
			n += len(l) + 1
		}
		return n
	}
	return 0
}

// webAuditDecided tells the gateway how a laptop card ended.
func (h *webHub) webAuditDecided(a *webAsk, choice, by string) {
	why := "confirmed_laptop"
	switch {
	case choice != webproto.ChoiceConfirm && by == "laptop":
		why = "cancelled_laptop"
	case choice != webproto.ChoiceConfirm:
		why = "cancelled_web"
	}
	h.auditNote(webproto.AuditNote{Conn: a.conn, Device: webAuditDevice(a.device), Op: a.op,
		OK: choice == webproto.ChoiceConfirm, Why: why, Summary: webAuditSummaryText(a.summary)})
}

// webAuditSummaryText cleans and cuts a summary built elsewhere.
func webAuditSummaryText(s string) string {
	s = webName(ai.Redact(strings.TrimSpace(s)))
	if r := []rune(s); len(r) > webAuditMax {
		s = string(r[:webAuditMax-1]) + "…"
	}
	return s
}
