//go:build unix

package webgw

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The gateway half of the op policy (3.2, T46): every request is checked
// against the op table before the hub sees it. The hub checks the same
// table again, so a bug in one side does not open the other.

// reqVerdict is what happens to one request.
type reqVerdict int

const (
	reqForward  reqVerdict = iota // on to the hub
	reqAnswered                   // refused with an err to the browser
	reqClosed                     // the connection was closed
)

// checkReq applies the op table to a request on the reader goroutine.
func (c *wsConn) checkReq(r *webproto.Req) reqVerdict {
	if r.ID <= c.lastID {
		c.protoError("request_id", webproto.ClosePolicy)
		return reqClosed
	}
	c.lastID = r.ID
	_, ok := webproto.Lookup(r.Op)
	switch {
	case !ok:
		return c.refuseReq(r.ID, webproto.CodeBadRequest, "Unknown action.")
	case !webproto.Allowed(hubRole(c.sess.Role), r.Op):
		_ = c.s.cfg.Audit.Log(Event{Kind: EvOp, Conn: c.id, Device: c.sess.Device, Op: r.Op, OK: new(bool), Why: "forbidden"})
		return c.refuseReq(r.ID, webproto.CodeForbidden, "You may not do that from here.")
	}
	if _, err := webproto.DecodeArgs(r.Op, r.Args); err != nil {
		if errors.Is(err, webproto.ErrTooBig) {
			return c.refuseReq(r.ID, webproto.CodeLimit, "That is too big.")
		}
		return c.refuseReq(r.ID, webproto.CodeBadRequest, "That request is not valid.")
	}
	return reqForward
}

// refuseReq answers a request with an err without asking the hub.
func (c *wsConn) refuseReq(id uint64, code webproto.ErrCode, msg string) reqVerdict {
	b, err := webproto.Encode(&webproto.ErrMsg{T: webproto.KindErr, ID: id, Code: code, Msg: msg})
	if err != nil {
		c.close(closeInternal)
		return reqClosed
	}
	wctx, cancel := context.WithTimeout(c.ctx, c.s.cfg.WriteWait)
	defer cancel()
	if c.ws.Write(wctx, websocket.MessageText, b) != nil {
		c.close(webproto.CloseSlowClient)
		return reqClosed
	}
	return reqAnswered
}

// Per-connection limits the gateway enforces (3.5). The hub checks again
// with 2× headroom.
const (
	// MaxInFlight requests of one connection may wait for the hub.
	MaxInFlight = 4
	// reqPerSecond and reqBurst cap all requests of one connection.
	reqPerSecond = 20
	reqBurst     = 40
	// msgPerSecond and msgBurst cap the other messages (sub, unsub, ping).
	msgPerSecond = 20
	msgBurst     = 40
	// DefaultThrottleMax: a connection held back by its rate limits for
	// this long in one go is closed with 1008.
	DefaultThrottleMax = 10 * time.Second
	// throttlePoll caps one wait, so a moved clock is seen soon.
	throttlePoll = 50 * time.Millisecond
)

// gate holds a connection's rate buckets and its requests in flight.
type gate struct {
	all, msgs *Bucket
	ops       map[string]*Bucket // per op, made on first use
	bytes     map[string]*Bucket // per op with a byte rate

	mu      sync.Mutex
	pending map[uint64]struct{} // request ids sent to the hub, not answered
	freed   chan struct{}       // a pending request was answered
}

func newGate() *gate {
	return &gate{all: NewBucket(reqPerSecond, reqBurst), msgs: NewBucket(msgPerSecond, msgBurst),
		ops: map[string]*Bucket{}, bytes: map[string]*Bucket{},
		pending: map[uint64]struct{}{}, freed: make(chan struct{}, 1)}
}

// throttle holds the reader back until every bucket of a message has
// room (backpressure: the browser's sends wait in its socket). false:
// the connection ended, or it was held back past ThrottleMax (1008).
func (c *wsConn) throttle(op string, size int) bool {
	g := c.gate
	start := time.Now()
	for {
		now := c.s.cfg.Now()
		wait := c.room(g, op, size, now)
		if wait == 0 {
			return true
		}
		if time.Since(start) >= c.s.cfg.ThrottleMax {
			_ = c.s.cfg.Audit.Log(Event{Kind: EvRateLimited, Conn: c.id, Device: c.sess.Device, Op: op})
			c.protoError("rate", webproto.ClosePolicy)
			return false
		}
		t := time.NewTimer(min(wait, throttlePoll))
		select {
		case <-c.ctx.Done():
			t.Stop()
			return false
		case <-t.C:
		}
	}
}

// room takes the message's units from its buckets if all have room, else
// says how long to wait (and takes nothing it would have to give back).
func (c *wsConn) room(g *gate, op string, size int, now time.Time) time.Duration {
	if op == "" {
		if g.msgs.Allow(now) {
			return 0
		}
		return max(g.msgs.NextN(now, 1), time.Millisecond)
	}
	p, _ := webproto.Lookup(op)
	b := g.ops[op]
	if b == nil {
		per := float64(p.Rate.N) / p.Rate.Per.Seconds()
		b = NewBucket(per, p.Rate.N)
		g.ops[op] = b
	}
	var bb *Bucket
	if p.BytesPerSec > 0 {
		if bb = g.bytes[op]; bb == nil {
			bb = NewBucket(float64(p.BytesPerSec), p.BytesPerSec)
			g.bytes[op] = bb
		}
	}
	wait := max(g.all.NextN(now, 1), b.NextN(now, 1))
	if bb != nil {
		wait = max(wait, bb.NextN(now, size))
	}
	if wait > 0 {
		return wait
	}
	g.all.Allow(now)
	b.Allow(now)
	if bb != nil {
		bb.AllowN(now, size)
	}
	return 0
}

// sendSlot waits until fewer than MaxInFlight requests wait for the hub
// and books id. false: the connection ended.
func (c *wsConn) sendSlot(id uint64) bool {
	g := c.gate
	for {
		g.mu.Lock()
		if len(g.pending) < MaxInFlight {
			g.pending[id] = struct{}{}
			g.mu.Unlock()
			return true
		}
		g.mu.Unlock()
		select {
		case <-g.freed:
		case <-c.ctx.Done():
			return false
		}
	}
}

// answered frees the slot of a request the hub answered.
func (g *gate) answered(id uint64) {
	g.mu.Lock()
	_, ok := g.pending[id]
	delete(g.pending, id)
	g.mu.Unlock()
	if ok {
		select {
		case g.freed <- struct{}{}:
		default:
		}
	}
}
