package webproto

import (
	"encoding/json"
	"reflect"
	"regexp"
	"testing"
)

// Spec (WEBP-ITERMOC): the Itermoc view of the web reads the laptop's AI
// setup (itermoc.state: a viewer may look, as at the ai topic) and asks
// the laptop to open one of its own setup forms (itermoc.ask: a
// controller op, only after a laptop card). An ask names a fixed action,
// never a path, a key or a free command.
func TestItermocOps(t *testing.T) {
	if p, ok := Lookup(OpItermoc); !ok || p.Role != RoleViewer || p.LaptopCard || p.Audit != AuditNo {
		t.Errorf("%s: %+v", OpItermoc, p)
	}
	if p, ok := Lookup(OpItermocAsk); !ok || p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes {
		t.Errorf("%s: %+v", OpItermocAsk, p)
	}
	for _, a := range ItermocActions {
		if _, err := DecodeArgs(OpItermocAsk, []byte(`{"action":"`+a+`"}`)); err != nil {
			t.Errorf("%s: %v", a, err)
		}
	}
	if len(ItermocActions) != 6 {
		t.Errorf("actions %v", ItermocActions)
	}
	for _, b := range []string{`{"action":""}`, `{"action":"save-all"}`, `{"action":"ai-profile-trust"}`, `{"action":"ai-add","key":"x"}`, `{}`} {
		if _, err := DecodeArgs(OpItermocAsk, []byte(b)); err == nil {
			t.Errorf("%s accepted", b)
		}
	}
}

// A key never travels: the state's types have no field a key could ride
// in, and a slot or a provider says only whether its key is there.
func TestItermocStateHasNoKeyText(t *testing.T) {
	bad := regexp.MustCompile(`(?i)apikey|token|secret|password|^key$|env`)
	var walk func(reflect.Type)
	seen := map[reflect.Type]bool{}
	walk = func(rt reflect.Type) {
		for rt.Kind() == reflect.Slice || rt.Kind() == reflect.Pointer {
			rt = rt.Elem()
		}
		if rt.Kind() != reflect.Struct || seen[rt] {
			return
		}
		seen[rt] = true
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			name := f.Tag.Get("json")
			if bad.MatchString(f.Name) || bad.MatchString(name) {
				t.Errorf("%s.%s (%s) could carry a key", rt.Name(), f.Name, name)
			}
			walk(f.Type)
		}
	}
	walk(reflect.TypeOf(Itermoc{}))
	b, err := json.Marshal(Itermoc{Slots: []ItermocSlot{{Name: "main", Provider: "openai", Model: "m", HasKey: "ok"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`"hasKey":"ok"`).Match(b) {
		t.Errorf("slot JSON %s", b)
	}
}
