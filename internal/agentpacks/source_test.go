package agentpacks

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitPack is a git repo holding samplePack with tag v1 on the first
// commit and one more commit after it.
func gitPack(t *testing.T) (url, v1 string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	dir := samplePack(t)
	gitT(t, dir, "init", "-q")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-q", "-m", "one")
	gitT(t, dir, "tag", "v1")
	v1 = gitT(t, dir, "rev-parse", "HEAD")
	write(t, filepath.Join(dir, "agents", "later.md"), "added after v1\n")
	gitT(t, dir, "add", ".")
	gitT(t, dir, "commit", "-q", "-m", "two")
	return "file://" + dir, v1
}

// Spec (OMX-46): a git source must name a version (url@tag or
// url@commit); Open reads the pack at exactly that version and keeps the
// commit, so the install is pinned to it.
func TestOpenGitSourceIsPinned(t *testing.T) {
	url, v1 := gitPack(t)
	ctx := context.Background()
	if _, err := Open(ctx, url, t.TempDir()); err == nil || !strings.Contains(err.Error(), "@") {
		t.Errorf("a git source without a version: %v", err)
	}
	p, err := Open(ctx, url+"@v1", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Commit != v1 || p.Source != url+"@v1" {
		t.Errorf("commit %q source %q", p.Commit, p.Source)
	}
	for _, f := range p.Files {
		if strings.Contains(f.Rel, "later") {
			t.Error("a file after the pinned version was read")
		}
	}
	if _, err := Open(ctx, url+"@no-such-tag", t.TempDir()); err == nil {
		t.Error("an unknown version must fail")
	}
}

// Spec (OMX-46): a local folder is read as it is.
func TestOpenLocalFolder(t *testing.T) {
	dir := samplePack(t)
	p, err := Open(context.Background(), dir, t.TempDir())
	if err != nil || p.Name != "demo-pack" || p.Commit != "" {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err := Open(context.Background(), filepath.Join(dir, "missing"), t.TempDir()); err == nil {
		t.Error("a missing folder must fail")
	}
}

// Spec (OMX-46): SplitSource finds the version after the last @ of the
// path part only.
func TestSplitSource(t *testing.T) {
	for in, want := range map[string]string{
		"https://example.com/a/pack.git@v2.1": "https://example.com/a/pack.git|v2.1",
		"git@example.com:a/pack@abc123":       "git@example.com:a/pack|abc123",
		"git@example.com:a/pack":              "git@example.com:a/pack|",
		"https://example.com/a/pack":          "https://example.com/a/pack|",
	} {
		u, r := SplitSource(in)
		if u+"|"+r != want {
			t.Errorf("%s: %s|%s want %s", in, u, r, want)
		}
	}
}
