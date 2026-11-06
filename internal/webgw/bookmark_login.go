//go:build unix

package webgw

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// Bookmark sign-in (WEBP-12): a paired device that chose "Remember this
// browser" opens the panel from a bookmark with its key alone:
//
//	POST /login/device {key} → {token, role, device, device_key} (or a TOTP ticket)
//	POST /login/forget {key} → {}   (the page's "Forget this browser")
//
// The key travels only in a POST body on the panel's own origin (the
// guard checks Host and Origin first), works only on the origin it was
// made on, and is replaced on every use. A second use of the key before
// (a copy) forgets the bookmark. Unused for BookmarkTTL, it expires.
// Removing the device on the laptop, or its Forget bookmark, ends it.

// BookmarkTTL is how long a bookmark key works without being used.
const BookmarkTTL = 30 * 24 * time.Hour

// errBookmarkGone: no device (any more) to keep a key for.
var errBookmarkGone = errors.New("bookmark gone")

type deviceKeyReq struct {
	Key string `json:"key"`
}

// store is the device list the login endpoints use.
func (l *Login) store() *webdevices.Store {
	if l.cfg.Store != nil {
		return l.cfg.Store
	}
	if l.cfg.Passkeys != nil {
		return l.cfg.Passkeys.store
	}
	return nil
}

// device signs a remembered browser in with its bookmark key.
func (l *Login) device(w http.ResponseWriter, r *http.Request) {
	var req deviceKeyReq
	if !decodeStrict(r.Body, &req) || req.Key == "" {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return
	}
	origin := r.Header.Get("Origin")
	dev, sec, err := ParseDeviceKey(req.Key)
	if err != nil {
		l.bookmarkFail(w, "", "bad_key", "login_failed")
		return
	}
	st := l.store()
	if st == nil {
		writeJSON(w, http.StatusNotFound, errorReply("not_found"))
		return
	}
	h := DeviceKeyHash(sec)
	now := l.cfg.Now()
	totp := l.totpOn.Load()
	var next string
	var nextHash []byte
	if !totp {
		if next, nextHash, err = NewDeviceKey(dev); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorReply("internal"))
			return
		}
	}
	var got webdevices.Device
	var oldPrev []byte
	why := ""
	found, err := st.Change(dev, func(d *webdevices.Device) {
		b := d.Bookmark
		switch {
		case b == nil:
			why = "no_bookmark"
		case len(b.Prev) > 0 && subtle.ConstantTimeCompare(b.Prev, h) == 1:
			why = "key_reused"
			d.Bookmark = nil
		case subtle.ConstantTimeCompare(b.Hash, h) != 1:
			why = "wrong_key"
		case origin == "" || b.Origin != origin:
			why = "origin"
		case !now.Before(b.Used.Add(BookmarkTTL)):
			why = "expired"
			d.Bookmark = nil
		case d.NeedsRepair:
			why = "clone_suspected"
		default:
			if !totp { // rotate now, in the same locked step as the check
				oldPrev = b.Prev
				b.Prev, b.Hash, b.Used = b.Hash, nextHash, now.UTC()
			}
			d.LastSeen = now.UTC()
			got = *d
		}
	})
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, errorReply("internal"))
		return
	}
	switch {
	case !found:
		l.bookmarkFail(w, dev, "removed", "removed")
		return
	case why == "key_reused":
		l.bookmarkFail(w, dev, why, "revoked")
		return
	case why == "expired":
		l.bookmarkFail(w, dev, why, "expired")
		return
	case why != "":
		l.bookmarkFail(w, dev, why, "login_failed")
		return
	}
	if totp {
		tk, err := l.newTicket(got, true, origin)
		if err != nil {
			writeJSON(w, http.StatusServiceUnavailable, errorReply("busy"))
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"totp": true, "ticket": tk})
		return
	}
	if !l.issueWith(w, got, next, "bookmark") {
		// No session, so the page keeps its key: put it back.
		_, _ = st.Change(dev, func(d *webdevices.Device) {
			if b := d.Bookmark; b != nil && subtle.ConstantTimeCompare(b.Hash, nextHash) == 1 {
				b.Hash, b.Prev = h, oldPrev
			}
		})
	}
}

// bookmarkFail refuses a key: an audit line with the rule (never the
// key) and a short error for the page.
func (l *Login) bookmarkFail(w http.ResponseWriter, dev, why, reply string) {
	_ = l.cfg.Audit.Log(Event{Kind: EvLoginFail, Code: http.StatusUnauthorized, Device: dev, Why: why, Summary: "bookmark"})
	writeJSON(w, http.StatusUnauthorized, errorReply(reply))
}

// forget ends a bookmark from the page: the key itself (now or the one
// before) proves which. The answer is the same either way.
func (l *Login) forget(w http.ResponseWriter, r *http.Request) {
	var req deviceKeyReq
	if !decodeStrict(r.Body, &req) {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return
	}
	dev, sec, err := ParseDeviceKey(req.Key)
	st := l.store()
	if err == nil && st != nil {
		h := DeviceKeyHash(sec)
		forgot := false
		_, _ = st.Change(dev, func(d *webdevices.Device) {
			if b := d.Bookmark; b != nil && (subtle.ConstantTimeCompare(b.Hash, h) == 1 ||
				len(b.Prev) > 0 && subtle.ConstantTimeCompare(b.Prev, h) == 1) {
				d.Bookmark, forgot = nil, true
			}
		})
		if forgot {
			_ = l.cfg.Audit.Log(Event{Kind: EvRevoke, Device: dev, Why: "bookmark", By: "web:" + dev})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{})
}

// rememberDevice makes a new bookmark key for a device on origin and
// keeps its hash (the old key, if any, becomes the one before).
func rememberDevice(st *webdevices.Store, dev, origin string, now time.Time) (string, error) {
	if st == nil || origin == "" {
		return "", errBookmarkGone
	}
	key, h, err := NewDeviceKey(dev)
	if err != nil {
		return "", err
	}
	found, err := st.Change(dev, func(d *webdevices.Device) { d.Bookmark = rotated(d.Bookmark, h, origin, now) })
	if err != nil {
		return "", err
	}
	if !found {
		return "", errBookmarkGone
	}
	return key, nil
}

// rotated is b with a new key hash on origin.
func rotated(b *webdevices.Bookmark, h []byte, origin string, now time.Time) *webdevices.Bookmark {
	now = now.UTC()
	if b == nil || b.Origin != origin {
		return &webdevices.Bookmark{Hash: h, Origin: origin, Created: now, Used: now}
	}
	return &webdevices.Bookmark{Hash: h, Prev: b.Hash, Origin: origin, Created: b.Created, Used: now}
}
