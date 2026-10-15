package testkit

import (
	"strings"
	"testing"
)

// Commit view actions (scm-graph design §3.5, task V3).

// Spec: Copy SHA on the card copies the full SHA and says so.
func TestSCMCommitViewCopySha(t *testing.T) {
	clip := sinkClipboard(t)
	d, r := scmDriver(t, graphRepo)
	short := openCommitView(t, d, r, "add parser", "v1.0")
	clickScreen(d, " Copy SHA ")
	full := r.git("rev-parse", "v1.0")
	if !until(d, func() bool { got, _ := clip.get(); return got == full }) {
		got, _ := clip.get()
		t.Fatalf("copied %q, want %q", got, full)
	}
	if !until(d, func() bool { return strings.Contains(text(d), "Copied "+short) }) {
		t.Errorf("no toast:%s", dump(d))
	}
}

// Spec (§3.2): a click on "parent <sha>" shows the parent commit.
func TestSCMCommitViewParentClick(t *testing.T) {
	d, r := scmDriver(t, graphRepo)
	openCommitView(t, d, r, "after merge", "HEAD")
	parent := r.git("rev-parse", "--short", "HEAD~1")
	clickScreen(d, "parent "+parent)
	if !until(d, func() bool { return strings.Contains(text(d), "Commit "+parent) }) {
		t.Fatalf("parent not shown:%s", dump(d))
	}
	if !until(d, func() bool { return strings.Contains(text(d), "Merge branch 'topic'") && strings.Contains(text(d), "parents ") }) {
		t.Errorf("merge header lacks its parents:%s", dump(d))
	}
}

// Spec (§3.5): at 100x30 the buttons that do not fit fold into ⋯; its
// menu offers them and runs the chosen one.
func TestSCMCommitViewMoreMenu(t *testing.T) {
	clip := sinkClipboard(t)
	d, r := scmDriver(t, graphRepo)
	d.Resize(80, 30)
	openCommitView(t, d, r, "add parser", "v1.0")
	if strings.Contains(text(d), "Cherry-pick…") {
		t.Fatalf("all buttons shown at 80 cells:%s", dump(d))
	}
	rows := strings.Split(text(d), "\n")
	for y := len(rows) - 1; y >= 0; y-- {
		if strings.Contains(rows[y], " Close ") {
			x := colOf(d, y, " Close ")
			d.Click(x-4, y) // the ⋯ button sits left of Close
			break
		}
	}
	if !until(d, func() bool { return strings.Contains(text(d), "Cherry-pick…") }) {
		t.Fatalf("⋯ menu not open:%s", dump(d))
	}
	clickText(d, "Cherry-pick…")
	if !until(d, func() bool { return strings.Contains(text(d), "Cherry-pick ") && strings.Contains(text(d), "Cancel") }) {
		t.Errorf("menu item did not run:%s", dump(d))
	}
	_ = clip
}

// Spec (§3.5, V3): Open file closes the card and opens the file of the
// working tree in the editor.
func TestSCMCommitViewOpenFile(t *testing.T) {
	d, r := scmNvimDriver(t, graphRepo)
	scmClickText(d, "add parser")
	if !waitText(d, " Open file ") {
		t.Fatalf("no card:%s", dump(d))
	}
	clickScreen(d, " Open file ")
	if !waitText(d, "func parse() {}") || strings.Contains(text(d), "(this commit)") {
		t.Fatalf("parser.go not open, or the card stays:%s", dump(d))
	}
	if name, err := d.Model().NvimLua(`return vim.api.nvim_buf_get_name(0)`); err != nil || !strings.HasSuffix(name, "/parser.go") {
		t.Errorf("current buffer %q (%v)", name, err)
	}
	_ = r
}

// Spec (§3.5, V3): File @ commit opens a read-only buffer "path @ sha"
// with the file as it was in that commit (not the working tree).
func TestSCMCommitViewFileAtCommit(t *testing.T) {
	d, r := scmNvimDriver(t, func(r *repo) {
		graphRepo(r)
		r.write("main.go", "package main // changed later\n")
	})
	short := r.git("rev-parse", "--short", "HEAD")
	scmClickText(d, "after merge")
	if !waitText(d, " File @ commit ") {
		t.Fatalf("no card:%s", dump(d))
	}
	clickScreen(d, " File @ commit ")
	if !waitText(d, "func main() { parse() }") {
		t.Fatalf("file at commit not shown:%s", dump(d))
	}
	got, err := d.Model().NvimLua(`return vim.api.nvim_buf_get_name(0) .. '|' .. tostring(vim.bo.modifiable) .. '|' .. vim.bo.buftype .. '|' .. table.concat(vim.api.nvim_buf_get_lines(0, 0, -1, false), '\n')`)
	if err != nil {
		t.Fatal(err)
	}
	want := r.git("show", "HEAD:main.go")
	parts := strings.SplitN(got, "|", 4)
	if len(parts) != 4 || !strings.HasSuffix(parts[0], "main.go @ "+short) || parts[1] != "false" || parts[2] != "nofile" || parts[3] != want {
		t.Errorf("buffer %q, want main.go @ %s, read-only nofile with %q", got, short, want)
	}
}
