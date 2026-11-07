package app

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/httpclient"
	"github.com/amin-jalali/termocode/internal/picker"
	"github.com/amin-jalali/termocode/internal/theme"
	"github.com/amin-jalali/termocode/internal/toast"
)

// The WebSocket client UI (API-17). Sending a request whose method is
// WEBSOCKET (or whose URL is ws:// / wss://) opens a session on the
// Response page instead of an HTTP send:
//
//	### chat
//	WEBSOCKET wss://echo.example/ws
//	Authorization: Bearer {{token}}
//
//	{"op":"hello"}
//	===
//	{"op":"ping"}
//
// The body's `===`-separated blocks are saved messages. On the page:
// m picks a saved message to send, i types one, Esc disconnects, q
// closes the page (the session stays until Esc or a new one). One
// session at a time; the page keeps the newest httpWSShow messages.

// promptKindHTTPWSType asks for a message to send.
const promptKindHTTPWSType promptKindEnum = 4622

// pickerKindHTTPWS is the saved-messages picker.
const pickerKindHTTPWS pickerKindEnum = 4630

// httpWSShow is how many messages the page shows (the engine keeps
// 1000); httpWSClip caps one message's text on the page.
const (
	httpWSShow = 300
	httpWSClip = 4000
)

type httpWSStateKind int

const (
	httpWSConnecting httpWSStateKind = iota
	httpWSOpen
	httpWSClosed
)

// httpWSState is the one WebSocket session.
type httpWSState struct {
	run    *httpRun
	conn   *httpclient.WSConn
	cancel context.CancelFunc
	saved  []string
	log    []httpclient.WSMessage
	state  httpWSStateKind
	err    string
	gen    int
}

// httpWSEvent is one step of a session: connected, a message or closed.
type httpWSEvent struct {
	conn *httpclient.WSConn
	msg  *httpclient.WSMessage
	err  error
	done bool
}

type httpWSEventMsg struct {
	gen int
	ch  <-chan httpWSEvent
	ev  httpWSEvent
}

func waitHTTPWS(gen int, ch <-chan httpWSEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return httpWSEventMsg{gen: gen, ch: ch, ev: ev}
	}
}

// httpStartWS opens a session for run (the old one closes first).
func (m *Model) httpStartWS(run *httpRun, hooks httpclient.Hooks) tea.Cmd {
	s := m.httpS()
	m.httpCloseWS()
	set := loadHTTPSettings()
	client := s.httpClient(set)
	if set.tlsInsecure() {
		hooks.Insecure = true
	}
	httpUserTLS(run, &hooks)
	s.wsGen++
	gen := s.wsGen
	ctx, cancel := context.WithCancel(context.Background())
	ws := &httpWSState{run: run, cancel: cancel, saved: httpclient.WSMessages(run.rr.Body), state: httpWSConnecting, gen: gen}
	s.ws = ws
	ch := make(chan httpWSEvent, 16)
	rr := run.rr
	crash.Go("the WebSocket client", func() {
		defer close(ch)
		conn, err := client.DialWS(ctx, rr, hooks)
		if err != nil {
			ch <- httpWSEvent{err: err, done: true}
			return
		}
		ch <- httpWSEvent{conn: conn}
		for {
			msg, err := conn.Read(ctx)
			if err != nil {
				if ctx.Err() != nil {
					err = nil // the user disconnected
				}
				ch <- httpWSEvent{err: err, done: true}
				return
			}
			ch <- httpWSEvent{msg: &msg}
		}
	})
	return tea.Batch(m.httpShowWS(), waitHTTPWS(gen, ch))
}

// httpWSHandle applies one session event.
func (m *Model) httpWSHandle(msg httpWSEventMsg) tea.Cmd {
	s := m.httpS()
	ws := s.ws
	if ws == nil || ws.gen != msg.gen {
		return nil
	}
	ev := msg.ev
	switch {
	case ev.conn != nil:
		ws.conn, ws.state = ev.conn, httpWSOpen
	case ev.msg != nil:
		ws.log = httpWSTail(ws.conn.Log())
	case ev.done:
		ws.state = httpWSClosed
		if ev.err != nil {
			ws.err = ai.Redact(ev.err.Error())
		}
		if ws.conn != nil {
			_ = ws.conn.Close()
		}
		return m.httpShowWS()
	}
	return tea.Batch(m.httpShowWS(), waitHTTPWS(msg.gen, msg.ch))
}

// httpCloseWS ends the session (no-op without one).
func (m *Model) httpCloseWS() {
	s := m.httpS()
	if s.ws == nil || s.ws.state == httpWSClosed {
		return
	}
	s.ws.cancel()
	if s.ws.conn != nil {
		_ = s.ws.conn.Close()
	}
	s.ws.state = httpWSClosed
}

// httpShowWS draws the session on the Response page.
func (m *Model) httpShowWS() tea.Cmd {
	s := m.httpS()
	if m.nvim == nil || s.ws == nil {
		return nil
	}
	s.wsShown = true
	return m.httpOpenPage(buildHTTPWSPage(s.ws))
}

// buildHTTPWSPage lays the session out like the Response page.
func buildHTTPWSPage(ws *httpWSState) httpPage {
	rr := ws.run.rr
	req := "WEBSOCKET " + ai.Redact(httpclient.RedactURL(rr.URL))
	sent, got := 0, 0
	for _, m := range ws.log {
		if m.Dir == httpclient.WSOut {
			sent++
		} else {
			got++
		}
	}
	var summary, hl string
	switch ws.state {
	case httpWSConnecting:
		summary, hl = httpGlyphFlight.String()+" connecting…", "TermocodeHttpSummary"
	case httpWSOpen:
		summary, hl = fmt.Sprintf("● connected · %d sent · %d received", sent, got), "TermocodeHttpStatus2xx"
	default:
		summary, hl = fmt.Sprintf("closed · %d sent · %d received", sent, got), "TermocodeHttpSummary"
		if ws.err != "" {
			summary, hl = "closed · "+ws.err, "TermocodeHttpStatusErr"
		}
	}
	var pairs [][2]string
	if ws.state == httpWSClosed {
		pairs = [][2]string{{"r", "reconnect"}, {"q", "close"}}
	} else {
		pairs = [][2]string{{"m", "send saved"}, {"i", "type"}, {"esc", "disconnect"}, {"q", "close"}}
	}
	hints := make([][]string, 0, len(pairs))
	for _, p := range pairs {
		k := p[0]
		if len(k) > 1 {
			k = theme.KeyLabel(k)
		}
		hints = append(hints, []string{k, p[1]})
	}
	name := ws.run.label
	head := map[string]any{
		"req": req, "name": name, "by": "", "view": 0,
		"chips": []string{}, "hints": hints,
		"g":    map[string]string{"hop": httpGlyphHop.String()},
		"st_n": 0, "st_hl": hl, "syntax": "", "summary": summary,
	}
	lines := []string{req, summary, ""}
	var rows []map[string]any
	add := func(kind, text string) {
		lines = append(lines, text)
		rows = append(rows, map[string]any{"l": len(lines), "k": kind})
	}
	if len(ws.log) == 0 {
		switch {
		case ws.state == httpWSOpen && len(ws.saved) > 0:
			add("note", fmt.Sprintf("Connected. m sends one of the %d saved messages, i types one.", len(ws.saved)))
		case ws.state == httpWSOpen:
			add("note", "Connected. i types a message. Saved messages go in the body, split by === lines.")
		}
	}
	for _, m := range ws.log {
		text := m.Text
		if m.Binary {
			text = "(binary, " + httpclient.FormatSize(int64(len(m.Text))) + ")"
		}
		if len([]rune(text)) > httpWSClip {
			text = string([]rune(text)[:httpWSClip]) + "…"
		}
		text = ai.Redact(text)
		parts := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
		ts := m.At.Format("15:04:05")
		if m.Dir == httpclient.WSOut {
			add("add", "→ "+parts[0]+"   "+ts)
		} else {
			add("t", "← "+parts[0]+"   "+ts)
		}
		for _, l := range parts[1:] {
			lines = append(lines, "  "+l)
		}
	}
	return httpPage{head: head, lines: lines, rows: rows}
}

// httpWSKey handles a page key while the page shows the session.
// ok=false: the key is not a session key.
func (m *Model) httpWSKey(key string) (tea.Cmd, bool) {
	s := m.httpS()
	ws := s.ws
	if !s.wsShown || ws == nil {
		return nil, false
	}
	switch key {
	case "m":
		return m.openHTTPWSSaved(), true
	case "i":
		if ws.state != httpWSOpen {
			return m.pushToastDetail(toast.Info, "Not connected", "r reconnects"), true
		}
		return m.openHTTPPrompt(promptKindHTTPWSType, "WebSocket: Send Message", "Message:", ""), true
	case "esc":
		if ws.state == httpWSClosed {
			return m.httpClosePage(), true
		}
		m.httpCloseWS()
		return m.httpShowWS(), true
	case "r":
		if ws.state != httpWSClosed {
			return nil, true
		}
		return m.startHTTPRun(ws.run, httpclient.Hooks{}), true
	case "q":
		s.wsShown = false
		return m.httpClosePage(), true
	}
	return nil, true // the HTTP views and their keys do not apply here
}

// openHTTPWSSaved lists the saved messages (and "Type a message…").
func (m *Model) openHTTPWSSaved() tea.Cmd {
	ws := m.httpS().ws
	if ws == nil || ws.state != httpWSOpen {
		return m.pushToastDetail(toast.Info, "No WebSocket session", "Send a WEBSOCKET request (Ctrl+Alt+R) first")
	}
	items := []picker.Item{{ID: "__type", Title: "Type a message…"}}
	for i, msg := range ws.saved {
		t := strings.Join(strings.Fields(msg), " ")
		if len([]rune(t)) > 80 {
			t = string([]rune(t)[:80]) + "…"
		}
		items = append(items, picker.Item{ID: itoa(i), Title: t})
	}
	return m.openHTTPPicker(pickerKindHTTPWS, " WebSocket: Send ", items, nil)
}

// httpWSPick sends the picked saved message.
func (m *Model) httpWSPick(id string) tea.Cmd {
	ws := m.httpS().ws
	if ws == nil {
		return nil
	}
	if id == "__type" {
		return m.openHTTPPrompt(promptKindHTTPWSType, "WebSocket: Send Message", "Message:", "")
	}
	var i int
	if _, err := fmt.Sscan(id, &i); err != nil || i < 0 || i >= len(ws.saved) {
		return nil
	}
	return m.httpWSSend(ws.saved[i])
}

// httpWSSend sends text off the UI loop; the page shows it once sent.
func (m *Model) httpWSSend(text string) tea.Cmd {
	s := m.httpS()
	ws := s.ws
	if ws == nil || ws.state != httpWSOpen || ws.conn == nil {
		return m.pushToastDetail(toast.Info, "No WebSocket session", "Send a WEBSOCKET request (Ctrl+Alt+R) first")
	}
	if text == "" {
		return nil
	}
	conn, gen := ws.conn, ws.gen
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadHTTPSettings().timeout())
		defer cancel()
		return httpWSSentMsg{gen: gen, err: conn.Send(ctx, text), log: conn.Log()}
	}
}

// httpWSSentMsg is the result of one send.
type httpWSSentMsg struct {
	gen int
	err error
	log []httpclient.WSMessage
}

func (m *Model) httpWSSent(msg httpWSSentMsg) tea.Cmd {
	ws := m.httpS().ws
	if ws == nil || ws.gen != msg.gen {
		return nil
	}
	if msg.err != nil {
		return m.pushToastDetail(toast.Warn, "WebSocket send failed", ai.Redact(msg.err.Error()))
	}
	ws.log = httpWSTail(msg.log)
	return m.httpShowWS()
}

// httpWSPaletteSend is HTTP: WebSocket Send Message….
func (m *Model) httpWSPaletteSend() tea.Cmd { return m.openHTTPWSSaved() }

// httpWSPaletteClose is HTTP: WebSocket Disconnect.
func (m *Model) httpWSPaletteClose() tea.Cmd {
	ws := m.httpS().ws
	if ws == nil || ws.state == httpWSClosed {
		return m.pushToastDetail(toast.Info, "No WebSocket session", "")
	}
	m.httpCloseWS()
	return tea.Batch(m.httpShowWS(), m.pushToastDetail(toast.Info, "WebSocket disconnected", ""))
}

// httpWSTail is the newest httpWSShow messages of a session log.
func httpWSTail(log []httpclient.WSMessage) []httpclient.WSMessage {
	if len(log) > httpWSShow {
		return log[len(log)-httpWSShow:]
	}
	return log
}
