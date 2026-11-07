package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/webpush"
)

// Web Push to paired phones (MOB-30). When something starts to wait for
// the user (a card: a tool call, a plan, a review …) and a paired phone
// that asked for it has no page open, the laptop pushes to that phone
// through its browser's own push service: no cloud of ours. The push is
// encrypted for the phone and says only a count (web_push_detail), so
// the vendor sees ciphertext and a locked screen shows a fixed text.
//
// The VAPID key and each phone's subscription live in the secret store
// (web.push.vapid, web.push.sub.<device>), never in a file of the web
// panel. A locked store sends nothing. A removed device's subscription
// is dropped at the next push. The check runs on Update only while some
// phone has a subscription, at most once a second, and a push goes at
// most every webPushCooldown, with the count of that moment.

func init() {
	webOps[webproto.OpPushKey] = (*Model).webPushKeyOp
	webOps[webproto.OpPushSubscribe] = (*Model).webPushSubscribe
	webOps[webproto.OpPushUnsubscribe] = (*Model).webPushUnsubscribe
}

const (
	webPushKeyName   = "web.push.vapid"
	webPushSubPrefix = "web.push.sub."
	webPushCooldown  = 30 * time.Second
	webPushEvery     = time.Second
	// webPushSubject is the VAPID contact the push services see.
	webPushSubject = "https://github.com/amin-jalali/termocode"
)

// Push states (webproto.PushState.State).
const (
	webPushReady  = "ready"
	webPushNone   = "none"
	webPushLocked = "locked"
	webPushOff    = "off"
)

// webPushState is the hub's push state (Update only).
type webPushState struct {
	any     bool      // some device has a subscription
	lastN   int       // what waited at the last check
	checked time.Time // the last check
	sent    time.Time // the last push
	tick    bool      // a webPushTickMsg is on its way
	sending bool      // a push is being sent
}

// webPushTickMsg brings a check that was too early back.
type webPushTickMsg struct{}

// webPushDevices lists the paired devices (a seam for tests).
var webPushDevices = func() ([]webdevices.Device, error) {
	dir, err := webDevicesDir()
	if err != nil {
		return nil, err
	}
	return webdevices.Open(dir).List()
}

// webPushKeyMu makes the key once when two phones ask at once.
var webPushKeyMu sync.Mutex

// webPushKey reads the VAPID key; create makes it when there is none.
func webPushKey(create bool) (*webpush.Key, string) {
	s, err := webPushStore()
	if err != nil || s == nil {
		return nil, webPushOff
	}
	if s.Locked() {
		return nil, webPushLocked
	}
	webPushKeyMu.Lock()
	defer webPushKeyMu.Unlock()
	v, ok, err := s.Get(webPushKeyName)
	if err != nil {
		return nil, webPushOff
	}
	if ok {
		if k, err := webpush.ParseKey(v); err == nil {
			return k, webPushReady
		}
		recordError("[web] push: the stored key does not parse; a new one is made")
	}
	if !create {
		return nil, webPushNone
	}
	k, err := webpush.NewKey(rand.Reader)
	if err != nil {
		return nil, webPushOff
	}
	if err := s.Set(webPushKeyName, k.Private()); err != nil {
		recordError("[web] push: save the key: " + secretsRedact(err))
		return nil, webPushOff
	}
	return k, webPushReady
}

// webPushDevice says whether dev is a paired device that may get pushes:
// not a launch-link tab, not a shared view.
func webPushDevice(dev string) bool {
	if !strings.HasPrefix(dev, "dev-") || !webPushValidName(webPushSubPrefix+dev) {
		return false
	}
	ds, err := webPushDevices()
	if err != nil {
		return false
	}
	for _, d := range ds {
		if d.ID == dev {
			return !d.Guest
		}
	}
	return false
}

// webPushHasSub says whether dev has a subscription.
func webPushHasSub(s webSecretStore, dev string) bool {
	for _, n := range s.Names() {
		if n == webPushSubPrefix+dev {
			return true
		}
	}
	return false
}

// webPushAny says whether some device has a subscription.
func webPushAny(s webSecretStore) bool {
	for _, n := range s.Names() {
		if strings.HasPrefix(n, webPushSubPrefix) {
			return true
		}
	}
	return false
}

// webPushLoad reads whether some device has a subscription (the hub
// starts: a phone may wait for pushes with no page open).
func webPushLoad() tea.Msg {
	s, err := webPushStore()
	return webPushSetAny(err == nil && s != nil && webPushAny(s))
}

// webPushSetAny tells Update what the store has now.
func webPushSetAny(has bool) tea.Msg {
	return applyMsg(func(m *Model) tea.Cmd {
		if m.web != nil {
			m.web.push.any = has
		}
		return nil
	})
}

func (m *Model) webPushKeyOp(call *webCall) tea.Cmd {
	create := call.args.(*webproto.PushKeyArgs).Make
	dev := call.conn.device
	call.later()
	return func() tea.Msg {
		if !webPushDevice(dev) {
			call.reply(webproto.PushState{State: webPushOff})
			return nil
		}
		k, st := webPushKey(create)
		out := webproto.PushState{State: st}
		if k != nil {
			out.Key = k.Public()
			if s, err := webPushStore(); err == nil && s != nil {
				out.On = webPushHasSub(s, dev)
			}
		}
		call.reply(out)
		return nil
	}
}

func (m *Model) webPushSubscribe(call *webCall) tea.Cmd {
	a := call.args.(*webproto.PushSubArgs)
	if a.Kind == webproto.PushUnifiedPush { // the gateway's kind (MOB-32); never reaches here from a browser
		call.fail(webproto.CodeBadRequest, "This push kind is kept by the gateway.")
		return nil
	}
	sub := webpush.Sub{Endpoint: a.Endpoint, P256dh: a.P256dh, Auth: a.Auth}
	dev := call.conn.device
	call.later()
	return func() tea.Msg {
		if !webPushDevice(dev) {
			call.fail(webproto.CodeForbidden, "Only a paired device gets alerts.")
			return nil
		}
		if sub.Check() != nil {
			call.fail(webproto.CodeBadRequest, "This browser gave a bad push address.")
			return nil
		}
		k, st := webPushKey(true)
		if k == nil {
			call.reply(webproto.PushState{State: st})
			return nil
		}
		b, err := json.Marshal(sub)
		s, serr := webPushStore()
		if err != nil || serr != nil || s == nil {
			call.fail(webproto.CodeInternal, "Could not save it on the laptop.")
			return nil
		}
		if err := s.Set(webPushSubPrefix+dev, string(b)); err != nil {
			recordError("[web] push: save a subscription: " + secretsRedact(err))
			call.fail(webproto.CodeInternal, "Could not save it on the laptop.")
			return nil
		}
		call.reply(webproto.PushState{State: webPushReady, Key: k.Public(), On: true})
		return webPushSetAny(true)
	}
}

func (m *Model) webPushUnsubscribe(call *webCall) tea.Cmd {
	dev := call.conn.device
	call.later()
	return func() tea.Msg {
		s, err := webPushStore()
		if err != nil || s == nil {
			call.reply(webproto.PushState{State: webPushOff})
			return nil
		}
		if strings.HasPrefix(dev, "dev-") && webPushValidName(webPushSubPrefix+dev) {
			if err := s.Delete(webPushSubPrefix + dev); err != nil {
				call.fail(webproto.CodeInternal, "Could not change it on the laptop.")
				return nil
			}
		}
		call.reply(webproto.PushState{State: webPushReady})
		return webPushSetAny(webPushAny(s))
	}
}

// decide says whether n waiting things make a push now: more wait than
// at the last check, nothing is being sent and the cooldown is over.
// wait > 0: look again then (the push is only held back).
func (p *webPushState) decide(n int, now time.Time) (send bool, wait time.Duration) {
	if n <= p.lastN {
		p.lastN = n
		return false, 0
	}
	if p.sending {
		return false, webPushEvery
	}
	if left := webPushCooldown - now.Sub(p.sent); !p.sent.IsZero() && left > 0 {
		return false, left
	}
	p.lastN, p.sent, p.sending = n, now, true
	return true, 0
}

// arm asks for one webPushTickMsg after d.
func (p *webPushState) arm(d time.Duration) tea.Cmd {
	if p.tick {
		return nil
	}
	p.tick = true
	return tea.Tick(d, func(time.Time) tea.Msg { return webPushTickMsg{} })
}

// webPushWaiting counts the cards that wait for the user (not a laptop
// card, which is the web's own request) and the kind of the newest.
func (m *Model) webPushWaiting() (n int, kind string) {
	newest := int64(-1)
	for _, r := range m.webCards(&webCardsPub{}, false) {
		c := r.card
		if c.Kind == webproto.CardLaptop {
			continue
		}
		n++
		if c.Since >= newest {
			newest, kind = c.Since, c.Kind
		}
	}
	return n, kind
}

// webPushAfterUpdate runs after every Update, also while no browser is
// connected (that is when a push matters).
func (m *Model) webPushAfterUpdate(msg tea.Msg) tea.Cmd {
	p := &m.web.push
	if _, ok := msg.(webPushTickMsg); ok {
		p.tick = false
	}
	if !p.any {
		p.lastN = 0
		return nil
	}
	now := time.Now()
	if early := webPushEvery - now.Sub(p.checked); early > 0 {
		return p.arm(early)
	}
	p.checked = now
	n, kind := m.webPushWaiting()
	send, wait := p.decide(n, now)
	if !send {
		if wait > 0 {
			return p.arm(wait)
		}
		return nil
	}
	payload := webpush.Payload(webSettings().PushDetail, n, kind)
	live := m.webLiveDevices()
	return func() tea.Msg {
		left := webPushSendAll(context.Background(), payload, live)
		return applyMsg(func(m *Model) tea.Cmd {
			if m.web != nil {
				m.web.push.sending, m.web.push.any = false, left
			}
			return nil
		})
	}
}

// webPushSender posts the pushes (a seam for tests).
var webPushSender = func(k *webpush.Key) *webpush.Sender {
	return &webpush.Sender{Key: k, Subject: webPushSubject}
}

// webPushSendAll pushes payload to every subscribed device without a
// page open (live) and says whether a subscription is left. A dead one
// (the push service said gone) and one of a device that is no longer
// paired are forgotten. An error names the device, never the address.
func webPushSendAll(ctx context.Context, payload []byte, live map[string]bool) bool {
	s, err := webPushStore()
	if err != nil || s == nil {
		return false
	}
	if s.Locked() {
		return true // kept: it goes again once the store is open
	}
	k, _ := webPushKey(false)
	if k == nil {
		return webPushAny(s)
	}
	paired := map[string]bool{}
	ds, derr := webPushDevices()
	for _, d := range ds {
		if !d.Guest {
			paired[d.ID] = true
		}
	}
	snd := webPushSender(k)
	left := false
	for _, name := range s.Names() {
		dev, ok := strings.CutPrefix(name, webPushSubPrefix)
		if !ok {
			continue
		}
		if derr == nil && !paired[dev] {
			_ = s.Delete(name)
			continue
		}
		v, ok, err := s.Get(name)
		var sub webpush.Sub
		if err != nil || !ok {
			left = true
			continue
		}
		if json.Unmarshal([]byte(v), &sub) != nil || sub.Check() != nil {
			_ = s.Delete(name)
			continue
		}
		if live[dev] {
			left = true
			continue // its page shows the card already
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		gone, err := snd.Send(cctx, sub, payload)
		cancel()
		switch {
		case gone:
			_ = s.Delete(name)
			continue
		case err != nil:
			recordError("[web] push to " + dev + ": " + err.Error())
		}
		left = true
	}
	return left
}
