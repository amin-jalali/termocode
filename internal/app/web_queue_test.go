package app

import (
	"bytes"
	"strings"
	"testing"
)

func webTestFrame(n int) []byte { return bytes.Repeat([]byte("x"), n) }

// Spec (WEB-7, T48): a connection queues at most 256 topic messages or
// 2 MiB. A topic message that does not fit turns its topic stale: it and
// every later delta of the topic are dropped, the next take ends with
// one resync for it, and only a new snap (resetTopic) ends it. Answers
// pass the soft caps; past the hard caps the queue says full.
func TestQueueCapsResync(t *testing.T) {
	q := newWebQueue()
	for i := 0; i < webQueueMsgs; i++ {
		if got := q.pushTopic("status", uint64(i+1), nil, webTestFrame(10)); got != webQueued {
			t.Fatalf("item %d: %v", i, got)
		}
	}
	if got := q.pushTopic("status", 999, nil, webTestFrame(10)); got != webDropped || !q.isStale("status") {
		t.Fatalf("over the count cap: %v", got)
	}
	if got := q.pushTopic("other", 1, nil, webTestFrame(10)); got != webDropped {
		t.Fatalf("another topic over the cap: %v", got)
	}
	if got := q.push([]byte(`{"t":"res","id":1}`)); got != webQueued {
		t.Fatalf("an answer over the soft cap: %v", got)
	}
	frames, ok := q.take()
	if !ok || len(frames) != webQueueMsgs+1+2 {
		t.Fatalf("take: %d frames", len(frames))
	}
	tail := string(frames[len(frames)-2]) + string(frames[len(frames)-1])
	if !strings.Contains(tail, `{"t":"resync","topic":"status"}`) || !strings.Contains(tail, `{"t":"resync","topic":"other"}`) {
		t.Fatalf("tail %s", tail)
	}
	// Still stale (no resync twice) until a new snap.
	if got := q.pushTopic("status", 1000, nil, webTestFrame(10)); got != webDropped {
		t.Fatalf("stale topic took a delta: %v", got)
	}
	q.resetTopic("status")
	if got := q.pushTopic("status", 1000, nil, webTestFrame(10)); got != webQueued {
		t.Fatalf("after the new snap: %v", got)
	}
	if frames, _ := q.take(); len(frames) != 1 {
		t.Fatalf("one frame and no second resync, got %d", len(frames))
	}

	// The byte cap; one big snap fits an empty queue.
	if got := q.pushTopic("buf:1", 1, nil, webTestFrame(webQueueBytes+1)); got != webQueued {
		t.Fatalf("a big snap into an empty queue: %v", got)
	}
	q.take()
	q.pushTopic("buf:1", 1, nil, webTestFrame(webQueueBytes/2))
	q.pushTopic("buf:1", 2, nil, webTestFrame(webQueueBytes/2))
	if got := q.pushTopic("buf:1", 3, nil, webTestFrame(1)); got != webDropped {
		t.Fatalf("over the byte cap: %v", got)
	}
	q.take()

	// The hard cap for answers.
	for i := 0; i < webQueueHardMsgs; i++ {
		q.push([]byte("{}"))
	}
	if got := q.push([]byte("{}")); got != webFull {
		t.Fatalf("past the hard cap: %v", got)
	}
	q.close()
	if got := q.push([]byte("{}")); got != webClosed {
		t.Fatalf("closed: %v", got)
	}
}
