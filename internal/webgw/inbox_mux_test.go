package webgw

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// MOB-53: the door keeps one merged inbox of every attached project for
// the watch socket and push counts.

const (
	muxHome = "aaaaaaaaaaaaaaaa"
	muxShop = "bbbbbbbbbbbbbbbb"
)

func muxSnap(t *testing.T, items ...webproto.InboxItem) json.RawMessage {
	t.Helper()
	s := webproto.InboxSnap{Items: items}
	for _, it := range items {
		s.Counts.Add(it.State)
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func muxCard(id string, since int64) webproto.InboxItem {
	return webproto.InboxItem{ID: "card:" + id, Kind: webproto.InboxCard, Ref: id, Title: "run go test", Since: since, State: webproto.InboxNeedsYou}
}

func TestInboxMuxMergesProjects(t *testing.T) {
	m := NewInboxMux(muxHome)
	if err := m.Set("", muxSnap(t, muxCard("1", 1))); err != nil {
		t.Fatal(err)
	}
	if err := m.Set(muxShop, muxSnap(t, muxCard("1", 2))); err != nil {
		t.Fatal(err)
	}
	m.SetName(muxShop, "shop")
	s := m.Snap()
	if len(s.Items) != 2 || s.Items[0].WS != muxShop {
		t.Fatalf("snap = %+v", s.Items)
	}
	if got := m.Line(); got != "2 things need you · shop, a project" {
		t.Fatalf("line = %q", got)
	}
}

func TestInboxMuxDropEndsAProject(t *testing.T) {
	m := NewInboxMux(muxHome)
	_ = m.Set(muxShop, muxSnap(t, muxCard("1", 2)))
	m.Drop(muxShop)
	if s := m.Snap(); len(s.Items) != 0 {
		t.Fatalf("a closed project stayed: %+v", s.Items)
	}
}

func TestInboxMuxRefuses(t *testing.T) {
	m := NewInboxMux(muxHome)
	if err := m.Set("../shop", muxSnap(t, muxCard("1", 1))); err == nil {
		t.Error("a path took a place as a ws")
	}
	if err := m.Set(muxShop, json.RawMessage(`{"items":[],"evil":1}`)); err == nil {
		t.Error("an unknown key got in")
	}
	if err := m.Set(muxShop, json.RawMessage(strings.Repeat(" ", maxInboxLine+1))); err == nil {
		t.Error("an oversized snap got in")
	}
	for i := 0; i < webproto.InboxMaxHubs; i++ {
		ws := strings.Repeat(string(rune('0'+i)), webproto.InboxWSLen)
		if err := m.Set(ws, muxSnap(t)); err != nil {
			t.Fatalf("project %d: %v", i, err)
		}
	}
	if err := m.Set(muxShop, muxSnap(t)); err == nil {
		t.Errorf("more than home + %d projects", webproto.InboxMaxHubs)
	}
	if err := m.Set("", muxSnap(t)); err != nil {
		t.Errorf("home refused at the cap: %v", err)
	}
}

func TestInboxMuxWatchWakesOnNewNeedsYou(t *testing.T) {
	m := NewInboxMux(muxHome)
	ch, stop := m.Watch()
	defer stop()
	_ = m.Set(muxShop, muxSnap(t, muxCard("1", 1)))
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no wake for a new card")
	}
	news := m.TakeNew()
	if len(news) != 1 || news[0].WS != muxShop {
		t.Fatalf("new = %+v", news)
	}
	if again := m.TakeNew(); len(again) != 0 {
		t.Fatalf("the same card came twice: %+v", again)
	}
	// The same card again (a resent snap) is not new.
	_ = m.Set(muxShop, muxSnap(t, muxCard("1", 1)))
	if again := m.TakeNew(); len(again) != 0 {
		t.Fatalf("a resent card was new: %+v", again)
	}
}

func TestInboxMuxWatchStop(t *testing.T) {
	m := NewInboxMux(muxHome)
	_, stop := m.Watch()
	stop()
	stop()                                          // twice is fine
	_ = m.Set(muxShop, muxSnap(t, muxCard("1", 1))) // never blocks on a gone watcher
	for i := 0; i < maxInboxWatchers+1; i++ {
		_, s := m.Watch()
		defer s()
	}
}
