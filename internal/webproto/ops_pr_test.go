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

// Spec (pull requests): merging, reviewing and checking out a pull
// request change it or the laptop's files: each waits for a laptop card.
// A review other than approve needs a message.
func TestPRActionOps(t *testing.T) {
	checkArgs(t, OpPRMerge, []string{`{"number":14,"method":"admin"}`, `{"number":0,"method":"squash"}`}, []string{`{"number":14,"method":"squash"}`, `{"number":14,"method":"merge"}`, `{"number":14,"method":"rebase"}`})
	checkRow(t, OpPRMerge, RoleController, true, AuditYes)
	checkArgs(t, OpPRReview, []string{`{"number":14,"kind":"comment","body":"  "}`, `{"number":14,"kind":"request_changes"}`, `{"number":14,"kind":"dismiss"}`, `{"number":14,"kind":"approve","body":"a\u001bb"}`},
		[]string{`{"number":14,"kind":"approve"}`, `{"number":14,"kind":"comment","body":"Looks good.\n\nOne nit."}`, `{"number":14,"kind":"request_changes","body":"Add tests"}`})
	checkRow(t, OpPRReview, RoleController, true, AuditYes)
	checkArgs(t, OpPRCheckout, []string{`{"number":-1}`}, []string{`{"number":14}`})
	checkRow(t, OpPRCheckout, RoleController, true, AuditYes)
}
