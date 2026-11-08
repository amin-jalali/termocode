//go:build browser

package web

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Typing fast. A key goes to the shell at once; the keys typed while the
// shell's term.send waits for its answer go together as one term.send
// when it comes, in the order typed. So a fast typist or a held key costs
// one request per round trip, not one per key, and never waits behind the
// gateway's request rate (20 a second).
func TestDeskTermKeysWhileOneWaitsGoTogether(t *testing.T) {
	p, c := openTerm(t, Desktop)
	showTerm(t, p, c, termSnap(3, 0, 80, "$"), termItem(3, "zsh", false, true))
	p.WaitJS(`document.activeElement === ` + deskInput)

	p.Key("a")
	first := expectReq(t, c, "term.send") // a lone key: at once, not held back
	if text := termSendText(t, first); text != "a" {
		t.Fatalf("the first key sent %q, want \"a\"", text)
	}
	burst := strings.Repeat("0123456789", 6)
	p.Key(burst) // while "a" waits for its answer
	waitKeys := time.After(300 * time.Millisecond)
	for wait := true; wait; {
		select {
		case m := <-c.in:
			if r, ok := m.(*webproto.Req); ok && (r.Op == "term.send" || r.Op == "term.interrupt") {
				t.Fatalf("page sent %s while the last term.send waits; want the keys kept for one term.send", show(m))
			}
		case <-waitKeys:
			wait = false
		}
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: first.ID})
	next := expectReq(t, c, "term.send")
	if text := termSendText(t, next); text != burst {
		t.Fatalf("after the answer the page sent %q, want the %d keys typed meanwhile as one text %q", text, len(burst), burst)
	}
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: next.ID})
	// Answered: the next key goes at once again.
	p.Key("z")
	if b, text := sendTo(t, c); b != 3 || text != "z" {
		t.Fatalf("a key after the answers sent %q to %d, want \"z\" to 3", text, b)
	}
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

func termSendText(t *testing.T, r *webproto.Req) string {
	t.Helper()
	var a webproto.TermSendArgs
	if err := json.Unmarshal(r.Args, &a); err != nil || a.Buf != 3 {
		t.Fatalf("term.send args %s", r.Args)
	}
	return a.Text
}
