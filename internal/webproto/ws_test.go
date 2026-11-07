package webproto

import (
	"errors"
	"testing"
)

// MOB-50: ws on frames is a workspace key or nothing.
func TestDecodeClientWS(t *testing.T) {
	ok := []string{
		`{"t":"sub","topics":["status"],"ws":"0123456789abcdef"}`,
		`{"t":"unsub","topics":["status"],"ws":"0123456789abcdef"}`,
		`{"t":"req","id":1,"op":"tree.list","ws":"0123456789abcdef"}`,
		`{"t":"sub","topics":["status"]}`,
	}
	for _, s := range ok {
		m, err := DecodeClient([]byte(s))
		if err != nil {
			t.Fatalf("%s: %v", s, err)
		}
		if r, isReq := m.(*Req); isReq && r.WS != "0123456789abcdef" {
			t.Fatalf("req ws %q", r.WS)
		}
	}
	bad := []string{
		`{"t":"sub","topics":["status"],"ws":"/home/me/shop"}`,
		`{"t":"sub","topics":["status"],"ws":"0123456789ABCDEF"}`,
		`{"t":"req","id":1,"op":"tree.list","ws":"abc"}`,
		`{"t":"unsub","topics":["status"],"ws":"0123456789abcdef00"}`,
	}
	for _, s := range bad {
		if _, err := DecodeClient([]byte(s)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err %v, want ErrInvalid", s, err)
		}
	}
	// ping and hello have no ws.
	if _, err := DecodeClient([]byte(`{"t":"ping","n":"1","ws":"0123456789abcdef"}`)); err == nil {
		t.Error("ping with ws accepted")
	}
}

func TestEventWSGoneRegistered(t *testing.T) {
	found := false
	for _, k := range EventKinds() {
		if k == EventWSGone {
			found = true
		}
	}
	if !found {
		t.Fatal("ws.gone is not a registered event")
	}
}
