//go:build unix

package webgw

import (
	"context"
	"errors"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The gateway half of the op policy (3.2, T46): every request is checked
// against the op table before the hub sees it. The hub checks the same
// table again, so a bug in one side does not open the other.

// reqVerdict is what happens to one request.
type reqVerdict int

const (
	reqForward reqVerdict = iota // on to the hub
	reqAnswered                  // refused with an err to the browser
	reqClosed                    // the connection was closed
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
