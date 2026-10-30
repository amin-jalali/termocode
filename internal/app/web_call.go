package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Web requests (WEB-6). The reader goroutine checks a request against the
// policy table (ids go up, the role may call the op, the args decode
// strictly within the op's size) and hands it to Update as a webCallMsg.
// There the op's handler runs on the Model. A handler answers at once
// (call.reply / call.fail) or says call.later() and answers from a
// tea.Cmd's message or a goroutine that watches call.ctx: anything slow
// (git, search, a big file, an nvim call that may wait) must not run on
// Update.

// webCallBudget is what a handler may take on Update. Longer work goes to
// a tea.Cmd or a goroutine (T41).
const webCallBudget = 20 * time.Millisecond

// webMaxInFlight caps the unanswered requests of one connection: the
// gateway allows 4, the hub checks again with 2× headroom.
const webMaxInFlight = 8

// webNow is the clock of the budget guard; tests swap it.
var webNow = time.Now

// webSlowCall hears a handler over its budget: a bug in that op, logged
// to errors.log. Tests swap it.
var webSlowCall = func(op string, d time.Duration) {
	recordError(fmt.Sprintf("[web] %s took %s on the UI thread (budget %s)", op, d.Round(time.Millisecond), webCallBudget))
}

// webOpFunc runs one request on Update.
type webOpFunc func(m *Model, call *webCall) tea.Cmd

// webOps are the request handlers by op name; the view tasks add theirs.
// An op with a policy row but no handler yet answers not_found.
var webOps = map[string]webOpFunc{}

// webCall is one request on its way to Update and back.
type webCall struct {
	ctx  context.Context // ends with the connection or the hub
	conn *webConn
	id   uint64
	op   string
	args any // the op's args type, decoded and checked

	answered atomic.Bool
	deferred bool // Update only: the handler answers later
}

// later says the handler answers after it returned.
func (c *webCall) later() { c.deferred = true }

// reply answers with res; data is marshalled to JSON (nil: no data).
// Only the first answer counts. Safe from any goroutine.
func (c *webCall) reply(data any) {
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			recordError("[web] " + c.op + ": reply: " + err.Error())
			c.fail(webproto.CodeInternal, "Something went wrong.")
			return
		}
		raw = b
	}
	c.answer(&webproto.Res{T: webproto.KindRes, ID: c.id, Data: raw})
}

// fail answers with err. msg is short English for the user: never a
// path or a secret. Only the first answer counts.
func (c *webCall) fail(code webproto.ErrCode, msg string) {
	c.answer(&webproto.ErrMsg{T: webproto.KindErr, ID: c.id, Code: code, Msg: msg})
}

func (c *webCall) answer(m webproto.Msg) {
	if !c.answered.CompareAndSwap(false, true) {
		return
	}
	c.conn.inflight.Add(-1)
	c.conn.send(m)
}

// webCallMsg runs one request on Update.
type webCallMsg struct {
	hub  *webHub
	call *webCall
}

// request checks one request on the reader goroutine and hands it to
// Update. A refused request is one err with its id; a reused or falling
// id ends the connection.
func (c *webConn) request(r *webproto.Req) bool {
	if r.ID <= c.lastID {
		return c.protocolError("Request ids must go up.")
	}
	c.lastID = r.ID
	refuse := func(code webproto.ErrCode, msg string) bool {
		return c.send(&webproto.ErrMsg{T: webproto.KindErr, ID: r.ID, Code: code, Msg: msg})
	}
	if _, ok := webproto.Lookup(r.Op); !ok {
		return refuse(webproto.CodeBadRequest, "Unknown action.")
	}
	if !webproto.Allowed(c.role, r.Op) {
		return refuse(webproto.CodeForbidden, "You may not do that from here.")
	}
	args, err := webproto.DecodeArgs(r.Op, r.Args)
	if errors.Is(err, webproto.ErrTooBig) {
		return refuse(webproto.CodeLimit, "That is too big.")
	}
	if err != nil {
		return refuse(webproto.CodeBadRequest, "That request is not valid.")
	}
	if c.inflight.Add(1) > webMaxInFlight {
		c.inflight.Add(-1)
		return refuse(webproto.CodeBusy, "Too many requests at once. Try again.")
	}
	call := &webCall{ctx: c.ctx, conn: c, id: r.ID, op: r.Op, args: args}
	return c.hub.send(c.ctx, webCallMsg{hub: c.hub, call: call}) == nil
}

// onWebCall runs one request on the Model.
func (m *Model) onWebCall(msg webCallMsg) tea.Cmd {
	next := waitWeb(msg.hub)
	call := msg.call
	if call.ctx.Err() != nil { // the connection or the hub is gone: do not run
		call.fail(webproto.CodeGone, "The connection ended.")
		return next
	}
	fn := msg.hub.ops[call.op]
	if fn == nil {
		call.fail(webproto.CodeNotFound, "This is not available yet.")
		return next
	}
	start := webNow()
	cmd := m.webRunOp(fn, call)
	if d := webNow().Sub(start); d > webCallBudget {
		webSlowCall(call.op, d)
	}
	if !call.deferred && !call.answered.Load() {
		recordError("[web] " + call.op + " gave no answer")
		call.fail(webproto.CodeInternal, "Something went wrong.")
	}
	return tea.Batch(cmd, next)
}

// webRunOp runs a handler; a panic is err internal for that request (a
// browser must not crash the editor).
func (m *Model) webRunOp(fn webOpFunc, call *webCall) (cmd tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			recordError(fmt.Sprintf("[web] %s panic: %v", call.op, r))
			call.fail(webproto.CodeInternal, "Something went wrong.")
			cmd = nil
		}
	}()
	return fn(m, call)
}
