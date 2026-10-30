package webproto

import "testing"

// Spec (pull requests, CI): pr.list, pr.view and ci.status are reads for
// viewers through the laptop's gh; they are not audited.
func TestPRReadOps(t *testing.T) {
	checkArgs(t, OpPRList, nil, []string{`{}`})
	checkRow(t, OpPRList, RoleViewer, false, AuditNo)
	checkArgs(t, OpPRView, []string{`{"number":0}`, `{"number":-3}`, `{"number":1000000001}`}, []string{`{"number":12}`})
	checkRow(t, OpPRView, RoleViewer, false, AuditNo)
	checkArgs(t, OpCIStatus, nil, []string{`{}`})
	checkRow(t, OpCIStatus, RoleViewer, false, AuditNo)
}
