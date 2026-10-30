//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The desktop's merge editor: a conflicted file opens from source
// control with the incoming and current sides on top and the result
// below; Accept puts the laptop's resolution in place; Complete Merge
// saves the result with the version read.

const mergeView = `document.querySelector('[data-view="merge"]')`

func mergeConflict() webproto.GitConflict {
	raw := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> side\n"
	return webproto.GitConflict{Path: "src/a.go", Text: "package a\n" + raw + "end\n", Version: strings.Repeat("ab", 32),
		Ours:   webproto.GitConflictSide{Present: true, Text: "package a\nours\nend\n"},
		Theirs: webproto.GitConflictSide{Present: true, Text: "package a\ntheirs\nend\n"},
		Base:   webproto.GitConflictSide{Present: true, Text: "package a\nbase\nend\n"},
		Blocks: []webproto.GitConflictPart{{Line: 2, Raw: raw, OursLabel: "HEAD", TheirsLabel: "side", Ours: "ours\n", Theirs: "theirs\n", Both: "ours\ntheirs\n"}}}
}

// paneText is the text the editor of one merge pane shows.
func paneText(p *Page, pane string) string {
	return Eval[string](p, `[...(`+mergeView+`.querySelector(".gm-`+pane+` .desk-editor")?.contentDocument?.querySelectorAll(".view-lines .view-line") ?? [])].sort((a, b) => a.offsetTop - b.offsetTop).map((l) => l.textContent.replace(/ /g, " ")).join("\n")`)
}

func openMerge(t *testing.T) (*Page, *HubConn) {
	t.Helper()
	snap := gitSnap()
	snap.Op = "merge"
	snap.Files = append(snap.Files, webproto.GitFile{Path: "src/c.go", X: "U", Y: "U", Conflict: true})
	p, c := openGit(t, Desktop, snap)
	p.TapJS(`document.querySelector('` + rowSel("src/c.go") + ` .git-open')`)
	r := waitReq(t, c, "git.conflict")
	if canonJSON(t, r.Args) != `{"path":"src/c.go"}` {
		t.Errorf("conflict args %s", r.Args)
	}
	mc := mergeConflict()
	mc.Path = "src/c.go"
	reply(t, c, r, mc)
	p.WaitJS(mergeView + `?.querySelectorAll(".desk-editor").length === 3`)
	p.WaitJS(`(() => { const d = ` + mergeView + `.querySelector(".gm-result .desk-editor").contentDocument; return d && d.querySelector(".view-line") && d.querySelector(".tc-mg-current") && d.querySelector(".tc-mg-incoming"); })()`)
	return p, c
}

func TestMergeEditorAcceptAndComplete(t *testing.T) {
	p, c := openMerge(t)
	if got := Eval[string](p, `document.querySelector(".ed-tab.is-on .ed-tab-name")?.textContent ?? ""`); got != "c.go (Merge)" {
		t.Errorf("the tab is %q", got)
	}
	p.WaitJS(`(() => { const d = ` + mergeView + `.querySelector(".gm-incoming .desk-editor").contentDocument; return d && d.querySelector(".tc-mg-incoming"); })()`)
	if got := p.Text(".gm-left"); got != "Conflict 1 of 1 remaining" {
		t.Errorf("left %q", got)
	}
	if !Eval[bool](p, mergeView+`.querySelector('[data-act="complete"]').disabled`) {
		t.Error("Complete Merge works while markers remain")
	}
	p.TapJS(mergeView + `.querySelector('[data-act="theirs"]')`)
	p.WaitJS(`document.querySelector(".gm-left")?.textContent === "No conflicts left"`)
	p.WaitJS(`!` + mergeView + `.querySelector('[data-act="complete"]').disabled`)
	p.WaitJS(`(() => { const d = ` + mergeView + `.querySelector(".gm-result .desk-editor").contentDocument; return d && !d.querySelector(".tc-mg-marker"); })()`)
	if got := strings.TrimSuffix(paneText(p, "result"), "\n"); got != "package a\ntheirs\nend" {
		t.Errorf("result %q", got)
	}
	p.TapJS(mergeView + `.querySelector('[data-act="complete"]')`)
	p.WaitJS(`document.activeElement?.textContent === "Cancel"`)
	p.TapJS(mergeView + `.querySelector('[data-act="save"]')`)
	r := waitReq(t, c, "git.resolve")
	want := `{"path":"src/c.go","text":"package a\ntheirs\nend\n","version":"` + strings.Repeat("ab", 32) + `"}`
	if got := canonJSON(t, r.Args); got != canonJSON(t, []byte(want)) {
		t.Errorf("resolve args\n got %s\nwant %s", got, want)
	}
	reply(t, c, r, webproto.GitResolved{Path: "src/c.go"})
	p.WaitJS(`!` + mergeView + ` && document.querySelector(".toasts")?.textContent.includes("Resolved c.go")`)
	if len(p.Problems()) > 0 {
		t.Errorf("problems: %v", p.Problems())
	}
}

// A file one side deleted keeps one side through the laptop.
func TestMergeEditorWholeFileAsksLaptop(t *testing.T) {
	snap := gitSnap()
	snap.Files = append(snap.Files, webproto.GitFile{Path: "img.png", X: "U", Y: "U", Conflict: true})
	p, c := openGit(t, Desktop, snap)
	p.TapJS(`document.querySelector('` + rowSel("img.png") + ` .git-open')`)
	reply(t, c, waitReq(t, c, "git.conflict"), webproto.GitConflict{Path: "img.png", Version: strings.Repeat("cd", 32), Blocks: []webproto.GitConflictPart{},
		Ours: webproto.GitConflictSide{Present: true, Binary: true}, Theirs: webproto.GitConflictSide{Present: false}})
	p.WaitJS(mergeView + `?.querySelector(".gm-whole")`)
	p.TapJS(mergeView + `.querySelector('[data-act="keep-ours"]')`)
	p.WaitJS(`document.querySelector(".ask .arm.is-armed")`)
	p.TapJS(ButtonByText("Ask the laptop"))
	r := waitReq(t, c, "git.resolve_side")
	if canonJSON(t, r.Args) != `{"path":"img.png","side":"ours"}` {
		t.Errorf("args %s", r.Args)
	}
}
