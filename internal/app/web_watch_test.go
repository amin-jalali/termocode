package app

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (MOB-23): what the hub hands the gateway for the watch socket
// keeps every count but only the newest needs-you items, at most
// webInboxWatchMax, so the control line stays far below its 64 KiB cap
// even for a full inbox of long titles.
func TestWebInboxForWatch(t *testing.T) {
	s := webproto.InboxSnap{Items: []webproto.InboxItem{}}
	long := ""
	for len(long) < webproto.MaxInboxTitle {
		long += "x"
	}
	for i := range webproto.MaxInbox {
		st := webproto.InboxNeedsYou
		if i%3 == 0 {
			st = webproto.InboxWorking
		}
		s.Items = append(s.Items, webproto.InboxItem{ID: "card:" + strconv.Itoa(i), Kind: webproto.InboxCard, Ref: strconv.Itoa(i),
			Title: long, Chat: long, State: st, Since: int64(1000 - i)})
		s.Counts.Add(st)
	}
	w := webInboxForWatch(s)
	if w.Counts != s.Counts {
		t.Errorf("counts changed: %+v != %+v", w.Counts, s.Counts)
	}
	if len(w.Items) != webInboxWatchMax || w.More != s.Counts.NeedsYou-webInboxWatchMax {
		t.Fatalf("items %d more %d", len(w.Items), w.More)
	}
	for i, it := range w.Items {
		if it.State != webproto.InboxNeedsYou {
			t.Errorf("item %d is %s", i, it.State)
		}
		if i > 0 && it.Since > w.Items[i-1].Since {
			t.Error("not newest first")
		}
	}
	b, _ := json.Marshal(w)
	if len(b) > 48<<10 {
		t.Errorf("watch inbox is %d bytes", len(b))
	}
}

// Spec: web.watching is answered by the hub (a control op), never a
// browser op.
func TestWebWatchingIsWired(t *testing.T) {
	if webOps[webproto.OpWebWatching] == nil {
		t.Fatal("web.watching has no hub handler")
	}
	if _, ok := webOps[webproto.OpAppWatchToken]; ok {
		t.Error("app.watch_token reaches the hub; the gateway answers it")
	}
}
