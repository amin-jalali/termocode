package lanemove

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/agentlog"
	"github.com/amin-jalali/termocode/internal/worktrees"
)

// CMP-6: a lane (a worktree and the work in it) moves from one machine
// to another in one pack: its commits, its changes not committed, its
// new files, its notes and its Claude Code chats. Unpack makes a NEW
// worktree and never writes over anything.

func run(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not in PATH")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := filepath.Join(t.TempDir(), "app")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, dir, "init", "-q", "-b", "main")
	run(t, dir, "config", "user.email", "t@example.com")
	run(t, dir, "config", "user.name", "T")
	run(t, dir, "config", "commit.gpgsign", "false")
	write(t, filepath.Join(dir, "a.txt"), "one\ntwo\n", 0o644)
	write(t, filepath.Join(dir, ".gitignore"), "*.log\n", 0o644)
	run(t, dir, "add", "-A")
	run(t, dir, "commit", "-q", "-m", "init")
	return dir
}

// A lane with a commit, a change, a staged new file, new files (one
// executable, one ignored) and a chat goes into a clone as a new
// worktree with all of it; the chat's folder is the new one.
func TestPackUnpackRoundTrip(t *testing.T) {
	src := newRepo(t)
	lane := filepath.Join(filepath.Dir(src), "app.worktrees", "fix-login")
	run(t, src, "worktree", "add", "-q", "-b", "fix/login", lane)
	lane = resolved(t, lane)
	write(t, filepath.Join(lane, "login.go"), "package login\n", 0o644)
	run(t, lane, "add", "login.go")
	run(t, lane, "commit", "-q", "-m", "login")
	write(t, filepath.Join(lane, "a.txt"), "one\nTWO\n", 0o644)
	write(t, filepath.Join(lane, "staged.txt"), "s\n", 0o644)
	run(t, lane, "add", "staged.txt")
	write(t, filepath.Join(lane, "notes", "todo.md"), "- finish\n", 0o644)
	write(t, filepath.Join(lane, "run.sh"), "#!/bin/sh\necho hi\n", 0o755)
	write(t, filepath.Join(lane, "debug.log"), "ignored\n", 0o644)

	claudeA := filepath.Join(t.TempDir(), "claudeA")
	chat := `{"type":"user","cwd":` + jsonStr(lane) + `,"file":` + jsonStr(lane+"/a.txt") + "}\n"
	write(t, filepath.Join(claudeA, "projects", agentlog.Slug(lane), "s-1.jsonl"), chat, 0o600)
	write(t, filepath.Join(claudeA, "projects", agentlog.Slug(lane), "agent-x.jsonl"), "{}\n", 0o600)

	var buf bytes.Buffer
	meta := worktrees.Meta{Name: "fix login", Agent: "claude", Notes: []worktrees.Note{{Text: "half done", At: time.Unix(1, 0).UTC()}}}
	man, err := Pack(lane, PackOptions{ClaudeHome: claudeA, Meta: meta, Host: "laptop", Now: time.Unix(1000, 0)}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if man.Branch != "fix/login" || man.Host != "laptop" || man.Path != lane || !man.Bundle || !man.Patch {
		t.Fatalf("manifest %+v", man)
	}
	if man.Changed != 2 {
		t.Errorf("changed %d, want 2 (a.txt, staged.txt)", man.Changed)
	}
	if strings.Join(man.Files, ",") != "notes/todo.md,run.sh" {
		t.Errorf("new files %v (ignored debug.log must stay)", man.Files)
	}
	if strings.Join(man.Chats, ",") != "s-1" {
		t.Errorf("chats %v", man.Chats)
	}
	if run(t, lane, "status", "--porcelain") == "" {
		t.Fatal("pack changed the worktree")
	}

	got, err := Inspect(bytes.NewReader(buf.Bytes()))
	if err != nil || got.Head != man.Head || got.Name != "fix login" {
		t.Fatalf("inspect %+v %v", got, err)
	}

	// The other machine: a clone of the main branch only.
	dst := filepath.Join(t.TempDir(), "server", "app")
	run(t, filepath.Dir(src), "clone", "-q", src, dst)
	dst = resolved(t, dst)
	file := filepath.Join(t.TempDir(), FileName(man))
	if err := os.WriteFile(file, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	claudeB := filepath.Join(t.TempDir(), "claudeB")
	res, err := UnpackFile(file, Into{Dir: dst, ClaudeHome: claudeB, Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	wantFolder := dst + ".worktrees/fix-login"
	if res.Folder != wantFolder || res.Branch != "fix/login" {
		t.Fatalf("placed at %s on %s", res.Folder, res.Branch)
	}
	if run(t, res.Folder, "rev-parse", "HEAD") != man.Head {
		t.Error("not on the lane's commit")
	}
	if b, _ := os.ReadFile(filepath.Join(res.Folder, "a.txt")); string(b) != "one\nTWO\n" {
		t.Errorf("a.txt = %q", b)
	}
	if _, err := os.Stat(filepath.Join(res.Folder, "staged.txt")); err != nil {
		t.Error("the staged new file is missing")
	}
	if b, _ := os.ReadFile(filepath.Join(res.Folder, "notes", "todo.md")); string(b) != "- finish\n" {
		t.Errorf("todo.md = %q", b)
	}
	if info, err := os.Stat(filepath.Join(res.Folder, "run.sh")); err != nil || info.Mode()&0o100 == 0 {
		t.Errorf("run.sh lost its exec bit: %v", err)
	}
	if _, err := os.Stat(filepath.Join(res.Folder, "debug.log")); err == nil {
		t.Error("an ignored file came along")
	}
	if !res.Patched || res.Files != 2 || res.Chats != 1 {
		t.Errorf("result %+v", res)
	}
	moved, err := os.ReadFile(filepath.Join(claudeB, "projects", agentlog.Slug(res.Folder), "s-1.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(moved), lane) || !strings.Contains(string(moved), jsonStr(res.Folder)) ||
		!strings.Contains(string(moved), jsonStr(res.Folder+"/a.txt")) {
		t.Errorf("chat paths not moved: %s", moved)
	}
	if _, err := os.Stat(filepath.Join(claudeB, "projects", agentlog.Slug(res.Folder), "agent-x.jsonl")); err == nil {
		t.Error("a subagent file came along")
	}
	common := run(t, dst, "rev-parse", "--path-format=absolute", "--git-common-dir")
	mm, err := worktrees.Load(common)
	if err != nil {
		t.Fatal(err)
	}
	md := mm[res.Folder]
	if md.Name != "fix login" || md.Agent != "claude" || len(md.Notes) != 2 || md.Notes[1].Text != "moved here from laptop:"+lane {
		t.Errorf("meta %+v", md)
	}
	if out := run(t, dst, "for-each-ref", "refs/termocode/"); out != "" {
		t.Errorf("a temporary ref stayed: %s", out)
	}

	// Again: the name is taken, so it gets -moved; nothing is written over.
	res2, err := UnpackFile(file, Into{Dir: dst, Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Branch != "fix/login-moved" || res2.Folder != dst+".worktrees/fix-login-moved" || res2.Chats != 0 {
		t.Errorf("second unpack %+v", res2)
	}
}

// The patch that does not apply leaves the worktree on its commit and
// keeps the patch next to it.
func TestUnpackKeepsPatchThatDoesNotApply(t *testing.T) {
	src := newRepo(t)
	write(t, filepath.Join(src, "a.txt"), "one\nTWO\n", 0o644)
	var buf bytes.Buffer
	man, err := Pack(src, PackOptions{}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	// A broken patch: the pack is rebuilt with a patch for a missing file.
	broken := rebuild(t, buf.Bytes(), map[string][]byte{patchName: []byte("diff --git a/zz.txt b/zz.txt\n--- a/zz.txt\n+++ b/zz.txt\n@@ -1 +1 @@\n-x\n+y\n")})
	dst := filepath.Join(t.TempDir(), "app")
	run(t, filepath.Dir(src), "clone", "-q", src, dst)
	res, err := Unpack(bytes.NewReader(broken), UnpackOptions{Repo: dst, Folder: dst + ".worktrees/x", Branch: "lane/x"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Patched || res.PatchAt != dst+".worktrees/x.lane.patch" {
		t.Fatalf("result %+v", res)
	}
	if run(t, res.Folder, "rev-parse", "HEAD") != man.Head {
		t.Error("not on its commit")
	}
}

// A repository without the commits the lane is built on says to fetch.
func TestUnpackMissingBase(t *testing.T) {
	origin := newRepo(t)
	work := filepath.Join(t.TempDir(), "work")
	run(t, filepath.Dir(origin), "clone", "-q", origin, work)
	run(t, work, "config", "user.email", "t@example.com")
	run(t, work, "config", "user.name", "T")
	write(t, filepath.Join(work, "b.txt"), "b\n", 0o644)
	run(t, work, "add", "b.txt")
	run(t, work, "commit", "-q", "-m", "b")
	var buf bytes.Buffer
	man, err := Pack(work, PackOptions{}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if !man.Bundle || man.Commits != 1 {
		t.Fatalf("the bundle should hold only the commit the remote lacks: %+v", man)
	}
	// Unrelated history. A second newRepo is not enough: made in the same
	// second, its first commit has the same hash as origin's.
	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, other, "init", "-q", "-b", "main")
	run(t, other, "config", "user.email", "t@example.com")
	run(t, other, "config", "user.name", "T")
	run(t, other, "config", "commit.gpgsign", "false")
	write(t, filepath.Join(other, "z.txt"), "unrelated\n", 0o644)
	run(t, other, "add", "z.txt")
	run(t, other, "commit", "-q", "-m", "unrelated")
	_, err = Unpack(bytes.NewReader(buf.Bytes()), UnpackOptions{Repo: other, Folder: other + ".worktrees/x", Branch: "x"})
	if !errors.Is(err, ErrMissingBase) {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(other + ".worktrees/x"); err == nil {
		t.Error("a folder was left behind")
	}
}

// A lane whose commits are all on the remote has no bundle.
func TestPackWithoutNewCommits(t *testing.T) {
	origin := newRepo(t)
	work := filepath.Join(t.TempDir(), "work")
	run(t, filepath.Dir(origin), "clone", "-q", origin, work)
	var buf bytes.Buffer
	man, err := Pack(work, PackOptions{}, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if man.Bundle || man.Patch || len(man.Files) != 0 {
		t.Errorf("manifest %+v", man)
	}
}

// A pack with a name outside the worktree, a link, an extra file or a
// newer version is refused before anything is made.
func TestUnpackRefusesBadPacks(t *testing.T) {
	head := strings.Repeat("a", 40)
	cases := map[string]func(tw *tar.Writer){
		"traversal": func(tw *tar.Writer) {
			man(tw, Manifest{Version: 1, Head: head, Files: []string{"../evil"}})
			file(tw, "files/../evil", "x")
		},
		"symlink": func(tw *tar.Writer) {
			man(tw, Manifest{Version: 1, Head: head, Files: []string{"x"}})
			_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeSymlink, Name: "files/x", Linkname: "/etc/passwd"})
		},
		"extra": func(tw *tar.Writer) {
			man(tw, Manifest{Version: 1, Head: head})
			file(tw, "files/sneaky", "x")
		},
		"git dir": func(tw *tar.Writer) {
			man(tw, Manifest{Version: 1, Head: head, Files: []string{".git/hooks/pre-commit"}})
			file(tw, "files/.git/hooks/pre-commit", "x")
		},
		"missing": func(tw *tar.Writer) {
			man(tw, Manifest{Version: 1, Head: head, Patch: true})
		},
		"bad head": func(tw *tar.Writer) {
			man(tw, Manifest{Version: 1, Head: "HEAD; rm -rf"})
		},
		"bad ref": func(tw *tar.Writer) {
			man(tw, Manifest{Version: 1, Head: head, Bundle: true, BundleRef: "--upload-pack=x"})
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := extract(bytes.NewReader(pack(build)), t.TempDir())
			if !errors.Is(err, ErrBadPack) {
				t.Fatalf("err %v", err)
			}
		})
	}
	_, err := Inspect(bytes.NewReader(pack(func(tw *tar.Writer) { man(tw, Manifest{Version: Version + 1, Head: head}) })))
	if !errors.Is(err, ErrNewerPack) {
		t.Errorf("newer: %v", err)
	}
	if _, err := Inspect(strings.NewReader("not a pack")); !errors.Is(err, ErrBadPack) {
		t.Errorf("junk: %v", err)
	}
}

func TestParseRemoteAndLines(t *testing.T) {
	for in, want := range map[string][2]string{
		"srv:~/src/app":       {"srv", "~/src/app"},
		"me@srv:/w/a b":       {"me@srv", "/w/a b"},
		"srv:":                {"srv", "."},
		"-oProxyCommand=x:/a": {},
		"no-colon":            {},
		"a b:/x":              {},
	} {
		h, d, err := ParseRemote(in)
		if want[0] == "" {
			if err == nil {
				t.Errorf("%q: no error", in)
			}
			continue
		}
		if err != nil || h != want[0] || d != want[1] {
			t.Errorf("%q: %q %q %v", in, h, d, err)
		}
	}
	if got := PackLine("~/src/it's", true); got != `termocode lane pack --out - --chats ~/'src/it'\''s'` {
		t.Errorf("pack line %s", got)
	}
	if got := UnpackLine("/w/app"); got != "termocode lane unpack --repo '/w/app' -" {
		t.Errorf("unpack line %s", got)
	}
	args := SSHArgs("srv", nil, true, "x")
	if strings.Join(args, " ") != "-o BatchMode=yes -- srv x" {
		t.Errorf("ssh args %v", args)
	}
}

// Fetch and Send run ssh with the remote termocode line.
func TestFetchAndSendUseSSH(t *testing.T) {
	old := sshRun
	t.Cleanup(func() { sshRun = old })
	var got []string
	sshRun = func(_ context.Context, args []string, in io.Reader, out io.Writer) error {
		got = args
		if in != nil {
			_, _ = io.Copy(io.Discard, in)
		}
		_, _ = io.WriteString(out, "unpacked x\n")
		return nil
	}
	var b bytes.Buffer
	if err := Fetch(context.Background(), "srv", "~/a", false, nil, true, &b); err != nil || b.String() != "unpacked x\n" {
		t.Fatal(err)
	}
	if got[len(got)-1] != "termocode lane pack --out - ~/'a'" {
		t.Errorf("fetch ran %v", got)
	}
	said, err := Send(context.Background(), "srv", "/w", nil, false, strings.NewReader("pack"))
	if err != nil || said != "unpacked x" || got[len(got)-1] != "termocode lane unpack --repo '/w' -" {
		t.Errorf("send %q %v %v", said, err, got)
	}
}

func TestFileNameAndPacks(t *testing.T) {
	m := Manifest{Name: "fix/login: now!", PackedAt: time.Date(2026, 11, 7, 9, 5, 3, 0, time.UTC)}
	if got := FileName(m); got != "fix-login--now-20261107-090503.tlane" {
		t.Errorf("file name %s", got)
	}
	d := t.TempDir()
	write(t, filepath.Join(d, "old.tlane"), "x", 0o600)
	write(t, filepath.Join(d, "new.tlane"), "x", 0o600)
	write(t, filepath.Join(d, ".lane-tmp.tlane"), "x", 0o600)
	write(t, filepath.Join(d, "other.zip"), "x", 0o600)
	_ = os.Chtimes(filepath.Join(d, "old.tlane"), time.Unix(1, 0), time.Unix(1, 0))
	got := Packs(d, filepath.Join(d, "missing"))
	if len(got) != 2 || filepath.Base(got[0]) != "new.tlane" {
		t.Errorf("packs %v", got)
	}
	if Dir(func(string) string { return "" }, "/h") != "/h/.local/share/termocode/lanes" {
		t.Error("lanes dir")
	}
}

// resolved is path with its links read (macOS: /var is /private/var).
func resolved(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func jsonStr(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func pack(build func(tw *tar.Writer)) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	build(tw)
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func man(tw *tar.Writer, m Manifest) {
	b, _ := json.Marshal(m)
	file(tw, manifestName, string(b))
}

func file(tw *tar.Writer, name, body string) {
	_ = tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o644, Size: int64(len(body))})
	_, _ = tw.Write([]byte(body))
}

// rebuild copies a pack, with some files replaced.
func rebuild(t *testing.T, in []byte, repl map[string][]byte) []byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	return pack(func(tw *tar.Writer) {
		for {
			h, err := tr.Next()
			if err != nil {
				return
			}
			b, _ := io.ReadAll(tr)
			if r, ok := repl[h.Name]; ok {
				b = r
			}
			file(tw, h.Name, string(b))
		}
	})
}
