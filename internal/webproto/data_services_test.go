package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (WEBP-4, services): the services topic exists (v2); refresh and
// logs are reads; start and restart need a controller (the page asks
// first); stop waits for a laptop card, as a container's stop does.
func TestServicesTopicAndRows(t *testing.T) {
	if tp, err := ParseTopic("services"); err != nil || tp.Name != "services" {
		t.Fatalf("services: %v %+v", err, tp)
	}
	want := map[string]struct {
		role Role
		card bool
	}{OpServicesRefresh: {RoleViewer, false}, OpServicesLogs: {RoleViewer, false}, OpServicesStart: {RoleController, false},
		OpServicesRestart: {RoleController, false}, OpServicesStop: {RoleController, true}}
	for op, w := range want {
		p, ok := Lookup(op)
		if !ok || p.Role != w.role || p.LaptopCard != w.card || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
	}
	for _, a := range []string{`{"name":""}`, `{"name":"a\nb"}`, `{"name":"db","cmd":"rm"}`} {
		if _, err := DecodeArgs(OpServicesStart, json.RawMessage(a)); err == nil {
			t.Errorf("start %s: accepted", a)
		}
	}
	if _, err := DecodeArgs(OpServicesLogs, json.RawMessage(`{"name":"db","lines":5000}`)); err == nil {
		t.Error("logs: too many lines")
	}
	if _, err := DecodeArgs(OpServicesLogs, json.RawMessage(`{"name":"web-1"}`)); err != nil {
		t.Errorf("logs: %v", err)
	}
}
