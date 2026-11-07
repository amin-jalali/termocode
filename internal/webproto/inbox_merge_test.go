package webproto

import (
	"fmt"
	"strings"
	"testing"
)

// MOB-53: the door merges the inbox of every attached project into one
// stream for the page, the watch socket and push counts.

const (
	wsHome = "aaaaaaaaaaaaaaaa"
	wsShop = "bbbbbbbbbbbbbbbb"
	wsAPI  = "cccccccccccccccc"
)

func inboxItem(id, state string, since int64) InboxItem {
	kind, ref, _ := strings.Cut(id, ":")
	return InboxItem{ID: id, Kind: kind, Ref: ref, Title: id, Since: since, State: state}
}

func inboxSnapOf(items ...InboxItem) InboxSnap {
	s := InboxSnap{Items: items}
	for _, it := range items {
		s.Counts.Add(it.State)
	}
	return s
}

func TestMergeInboxOrderNewestFirst(t *testing.T) {
	got := MergeInbox(wsHome, []InboxPart{
		{WS: "", Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, 10), inboxItem("run:a", InboxWorking, 30))},
		{WS: wsShop, Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, 20))},
	})
	var order []string
	for _, it := range got.Items {
		order = append(order, it.WS+"/"+it.ID)
	}
	want := []string{wsHome + "/run:a", wsShop + "/card:1", wsHome + "/card:1"}
	if fmt.Sprint(order) != fmt.Sprint(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if got.Counts.NeedsYou != 2 || got.Counts.Working != 1 {
		t.Fatalf("counts = %+v", got.Counts)
	}
}

func TestMergeInboxSameTimeIsStable(t *testing.T) {
	parts := []InboxPart{
		{WS: wsShop, Snap: inboxSnapOf(inboxItem("card:2", InboxNeedsYou, 5), inboxItem("card:1", InboxNeedsYou, 5))},
		{WS: wsHome, Snap: inboxSnapOf(inboxItem("card:9", InboxNeedsYou, 5))},
	}
	a := MergeInbox(wsHome, parts)
	b := MergeInbox(wsHome, []InboxPart{parts[1], parts[0]})
	if fmt.Sprint(a.Items) != fmt.Sprint(b.Items) {
		t.Fatalf("part order changed the stream:\n%v\n%v", a.Items, b.Items)
	}
	if a.Items[0].WS != wsHome || a.Items[1].ID != "card:1" {
		t.Fatalf("tie order = %v", a.Items)
	}
}

// The home hub can come as "" and as its own key: one item, not two.
func TestMergeInboxDedupe(t *testing.T) {
	got := MergeInbox(wsHome, []InboxPart{
		{WS: "", Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, 10))},
		{WS: wsHome, Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, 12))},
	})
	if len(got.Items) != 1 {
		t.Fatalf("items = %v, want one", got.Items)
	}
	if got.Items[0].Since != 12 {
		t.Fatalf("kept the older copy: %+v", got.Items[0])
	}
	if got.Counts.NeedsYou != 1 {
		t.Fatalf("a duplicate was counted: %+v", got.Counts)
	}
}

// A hub never sets ws itself: the door's key wins over the item's.
func TestMergeInboxWSIsTheDoors(t *testing.T) {
	it := inboxItem("card:1", InboxNeedsYou, 1)
	it.WS = wsAPI
	got := MergeInbox(wsHome, []InboxPart{{WS: wsShop, Snap: inboxSnapOf(it)}})
	if got.Items[0].WS != wsShop {
		t.Fatalf("ws = %q, want the part's %q", got.Items[0].WS, wsShop)
	}
}

func TestMergeInboxSkipsBadWS(t *testing.T) {
	got := MergeInbox(wsHome, []InboxPart{
		{WS: "../shop", Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, 1))},
		{WS: "BBBBBBBBBBBBBBBB", Snap: inboxSnapOf(inboxItem("card:2", InboxNeedsYou, 1))},
	})
	if len(got.Items) != 0 || got.Counts.NeedsYou != 0 {
		t.Fatalf("a bad ws got in: %+v", got)
	}
}

func TestMergeInboxCap(t *testing.T) {
	var a, b []InboxItem
	for i := 0; i < MaxInbox; i++ {
		a = append(a, inboxItem(fmt.Sprintf("run:%d", i), InboxWorking, int64(2*i)))
		b = append(b, inboxItem(fmt.Sprintf("run:%d", i), InboxDone, int64(2*i+1)))
	}
	sa, sb := inboxSnapOf(a...), inboxSnapOf(b...)
	sa.More, sa.Counts.Working = 3, MaxInbox+3 // the hub cut 3 already
	got := MergeInbox(wsHome, []InboxPart{{WS: wsHome, Snap: sa}, {WS: wsShop, Snap: sb}})
	if len(got.Items) != MaxInbox {
		t.Fatalf("len = %d, want %d", len(got.Items), MaxInbox)
	}
	if got.More != MaxInbox+3 {
		t.Fatalf("more = %d, want %d", got.More, MaxInbox+3)
	}
	if got.Counts.Working != MaxInbox+3 || got.Counts.Done != MaxInbox {
		t.Fatalf("counts = %+v", got.Counts)
	}
	if got.Items[0].Since != int64(2*MaxInbox-1) {
		t.Fatalf("not the newest first: %+v", got.Items[0])
	}
}

func TestMergeInboxDropsBadItems(t *testing.T) {
	bad := inboxItem("card:1", "maybe", 1)
	empty := InboxItem{State: InboxNeedsYou}
	got := MergeInbox(wsHome, []InboxPart{{WS: wsHome, Snap: InboxSnap{Items: []InboxItem{bad, empty}}}})
	if len(got.Items) != 0 {
		t.Fatalf("kept a bad item: %v", got.Items)
	}
}

func TestMergeInboxEmpty(t *testing.T) {
	got := MergeInbox(wsHome, nil)
	if got.Items == nil {
		t.Fatal("items must be [] on the wire, not null")
	}
}

func TestInboxNeedsYouLine(t *testing.T) {
	names := map[string]string{wsHome: "termocode", wsShop: "shop", wsAPI: "api"}
	for _, tc := range []struct {
		items []InboxPart
		want  string
	}{
		{nil, ""},
		{[]InboxPart{{WS: wsShop, Snap: inboxSnapOf(inboxItem("run:1", InboxWorking, 1))}}, ""},
		{[]InboxPart{{WS: wsShop, Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, 1))}}, "1 thing needs you · shop"},
		{[]InboxPart{
			{WS: wsShop, Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, 1), inboxItem("card:2", InboxNeedsYou, 3))},
			{WS: "", Snap: inboxSnapOf(inboxItem("card:3", InboxNeedsYou, 2))},
		}, "3 things need you · shop, termocode"},
		{[]InboxPart{{WS: "dddddddddddddddd", Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, 1))}}, "1 thing needs you · a project"},
	} {
		if got := InboxNeedsYouLine(MergeInbox(wsHome, tc.items), names); got != tc.want {
			t.Errorf("line = %q, want %q", got, tc.want)
		}
	}
}

func TestInboxNeedsYouLineManyProjects(t *testing.T) {
	var parts []InboxPart
	names := map[string]string{}
	for i := 0; i < 5; i++ {
		ws := strings.Repeat(fmt.Sprint(i), InboxWSLen)
		names[ws] = fmt.Sprintf("p%d", i)
		parts = append(parts, InboxPart{WS: ws, Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, int64(10-i)))})
	}
	got := InboxNeedsYouLine(MergeInbox(wsHome, parts), names)
	if got != "5 things need you · p0, p1, p2 +2" {
		t.Fatalf("line = %q", got)
	}
}

// A project name never brings a path or a long line into a notification.
func TestInboxNeedsYouLineCleanName(t *testing.T) {
	names := map[string]string{wsShop: "/home/fu/work/shop\n" + strings.Repeat("x", 100)}
	got := InboxNeedsYouLine(MergeInbox(wsHome, []InboxPart{{WS: wsShop, Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, 1))}}), names)
	if strings.Contains(got, "/") || strings.Contains(got, "\n") || len(got) > 80 {
		t.Fatalf("line = %q", got)
	}
}

func TestInboxNewNeedsYou(t *testing.T) {
	prev := MergeInbox(wsHome, []InboxPart{{WS: wsShop, Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, 1))}})
	next := MergeInbox(wsHome, []InboxPart{
		{WS: wsShop, Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, 1), inboxItem("run:2", InboxWorking, 2))},
		{WS: wsHome, Snap: inboxSnapOf(inboxItem("card:1", InboxNeedsYou, 3))},
	})
	got := InboxNewNeedsYou(prev, next)
	if len(got) != 1 || got[0].WS != wsHome || got[0].ID != "card:1" {
		t.Fatalf("new = %+v, want home's card:1 only", got)
	}
}

func TestInboxItemLink(t *testing.T) {
	card := inboxItem("card:c-7", InboxNeedsYou, 1)
	card.WS = wsShop
	if got := InboxItemLink(card); got != "#/home?card=c-7&ws="+wsShop {
		t.Errorf("card link = %q", got)
	}
	run := inboxItem("run:a b", InboxWorking, 1)
	if got := InboxItemLink(run); got != "#/home?item=run%3Aa+b" {
		t.Errorf("run link = %q", got)
	}
}

func FuzzMergeInbox(f *testing.F) {
	f.Add("card:1", wsShop, int64(1), InboxNeedsYou)
	f.Add("", "../x", int64(-1), "x")
	f.Fuzz(func(t *testing.T, id, ws string, since int64, state string) {
		it := inboxItem(id, state, since)
		got := MergeInbox(wsHome, []InboxPart{{WS: ws, Snap: inboxSnapOf(it, it)}, {WS: "", Snap: inboxSnapOf(it)}})
		if len(got.Items) > MaxInbox {
			t.Fatal("over the cap")
		}
		seen := map[string]bool{}
		for _, x := range got.Items {
			if !InboxWS(x.WS) {
				t.Fatalf("bad ws %q in the stream", x.WS)
			}
			k := x.WS + "\x00" + x.ID
			if seen[k] {
				t.Fatalf("duplicate %q", k)
			}
			seen[k] = true
		}
		_ = InboxNeedsYouLine(got, map[string]string{ws: id})
	})
}
