package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (MOB-23): app.watch_token is a gateway op any paired session may
// call (slow, audited; the hub never sees it); web.watching is a control
// op only.
func TestWatchOpRows(t *testing.T) {
	p, ok := Lookup(OpAppWatchToken)
	if !ok || !p.Gateway || p.Role != RoleViewer || p.Audit != AuditYes || p.LaptopCard || p.Phase != PhaseV2 {
		t.Fatalf("app.watch_token row: %+v", p)
	}
	if p.Rate.N != 1 || p.Rate.Per < 10e9 {
		t.Errorf("app.watch_token rate %+v, want at most 1 per 10 s", p.Rate)
	}
	w, ok := Lookup(OpWebWatching)
	if !ok || w.Role != RoleCtl || w.Gateway {
		t.Fatalf("web.watching row: %+v", w)
	}
	if Allowed(RoleController, OpWebWatching) || Allowed(RoleViewer, OpWebWatching) {
		t.Error("a browser may call web.watching")
	}
	if !Allowed(RoleViewer, OpAppWatchToken) {
		t.Error("a paired viewer may not ask for a watch token")
	}
}

// Spec: web.watching takes only {on}; an unknown key is refused.
func TestWatchingArgsStrict(t *testing.T) {
	a, err := DecodeArgs(OpWebWatching, json.RawMessage(`{"on":true}`))
	if err != nil || !a.(*WatchingArgs).On {
		t.Fatalf("decode: %v %#v", err, a)
	}
	if _, err := DecodeArgs(OpWebWatching, json.RawMessage(`{"on":true,"ws":"x"}`)); err == nil {
		t.Error("an unknown key was taken")
	}
	if _, err := DecodeArgs(OpAppWatchToken, json.RawMessage(`{"device":"dev-1"}`)); err == nil {
		t.Error("app.watch_token took a device from the phone")
	}
}

func watchSnap(items ...InboxItem) InboxSnap {
	s := InboxSnap{Items: items}
	for _, it := range items {
		s.Counts.Add(it.State)
	}
	return s
}

// Spec: the first frame has the line and counts and no new items; a later
// frame lists only items that newly need the user, with their tap link
// (no ws for the home project), at most MaxWatchNew.
func TestWatchFrameOf(t *testing.T) {
	const home = "00112233445566aa"
	a := InboxItem{ID: "card:1", WS: home, Kind: InboxCard, Ref: "1", Title: "Allow a write?", State: InboxNeedsYou, Since: 1}
	b := InboxItem{ID: "run:k", WS: home, Kind: InboxRun, Ref: "k", Title: "Run is going", State: InboxWorking, Since: 2}
	first := WatchFrameOf(nil, watchSnap(a, b), "1 thing needs you", home)
	if first.T != "watch" || first.Line != "1 thing needs you" || first.Counts.NeedsYou != 1 || first.Counts.Working != 1 || len(first.New) != 0 {
		t.Fatalf("first frame: %+v", first)
	}
	prev := watchSnap(a, b)
	c := InboxItem{ID: "card:2", WS: home, Kind: InboxCard, Ref: "2", Title: "Merge?", State: InboxNeedsYou, Since: 3}
	o := InboxItem{ID: "card:3", WS: "ffeeddccbbaa9988", Kind: InboxCard, Ref: "3", Title: "Other project", State: InboxNeedsYou, Since: 4}
	f := WatchFrameOf(&prev, watchSnap(o, c, a, b), "3 things need you", home)
	if len(f.New) != 2 {
		t.Fatalf("new items: %+v", f.New)
	}
	links := map[string]string{}
	for _, it := range f.New {
		links[it.ID] = it.Link
	}
	if links["card:2"] != "#/home?card=2" {
		t.Errorf("home link %q", links["card:2"])
	}
	if !strings.Contains(links["card:3"], "&ws=ffeeddccbbaa9988") {
		t.Errorf("other project's link %q has no ws", links["card:3"])
	}
	var many []InboxItem
	for i := range MaxWatchNew + 5 {
		many = append(many, InboxItem{ID: "card:n" + string(rune('a'+i)), Kind: InboxCard, Ref: "n", Title: "x", State: InboxNeedsYou, Since: int64(i)})
	}
	empty := InboxSnap{}
	if g := WatchFrameOf(&empty, watchSnap(many...), "", home); len(g.New) != MaxWatchNew {
		t.Errorf("new items not capped: %d", len(g.New))
	}
}

// Spec: a frame never carries a ref, a lane, a chat title or since: only
// what a notification shows.
func TestWatchFrameFields(t *testing.T) {
	prev := InboxSnap{}
	it := InboxItem{ID: "lane:abc", Kind: InboxLane, Ref: "abc", Lane: "abc", Chat: "secret chat title", Title: "Lane waits", State: InboxNeedsYou, Since: 9}
	b, err := json.Marshal(WatchFrameOf(&prev, watchSnap(it), "1 thing needs you", ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"secret chat title", `"ref"`, `"since"`, `"lane":`} {
		if strings.Contains(string(b), bad) {
			t.Errorf("frame carries %s: %s", bad, b)
		}
	}
}
