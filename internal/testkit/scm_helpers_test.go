package testkit

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/amin-jalali/termocode/internal/clock"
	"github.com/amin-jalali/termocode/internal/theme"
)

// Helpers for the Source Control behaviour tests (scm_*_test.go).

// scmEpoch is the fixed author/committer date of the first commit in a
// test repo; each later commit is one hour after the one before.
var scmEpoch = time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)

// scmNow is the frozen "now" for relative dates: three days after scmEpoch.
var scmNow = scmEpoch.Add(72 * time.Hour)

// repo drives git in a test repository with fixed identities and dates.
type repo struct {
	t     testing.TB
	dir   string
	clock time.Time // date of the next commit
}

func newRepo(t testing.TB, dir string) *repo {
	t.Helper()
	r := &repo{t: t, dir: dir, clock: scmEpoch}
	r.git("init", "-q", "-b", "main")
	r.git("config", "commit.gpgsign", "false")
	r.git("config", "user.name", "Test")
	r.git("config", "user.email", "test@example.com")
	return r
}

// git runs git in the repo and returns trimmed stdout; it fails the test
// on error.
func (r *repo) git(args ...string) string {
	r.t.Helper()
	out, err := r.try(args...)
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

// try runs git and returns trimmed combined output and the error.
func (r *repo) try(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = r.dir
	d := r.clock.Format(time.RFC3339)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE="+d, "GIT_COMMITTER_DATE="+d,
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_TERMINAL_PROMPT=0", "GIT_EDITOR=true", "GIT_MERGE_AUTOEDIT=no")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func (r *repo) write(rel, body string) {
	r.t.Helper()
	p := filepath.Join(r.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) read(rel string) string {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, rel))
	if err != nil {
		return "<missing>"
	}
	return string(b)
}

func (r *repo) remove(rel string) {
	r.t.Helper()
	if err := os.Remove(filepath.Join(r.dir, rel)); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(r.dir, rel))
	return err == nil
}

// commit stages everything and commits with the next fixed date.
func (r *repo) commit(msg string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", msg)
	r.clock = r.clock.Add(time.Hour)
}

// addOrigin makes a local bare repo, adds it as "origin" and pushes main to
// it with upstream tracking. It returns the bare repo's path.
func (r *repo) addOrigin() string {
	r.t.Helper()
	bare := filepath.Join(looseTempDir(r.t), "origin.git")
	r.git("init", "-q", "--bare", "-b", "main", bare)
	r.git("remote", "add", "origin", bare)
	r.git("push", "-q", "-u", "origin", "main")
	return bare
}

// otherClone clones the bare repo into a new dir: "someone else" who
// pushes to origin. It shares this repo's fake clock.
func (r *repo) otherClone(bare string) *repo {
	r.t.Helper()
	dir := filepath.Join(looseTempDir(r.t), "other")
	r.git("clone", "-q", bare, dir)
	o := &repo{t: r.t, dir: dir, clock: r.clock}
	o.git("config", "commit.gpgsign", "false")
	return o
}

// status is `git status --porcelain=v1` as one string per line.
func (r *repo) status() []string {
	r.t.Helper()
	out := r.git("-c", "core.quotePath=false", "status", "--porcelain=v1")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// porcelain renders status lines joined by "|", for compact assertions.
func (r *repo) porcelain() string {
	r.t.Helper()
	cmd := exec.Command("git", "-c", "core.quotePath=false", "status", "--porcelain=v1")
	cmd.Dir = r.dir
	b, _ := cmd.Output()
	out := strings.ReplaceAll(string(b), `"`, "") // names with spaces are quoted
	return strings.Join(strings.Split(strings.TrimRight(out, "\n"), "\n"), "|")
}

// scmFiles is a small workspace for the Source Control tests.
var scmFiles = map[string]string{
	"README.md":       "# Demo\n",
	"main.go":         "package main\n\nfunc main() {}\n",
	"pkg/util.go":     "package pkg\n\n// Add adds.\nfunc Add(a, b int) int { return a + b }\n",
	"docs/guide.md":   "# Guide\n\nline 2\nline 3\n",
	"docs/remove.txt": "to be deleted\n",
}

// scmDriver starts the app on a repo that setup builds. The repo starts
// with one commit of scmFiles. clock.Now is frozen at scmNow and TZ=UTC so
// dates render the same everywhere.
func scmDriver(t *testing.T, setup func(r *repo)) (*Driver, *repo) {
	t.Helper()
	d, r := scmStart(t, setup)
	// Git state loads in the background; wait for it.
	if !until(d, func() bool { return section(d, "GRAPH") != nil && !inSection(d, "GRAPH", "No commits") }) {
		t.Fatalf("Source Control did not load:%s", dump(d))
	}
	return d, r
}

// scmStart is scmDriver without waiting for the first git refresh.
func scmStart(t *testing.T, setup func(r *repo)) (*Driver, *repo) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Cleanup(clock.Set(func() time.Time { return scmNow }))
	t.Setenv("TZ", "UTC")
	var r *repo
	d := New(t, Options{W: 140, H: 44, Files: scmFiles, Setup: func(tb testing.TB, dir string) {
		r = newRepo(tb, dir)
		r.commit("init")
		if setup != nil {
			setup(r)
		}
	}})
	d.Action("view:git")
	return d, r
}

// until pumps messages (re-rendering each time) until cond holds or 3s
// pass, and reports cond's final value.
func until(d *Driver, cond func() bool) bool {
	d.T.Helper()
	return d.WaitFor(func() bool { d.render(); return cond() }, 3*time.Second)
}

// text is the current frame without ANSI codes.
func text(d *Driver) string { return ansi.Strip(d.Frame()) }

// refresh asks the app to re-read git state and waits for want to show.
func (d *Driver) refreshGit(want string) {
	d.T.Helper()
	d.Palette("git-refresh")
	if want != "" && !d.WaitForText(want, 3*time.Second) {
		d.T.Errorf("after refresh, %q not shown:\n%s", want, text(d))
	}
}

// sidebar returns the Source Control sidebar column of every row (ANSI
// stripped), so assertions do not match the editor or other panels.
func sidebar(d *Driver) []string {
	var out []string
	for _, l := range strings.Split(text(d), "\n") {
		// Activity bar + sidebar: everything left of the sidebar's edge.
		if i := strings.Index(l, "▕"); i >= 0 {
			l = l[:i]
		}
		out = append(out, l)
	}
	return out
}

// rowWith returns the index of the first sidebar row containing s, or -1.
func rowWith(d *Driver, s string) int {
	for i, l := range sidebar(d) {
		if strings.Contains(l, s) {
			return i
		}
	}
	return -1
}

// mustRow is rowWith that fails the test when s is not in the sidebar.
func mustRow(d *Driver, s string) int {
	d.T.Helper()
	i := rowWith(d, s)
	if i < 0 {
		d.T.Fatalf("%q not in the Source Control panel:\n%s", s, strings.Join(sidebar(d), "\n"))
	}
	return i
}

// colOf returns the cell column of s in row y (ANSI stripped), or -1.
func colOf(d *Driver, y int, s string) int {
	rows := strings.Split(text(d), "\n")
	if y < 0 || y >= len(rows) {
		return -1
	}
	i := strings.Index(rows[y], s)
	if i < 0 {
		return -1
	}
	return ansi.StringWidth(rows[y][:i])
}

func dump(d *Driver) string { return fmt.Sprintf("\n%s", text(d)) }

// section returns the sidebar rows under the accordion header title
// ("STAGED", "CHANGES", "CONFLICTS", "GRAPH") up to the next blank row,
// each trimmed, or nil when the header is not shown.
func section(d *Driver, title string) []string {
	rows := sidebar(d)
	start := -1
	for i, l := range rows {
		if strings.Contains(l, "▾ "+title+" ") || strings.Contains(l, "▸ "+title+" ") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	var out []string
	for _, l := range rows[start+1:] {
		// Strip the activity-bar columns (left of the sidebar).
		if rs := []rune(l); len(rs) > 4 {
			l = string(rs[4:])
		}
		l = strings.TrimSpace(l)
		if l == "" {
			break
		}
		out = append(out, l)
	}
	return out
}

// inSection reports whether some row of the section contains s.
func inSection(d *Driver, title, s string) bool {
	for _, l := range section(d, title) {
		if strings.Contains(l, s) {
			return true
		}
	}
	return false
}

// clickText left-clicks the first sidebar cell showing s.
func scmClickText(d *Driver, s string) {
	d.T.Helper()
	y := mustRow(d, s)
	d.Click(colOf(d, y, s), y)
}

// typeCommitMessage clicks the commit message box and types msg.
func typeCommitMessage(d *Driver, msg string) {
	d.T.Helper()
	scmClickText(d, "Message…")
	d.Type(msg)
}

// clickScreen left-clicks the first cell of the frame showing s (any panel
// or overlay); it fails the test when s is not shown.
func clickScreen(d *Driver, s string) {
	d.T.Helper()
	for y, l := range strings.Split(text(d), "\n") {
		if strings.Contains(l, s) {
			d.Click(colOf(d, y, s), y)
			return
		}
	}
	d.T.Fatalf("%q not on screen:%s", s, dump(d))
}

// rawRow returns row y of the frame with its ANSI codes.
func rawRow(d *Driver, y int) string {
	rows := strings.Split(d.Frame(), "\n")
	if y < 0 || y >= len(rows) {
		return ""
	}
	return rows[y]
}

// hasFg reports whether row y paints some text in colour c (truecolor SGR).
func hasFg(d *Driver, y int, c theme.Color256) bool {
	v := theme.ToRGB(c)
	return strings.Contains(rawRow(d, y), fmt.Sprintf("38;2;%d;%d;%d", v.R, v.G, v.B))
}
