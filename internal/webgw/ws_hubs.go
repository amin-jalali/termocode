//go:build unix

package webgw

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The door (MOB-50): one browser connection, several hubs. A frame with
// a "ws" that is not the home workspace goes to that workspace's hub,
// opened on first use over its own unix socket. Each project hub has its
// own writer goroutine and a bounded queue, so a stuck hub ends only its
// project (ev "ws.gone"), never the home link. The rules of the wire are
// in internal/webproto/ws.go.

// sideQueue is how many lines may wait for one project hub; a hub that
// lets more pile up is too slow and is dropped (why "busy").
const sideQueue = 64

// sideHub is the connection to one other workspace's hub.
type sideHub struct {
	ws   string
	out  chan []byte   // lines for the hub, in order
	done chan struct{} // closed when the hub is dropped
	once sync.Once

	mu      sync.Mutex
	conn    net.Conn            // set once dialled
	gone    bool                // dropped: take no more requests
	pending map[uint64]struct{} // request ids sent to this hub, not answered
}

// frameWS is the ws a browser frame names ("" for kinds without one).
func frameWS(m webproto.Msg) string {
	switch m := m.(type) {
	case *webproto.Sub:
		return m.WS
	case *webproto.Unsub:
		return m.WS
	case *webproto.Req:
		return m.WS
	}
	return ""
}

// stripWS re-encodes a browser frame without its ws: a hub never sees
// the field (it serves only its own workspace).
func stripWS(m webproto.Msg) ([]byte, error) {
	switch m := m.(type) {
	case *webproto.Sub:
		c := *m
		c.WS = ""
		return webproto.Encode(&c)
	case *webproto.Unsub:
		c := *m
		c.WS = ""
		return webproto.Encode(&c)
	case *webproto.Req:
		c := *m
		c.WS = ""
		return webproto.Encode(&c)
	}
	return webproto.Encode(m)
}

// withWS sets ws on a hub line: {"t":…} becomes {"ws":"<ws>","t":…}.
// ws is 16 hex digits, so it needs no escaping. ok is false for a line
// that is not a JSON object with at least one field.
func withWS(line []byte, ws string) ([]byte, bool) {
	if len(line) < 3 || line[0] != '{' || line[len(line)-1] != '}' {
		return nil, false
	}
	out := make([]byte, 0, len(line)+len(ws)+8)
	out = append(out, `{"ws":"`...)
	out = append(out, ws...)
	out = append(out, `",`...)
	return append(out, line[1:]...), true
}

// route says where a browser frame goes: "" is the home hub. A frame
// that names the home workspace is the home hub too.
func (c *wsConn) route(m webproto.Msg) string {
	ws := frameWS(m)
	if ws == c.s.cfg.Home {
		return ""
	}
	return ws
}

// toSide queues a stripped line for the hub of ws, opening it on first
// use. id is the request id the line carries (0: not a request); its
// slot was booked with sendSlot and is freed when the hub answers or
// goes. false: the browser connection ended.
func (c *wsConn) toSide(ws string, line []byte, id uint64) bool {
	h, why := c.side(ws)
	if h == nil {
		return c.sideRefused(ws, why, id)
	}
	if id != 0 {
		h.mu.Lock()
		if h.gone {
			h.mu.Unlock()
			return c.sideRefused(ws, webproto.WSGoneClosed, id)
		}
		h.pending[id] = struct{}{}
		h.mu.Unlock()
	}
	select {
	case h.out <- line:
	default:
		c.endSide(h, webproto.WSGoneBusy)
	}
	select {
	case <-c.closed:
		return false
	default:
		return true
	}
}

// side returns the open hub of ws or opens it; nil and why when it may
// not be opened.
func (c *wsConn) side(ws string) (*sideHub, string) {
	c.sideMu.Lock()
	defer c.sideMu.Unlock()
	select {
	case <-c.closed:
		return nil, webproto.WSGoneClosed
	default:
	}
	if h := c.sides[ws]; h != nil {
		return h, ""
	}
	if c.s.cfg.DialWS == nil {
		return nil, webproto.WSGoneOff
	}
	if len(c.sides) >= c.s.cfg.MaxHubs {
		return nil, webproto.WSGoneLimit
	}
	if c.sides == nil {
		c.sides = map[string]*sideHub{}
	}
	h := &sideHub{ws: ws, out: make(chan []byte, sideQueue), done: make(chan struct{}), pending: map[uint64]struct{}{}}
	c.sides[ws] = h
	crash.GoWith("web gateway: project hub writer", func() { c.sideWriter(h) }, func() { c.endSide(h, webproto.WSGoneBad) })
	return h, ""
}

// sideRefused answers a frame for a project that cannot be reached: a
// request gets err (its slot is freed), anything else ev "ws.gone".
func (c *wsConn) sideRefused(ws, why string, id uint64) bool {
	if id != 0 {
		c.gate.answered(id)
		code, msg := webproto.CodeGone, "That project is not open."
		switch why {
		case webproto.WSGoneLimit:
			code, msg = webproto.CodeLimit, "Too many projects are open on this device."
		case webproto.WSGoneOff:
			code, msg = webproto.CodeNotFound, "Other projects are not available here."
		}
		return c.toBrowser(&webproto.ErrMsg{T: webproto.KindErr, ID: id, WS: ws, Code: code, Msg: msg})
	}
	return c.toBrowser(wsGone(ws, why))
}

// wsGone is ev "ws.gone" for ws.
func wsGone(ws, why string) *webproto.Event {
	data, _ := json.Marshal(webproto.WSGone{Why: why})
	return &webproto.Event{T: webproto.KindEv, Kind: webproto.EventWSGone, WS: ws, Data: data}
}

// toBrowser writes one message of the gateway's own to the browser.
// false: the connection ended (or was too slow and was closed).
func (c *wsConn) toBrowser(m webproto.Msg) bool {
	b, err := webproto.Encode(m)
	if err != nil {
		c.close(closeInternal)
		return false
	}
	wctx, cancel := context.WithTimeout(c.ctx, c.s.cfg.WriteWait)
	defer cancel()
	if c.ws.Write(wctx, websocket.MessageText, b) != nil {
		c.close(webproto.CloseSlowClient)
		return false
	}
	return true
}

// sideWriter dials the project hub, sends the open line and the hello,
// starts the reader, then writes queued lines until the hub is dropped.
func (c *wsConn) sideWriter(h *sideHub) {
	dctx, cancel := context.WithTimeout(c.ctx, c.s.cfg.HubWait)
	conn, err := c.s.cfg.DialWS(dctx, h.ws)
	cancel()
	if err != nil {
		c.endSide(h, webproto.WSGoneNotRunning)
		return
	}
	h.mu.Lock()
	if h.gone {
		h.mu.Unlock()
		_ = conn.Close()
		return
	}
	h.conn = conn
	h.mu.Unlock()
	w := bufio.NewWriterSize(conn, 64<<10)
	write := func(line []byte) bool {
		_ = conn.SetWriteDeadline(time.Now().Add(c.s.cfg.HubWait))
		_, _ = w.Write(line)
		_ = w.WriteByte('\n')
		if err := w.Flush(); err != nil {
			why := webproto.WSGoneClosed
			if errors.Is(err, os.ErrDeadlineExceeded) {
				why = webproto.WSGoneBusy
			}
			c.endSide(h, why)
			return false
		}
		return true
	}
	open, err := webproto.Encode(&webproto.Open{T: webproto.KindOpen, Conn: c.id,
		Role: hubRole(c.sess.Role), Device: c.sess.Device, Name: c.name})
	if err != nil || len(c.sideHello) == 0 {
		c.endSide(h, webproto.WSGoneBad)
		return
	}
	if !write(open) || !write(c.sideHello) {
		return
	}
	crash.GoWith("web gateway: project hub reader", func() { c.sideReader(h, conn) }, func() { c.endSide(h, webproto.WSGoneBad) })
	for {
		select {
		case <-h.done:
			return
		case line := <-h.out:
			if !write(line) {
				return
			}
		}
	}
}

// sideReader copies project hub lines to the browser with ws set, until
// the hub ends. Pongs are dropped (pings go to the home hub only); a bye
// ends the project, not the browser connection.
func (c *wsConn) sideReader(h *sideHub, conn net.Conn) {
	br := bufio.NewReaderSize(conn, 64<<10)
	for {
		line, err := readLine(br, maxHubLine)
		if err != nil {
			c.endSide(h, webproto.WSGoneClosed)
			return
		}
		var head struct {
			T  string `json:"t"`
			ID uint64 `json:"id"`
			WS string `json:"ws"`
		}
		if json.Unmarshal(line, &head) != nil || head.WS != "" { // a hub never sets ws
			c.endSide(h, webproto.WSGoneBad)
			return
		}
		switch head.T {
		case webproto.KindPong:
			continue
		case webproto.KindBye:
			c.endSide(h, webproto.WSGoneClosed)
			return
		case webproto.KindRes, webproto.KindErr:
			if head.ID != 0 {
				if !h.take(head.ID) {
					continue // not a request of ours (or answered as gone already)
				}
				c.gate.answered(head.ID)
			}
		}
		out, ok := withWS(line, h.ws)
		if !ok {
			c.endSide(h, webproto.WSGoneBad)
			return
		}
		slow := time.AfterFunc(c.s.cfg.WriteWait, func() { c.end(webproto.CloseSlowClient, false) })
		err = c.ws.Write(c.ctx, websocket.MessageText, out)
		slow.Stop()
		if err != nil {
			c.close(webproto.CloseNormal) // no-op when it was closed first
			return
		}
	}
}

// take removes an answered request id; false if it was not pending.
func (h *sideHub) take(id uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.pending[id]; !ok {
		return false
	}
	delete(h.pending, id)
	return true
}

// endSide drops a project hub once: its connection closes, its pending
// requests get err "gone" and the page ev "ws.gone" (unless the whole
// browser connection is ending).
func (c *wsConn) endSide(h *sideHub, why string) {
	h.once.Do(func() {
		h.mu.Lock()
		h.gone = true
		conn := h.conn
		ids := make([]uint64, 0, len(h.pending))
		for id := range h.pending {
			ids = append(ids, id)
		}
		h.pending = nil
		h.mu.Unlock()
		close(h.done)
		if conn != nil {
			_ = conn.Close()
		}
		c.sideMu.Lock()
		if c.sides[h.ws] == h {
			delete(c.sides, h.ws)
		}
		c.sideMu.Unlock()
		for _, id := range ids {
			c.gate.answered(id)
		}
		select {
		case <-c.closed:
			return
		default:
		}
		_ = c.s.cfg.Audit.Log(Event{Kind: EvProject, Conn: c.id, Device: c.sess.Device, Why: why, Summary: h.ws})
		for _, id := range ids {
			if !c.toBrowser(&webproto.ErrMsg{T: webproto.KindErr, ID: id, WS: h.ws, Code: webproto.CodeGone, Msg: "That project closed."}) {
				return
			}
		}
		_ = c.toBrowser(wsGone(h.ws, why))
	})
}

// endSides drops every project hub; the browser connection is ending.
func (c *wsConn) endSides() {
	c.sideMu.Lock()
	hs := make([]*sideHub, 0, len(c.sides))
	for _, h := range c.sides {
		hs = append(hs, h)
	}
	c.sideMu.Unlock()
	for _, h := range hs {
		c.endSide(h, webproto.WSGoneClosed)
	}
}

// Sides is the number of project hubs open over all browsers now.
func (s *WS) Sides() int {
	s.mu.Lock()
	conns := make([]*wsConn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	n := 0
	for _, c := range conns {
		c.sideMu.Lock()
		n += len(c.sides)
		c.sideMu.Unlock()
	}
	return n
}
