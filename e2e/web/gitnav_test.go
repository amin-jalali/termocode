//go:build browser

package web

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// The laptop's git moves in the workbench's commands (Ctrl/Cmd+Shift+P):
// next and previous change and conflict in the file, next and previous
// conflict in the merge editor, and source control's tree or flat list.

// runCmd runs a workbench command by its name from the command list.
// The first open asks the laptop for its own commands: none here.
func runCmd(t *testing.T, p *Page, c *HubConn, name string, first bool) {
	t.Helper()
	wbKey(p, "P", true, true)
	if first {
		reply(t, c, waitReq(t, c, "palette.list"), webproto.PaletteList{Items: []webproto.PaletteItem{}})
	}
	p.WaitJS(`document.activeElement === document.querySelector(".quick .quick-box")`)
	p.Key(name)
	row := `[...document.querySelectorAll(".quick-row")].find((r) => r.querySelector(".quick-label")?.textContent === ` + jsq(name) + `)`
	p.WaitJS(row)
	p.TapJS(row)
	p.WaitJS(`!document.querySelector(".quick")`)
}

func waitLine(p *Page, n string) {
	p.WaitJS(`document.querySelector(".statusbar .sb-pos")?.textContent.startsWith("Ln ` + n + `,")`)
}

// Next and Previous Change go round the file's git changes.
func TestGitNavNextChange(t *testing.T) {
	p, c := openFiles(t, Desktop)
	c.Send(t, Snap("git", 1, webproto.GitSnap{Repo: true, Branch: webproto.GitBranch{Name: "main"},
		Files: []webproto.GitFile{{Path: "a.txt", X: " ", Y: "M", Unstaged: true}}}))
	openDesk(t, p, c, 2, "a.txt", "one", "TWO", "three", "new", "five")
	r := expectReq(t, c, "git.diff")
	c.Send(t, &webproto.Res{T: webproto.KindRes, ID: r.ID, Data: raw(webproto.GitDiff{Path: "a.txt", Rows: []webproto.DiffRow{
		{K: "hunk", Text: "@@ -1,5 +1,5 @@"},
		{K: "ctx", Text: "one", Old: 1, New: 1},
		{K: "del", Text: "two", Old: 2},
		{K: "add", Text: "TWO", New: 2},
		{K: "ctx", Text: "three", Old: 3, New: 3},
		{K: "add", Text: "new", New: 4},
		{K: "ctx", Text: "five", Old: 4, New: 5},
	}})})
	p.WaitJS(deskDoc + `.querySelector(".tc-gm-mod") && ` + deskDoc + `.querySelector(".tc-gm-add")`)
	runCmd(t, p, c, "Git: Go to Next Change", true)
	waitLine(p, "2")
	runCmd(t, p, c, "Git: Go to Next Change", false)
	waitLine(p, "4")
	runCmd(t, p, c, "Git: Go to Next Change", false)
	waitLine(p, "2") // round to the first
	runCmd(t, p, c, "Git: Go to Previous Change", false)
	waitLine(p, "4")
}

// Next and Previous Merge Conflict go to the "<<<<<<<" rows of the file.
func TestGitNavNextConflict(t *testing.T) {
	p, c := openFiles(t, Desktop)
	openDesk(t, p, c, 2, "a.txt", "one", "<<<<<<< HEAD", "a", "=======", "b", ">>>>>>> side", "mid", "<<<<<<< HEAD", "c", "=======", "d", ">>>>>>> side")
	runCmd(t, p, c, "Git: Go to Next Merge Conflict", true)
	waitLine(p, "2")
	runCmd(t, p, c, "Git: Go to Next Merge Conflict", false)
	waitLine(p, "8")
	runCmd(t, p, c, "Git: Go to Previous Merge Conflict", false)
	waitLine(p, "2")
}

// The merge editor's Next and Previous Conflict are commands too.
func TestGitNavMergeEditor(t *testing.T) {
	snap := gitSnap()
	snap.Op = "merge"
	snap.Files = append(snap.Files, webproto.GitFile{Path: "src/c.go", X: "U", Y: "U", Conflict: true})
	p, c := openGit(t, Desktop, snap)
	p.TapJS(scm + `.querySelector('.tr[data-group="merge"][data-path="src/c.go"]')`)
	r := waitReq(t, c, "git.conflict")
	one := "<<<<<<< HEAD\nours\n=======\ntheirs\n>>>>>>> side\n"
	two := "<<<<<<< HEAD\nours2\n=======\ntheirs2\n>>>>>>> side\n"
	mc := webproto.GitConflict{Path: "src/c.go", Text: "package a\n" + one + "mid\n" + two, Version: strings.Repeat("ab", 32),
		Ours:   webproto.GitConflictSide{Present: true, Text: "package a\nours\nmid\nours2\n"},
		Theirs: webproto.GitConflictSide{Present: true, Text: "package a\ntheirs\nmid\ntheirs2\n"},
		Base:   webproto.GitConflictSide{Present: true, Text: "package a\nmid\n"},
		Blocks: []webproto.GitConflictPart{
			{Line: 2, Raw: one, OursLabel: "HEAD", TheirsLabel: "side", Ours: "ours\n", Theirs: "theirs\n", Both: "ours\ntheirs\n"},
			{Line: 8, Raw: two, OursLabel: "HEAD", TheirsLabel: "side", Ours: "ours2\n", Theirs: "theirs2\n", Both: "ours2\ntheirs2\n"}}}
	reply(t, c, r, mc)
	p.WaitJS(mergeView + `?.querySelectorAll(".desk-editor").length === 3`)
	p.WaitJS(`document.querySelector(".gm-left")?.textContent === "Conflict 1 of 2 remaining"`)
	runCmd(t, p, c, "Git: Merge View: Next Conflict", true)
	p.WaitJS(`document.querySelector(".gm-left")?.textContent === "Conflict 2 of 2 remaining"`)
	runCmd(t, p, c, "Git: Merge View: Previous Conflict", false)
	p.WaitJS(`document.querySelector(".gm-left")?.textContent === "Conflict 1 of 2 remaining"`)
}

// Toggle Tree/Flat View: the changes grouped under their folders, and
// back to one row per file.
func TestGitNavTreeView(t *testing.T) {
	p, c := openDeskSCM(t, nil)
	if n := Eval[int](p, scm+`.querySelectorAll(".tr[data-dir]").length`); n != 0 {
		t.Fatalf("the flat list has %d folder rows", n)
	}
	runCmd(t, p, c, "Git: Toggle Tree/Flat View", true)
	p.WaitJS(scm + `.querySelector('.tr[data-dir="src"]') && ` + scmRow("changes", "src/a.go") + `?.querySelector(".tr-desc") === null`)
	runCmd(t, p, c, "Git: Toggle Tree/Flat View", false)
	p.WaitJS(`!` + scm + `.querySelector(".tr[data-dir]") && ` + scmRow("changes", "src/a.go") + `?.querySelector(".tr-desc")?.textContent === "src"`)
}
