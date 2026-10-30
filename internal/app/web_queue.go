package app

import (
	"sync"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// A connection's outbound queue (WEB-7). Only Update queues topic
// messages (snap, delta); answers may come from any goroutine. Topic
// messages obey the soft caps (webQueueMsgs / webQueueBytes): when a
// topic message does not fit, that topic turns stale, its deltas are
// dropped, and one resync goes out as soon as the writer made room. The
// browser then subscribes again and gets a fresh snap. A new delta may
// merge with the newest queued delta of its topic (webCoalescer).
// Messages not tied to a topic (res, err, meta, pong) are never dropped;
// a queue past the hard caps means a stuck gateway and the connection is
// closed.

// Queue caps (T48).
const (
	webQueueMsgs      = 256
	webQueueBytes     = 2 << 20
	webQueueHardMsgs  = 2 * webQueueMsgs
	webQueueHardBytes = 2 * webQueueBytes
)

// webPush is what happened to a queued message.
type webPush int

const (
	webQueued  webPush = iota // a new item
	webMerged                 // merged into the newest delta of its topic
	webDropped                // the topic is stale: dropped, resync owed
	webClosed                 // the queue is closed
	webFull                   // past the hard caps: close the connection
)

// webCoalescer is a delta that can absorb the next delta of its topic.
// coalesce returns the merged delta, or ok=false when next does not fit.
type webCoalescer interface {
	coalesce(next any) (merged any, ok bool)
}

// webOut is one queued message.
type webOut struct {
	topic string // "" for messages not tied to a topic
	delta any    // the typed delta, for coalescing (nil for others)
	seq   uint64
	frame []byte
}

// webQueue is a connection's outbound queue. Nothing in it blocks.
type webQueue struct {
	mu     sync.Mutex
	items  []*webOut
	bytes  int
	last   map[string]*webOut // the newest queued topic item per topic
	stale  map[string]bool    // topics whose deltas are dropped until reset
	owed   []string           // stale topics whose resync is not out yet
	closed bool
	wake   chan struct{} // 1 slot: "something changed"
}

func newWebQueue() *webQueue {
	return &webQueue{wake: make(chan struct{}, 1), last: map[string]*webOut{}, stale: map[string]bool{}}
}

func (q *webQueue) signal() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *webQueue) add(o *webOut) {
	q.items = append(q.items, o)
	q.bytes += len(o.frame)
	if o.topic != "" {
		q.last[o.topic] = o
	}
}

// push queues a message not tied to a topic.
func (q *webQueue) push(frame []byte) webPush {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return webClosed
	}
	if len(q.items) >= webQueueHardMsgs || q.bytes+len(frame) > webQueueHardBytes {
		return webFull
	}
	q.add(&webOut{frame: frame})
	q.signal()
	return webQueued
}

// fits reports whether one more topic item of n bytes fits the soft caps
// (a single big snap fits an empty queue).
func (q *webQueue) fits(n int) bool {
	return len(q.items) < webQueueMsgs && (q.bytes+n <= webQueueBytes || len(q.items) == 0)
}

// pushTopic queues a snap or delta frame of topic (delta: the typed
// value for coalescing, nil for a snap).
func (q *webQueue) pushTopic(topic string, seq uint64, delta any, frame []byte) webPush {
	q.mu.Lock()
	defer q.mu.Unlock()
	switch {
	case q.closed:
		return webClosed
	case q.stale[topic]:
		return webDropped
	case !q.fits(len(frame)):
		q.stale[topic] = true
		q.owed = append(q.owed, topic)
		return webDropped
	}
	q.add(&webOut{topic: topic, delta: delta, seq: seq, frame: frame})
	q.signal()
	return webQueued
}

// mergeDelta merges next into the newest queued delta of topic when that
// delta is a webCoalescer that takes it; encode makes the new frame for
// the merged delta at the old seq. ok=false: nothing merged.
func (q *webQueue) mergeDelta(topic string, next any, encode func(merged any, seq uint64) ([]byte, error)) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	o := q.last[topic]
	if q.closed || q.stale[topic] || o == nil || o.delta == nil {
		return false
	}
	c, ok := o.delta.(webCoalescer)
	if !ok {
		return false
	}
	merged, ok := c.coalesce(next)
	if !ok {
		return false
	}
	frame, err := encode(merged, o.seq)
	if err != nil || q.bytes-len(o.frame)+len(frame) > webQueueBytes {
		return false
	}
	q.bytes += len(frame) - len(o.frame)
	o.delta, o.frame = merged, frame
	return true
}

// resetTopic forgets the queued items of topic and its stale mark (a new
// snap follows, or the topic was dropped).
func (q *webQueue) resetTopic(topic string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.items[:0]
	for _, o := range q.items {
		if o.topic == topic {
			q.bytes -= len(o.frame)
			continue
		}
		kept = append(kept, o)
	}
	for i := len(kept); i < len(q.items); i++ {
		q.items[i] = nil
	}
	q.items = kept
	delete(q.last, topic)
	delete(q.stale, topic)
	for i, t := range q.owed {
		if t == topic {
			q.owed = append(q.owed[:i], q.owed[i+1:]...)
			break
		}
	}
}

// isStale reports whether topic's deltas are being dropped.
func (q *webQueue) isStale(topic string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.stale[topic]
}

// take waits for frames and returns all of them, followed by the resync
// of every topic that turned stale (the queue is empty now, so there is
// room). ok=false once the queue is closed and empty.
func (q *webQueue) take() ([][]byte, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 || len(q.owed) > 0 {
			frames := make([][]byte, 0, len(q.items)+len(q.owed))
			for _, o := range q.items {
				frames = append(frames, o.frame)
			}
			for _, t := range q.owed {
				if b, err := webproto.Encode(&webproto.Resync{T: webproto.KindResync, Topic: t}); err == nil {
					frames = append(frames, b)
				}
			}
			q.items, q.bytes, q.owed = nil, 0, nil
			clear(q.last)
			q.mu.Unlock()
			return frames, true
		}
		closed := q.closed
		q.mu.Unlock()
		if closed {
			return nil, false
		}
		<-q.wake
	}
}

// close stops new messages; take still returns what is queued.
func (q *webQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.signal()
}
