//go:build unix

package webgw

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// This phone (MOBUX-20): a paired device reads its own record
// (phone.get), keeps its alerts, quiet hours and app lock on the laptop
// (phone.prefs) and forgets itself (phone.forget). Each op acts on the
// session's own device only: there is no device id in the args. A
// launch-link tab has no record; a shared viewer (guest) may read and
// forget, never keep choices (it is for one visit).

// isPhoneOp reports the ops phoneOp answers.
func isPhoneOp(op string) bool {
	return op == webproto.OpPhoneGet || op == webproto.OpPhonePrefs || op == webproto.OpPhoneForget
}

// laptopName is this laptop's name for the phone's laptops list.
func laptopName() string {
	h, err := os.Hostname()
	if err != nil {
		return "laptop"
	}
	if n := webdevices.CleanName(strings.SplitN(h, ".", 2)[0]); n != "" {
		return n
	}
	return "laptop"
}

// phoneOp answers one This phone op on the reader goroutine.
func (c *wsConn) phoneOp(r *webproto.Req) {
	ctrl := c.s.cfg.Control
	dev := c.sess.Device
	if !strings.HasPrefix(dev, "dev-") {
		c.refuseReq(r.ID, webproto.CodeForbidden, "This browser is not paired.")
		return
	}
	d, ok, err := ctrl.Store.Get(dev)
	if err != nil {
		c.refuseReq(r.ID, webproto.CodeBusy, "Try again in a minute.")
		return
	}
	if !ok {
		c.refuseReq(r.ID, webproto.CodeForbidden, "This phone is not paired any more.")
		return
	}
	switch r.Op {
	case webproto.OpPhoneGet:
		c.answerReq(r.ID, phoneInfo(d, c.sess.Role, ctrl.Laptop))
	case webproto.OpPhonePrefs:
		c.phonePrefs(r, d)
	case webproto.OpPhoneForget:
		c.phoneForget(r, d)
	}
}

// phoneInfo is d as the phone sees it.
func phoneInfo(d webdevices.Device, role Role, laptop string) webproto.PhoneInfo {
	info := webproto.PhoneInfo{
		Device: d.ID, Name: d.Name, Role: hubRole(role), Guest: d.Guest,
		Passkey: len(d.CredID) > 0, NeedsRepair: d.NeedsRepair, Remembered: d.Bookmark != nil,
		PairedMs: unixMs(d.Created), LastSeenMs: unixMs(d.LastSeen),
		Laptops: []webproto.PhoneLaptop{{Name: laptop, This: true}},
	}
	if p := d.Phone; p != nil {
		info.Prefs = webproto.PhonePrefsArgs{Alerts: p.Alerts, QuietFrom: p.QuietFrom, QuietTo: p.QuietTo, UTCOffsetMin: p.UTCOffsetMin, Lock: p.Lock}
	}
	return info
}

func unixMs(t time.Time) int64 {
	if ms := t.UnixMilli(); ms > 0 {
		return ms
	}
	return 0
}

// phonePrefs keeps the phone's choices with its record.
func (c *wsConn) phonePrefs(r *webproto.Req, d webdevices.Device) {
	ctrl := c.s.cfg.Control
	if d.Guest {
		c.phoneAudit(r.Op, false, "guest", "")
		c.refuseReq(r.ID, webproto.CodeForbidden, "A shared view keeps no choices.")
		return
	}
	v, err := webproto.DecodeArgs(r.Op, r.Args) // checked already (checkReq)
	a, ok := v.(*webproto.PhonePrefsArgs)
	if err != nil || !ok {
		c.refuseReq(r.ID, webproto.CodeBadRequest, "Those choices are not valid.")
		return
	}
	now := ctrl.Now()
	found, err := ctrl.Store.Change(d.ID, func(x *webdevices.Device) {
		x.Phone = &webdevices.Phone{Alerts: a.Alerts, QuietFrom: a.QuietFrom, QuietTo: a.QuietTo,
			UTCOffsetMin: a.UTCOffsetMin, Lock: a.Lock, Changed: now}
	})
	if err != nil || !found {
		c.phoneAudit(r.Op, false, "removed", "")
		c.refuseReq(r.ID, webproto.CodeForbidden, "This phone is not paired any more.")
		return
	}
	c.phoneAudit(r.Op, true, "", prefsSummary(*a))
	c.answerReq(r.ID, nil)
}

// prefsSummary is the audit line of a prefs change: switches and times only.
func prefsSummary(a webproto.PhonePrefsArgs) string {
	onOff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}
	quiet := "off"
	if a.QuietFrom != "" {
		quiet = a.QuietFrom + "-" + a.QuietTo
	}
	return fmt.Sprintf("alerts %s, quiet %s, lock %s", onOff(a.Alerts), quiet, onOff(a.Lock))
}

// phoneForget removes this device from the laptop: its record, its
// passkey and bookmark with it, every token, and then every link of it
// (4003). The answer comes first, so the page knows it was its own
// choice.
func (c *wsConn) phoneForget(r *webproto.Req, d webdevices.Device) {
	ctrl := c.s.cfg.Control
	if _, _, err := ctrl.Store.Remove(d.ID); err != nil {
		c.phoneAudit(r.Op, false, "store", "")
		c.refuseReq(r.ID, webproto.CodeBusy, "Not forgotten. Try again in a minute.")
		return
	}
	n := ctrl.Tokens.RevokeDevice(d.ID)
	_ = ctrl.Audit.Log(Event{Kind: EvRevoke, Device: d.ID, Why: "phone", By: "web:" + d.ID,
		Summary: fmt.Sprintf("%d sessions closed", n)})
	c.answerReq(r.ID, nil)
	crash.Go("web gateway: a forgotten phone", func() { c.s.CloseDevice(d.ID, webproto.CloseDeviceRevoked) })
}

// phoneAudit logs one This phone op.
func (c *wsConn) phoneAudit(op string, ok bool, why, summary string) {
	_ = c.s.cfg.Audit.Log(Event{Kind: EvOp, Conn: c.id, Device: c.sess.Device, Op: op, OK: &ok, Why: why,
		Summary: summary, By: "web:" + c.sess.Device})
}
