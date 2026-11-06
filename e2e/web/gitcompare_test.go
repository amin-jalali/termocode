//go:build browser

package web

import (
	"strings"
	"testing"
)

// Git: Compare with Revision… picks a commit (branches and tags by
// name) and opens the file at that commit beside the working file.
func TestGitCompareWithRevision(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "one", "TWO")
	runCmd(t, p, c, "Git: Compare with Revision…", true)
	reply(t, c, waitReq(t, c, "git.graph"), graphRows())
	p.WaitJS(`document.querySelectorAll(".quick-row").length === 4`)
	if got := Eval[string](p, `[...document.querySelectorAll(".quick-row")].find((r) => r.querySelector(".quick-label")?.textContent === "Feature work")?.querySelector(".quick-detail")?.textContent ?? ""`); !strings.Contains(got, "cccc333") || !strings.Contains(got, "feature") {
		t.Errorf("a commit's detail is %q", got)
	}
	p.Key("Main work")
	p.WaitJS(`document.querySelector(".quick-row.is-on .quick-label")?.textContent === "Main work"`)
	p.Key("\r")
	r := waitReq(t, c, "git.text")
	if got := canonJSON(t, r.Args); got != `{"path":"a.txt","rev":"bbbb222"}` {
		t.Errorf("git.text args %s", got)
	}
	reply(t, c, r, map[string]any{"path": "a.txt", "rev": "bbbb222", "text": "one\ntwo\n"})
	p.WaitJS(`decodeURIComponent(location.hash).includes("@rev:bbbb222:a.txt")`)
	p.WaitJS(`document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent === "a.txt (bbbb222 ↔ Working Tree)"`)
	p.WaitJS(diffDoc + `?.querySelector(".monaco-diff-editor .editor.original .view-line")`)
}
