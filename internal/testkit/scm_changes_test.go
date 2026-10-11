package testkit

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/theme"
)

// Source Control: the changes list (STAGED / CHANGES), tree vs flat view,
// stage / unstage, discard.

// Spec: Given a repo with a modified, a staged-new, a staged-deleted, a
// worktree-deleted, a renamed and an untracked file, when Source Control
// opens, then each file shows once in the right section (STAGED for index
// changes, CHANGES for worktree changes), the section counts match git, and
// the status dot has the colour of its kind.
func TestSCMChangesListGroupsEveryKind(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n\nfunc main() { println(1) }\n") // " M"
		r.write("new.go", "package main\n")                                // "A "
		r.git("add", "new.go")
		r.git("rm", "-q", "docs/remove.txt")   // "D "
		r.git("mv", "README.md", "README2.md") // "R "
		r.write("notes.txt", "draft\n")        // "??"
		r.write("docs/guide.md", "")           // " M" (emptied)
	})
	if !d.WaitForText("notes.txt", 3*time.Second) {
		t.Fatalf("changes not listed:%s", dump(d))
	}
	if got, want := r.porcelain(), "R  README.md -> README2.md| M docs/guide.md|D  docs/remove.txt| M main.go|A  new.go|?? notes.txt"; got != want {
		t.Fatalf("setup: git status = %q, want %q", got, want)
	}
	staged, changes := section(d, "STAGED"), section(d, "CHANGES")
	for _, f := range []string{"new.go", "remove.txt", "README2.md"} {
		if !inSection(d, "STAGED", f) || inSection(d, "CHANGES", f) {
			t.Errorf("%s: want only in STAGED\nSTAGED=%q\nCHANGES=%q", f, staged, changes)
		}
	}
	for _, f := range []string{"main.go", "notes.txt", "guide.md"} {
		if !inSection(d, "CHANGES", f) || inSection(d, "STAGED", f) {
			t.Errorf("%s: want only in CHANGES\nSTAGED=%q\nCHANGES=%q", f, staged, changes)
		}
	}
	if y := mustRow(d, "STAGED"); !strings.HasSuffix(strings.TrimSpace(sidebar(d)[y]), "3") {
		t.Errorf("STAGED count: %q, want 3", sidebar(d)[y])
	}
	if y := mustRow(d, "CHANGES"); !strings.HasSuffix(strings.TrimSpace(sidebar(d)[y]), "3") {
		t.Errorf("CHANGES count: %q, want 3", sidebar(d)[y])
	}
	// Status dots: filled for tracked changes, hollow for untracked; the
	// colour tells the kind.
	dots := []struct {
		file, glyph string
		c           theme.Color256
	}{
		{"main.go", "●", theme.GitModified},
		{"new.go", "●", theme.GitAdded},
		{"remove.txt", "●", theme.AccentRedCoral},
		{"README2.md", "●", theme.AccentBlue},
		{"notes.txt", "○", theme.GitAdded},
	}
	for _, c := range dots {
		y := mustRow(d, c.file)
		if !strings.Contains(sidebar(d)[y], c.glyph+"  "+c.file) {
			t.Errorf("%s: want %q before the name, row %q", c.file, c.glyph, sidebar(d)[y])
		}
		if !hasFg(d, y, c.c) {
			t.Errorf("%s: dot not in the %s colour: %q", c.file, c.file, rawRow(d, y))
		}
	}
	// The commit button counts the staged files.
	if mustRow(d, "Commit (3)") < 0 {
		t.Error("commit button does not show the staged count")
	}
}

// Spec: Given changes in nested folders, when the view is a tree then each
// folder is a header row with its files indented below it; when the user
// presses t (flat), then the folders go away and each file shows its folder
// as a dim suffix; t again brings the tree back.
func TestSCMTreeAndFlatView(t *testing.T) {
	d, _ := scmDriver(t, func(r *repo) {
		r.write("docs/guide.md", "# Guide\nchanged\n")
		r.write("pkg/util.go", "package pkg\n")
		r.write("main.go", "package main\n// x\n")
	})
	d.WaitForText("guide.md", 3*time.Second)
	if !strings.Contains(strings.Join(sidebar(d), "\n"), " tree") {
		t.Fatalf("default view is not tree:%s", dump(d))
	}
	ch := section(d, "CHANGES")
	joined := strings.Join(ch, "\n")
	for _, want := range []string{"▾ docs", "▾ pkg", "guide.md", "util.go", "main.go"} {
		if !strings.Contains(joined, want) {
			t.Errorf("tree view: %q missing from CHANGES:\n%s", want, joined)
		}
	}
	// A folder comes right before its file.
	if i := indexOf(ch, "docs"); i < 0 || i+1 >= len(ch) || !strings.Contains(ch[i+1], "guide.md") {
		t.Errorf("tree view: guide.md not under docs:\n%s", joined)
	}

	d.Palette("git-focus")
	d.Key("t")
	ch = section(d, "CHANGES")
	joined = strings.Join(ch, "\n")
	if strings.Contains(joined, "▾ docs") {
		t.Errorf("flat view still shows folder rows:\n%s", joined)
	}
	if !strings.Contains(joined, "guide.md  docs") || !strings.Contains(joined, "util.go  pkg") {
		t.Errorf("flat view: want 'name  dir' rows:\n%s", joined)
	}
	if !strings.Contains(strings.Join(sidebar(d), "\n"), " flat") {
		t.Errorf("sub-header does not say flat:%s", dump(d))
	}
	d.Key("t")
	if !strings.Contains(strings.Join(section(d, "CHANGES"), "\n"), "▾ docs") {
		t.Errorf("t did not return to tree view:%s", dump(d))
	}
}

func indexOf(rows []string, s string) int {
	for i, r := range rows {
		if strings.Contains(r, s) {
			return i
		}
	}
	return -1
}

// Spec: Given a modified file, when the user selects it and presses s, then
// git has it staged and it moves from CHANGES to STAGED; when s is pressed
// on the staged row, then it is unstaged again (back to " M").
func TestSCMStageAndUnstageFile(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// edit\n")
	})
	d.WaitForText("main.go", 3*time.Second)
	clickText(d, "main.go")
	d.Key("s")
	until(d, func() bool { return inSection(d, "STAGED", "main.go") })
	if got := r.porcelain(); got != "M  main.go" {
		t.Errorf("after s: git status %q, want %q", got, "M  main.go")
	}
	if !inSection(d, "STAGED", "main.go") || inSection(d, "CHANGES", "main.go") {
		t.Errorf("after s: main.go not moved to STAGED:%s", dump(d))
	}
	if !strings.Contains(text(d), "Staged") {
		t.Errorf("no 'Staged' toast:%s", dump(d))
	}

	clickText(d, "main.go") // the STAGED row now
	d.Key("s")
	until(d, func() bool { return inSection(d, "CHANGES", "main.go") })
	if got := r.porcelain(); got != " M main.go" {
		t.Errorf("after second s: git status %q, want %q", got, " M main.go")
	}
	if section(d, "STAGED") != nil {
		t.Errorf("STAGED section still shown:%s", dump(d))
	}
}

// Spec: Given a file with staged AND unstaged edits ("MM"), when the user
// presses s on its CHANGES row, then the rest of the edits are staged too
// ("M ") and it is listed only under STAGED.
func TestSCMStagePartlyStagedFile(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// one\n")
		r.git("add", "main.go")
		r.write("main.go", "package main\n// one\n// two\n")
	})
	d.WaitForText("main.go", 3*time.Second)
	if !inSection(d, "STAGED", "main.go") || !inSection(d, "CHANGES", "main.go") {
		t.Fatalf("an MM file should be in both sections:%s", dump(d))
	}
	y := -1
	for i, l := range sidebar(d) {
		if strings.Contains(l, "main.go") {
			y = i // the last one: the CHANGES row
		}
	}
	d.Click(colOf(d, y, "main.go"), y)
	d.Key("s")
	until(d, func() bool { return !inSection(d, "CHANGES", "main.go") })
	if got := r.porcelain(); got != "M  main.go" {
		t.Errorf("git status %q, want %q", got, "M  main.go")
	}
}

// Spec: Given modified, deleted and untracked files, when the user runs
// "Git: Stage All Changes", then every change is in the index (untracked
// added, deletion staged) and CHANGES says "No changes".
func TestSCMStageAll(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// edit\n")
		r.write("notes.txt", "n\n")
		r.remove("docs/remove.txt") // " D"
	})
	d.WaitForText("notes.txt", 3*time.Second)
	d.Palette("git-stage-all")
	until(d, func() bool { return inSection(d, "CHANGES", "No changes") })
	if got, want := r.porcelain(), "D  docs/remove.txt|M  main.go|A  notes.txt"; got != want {
		t.Errorf("git status %q, want %q", got, want)
	}
	if !inSection(d, "CHANGES", "No changes") {
		t.Errorf("CHANGES not empty:%s", dump(d))
	}
	if !strings.Contains(text(d), "Commit (3)") {
		t.Errorf("commit button does not count 3 staged files:%s", dump(d))
	}
}

// Spec: Given a modified file, when the user presses x and then Esc (or
// Cancel), then nothing changes; when the user presses x and confirms,
// then the file is back to its committed content and leaves the list.
func TestSCMDiscardAsksFirst(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// edit\n")
	})
	d.WaitForText("main.go", 3*time.Second)
	clickText(d, "main.go")
	d.Key("x")
	if !strings.Contains(text(d), "Discard changes?") || !strings.Contains(text(d), `revert "main.go"`) {
		t.Fatalf("x did not ask for confirmation:%s", dump(d))
	}
	d.Key("esc")
	if r.read("main.go") != "package main\n// edit\n" {
		t.Fatalf("Esc discarded the change")
	}
	clickText(d, "main.go")
	d.Key("x")
	d.Key("right") // Cancel
	d.Key("enter")
	if r.read("main.go") != "package main\n// edit\n" {
		t.Fatalf("Cancel discarded the change")
	}

	clickText(d, "main.go")
	d.Key("x")
	d.Key("enter") // Discard is the default button
	until(d, func() bool { return inSection(d, "CHANGES", "No changes") })
	if got := r.read("main.go"); got != scmFiles["main.go"] {
		t.Errorf("main.go = %q after discard, want the committed text", got)
	}
	if r.porcelain() != "" {
		t.Errorf("git status not clean: %q", r.porcelain())
	}
	if !inSection(d, "CHANGES", "No changes") {
		t.Errorf("CHANGES still lists files:%s", dump(d))
	}
}

// Spec: Given an untracked file, when the user discards it and confirms,
// then the file is deleted from disk.
func TestSCMDiscardUntrackedDeletes(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("scratch.txt", "tmp\n")
	})
	d.WaitForText("scratch.txt", 3*time.Second)
	clickText(d, "scratch.txt")
	d.Key("x")
	d.Key("enter")
	until(d, func() bool { return rowWith(d, "scratch.txt") < 0 })
	if r.exists("scratch.txt") {
		t.Error("scratch.txt still on disk")
	}
	if rowWith(d, "scratch.txt") >= 0 {
		t.Errorf("scratch.txt still listed:%s", dump(d))
	}
}

// Spec: Given a file with a staged edit and a later unstaged edit, when the
// user discards from the CHANGES row, then only the unstaged edit is
// dropped: the staged edit stays in the index and in the file.
func TestSCMDiscardKeepsStagedPart(t *testing.T) {
	d, r := scmDriver(t, func(r *repo) {
		r.write("main.go", "package main\n// staged\n")
		r.git("add", "main.go")
		r.write("main.go", "package main\n// staged\n// unstaged\n")
	})
	d.WaitForText("main.go", 3*time.Second)
	y := -1
	for i, l := range sidebar(d) {
		if strings.Contains(l, "main.go") {
			y = i
		}
	}
	d.Click(colOf(d, y, "main.go"), y)
	d.Key("x")
	d.Key("enter")
	until(d, func() bool { return !inSection(d, "CHANGES", "main.go") })
	if got := r.read("main.go"); got != "package main\n// staged\n" {
		t.Errorf("main.go = %q, want the staged text", got)
	}
	if got := r.porcelain(); got != "M  main.go" {
		t.Errorf("git status %q, want %q", got, "M  main.go")
	}
}
