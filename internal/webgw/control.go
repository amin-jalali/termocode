//go:build unix

package webgw

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Give and take control (WEB-34, T32). A paired viewer asks with
// control.challenge / control.ask: it signs a fresh challenge with its
// own passkey (Face ID, fingerprint or PIN on the phone), then the
// gateway asks the laptop (web.grant_ask, a Risky card there). Only both
// together give control, and only while the assertion is at most
// GrantFresh old. The new role is stored with the device; its links
// close with 4013 and come back with it. The laptop takes control back
// with a role event. A shared viewer (guest) and a launch-link tab never
// take part.

// GrantFresh is how old the passkey assertion may be when the owner's
// yes comes.
const GrantFresh = 5 * time.Minute

// Control answers the two gateway ops.
type Control struct {
	Passkeys *Passkeys
	Tokens   *Tokens
	Store    *webdevices.Store
	Audit    *Audit
	Now      func() time.Time
	// Ask shows the laptop's card; nil error is the owner's yes.
	Ask func(ctx context.Context, a webproto.GrantAskArgs) error
}

// gatewayOp answers a request the gateway handles itself, on the reader
// goroutine (the laptop's answer comes on a goroutine of its own).
func (c *wsConn) gatewayOp(r *webproto.Req) {
	ctrl := c.s.cfg.Control
	if ctrl == nil {
		c.refuseReq(r.ID, webproto.CodeNotFound, "This is not available here.")
		return
	}
	if c.sess.Role == RoleControl {
		c.refuseReq(r.ID, webproto.CodeConflict, "This device has control already.")
		return
	}
	d, ok, err := ctrl.Store.Get(c.sess.Device)
	if !strings.HasPrefix(c.sess.Device, "dev-") || err != nil || !ok || d.Guest {
		c.controlFail(r.ID, "guest", "A shared view cannot get control.")
		return
	}
	switch r.Op {
	case webproto.OpControlChallenge:
		id, opts, err := ctrl.Passkeys.BeginLogin()
		if err != nil {
			c.refuseReq(r.ID, webproto.CodeBusy, "Try again in a minute.")
			return
		}
		c.grantLogin = id
		c.answerReq(r.ID, webproto.ControlChallenge{Options: opts})
	case webproto.OpControlAsk:
		c.controlAsk(r, d)
	}
}

// controlAsk checks the passkey answer and, in the background, asks the
// laptop.
func (c *wsConn) controlAsk(r *webproto.Req, d webdevices.Device) {
	ctrl := c.s.cfg.Control
	var args webproto.ControlAskArgs
	if json.Unmarshal(r.Args, &args) != nil || c.grantLogin == "" {
		c.controlFail(r.ID, "no_challenge", "Ask again.")
		return
	}
	login := c.grantLogin
	c.grantLogin = ""
	got, err := ctrl.Passkeys.FinishLogin(login, json.RawMessage(args.Credential))
	if err != nil || got.ID != d.ID {
		c.controlFail(r.ID, "passkey", "The passkey did not match this device.")
		return
	}
	if !c.asking.CompareAndSwap(false, true) {
		c.refuseReq(r.ID, webproto.CodeBusy, "The laptop is asked already.")
		return
	}
	asserted := ctrl.Now()
	id := r.ID
	crash.Go("web gateway: a control request", func() {
		defer c.asking.Store(false)
		ctx, cancel := context.WithTimeout(c.ctx, GrantFresh)
		defer cancel()
		if err := ctrl.Ask(ctx, webproto.GrantAskArgs{Device: d.ID, Name: d.Name}); err != nil {
			c.controlFail(id, "denied", "Not given: the laptop said Not now.")
			return
		}
		if ctrl.Now().Sub(asserted) > GrantFresh {
			c.controlFail(id, "stale", "That took too long. Ask again.")
			return
		}
		if ok, err := ctrl.Store.Change(d.ID, func(x *webdevices.Device) { x.Role = webdevices.RoleControl }); err != nil || !ok {
			c.controlFail(id, "removed", "This device is not paired any more.")
			return
		}
		ctrl.Tokens.SetDeviceRole(d.ID, RoleControl)
		_ = ctrl.Audit.Log(Event{Kind: EvRole, Device: d.ID, Summary: webdevices.RoleControl, By: "web:" + d.ID})
		c.answerReq(id, nil)
		c.s.CloseDevice(d.ID, webproto.CloseRoleChanged)
	})
}

// controlFail refuses a control request and logs why.
func (c *wsConn) controlFail(id uint64, why, msg string) {
	f := false
	_ = c.s.cfg.Audit.Log(Event{Kind: EvOp, Conn: c.id, Device: c.sess.Device, Op: "control.ask", OK: &f, Why: why})
	c.refuseReq(id, webproto.CodeForbidden, msg)
}

// answerReq answers a request the gateway handled with res.
func (c *wsConn) answerReq(id uint64, data any) {
	var raw json.RawMessage
	if data != nil {
		b, err := json.Marshal(data)
		if err != nil {
			c.close(closeInternal)
			return
		}
		raw = b
	}
	b, err := webproto.Encode(&webproto.Res{T: webproto.KindRes, ID: id, Data: raw})
	if err != nil {
		c.close(closeInternal)
		return
	}
	wctx, cancel := context.WithTimeout(c.ctx, c.s.cfg.WriteWait)
	defer cancel()
	if c.ws.Write(wctx, websocket.MessageText, b) != nil {
		c.close(webproto.CloseSlowClient)
	}
}

// SetDeviceRole changes the role of every session of device and says how
// many.
func (t *Tokens) SetDeviceRole(device string, role Role) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, s := range t.byHash {
		if s.Device == device {
			s.Role = role
			n++
		}
	}
	return n
}
