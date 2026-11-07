package assets

import (
	"os"
	"strings"
	"testing"
)

// Spec (MOB-33): the web guide explains phone notifications (Web Push,
// UnifiedPush, web_phone) and their threat notes: what a push may say,
// who sees it, where the keys live, and how to stop it. The safety list
// names the push rule too, so the promise stays in one place.
func TestPushDocNotes(t *testing.T) {
	b, err := os.ReadFile("../../../docs/guides/web.md")
	if err != nil {
		t.Skip("not in the repository")
	}
	doc := string(b)
	sec := docSection(t, doc, "### Notifications on the phone")
	for _, want := range []string{
		"Web Push", "UnifiedPush", "VAPID",
		"`web_phone`", "`web_push_detail`",
		"`none`", "`count`", "`title`",
		"secret store", "no content",
		"#### What a push can tell",
	} {
		if !strings.Contains(sec, want) {
			t.Errorf("the notifications section does not say %q", want)
		}
	}
	for _, bad := range []string{"cloud of ours is needed", "our server"} {
		if strings.Contains(sec, bad) {
			t.Errorf("the notifications section says %q", bad)
		}
	}
	safe := docSection(t, doc, "## How it keeps you safe")
	if !strings.Contains(safe, "**A push says almost nothing.**") {
		t.Error("the safety list has no push rule")
	}
}

// docSection is the text under head, up to the next heading of the same
// or a higher level.
func docSection(t *testing.T, doc, head string) string {
	t.Helper()
	i := strings.Index(doc, "\n"+head+"\n")
	if i < 0 {
		t.Fatalf("docs/guides/web.md has no %q section", head)
	}
	sec := doc[i+1+len(head):]
	level := head[:strings.IndexByte(head, ' ')]
	for _, stop := range []string{"\n# ", "\n## ", "\n### "} {
		if len(stop)-2 > len(level) {
			continue
		}
		if j := strings.Index(sec, stop); j >= 0 {
			sec = sec[:j]
		}
	}
	return sec
}
