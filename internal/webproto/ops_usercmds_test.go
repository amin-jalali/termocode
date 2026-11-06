package webproto

import (
	"encoding/json"
	"strings"
	"testing"
)

// Spec (WEBP-7): the user's own commands (commands.json) from the web.
// Listing and saving them are controller ops (a command line is the
// user's own text); a save is audited. A save is the whole list: each
// command has an id, a title and a command line; ids are unique.
func TestUserCmdOps(t *testing.T) {
	for op, w := range map[string]struct {
		audit Audit
		args  string
	}{
		OpUserCmdsList: {AuditNo, `{}`},
		OpUserCmdsSave: {AuditYes, `{"items":[{"id":"build","title":"Build","cmd":"go build ./..."},{"id":"web_2","title":"Web","cmd":"npm run dev","cwd":"web"}]}`},
	} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleController || p.Audit != w.audit || p.LaptopCard || p.Phase != PhaseV2 {
			t.Errorf("%s row %+v", op, p)
		}
		if _, err := DecodeArgs(op, json.RawMessage(w.args)); err != nil {
			t.Errorf("%s %s: %v", op, w.args, err)
		}
		if _, ok := ResultOf(op); !ok {
			t.Errorf("%s has no result type", op)
		}
	}
	if _, err := DecodeArgs(OpUserCmdsSave, json.RawMessage(`{"items":[]}`)); err != nil {
		t.Errorf("an empty list: %v", err)
	}
	many := make([]string, MaxUserCmds+1)
	for i := range many {
		many[i] = `{"id":"c` + strings.Repeat("x", i%30) + string(rune('a'+i%26)) + `","title":"T","cmd":"true"}`
	}
	for _, bad := range []string{
		`{"items":[{"id":"","title":"T","cmd":"true"}]}`,
		`{"items":[{"id":"a b","title":"T","cmd":"true"}]}`,
		`{"items":[{"id":"` + strings.Repeat("x", 41) + `","title":"T","cmd":"true"}]}`,
		`{"items":[{"id":"a","title":"","cmd":"true"}]}`,
		`{"items":[{"id":"a","title":"` + strings.Repeat("x", 81) + `","cmd":"true"}]}`,
		`{"items":[{"id":"a","title":"T\u0007","cmd":"true"}]}`,
		`{"items":[{"id":"a","title":"T","cmd":"  "}]}`,
		`{"items":[{"id":"a","title":"T","cmd":"echo\u0000"}]}`,
		`{"items":[{"id":"a","title":"T","cmd":"` + strings.Repeat("x", MaxUserCmdLen+1) + `"}]}`,
		`{"items":[{"id":"a","title":"T","cmd":"true","cwd":"a\nb"}]}`,
		`{"items":[{"id":"a","title":"T","cmd":"true"},{"id":"a","title":"U","cmd":"false"}]}`,
		`{"items":[` + strings.Join(many, ",") + `]}`,
	} {
		if _, err := DecodeArgs(OpUserCmdsSave, json.RawMessage(bad)); err == nil {
			t.Errorf("save %.90s: accepted", bad)
		}
	}
}
