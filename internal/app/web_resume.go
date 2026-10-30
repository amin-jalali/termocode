package app

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Resume (WEB-7). When a browser connection drops, the hub keeps its
// topic names and seq marks for webResumeFor (names and ints only, T48).
// Each topic with a subscriber or a kept entry also has a small ring of
// its last published deltas. A browser that comes back in time sends
// hello.resume with its old connection id and the last seq it saw per
// topic: when the ring still holds everything after that seq, it gets the
// missed deltas with its seqs going on; otherwise a fresh snap. Expiry is
// checked when a connection comes or goes: no timer runs in the UI.

// Resume limits.
const (
	webResumeFor = 60 * time.Second
	webRingLen   = 64       // deltas kept per topic
	webRingBytes = 64 << 10 // bytes of delta data kept per topic
	webMarksLen  = webRingLen
)

// webMark ties a connection's seq to the publish counter: the frame with
// seq includes every publish of its topic up to gseq.
type webMark struct{ seq, gseq uint64 }

// webResumeTopic is what a dropped connection leaves of one topic.
type webResumeTopic struct {
	marks []webMark
	stale bool
}

// webResumeEntry is a dropped connection, kept for webResumeFor.
type webResumeEntry struct {
	at     int64 // webNow().UnixNano() when it dropped
	topics map[string]webResumeTopic
}

// webRing keeps the last published deltas of one topic.
type webRing struct {
	lost  uint64 // publishes with gseq ≤ lost are not here
	items []webRingItem
	bytes int
}

type webRingItem struct {
	gseq  uint64
	delta any
	data  json.RawMessage // already redacted
}

// ring returns the topic's ring, making it when missing (it then knows
// nothing published before now).
func (h *webHub) ring(topic string) *webRing {
	r := h.rings[topic]
	if r == nil {
		r = &webRing{lost: h.gseq}
		h.rings[topic] = r
	}
	return r
}

// add keeps one publish, dropping the oldest past the caps.
func (r *webRing) add(it webRingItem) {
	r.items = append(r.items, it)
	r.bytes += len(it.data)
	for len(r.items) > webRingLen || (r.bytes > webRingBytes && len(r.items) > 1) {
		r.lost = r.items[0].gseq
		r.bytes -= len(r.items[0].data)
		r.items[0] = webRingItem{}
		r.items = r.items[1:]
	}
}

// mark records that seq of topic on c covers publishes up to gseq; a
// merged frame moves its mark up (same seq).
func (s *webSub) mark(seq, gseq uint64) {
	if n := len(s.marks); n > 0 && s.marks[n-1].seq == seq {
		s.marks[n-1].gseq = gseq
		return
	}
	s.marks = append(s.marks, webMark{seq, gseq})
	if len(s.marks) > webMarksLen {
		s.marks = append(s.marks[:0:0], s.marks[len(s.marks)-webMarksLen:]...)
	}
}

// keepForResume records a dropped browser connection's topics.
func (h *webHub) keepForResume(c *webConn) {
	if len(c.subs) == 0 {
		return
	}
	e := &webResumeEntry{at: webNow().UnixNano(), topics: map[string]webResumeTopic{}}
	for name, s := range c.subs {
		e.topics[name] = webResumeTopic{marks: append([]webMark(nil), s.marks...), stale: c.out.isStale(name)}
	}
	h.resume[c.id] = e
}

// pruneResume drops expired entries and the rings nobody needs.
func (h *webHub) pruneResume() {
	now := webNow().UnixNano()
	need := map[string]bool{}
	for id, e := range h.resume {
		if now-e.at > int64(webResumeFor) {
			delete(h.resume, id)
			continue
		}
		for name := range e.topics {
			need[name] = true
		}
	}
	for c := range h.live {
		for name := range c.subs {
			need[name] = true
		}
	}
	for name := range h.rings {
		if !need[name] {
			delete(h.rings, name)
		}
	}
}

// webResume continues the topics of an earlier connection on c (Update,
// right after meta). A topic that cannot continue gets a fresh snap.
func (m *Model) webResume(h *webHub, c *webConn, r *webproto.Resume) {
	h.pruneResume() // an expired entry is gone now
	e := h.resume[r.Conn]
	delete(h.resume, r.Conn)
	names := make([]string, 0, len(r.Topics))
	for name := range r.Topics {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if e != nil && h.resumeTopic(c, name, r.Topics[name], e.topics[name]) {
			continue
		}
		m.onWebSub(webSubMsg{hub: h, conn: c, topics: []string{name}})
	}
	h.pruneResume()
}

// resumeTopic continues one topic from the browser's last seq; false when
// it cannot (stale, unknown seq, or the ring lost a delta since).
func (h *webHub) resumeTopic(c *webConn, name string, seen uint64, rt webResumeTopic) bool {
	if rt.stale || c.subs[name] != nil {
		return false
	}
	var gseq uint64
	found := false
	for _, mk := range rt.marks {
		if mk.seq == seen {
			gseq, found = mk.gseq, true
		}
	}
	ring := h.rings[name]
	if !found || ring == nil || ring.lost > gseq {
		return false
	}
	t, err := webproto.ParseTopic(name)
	if err != nil || webSubCaps(c, t) != nil {
		return false
	}
	sub := &webSub{seq: seen}
	sub.mark(seen, gseq)
	c.subs[name] = sub
	for _, it := range ring.items {
		if it.gseq > gseq {
			h.deliver(c, name, it.delta, it.data, it.gseq)
		}
	}
	return true
}
