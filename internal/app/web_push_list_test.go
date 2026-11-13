package app

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// pushOp runs a push op as device dev and returns the answer message.
func pushOp(t *testing.T, m *Model, dev, op, args string) webproto.Msg {
	t.Helper()
	c := webTestSub(t, m)
	c.device = dev
	a, err := webproto.DecodeArgs(op, json.RawMessage(args))
	if err != nil {
		t.Fatal(err)
	}
	var got webproto.Msg
	call := &webCall{ctx: c.ctx, conn: c, id: 1, op: op, args: a, sink: func(msg webproto.Msg) { got = msg }}
	c.inflight.Add(1)
	cmd, found := m.webRunCall(m.web, call)
	if !found {
		t.Fatalf("no handler for %s", op)
	}
	if cmd != nil {
		cmd()
	}
	if got == nil {
		t.Fatalf("%s gave no answer", op)
	}
	return got
}

func pushList(t *testing.T, msg webproto.Msg) webproto.PushList {
	t.Helper()
	res, ok := msg.(*webproto.Res)
	if !ok {
		t.Fatalf("not a result: %#v", msg)
	}
	var l webproto.PushList
	if err := json.Unmarshal(res.Data, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

// Spec (CMP-47): the push subscriptions are listed with the device's
// name, the day it was added and which one is this phone; no endpoint,
// key or secret is in the list; a device with no subscription is not.
func TestWebPushListOp(t *testing.T) {
	withConfigDir(t)
	r := newWebPushRig(t)
	r.devs = []webdevices.Device{{ID: "dev-0000000a", Name: "Pixel"}, {ID: "dev-0000000b", Name: "iPad"}, {ID: "dev-0000000c", Name: "Quiet"}}
	added := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC).UnixMilli()
	r.subscribeAt(t, "dev-0000000a", added)
	r.subscribeAt(t, "dev-0000000b", added+1000)
	m := aiTestModel(t)
	webTestHub(t, &m)
	msg := pushOp(t, &m, "dev-0000000a", webproto.OpPushList, `{}`)
	l := pushList(t, msg)
	if len(l.Items) != 2 {
		t.Fatalf("items %+v", l.Items)
	}
	a, b := l.Items[0], l.Items[1]
	if a.Device != "dev-0000000a" || a.Name != "Pixel" || a.Added != added || !a.This || b.Device != "dev-0000000b" || b.Name != "iPad" || b.This {
		t.Errorf("items %+v", l.Items)
	}
	raw, _ := json.Marshal(msg)
	for _, secret := range []string{r.endpoint, "p256dh", "auth"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the list holds %q: %s", secret, raw)
		}
	}
}

// Spec (CMP-47): a controller removes a device's push subscription (its
// own or another's) and gets the new list; that phone gets no more
// pushes; a device with none is "not found"; a viewer's role is
// refused by the op table (controller only).
func TestWebPushRemoveOp(t *testing.T) {
	withConfigDir(t)
	r := newWebPushRig(t)
	r.devs = []webdevices.Device{{ID: "dev-0000000a", Name: "Pixel"}, {ID: "dev-0000000b", Name: "iPad"}}
	r.subscribeAt(t, "dev-0000000a", 1)
	r.subscribeAt(t, "dev-0000000b", 2)
	m := aiTestModel(t)
	webTestHub(t, &m)
	l := pushList(t, pushOp(t, &m, "dev-0000000a", webproto.OpPushRemove, `{"device":"dev-0000000b"}`))
	if len(l.Items) != 1 || l.Items[0].Device != "dev-0000000a" || webPushHasSub(r.store, "dev-0000000b") {
		t.Fatalf("after remove: %+v", l.Items)
	}
	if e, ok := pushOp(t, &m, "dev-0000000a", webproto.OpPushRemove, `{"device":"dev-0000000b"}`).(*webproto.ErrMsg); !ok || e.Code != webproto.CodeNotFound {
		t.Errorf("second remove: %#v", e)
	}
	if _, err := webproto.DecodeArgs(webproto.OpPushRemove, json.RawMessage(`{"device":"../x"}`)); err == nil {
		t.Error("a bad device id was accepted")
	}
	for _, op := range []string{webproto.OpPushList, webproto.OpPushRemove} {
		if p, ok := webproto.Lookup(op); !ok || p.Role != webproto.RoleController {
			t.Errorf("%s: %+v", op, p)
		}
	}
}
