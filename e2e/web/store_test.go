//go:build browser

package web

import (
	"maps"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// chatTopic is a view topic the page shows with its stale state.
const chatTopic = `document.querySelector('[data-view="chat"] .topic')`

// reconnect cuts the link and returns the page's next connection and the
// hello it sent; the reply is meta for conn.
func reconnect(t *testing.T, p *Page, c *HubConn, conn string) (*HubConn, *webproto.Hello) {
	t.Helper()
	c.Drop()
	p.WaitJS(`document.documentElement.dataset.link === "offline"`)
	c2 := p.S.Hub.Accept(t)
	return c2, c2.Handshake(t, conn)
}

// The store folds a snap and the deltas after it in order: the strip
// shows the last value, and the seqs it reports on resume are the last
// ones it applied.
func TestStoreAppliesDeltasInOrderAndResumesWithSeqs(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			c := p.Login("c-1")
			c.Send(t, Delta("status", 2, statusData("a", 1)))
			c.Send(t, Delta("status", 3, statusData("b", 2)))
			c.Send(t, Delta("status", 4, statusData("feat/last", 4)))
			c.Send(t, Snap("chat", 9, map[string]any{"n": 1}))
			c.Send(t, Delta("chat", 10, map[string]any{"n": 2}))
			p.WaitJS(`document.querySelector(".strip .branch")?.textContent.includes("feat/last")`)
			p.WaitJS(chatTopic + ` && !` + chatTopic + `.classList.contains("is-stale")`)

			_, hello := reconnect(t, p, c, "c-1")
			if hello.Resume == nil || hello.Resume.Conn != "c-1" {
				t.Fatalf("hello.resume = %+v, want conn c-1", hello.Resume)
			}
			want := map[string]uint64{"status": 4, "toasts": 1, "chat": 10}
			if !maps.Equal(hello.Resume.Topics, want) {
				t.Errorf("resume seqs = %v, want %v", hello.Resume.Topics, want)
			}
		})
	}
}

// A delta whose seq is not last + 1 marks the topic stale and broken and
// asks for it again; deltas that follow are ignored until a fresh snap.
func TestStoreGapAsksForResyncAndWaitsForSnap(t *testing.T) {
	for _, vp := range Both {
		t.Run(vp.Name, func(t *testing.T) {
			s := StartServer(t)
			p := NewPage(t, s, vp)
			c := p.Login("c-1")
			c.Send(t, Snap("chat", 1, map[string]any{"n": 1}))
			p.WaitJS(chatTopic)
			c.Send(t, Delta("chat", 3, map[string]any{"n": 3})) // 2 was lost
			c.WaitSub(t, "chat")
			p.WaitJS(chatTopic + `.classList.contains("is-stale")`)

			c.Send(t, Delta("chat", 2, map[string]any{"n": 2})) // fits the old seq: still not trusted
			c.Quiet(t, 500*time.Millisecond)                    // and no second sub
			p.Holds(chatTopic+`.classList.contains("is-stale")`, 200*time.Millisecond)

			c.Send(t, Snap("chat", 7, map[string]any{"n": 7}))
			p.WaitJS(`!` + chatTopic + `.classList.contains("is-stale")`)
		})
	}
}

// A broken topic is left out of hello.resume, so the hub sends a snap
// for it instead of deltas after a seq the page cannot trust.
func TestStoreBrokenTopicLeftOutOfResume(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Phone)
	c := p.Login("c-1")
	c.Send(t, Snap("chat", 1, map[string]any{"n": 1}))
	p.WaitJS(chatTopic)
	c.Send(t, Delta("chat", 5, map[string]any{"n": 5}))
	c.WaitSub(t, "chat")

	c2, hello := reconnect(t, p, c, "c-1")
	if hello.Resume == nil {
		t.Fatal("hello has no resume")
	}
	if _, ok := hello.Resume.Topics["chat"]; ok {
		t.Errorf("resume names the broken chat topic: %v", hello.Resume.Topics)
	}
	if hello.Resume.Topics["status"] != 1 {
		t.Errorf("resume lost the good status topic: %v", hello.Resume.Topics)
	}
	c2.WaitSub(t, "chat") // asked for afresh after the resume
}

// A resync from the hub (it dropped deltas) makes the page ask again and
// show the topic as stale until the new snap.
func TestStoreResyncFromHub(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	c := p.Login("c-1")
	c.Send(t, Snap("chat", 1, map[string]any{"n": 1}))
	p.WaitJS(chatTopic)
	c.Send(t, Resync("chat"))
	c.WaitSub(t, "chat")
	p.WaitJS(chatTopic + `.classList.contains("is-stale")`)
	c.Send(t, Snap("chat", 2, map[string]any{"n": 2}))
	p.WaitJS(`!` + chatTopic + `.classList.contains("is-stale")`)
}
