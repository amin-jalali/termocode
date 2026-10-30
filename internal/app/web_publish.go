package app

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Topics (WEB-7). A browser subscribes to topics ("status", "buf:12");
// each answers with a snap built on Update, then the Model publishes
// deltas from the places that already handle the change (webPublish).
// Seq numbers count per connection and topic: a snap and every delta
// after it take the next one, so the browser can tell a gap.

// webTopicDef is how the hub serves one topic name; the view tasks
// register theirs in webTopics.
type webTopicDef struct {
	// snapshot builds the full state on Update (key: "" or the buffer
	// number of a keyed topic). It must be fast (the call budget) and
	// return a hand-written struct with no secret fields; a *webError
	// picks the err code.
	snapshot func(m *Model, key string) (any, error)
	// apply folds one delta into a snapshot (both JSON, as on the wire):
	// the replay goldens check that apply(snap₀, deltas…) == snapₙ, and
	// the browser store mirrors it.
	apply func(snap, delta json.RawMessage) (json.RawMessage, error)
}

// webTopics are the served topics by name.
var webTopics = map[string]webTopicDef{}

// Per-connection topic caps (T48); webproto.MaxSubscriptions caps all.
const (
	webMaxBufTopics  = 16
	webMaxTermTopics = 8
)

// webError is an error with the err code the browser should see.
type webError struct {
	code webproto.ErrCode
	msg  string
}

func (e *webError) Error() string { return e.msg }

// webErr makes a *webError; msg is short English for the user.
func webErr(code webproto.ErrCode, msg string) error { return &webError{code: code, msg: msg} }

// webSub is one live subscription of a connection (Update only).
type webSub struct {
	seq   uint64    // the seq of the last snap or delta queued
	marks []webMark // the last seqs and the publishes they cover (resume)
}

// webSubMsg: a browser subscribed to or dropped topics.
type webSubMsg struct {
	hub    *webHub
	conn   *webConn
	topics []string
	unsub  bool
}

// onWebSub handles sub / unsub on Update.
func (m *Model) onWebSub(msg webSubMsg) {
	c := msg.conn
	for _, name := range msg.topics {
		if msg.unsub {
			delete(c.subs, name)
			c.out.resetTopic(name)
			msg.hub.pruneResume()
			continue
		}
		if err := m.webSubscribe(msg.hub, c, name); err != nil {
			code, text := webproto.CodeInternal, "This view could not be loaded."
			var we *webError
			if errors.As(err, &we) {
				code, text = we.code, we.msg
			} else {
				recordError("[web] snapshot " + name + ": " + err.Error())
			}
			data, _ := webData(map[string]string{"topic": name})
			c.send(&webproto.ErrMsg{T: webproto.KindErr, Code: code, Msg: webText(text), Data: data})
		}
	}
}

// webSubscribe (re)subscribes c to one topic and queues its snap.
func (m *Model) webSubscribe(h *webHub, c *webConn, name string) error {
	t, err := webproto.ParseTopic(name)
	if err != nil {
		return webErr(webproto.CodeBadRequest, "Unknown view.")
	}
	def, ok := h.topics[t.Name]
	if !ok || def.snapshot == nil {
		return webErr(webproto.CodeNotFound, "This view is not available yet.")
	}
	sub := c.subs[name]
	if sub == nil {
		if err := webSubCaps(c, t); err != nil {
			return err
		}
		sub = &webSub{}
	}
	start := webNow()
	snap, err := webSnapshot(m, def, t.Key)
	if d := webNow().Sub(start); d > webCallBudget {
		webSlowCall("snapshot "+t.Name, d)
	}
	if err != nil {
		return err
	}
	data, err := webData(snap)
	if err != nil {
		return err
	}
	c.subs[name] = sub
	c.out.resetTopic(name) // queued deltas are older than this snap
	sub.seq++
	sub.marks = sub.marks[:0]
	sub.mark(sub.seq, h.gseq)
	h.ring(name) // keep its deltas from now on, for a resume
	frame, err := webproto.Encode(&webproto.Snap{T: webproto.KindSnap, Topic: name, Seq: sub.seq, Data: data})
	if err != nil {
		return err
	}
	c.out.pushTopic(name, sub.seq, nil, frame) // a full queue turns it stale: resync
	return nil
}

// webSubCaps checks the per-connection topic caps for a new topic.
func webSubCaps(c *webConn, t webproto.Topic) error {
	if len(c.subs) >= webproto.MaxSubscriptions {
		return webErr(webproto.CodeLimit, "Too many open views.")
	}
	max := 0
	switch t.Name {
	case "buf":
		max = webMaxBufTopics
	case "term":
		max = webMaxTermTopics
	default:
		return nil
	}
	n := 0
	for name := range c.subs {
		if s, err := webproto.ParseTopic(name); err == nil && s.Name == t.Name {
			n++
		}
	}
	if n >= max {
		return webErr(webproto.CodeLimit, "Too many open "+t.Name+" views.")
	}
	return nil
}

// webSnapshot runs a snapshot; a panic is an error.
func webSnapshot(m *Model, def webTopicDef, key string) (snap any, err error) {
	defer func() {
		if r := recover(); r != nil {
			snap, err = nil, fmt.Errorf("panic: %v", r)
		}
	}()
	return def.snapshot(m, key)
}

// webPublish sends one delta of topic to every connection subscribed to
// it. Update only. It never blocks: a connection that cannot take it
// turns the topic stale and gets a resync (web_queue.go). Without a hub
// it does nothing.
func (m *Model) webPublish(topic string, delta any) {
	if m.web != nil {
		m.web.publish(topic, delta)
	}
}

// publish is webPublish on the hub (Update only).
func (h *webHub) publish(topic string, delta any) {
	var subs []*webConn
	for c := range h.live {
		if c.subs[topic] != nil {
			subs = append(subs, c)
		}
	}
	ring := h.rings[topic]
	if len(subs) == 0 && ring == nil {
		return
	}
	data, err := webData(delta)
	if err != nil {
		recordError("[web] publish " + topic + ": " + err.Error())
		return
	}
	h.gseq++
	if ring != nil { // kept for a browser that comes back (web_resume.go)
		ring.add(webRingItem{gseq: h.gseq, delta: delta, data: data})
	}
	for _, c := range subs {
		h.deliver(c, topic, delta, data, h.gseq)
	}
}

// deliver queues one delta of topic on c: merged into the newest queued
// delta when it can be, else with the next seq; dropped when the topic
// is stale there.
func (h *webHub) deliver(c *webConn, topic string, delta any, data []byte, gseq uint64) {
	sub := c.subs[topic]
	encodeMerged := func(merged any, seq uint64) ([]byte, error) {
		d, err := webData(merged)
		if err != nil {
			return nil, err
		}
		return webproto.Encode(&webproto.Delta{T: webproto.KindDelta, Topic: topic, Seq: seq, Data: d})
	}
	if c.out.mergeDelta(topic, delta, encodeMerged) {
		sub.mark(sub.seq, gseq)
		return
	}
	frame, err := webproto.Encode(&webproto.Delta{T: webproto.KindDelta, Topic: topic, Seq: sub.seq + 1, Data: data})
	if err != nil {
		recordError("[web] publish " + topic + ": " + err.Error())
		return
	}
	if c.out.pushTopic(topic, sub.seq+1, delta, frame) == webQueued {
		sub.seq++
		sub.mark(sub.seq, gseq)
	}
}

// webGoneMsg: a connection ended; Update forgets it.
type webGoneMsg struct {
	hub  *webHub
	conn *webConn
}

// onWebGone forgets a connection and keeps its topics for a resume.
func (m *Model) onWebGone(msg webGoneMsg) {
	h := msg.hub
	if !h.live[msg.conn] {
		return
	}
	delete(h.live, msg.conn)
	h.keepForResume(msg.conn)
	h.pruneResume()
}
