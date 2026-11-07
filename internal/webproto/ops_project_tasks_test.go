package webproto

import (
	"encoding/json"
	"testing"
)

// Spec (MOB-52): project.tasks is a read for viewers (the laptop's gh and
// files), not audited; it names one kind or all, never a project or path.
func TestProjectTasksOp(t *testing.T) {
	checkArgs(t, OpProjectTasks,
		[]string{`{"only":"wiki"}`, `{"only":"ISSUES"}`},
		[]string{`{}`, `{"only":"checklist"}`, `{"only":"issues","done":true}`, `{"only":"prs"}`, `{"only":"mrs"}`})
	checkRow(t, OpProjectTasks, RoleViewer, false, AuditNo)
	if p, _ := Lookup(OpProjectTasks); p.Rate.N != 1 || p.Live != 1 {
		t.Errorf("row %+v", p)
	}
	for _, b := range []string{`{"path":"/etc"}`, `{"root":"/home"}`, `{"done":"yes"}`} {
		if _, err := DecodeArgs(OpProjectTasks, json.RawMessage(b)); err == nil {
			t.Errorf("%s accepted", b)
		}
	}
}
