package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/docs"
)

// Spec (WFX-8): the guide the key card opens exists, has the keys
// section, names every key of the card and every CLI command.
func TestWorkflowsGuideCoversKeysAndCLI(t *testing.T) {
	b, err := docs.FS.ReadFile(workflowsGuidePage)
	if err != nil {
		t.Fatal(err)
	}
	g := string(b)
	if !strings.Contains(g, "\n## Keys\n") {
		t.Fatal("no Keys section (the card opens #keys)")
	}
	for _, r := range wfxKeyRowsList() {
		if len(r) == 2 && !strings.Contains(g, "`"+strings.Fields(r[0])[0]+"`") {
			t.Errorf("guide misses key %q", r[0])
		}
	}
	for _, c := range []string{"workflow list", "workflow show", "workflow validate", "workflow run", "--dry-run", "workflow runs", "workflow logs", "workflow scores"} {
		if !strings.Contains(g, c) {
			t.Errorf("guide misses %q", c)
		}
	}
}
