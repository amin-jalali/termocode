//go:build unix

package webgw

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/webdevices"
)

// approvedPair starts a pairing and lets the owner say yes on the laptop.
func (r *pairRig) approvedPair(name string) string {
	r.t.Helper()
	id, _ := r.start(name)
	r.approve(webdevices.RoleControl)
	r.waitState(id, "approved")
	return id
}

// /pair/finish refuses a bad body and a pairing the owner has not yet
// approved; neither stores a device.
func TestPairFinishRefusesEarly(t *testing.T) {
	r := newPairRig(t)
	if code, m := r.post("/pair/finish", `{"pair_id":1}`); code != http.StatusBadRequest || m["error"] != "bad_request" {
		t.Fatalf("bad body: %d %v", code, m)
	}
	id, _ := r.start("Phone")
	if code, m := r.post("/pair/finish", `{"pair_id":"`+id+`"}`); code != http.StatusForbidden || m["error"] != "not_approved" {
		t.Fatalf("before approval: %d %v", code, m)
	}
	if ds, _ := r.store.List(); len(ds) != 0 {
		t.Fatalf("stored %v", ds)
	}
	r.approve(webdevices.RoleControl) // end the laptop ask
}

// A full device list says "full"; the pairing is used up all the same.
func TestPairFinishStoreFull(t *testing.T) {
	r := newPairRig(t)
	for i := range webdevices.MaxDevices {
		if err := r.store.Add(webdevices.Device{ID: fmt.Sprintf("dev-%d", i), Name: "x", Role: webdevices.RoleViewer, Created: time.Unix(1700000000, 0)}); err != nil {
			t.Fatal(err)
		}
	}
	id := r.approvedPair("Phone")
	if code, m := r.post("/pair/finish", `{"pair_id":"`+id+`"}`); code != http.StatusServiceUnavailable || m["error"] != "full" || m["token"] != nil {
		t.Fatalf("full: %d %v", code, m)
	}
	if code, _ := r.post("/pair/finish", `{"pair_id":"`+id+`"}`); code != http.StatusGone {
		t.Fatalf("second finish: %d, want gone", code)
	}
}

// With every session slot taken, no token is given ("too_many_sessions").
func TestPairFinishTooManySessions(t *testing.T) {
	r := newPairRig(t)
	for range MaxSessions {
		if _, _, err := r.tokens.Issue("dev-x", RoleViewer, r.clock.Now()); err != nil {
			t.Fatal(err)
		}
	}
	id := r.approvedPair("Phone")
	if code, m := r.post("/pair/finish", `{"pair_id":"`+id+`"}`); code != http.StatusServiceUnavailable || m["error"] != "too_many_sessions" || m["token"] != nil {
		t.Fatalf("too many sessions: %d %v", code, m)
	}
}
