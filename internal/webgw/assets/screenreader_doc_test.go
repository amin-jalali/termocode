package assets

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// The screen-reader check list (WEB-47) lives in the web guide. The
// owner walks it by hand with a real reader; axe and the browser tests
// cannot hear the page. Each label it names must be the page's own
// English text, so the list never drifts from what a reader says.
func TestScreenReaderChecklist(t *testing.T) {
	b, err := os.ReadFile("../../../docs/guides/web.md")
	if err != nil {
		t.Skip("not in the repository")
	}
	doc := string(b)
	const head = "## Check it with a screen reader"
	i := strings.Index(doc, head)
	if i < 0 {
		t.Fatalf("docs/guides/web.md has no %q section", head)
	}
	sec := doc[i+len(head):]
	if j := strings.Index(sec, "\n## "); j >= 0 {
		sec = sec[:j]
	}
	if n := strings.Count(sec, "\n- [ ] "); n < 12 {
		t.Errorf("the check list has %d items, want at least 12", n)
	}
	for _, r := range []string{"VoiceOver", "TalkBack", "NVDA", "Orca"} {
		if !strings.Contains(sec, r) {
			t.Errorf("the check list does not name %s", r)
		}
	}
	src, err := os.ReadFile("src/strings.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"a11y.skip", "tab.chat", "tab.files", "tab.term", "tab.git", "tab.more",
		"term.read_last", "offline.lost_sr", "card.arming", "card.deny", "card.later",
	} {
		m := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `":\s*"([^"]+)"`).FindSubmatch(src)
		if m == nil {
			t.Errorf("strings.ts has no %s", key)
			continue
		}
		if want := "**" + string(m[1]) + "**"; !strings.Contains(sec, want) {
			t.Errorf("the check list does not name %s (%s)", want, key)
		}
	}
}
