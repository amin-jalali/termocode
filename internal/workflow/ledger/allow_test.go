package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (WFX-6, plan §3.7): "Allow for this workflow" adds the step to the
// allow-list in <root>/<name>.state.json, bound to the file hash; a
// changed file drops the allow-list and the trust (the card asks again).
func TestAllowListDroppedOnHashChange(t *testing.T) {
	root := t.TempDir()
	h1 := FileHash([]byte(`{"name":"wf"}`))
	h2 := FileHash([]byte(`{"name":"wf","steps":[]}`))
	if ok, err := Allowed(root, "wf", h1, "pr"); ok || err != nil {
		t.Fatalf("allowed before any answer: %v %v", ok, err)
	}
	if err := AllowStep(root, "wf", h1, "pr"); err != nil {
		t.Fatal(err)
	}
	if err := AllowStep(root, "wf", h1, "note"); err != nil {
		t.Fatal(err)
	}
	if err := AllowStep(root, "wf", h1, "pr"); err != nil { // twice: one row
		t.Fatal(err)
	}
	st, _ := LoadState(root, "wf", h1)
	if strings.Join(st.Allow, ",") != "note,pr" || st.AllowHash != h1 {
		t.Fatalf("state %+v", st)
	}
	if ok, _ := Allowed(root, "wf", h1, "pr"); !ok {
		t.Fatal("pr not allowed with the same hash")
	}
	if ok, _ := Allowed(root, "wf", h1, "state"); ok {
		t.Fatal("another step allowed")
	}
	if ok, _ := Allowed(root, "other", h1, "pr"); ok {
		t.Fatal("allowed for another workflow")
	}
	st.TrustedHash, st.Enabled = h1, true
	if err := SaveState(root, "wf", st); err != nil {
		t.Fatal(err)
	}
	// The file changed: nothing is allowed, trust is gone, the drop is saved.
	if ok, _ := Allowed(root, "wf", h2, "pr"); ok {
		t.Fatal("still allowed after the file changed")
	}
	b, _ := os.ReadFile(filepath.Join(root, "wf.state.json"))
	if strings.Contains(string(b), `"pr"`) || strings.Contains(string(b), h1) {
		t.Fatalf("allow-list not dropped on disk: %s", b)
	}
	if !strings.Contains(string(b), `"enabled": true`) {
		t.Fatalf("other state lost: %s", b)
	}
	// Going back to the old bytes does not bring the allow-list back.
	if ok, _ := Allowed(root, "wf", h1, "pr"); ok {
		t.Fatal("allow-list came back")
	}
	if err := AllowStep(root, "wf", "", "pr"); err == nil {
		t.Fatal("allowed without a file hash")
	}
}
