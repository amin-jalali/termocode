package webproto

import (
	"testing"
	"time"
)

// Spec (MOB-51): project.open is a controller op that waits for a laptop
// card, slow and audited; project.close is a controller op, slow and
// audited, with no card (it works only for a project the phone opened).
// A viewer may call neither.
func TestProjectOps(t *testing.T) {
	open, ok := Lookup(OpProjectOpen)
	if !ok || open.Role != RoleController || !open.LaptopCard || open.Audit != AuditYes || open.Rate.Per < 5*time.Second {
		t.Fatalf("project.open: %+v", open)
	}
	cl, ok := Lookup(OpProjectClose)
	if !ok || cl.Role != RoleController || cl.LaptopCard || cl.Audit != AuditYes || cl.Rate.Per < 5*time.Second {
		t.Fatalf("project.close: %+v", cl)
	}
	for _, op := range []string{OpProjectOpen, OpProjectClose} {
		if Allowed(RoleViewer, op) {
			t.Errorf("%s: a viewer may call it", op)
		}
	}
}

// Spec (MOB-51): a project is named by its ws only (16 lower-case hex
// digits): never a path, a name, an option or more fields.
func TestProjectArgs(t *testing.T) {
	for _, op := range []string{OpProjectOpen, OpProjectClose} {
		if _, err := DecodeArgs(op, []byte(`{"ws":"0123456789abcdef"}`)); err != nil {
			t.Errorf("%s good: %v", op, err)
		}
		for _, b := range []string{
			`{}`, `{"ws":""}`, `{"ws":"/home/me/shop"}`, `{"ws":"../0123456789ab"}`,
			`{"ws":"0123456789ABCDEF"}`, `{"ws":"0123456789abcde"}`, `{"ws":"0123456789abcdef0"}`,
			`{"ws":"-123456789abcdef"}`, `{"ws":"0123456789abcdef","path":"/tmp"}`,
		} {
			if _, err := DecodeArgs(op, []byte(b)); err == nil {
				t.Errorf("%s %s accepted", op, b)
			}
		}
	}
}
