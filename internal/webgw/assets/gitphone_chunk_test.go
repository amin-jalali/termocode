package assets

import (
	"strings"
	"testing"
)

// budgetGitPhone is the phone's Git pages (gitphone.js and gitphone.css,
// page designs 05-08), gzipped: the changes, the commit sheet, branches
// and history, a file's diff.
const budgetGitPhone = 16 << 10

// The phone's Git pages are gitphone.js and gitphone.css alone: home.js
// loads them (lib/gitphone.ts) and gives them what they need, so they
// import nothing, and neither the page's first files nor the workbench
// carry them.
func TestGitPhoneChunk(t *testing.T) {
	js, css := readBuilt(t, "gitphone.js"), readBuilt(t, "gitphone.css")
	for _, bad := range []string{"import ", "import(", " from\""} {
		if strings.Contains(js, bad) {
			t.Errorf("gitphone.js imports something (%q): home.js gives it what it needs", bad)
		}
	}
	for _, want := range []string{"gp-chip", "gp-dock", "gp-menu"} {
		if !strings.Contains(js, want) || !strings.Contains(css, want) {
			t.Errorf("gitphone.js or gitphone.css has no %s", want)
		}
	}
	if home := readBuilt(t, "home.js"); !strings.Contains(home, `import("/assets/gitphone.js")`) || !strings.Contains(home, `"/assets/gitphone.css"`) {
		t.Error("home.js does not load gitphone.js and gitphone.css")
	}
	for _, f := range []string{"index.html", "app.js", "workbench.js"} {
		if strings.Contains(readBuilt(t, f), "gitphone") {
			t.Errorf("%s names gitphone: only the phone's home.js loads it", f)
		}
	}
	if strings.Contains(readPage(t), "gp-chip") {
		t.Error("the page's first files hold the phone's Git pages")
	}
	n := gzSize(t, "gitphone.js") + gzSize(t, "gitphone.css")
	t.Logf("gzipped: gitphone.js + gitphone.css %d B", n)
	if n > budgetGitPhone {
		t.Errorf("gitphone.js + gitphone.css is %d B gzipped, budget %d B", n, budgetGitPhone)
	}
}
