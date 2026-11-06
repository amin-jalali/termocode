//go:build unix

package webgw

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// Bookmark sign-in (WEBP-12): a paired device that chose "Remember this
// browser" opens the panel from a bookmark with its key alone:
//
//	POST /login/device {} → {token, role, device, remembered} (or a TOTP ticket)
//	POST /login/forget {} → {}   (the page's "Forget this browser")
//
// The key lives in an httpOnly, SameSite=Strict cookie (DeviceCookie) the
// laptop sets, sent only to /login/ paths of the panel's own origin (the
// guard checks Host and Origin first): page code never reads it. A page
// from before the cookie may still send its old key once as {key}; the
// answer moves it into the cookie. The key works only on the origin it was
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

// DeviceCookie is the cookie that holds a remembered browser's bookmark
// key: httpOnly, SameSite=Strict, Path /login, Secure over TLS.
const DeviceCookie = "termocode_device"

// deviceCookiePath: the cookie goes to the sign-in paths alone, never to
// the socket or the files.
const deviceCookiePath = "/login"

// cookieSecure: the page came over TLS, here or at a proxy in front (an
// https Origin, which the guard has checked). Plain loopback http stays
// without Secure, or the browser would never send the cookie back.
func cookieSecure(r *http.Request) bool {
	return r.TLS != nil || strings.HasPrefix(r.Header.Get("Origin"), "https://")
}

// setDeviceCookie hands the browser its (next) bookmark key.
func setDeviceCookie(w http.ResponseWriter, r *http.Request, key string) {
	http.SetCookie(w, &http.Cookie{Name: DeviceCookie, Value: key, Path: deviceCookiePath,
		MaxAge: int(BookmarkTTL / time.Second), HttpOnly: true, Secure: cookieSecure(r), SameSite: http.SameSiteStrictMode})
}

// clearDeviceCookie makes the browser drop its bookmark key.
func clearDeviceCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: DeviceCookie, Value: "", Path: deviceCookiePath,
		MaxAge: -1, HttpOnly: true, Secure: cookieSecure(r), SameSite: http.SameSiteStrictMode})
}

// requestKey is the bookmark key of a request: the cookie, else the key
// an older page sends in the body (moved into the cookie by the answer).
func requestKey(r *http.Request) (string, bool) {
	var req deviceKeyReq
	if !decodeStrict(r.Body, &req) {
		return "", false
	}
	if c, err := r.Cookie(DeviceCookie); err == nil && c.Value != "" {
		return c.Value, true
	}
	return req.Key, true
}

// device signs a remembered browser in with its bookmark key.
func (l *Login) device(w http.ResponseWriter, r *http.Request) {
	key, ok := requestKey(r)
	if !ok || key == "" {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return
	}
	origin := r.Header.Get("Origin")
	dev, sec, err := ParseDeviceKey(key)
	if err != nil {
		l.bookmarkFail(w, r, "", "bad_key", "login_failed")
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
		l.bookmarkFail(w, r, dev, "removed", "removed")
		return
	case why == "key_reused":
		l.bookmarkFail(w, r, dev, why, "revoked")
		return
	case why == "expired":
		l.bookmarkFail(w, r, dev, why, "expired")
		return
	case why != "":
		l.bookmarkFail(w, r, dev, why, "login_failed")
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
	if !l.issueWith(w, r, got, next, "bookmark") {
		// No session, so the page keeps its key: put it back.
		_, _ = st.Change(dev, func(d *webdevices.Device) {
			if b := d.Bookmark; b != nil && subtle.ConstantTimeCompare(b.Hash, nextHash) == 1 {
				b.Hash, b.Prev = h, oldPrev
			}
		})
	}
}

// bookmarkFail refuses a key: an audit line with the rule (never the
// key), the cookie cleared, and a short error for the page.
func (l *Login) bookmarkFail(w http.ResponseWriter, r *http.Request, dev, why, reply string) {
	clearDeviceCookie(w, r)
	_ = l.cfg.Audit.Log(Event{Kind: EvLoginFail, Code: http.StatusUnauthorized, Device: dev, Why: why, Summary: "bookmark"})
	writeJSON(w, http.StatusUnauthorized, errorReply(reply))
}

// forget ends a bookmark from the page: the key itself (now or the one
// before) proves which. The answer is the same either way.
func (l *Login) forget(w http.ResponseWriter, r *http.Request) {
	key, ok := requestKey(r)
	if !ok {
		writeJSON(w, http.StatusBadRequest, errorReply("bad_request"))
		return
	}
	clearDeviceCookie(w, r)
	dev, sec, err := ParseDeviceKey(key)
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
	guest := false
	found, err := st.Change(dev, func(d *webdevices.Device) {
		if guest = d.Guest; !guest { // a guest is for one visit: never a bookmark
			d.Bookmark = rotated(d.Bookmark, h, origin, now)
		}
	})
	if err != nil {
		return "", err
	}
	if !found || guest {
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
