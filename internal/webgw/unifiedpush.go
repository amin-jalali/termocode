//go:build unix

package webgw

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// UnifiedPush wake on Android (MOB-32). A paired phone with a UnifiedPush
// distributor of the user's own (a self-hosted server or an app on the
// phone: no cloud of ours) gives the gateway its endpoint with
// push.subscribe. When the hub says more cards wait (a wake event on the
// control connection), the gateway POSTs the fixed body WakeBody to the
// endpoint of every subscribed phone that has no live link now, at most
// once per WakeGap each. The phone wakes, opens its link and reads the
// cards as usual: the push itself carries nothing (no count, no title,
// no id), so the distributor learns only that something happened.
//
// The endpoints live in the gateway's memory only, for this run: the
// phone subscribes again each time it connects. An endpoint is a
// capability URL; the audit log keeps its host, never its path.

// WakeBody is the whole body of a wake.
const WakeBody = "wake"

// WakeGap is the least time between two wakes of one phone.
const WakeGap = 30 * time.Second

// MaxPushSubs caps the subscriptions (one per paired device).
const MaxPushSubs = webdevices.MaxDevices

// watchRetry is the wait before telling the hub again after a failure.
const watchRetry = 2 * time.Second

// wakeTimeout bounds one POST to a distributor.
const wakeTimeout = 10 * time.Second

// ErrPushFull: every subscription slot is taken.
var ErrPushFull = errors.New("too many phones want wakes")

// UnifiedPushConfig sets up the wakes.
type UnifiedPushConfig struct {
	// Client posts the wakes (nil: pushClient, which never dials this
	// machine or a link-local address and follows no redirect).
	Client *http.Client
	// Now is the clock (nil: time.Now).
	Now func() time.Time
	// Live says whether device has a live link now (nil: never).
	Live func(device string) bool
	// Watch tells the hub whether any phone wants wakes (web.push_watch).
	Watch func(ctx context.Context, on bool) error
	// Audit hears subscriptions and wakes (nil: none).
	Audit *Audit
}

// UnifiedPush holds the endpoints and sends the wakes.
type UnifiedPush struct {
	cfg  UnifiedPushConfig
	kick chan struct{} // the watch loop has a new state to send

	mu   sync.Mutex
	subs map[string]*upSub // by device
	wg   sync.WaitGroup
}

type upSub struct {
	endpoint string
	last     time.Time // the last wake started
	sending  bool
}

// NewUnifiedPush makes an empty list.
func NewUnifiedPush(cfg UnifiedPushConfig) *UnifiedPush {
	if cfg.Client == nil {
		cfg.Client = pushClient()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &UnifiedPush{cfg: cfg, kick: make(chan struct{}, 1), subs: map[string]*upSub{}}
}

// Subscribe keeps device's endpoint (checked by the op's args already);
// a new one replaces the old.
func (u *UnifiedPush) Subscribe(device, endpoint string) error {
	if device == "" || endpoint == "" {
		return errors.New("no device or endpoint")
	}
	u.mu.Lock()
	if s, ok := u.subs[device]; ok {
		s.endpoint = endpoint
		u.mu.Unlock()
		return nil
	}
	if len(u.subs) >= MaxPushSubs {
		u.mu.Unlock()
		return ErrPushFull
	}
	u.subs[device] = &upSub{endpoint: endpoint}
	u.mu.Unlock()
	u.changed()
	return nil
}

// Unsubscribe drops device's endpoint; false when it had none.
func (u *UnifiedPush) Unsubscribe(device string) bool {
	u.mu.Lock()
	_, ok := u.subs[device]
	delete(u.subs, device)
	u.mu.Unlock()
	if ok {
		u.changed()
	}
	return ok
}

// Forget drops a removed device's endpoint.
func (u *UnifiedPush) Forget(device string) { u.Unsubscribe(device) }

// Len is the number of subscribed phones.
func (u *UnifiedPush) Len() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.subs)
}

// changed wakes the watch loop (never blocks).
func (u *UnifiedPush) changed() {
	select {
	case u.kick <- struct{}{}:
	default:
	}
}

// Run tells the hub, one call at a time, whether any phone wants wakes,
// until ctx ends. Only the newest state is sent.
func (u *UnifiedPush) Run(ctx context.Context) {
	sent := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-u.kick:
		}
		on := u.Len() > 0
		if on == sent || u.cfg.Watch == nil {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, wakeTimeout)
		err := u.cfg.Watch(cctx, on)
		cancel()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			t := time.NewTimer(watchRetry) // then try again
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
			u.changed()
			continue
		}
		sent = on
	}
}

// Wake starts a wake for every subscribed phone without a live link
// whose last wake is WakeGap old, and says how many it started.
func (u *UnifiedPush) Wake(ctx context.Context) int {
	now := u.cfg.Now()
	type job struct{ device, endpoint string }
	var jobs []job
	u.mu.Lock()
	for dev, s := range u.subs {
		if s.sending || (!s.last.IsZero() && now.Sub(s.last) < WakeGap) {
			continue
		}
		if u.cfg.Live != nil && u.cfg.Live(dev) {
			continue
		}
		s.sending, s.last = true, now
		jobs = append(jobs, job{dev, s.endpoint})
	}
	u.mu.Unlock()
	for _, j := range jobs {
		u.wg.Add(1)
		crash.Go("web gateway: a push wake", func() {
			defer u.wg.Done()
			u.send(ctx, j.device, j.endpoint)
		})
	}
	return len(jobs)
}

// Wait waits for the wakes on their way.
func (u *UnifiedPush) Wait() { u.wg.Wait() }

// send posts one wake; a gone endpoint (404, 410) is dropped.
func (u *UnifiedPush) send(ctx context.Context, device, endpoint string) {
	ctx, cancel := context.WithTimeout(ctx, wakeTimeout)
	defer cancel()
	code, err := u.post(ctx, endpoint)
	ok := err == nil && code >= 200 && code < 300
	gone := code == http.StatusNotFound || code == http.StatusGone
	u.mu.Lock()
	if s, has := u.subs[device]; has {
		s.sending = false
		if gone && s.endpoint == endpoint {
			delete(u.subs, device)
		} else {
			gone = false
		}
	} else {
		gone = false
	}
	u.mu.Unlock()
	if gone {
		u.changed()
	}
	why := ""
	switch {
	case gone:
		why = "endpoint_gone"
	case err != nil:
		why = "unreachable"
	case !ok:
		why = "refused"
	}
	_ = u.cfg.Audit.Log(Event{Kind: EvPushWake, Device: device, OK: &ok, Code: code, Why: why, Summary: pushHost(endpoint)})
}

func (u *UnifiedPush) post(ctx context.Context, endpoint string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(WakeBody))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("TTL", "600")      // a wake older than 10 minutes is no use
	req.Header.Set("Urgency", "high") // a card waits for the user
	res, err := u.cfg.Client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4<<10))
	return res.StatusCode, nil
}

// pushHost is what the audit keeps of an endpoint: its host.
func pushHost(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err != nil {
		return ""
	}
	return u.Host
}

// pushAddrOK reports whether a wake may be dialed to ip.
func pushAddrOK(ip netip.Addr) bool { return webproto.PushAddrOK(ip) }

// pushClient posts wakes: no proxy, no redirect, a short timeout, and a
// dialer that checks the address it really dials (a name that resolves
// to this machine or a link-local address is refused).
func pushClient() *http.Client {
	d := &net.Dialer{Timeout: wakeTimeout, Control: func(_, address string, _ syscall.RawConn) error {
		ap, err := netip.ParseAddrPort(address)
		if err != nil || !pushAddrOK(ap.Addr()) {
			return errors.New("push endpoint address refused")
		}
		return nil
	}}
	return &http.Client{
		Timeout: wakeTimeout,
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           d.DialContext,
			TLSHandshakeTimeout:   wakeTimeout,
			ResponseHeaderTimeout: wakeTimeout,
			MaxIdleConns:          4,
			IdleConnTimeout:       time.Minute,
			ForceAttemptHTTP2:     true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// pushOp answers push.subscribe and push.unsubscribe: a paired phone's
// own (never a guest's or a launch link's), for itself only.
func (c *wsConn) pushOp(r *webproto.Req) {
	up := c.s.cfg.Push
	if up == nil {
		c.refuseReq(r.ID, webproto.CodeNotFound, "This is not available here.")
		return
	}
	dev := c.sess.Device
	if st := c.s.cfg.Devices; !strings.HasPrefix(dev, "dev-") || st == nil {
		c.pushAudit(r.Op, false, "not_paired", "")
		c.refuseReq(r.ID, webproto.CodeForbidden, "Pair this phone first.")
		return
	} else if d, ok, err := st.Get(dev); err != nil || !ok || d.Guest {
		c.pushAudit(r.Op, false, "guest", "")
		c.refuseReq(r.ID, webproto.CodeForbidden, "A shared view gets no wakes.")
		return
	}
	args, err := webproto.DecodeArgs(r.Op, r.Args)
	if err != nil {
		c.refuseReq(r.ID, webproto.CodeBadRequest, "That request is not valid.")
		return
	}
	switch a := args.(type) {
	case *webproto.PushSubArgs:
		if err := up.Subscribe(dev, a.Endpoint); err != nil {
			c.pushAudit(r.Op, false, "full", pushHost(a.Endpoint))
			c.refuseReq(r.ID, webproto.CodeLimit, "Too many phones want wakes. Remove one on the laptop.")
			return
		}
		c.pushAudit(r.Op, true, "", pushHost(a.Endpoint))
	case *webproto.PushUnsubscribeArgs:
		up.Unsubscribe(dev)
		c.pushAudit(r.Op, true, "", "")
	}
	c.answerReq(r.ID, nil)
}

func (c *wsConn) pushAudit(op string, ok bool, why, summary string) {
	_ = c.s.cfg.Audit.Log(Event{Kind: EvOp, Conn: c.id, Device: c.sess.Device, Op: op, OK: &ok, Why: why,
		Summary: summary, By: "web:" + c.sess.Device})
}

// HasDevice reports whether device has a live link now.
func (s *WS) HasDevice(device string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for c := range s.conns {
		if c.sess.Device == device {
			return true
		}
	}
	return false
}
