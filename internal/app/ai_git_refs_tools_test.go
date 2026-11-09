package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// aiGitToolAt is the git tool name of aiGitTools on dir; its UI part
// runs on m.
func aiGitToolAt(t *testing.T, m *Model, dir, name string) ai.Tool {
	t.Helper()
	call := func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	}
	tl, ok := ai.FindTool(aiGitTools(dir, call), name)
	if !ok {
		t.Fatalf("no tool %s", name)
	}
	return tl
}

// aiRun validates and runs one call; it fails the test on an error.
func aiRun(t *testing.T, tl ai.Tool, args map[string]any) string {
	t.Helper()
	out, err := aiRunErr(tl, args)
	if err != nil {
		t.Fatalf("%s %v: %v", tl.Name, args, err)
	}
	return out
}

// aiRunErr validates and runs one call.
func aiRunErr(tl ai.Tool, args map[string]any) (string, error) {
	if tl.Validate != nil {
		if err := tl.Validate(args); err != nil {
			return "", err
		}
	}
	return tl.Run(context.Background(), args)
}

// aiWantAtOnce fails unless tl runs args with no card.
func aiWantAtOnce(t *testing.T, tl ai.Tool, args map[string]any) {
	t.Helper()
	if g := aiGated(tl, args); g.Mutating {
		t.Errorf("%s %v asks (class %q); want it at once", tl.Name, args, g.Class)
	}
}

// aiRemoteRepo gives dir a bare remote "origin" with main pushed and
// tracked; it returns the remote's folder.
func aiRemoteRepo(t *testing.T, dir string) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "origin.git")
	covGit(t, dir, "init", "-q", "--bare", bare)
	covGit(t, dir, "remote", "add", "origin", bare)
	covGit(t, dir, "push", "-q", "-u", "origin", "main")
	return bare
}

// Spec: git_branch does the GUI's branch actions: list, create (from a
// ref, switching to it), rename, delete (a branch with commits of its
// own only with force), merge, set / unset the upstream, compare, and
// delete on the remote. It asks where the GUI asks: delete and merge on a
// Cancel-first card every time; create, rename and the upstream with
// Always allow git; delete_remote opens the GUI's own card, where the
// user types the branch name.
func TestAIGitBranchOps(t *testing.T) {
	m, dir := covGitRepo(t)
	tl := aiGitToolAt(t, m, dir, "git_branch")
	aiWantAtOnce(t, tl, map[string]any{"op": "list"})
	aiWantAtOnce(t, tl, map[string]any{"op": "compare", "base": "main"})
	aiWantAtOnce(t, tl, map[string]any{"op": "delete_remote", "name": "origin/x"})
	for _, op := range []string{"create", "rename", "set_upstream", "unset_upstream"} {
		aiWantAlways(t, tl, map[string]any{"op": op}, ai.ClassGit)
	}
	for _, op := range []string{"delete", "merge"} {
		aiWantCancelFirst(t, tl, map[string]any{"op": op})
	}
	if out := aiRun(t, tl, map[string]any{}); !strings.Contains(out, "* main") {
		t.Errorf("list with no op %q, want the current branch marked", out)
	}

	aiRun(t, tl, map[string]any{"op": "create", "name": "feat", "from": "main"})
	if b := covGit(t, dir, "branch", "--show-current"); b != "feat" {
		t.Errorf("after create on %q, want feat", b)
	}
	covWrite(t, dir, "b.txt", "b\n")
	covCommit(t, dir, "add b")
	aiRun(t, tl, map[string]any{"op": "rename", "name": "feat", "new_name": "feat2"})
	if out := aiRun(t, tl, map[string]any{"op": "compare", "base": "main", "head": "feat2"}); !strings.Contains(out, "add b") || !strings.Contains(out, "b.txt") {
		t.Errorf("compare %q, want the commit and its file", out)
	}
	covGit(t, dir, "checkout", "-q", "main")
	if _, err := aiRunErr(tl, map[string]any{"op": "delete", "name": "feat2"}); err == nil || !strings.Contains(err.Error(), "force") {
		t.Errorf("delete of an unmerged branch: %v, want a refusal that names force", err)
	}
	if out := aiRun(t, tl, map[string]any{"op": "merge", "name": "feat2"}); !strings.Contains(out, "merged") {
		t.Errorf("merge %q", out)
	}
	if s := covGit(t, dir, "log", "-1", "--format=%s"); s != "add b" {
		t.Errorf("main at %q after the merge, want add b", s)
	}
	aiRun(t, tl, map[string]any{"op": "delete", "name": "feat2"})
	if out := covGit(t, dir, "branch", "--list", "feat2"); out != "" {
		t.Errorf("feat2 still there: %q", out)
	}

	bare := aiRemoteRepo(t, dir)
	aiRun(t, tl, map[string]any{"op": "unset_upstream"})
	if _, err := aiRunErr(aiGitToolAt(t, m, dir, "git_branch"), map[string]any{"op": "list"}); err != nil {
		t.Fatal(err)
	}
	aiRun(t, tl, map[string]any{"op": "set_upstream", "upstream": "origin/main"})
	if u := covGit(t, dir, "rev-parse", "--abbrev-ref", "main@{upstream}"); u != "origin/main" {
		t.Errorf("upstream %q", u)
	}
	covGit(t, dir, "push", "-q", "origin", "main:gone")
	covGit(t, dir, "fetch", "-q")
	covRefresh(m)
	out := aiRun(t, tl, map[string]any{"op": "delete_remote", "name": "origin/gone"})
	if !m.confirmOpen || m.confirmKind != confirmKindGitDeleteRemote {
		t.Errorf("delete_remote opened no GUI card (%v, kind %v): %q", m.confirmOpen, m.confirmKind, out)
	}
	if !strings.Contains(out, "type") {
		t.Errorf("delete_remote %q, want it to say the user types the name", out)
	}
	if got := covGit(t, bare, "branch", "--list", "gone"); got == "" {
		t.Error("the remote branch went without the user typing its name")
	}
}

// Spec: git_tag does the GUI's tag actions: list, show, create
// (annotated with a message), delete, push (one or all), checkout
// (detached) and delete on the remote. Create asks with Always allow
// git; delete, push and checkout on a Cancel-first card; delete_remote
// opens the GUI's card, where the user types the tag name.
func TestAIGitTagOps(t *testing.T) {
	m, dir := covGitRepo(t)
	bare := aiRemoteRepo(t, dir)
	tl := aiGitToolAt(t, m, dir, "git_tag")
	aiWantAtOnce(t, tl, map[string]any{"op": "list"})
	aiWantAtOnce(t, tl, map[string]any{"op": "show", "name": "v1"})
	aiWantAtOnce(t, tl, map[string]any{"op": "delete_remote", "name": "v1"})
	aiWantAlways(t, tl, map[string]any{"op": "create"}, ai.ClassGit)
	for _, op := range []string{"delete", "push", "checkout"} {
		aiWantCancelFirst(t, tl, map[string]any{"op": op})
	}
	aiRun(t, tl, map[string]any{"op": "create", "name": "v1", "message": "first release"})
	if out := aiRun(t, tl, map[string]any{"op": "list"}); !strings.Contains(out, "v1") {
		t.Errorf("list %q", out)
	}
	if out := aiRun(t, tl, map[string]any{"op": "show", "name": "v1"}); !strings.Contains(out, "first release") || !strings.Contains(out, "annotated") {
		t.Errorf("show %q", out)
	}
	aiRun(t, tl, map[string]any{"op": "push", "name": "v1"})
	if got := covGit(t, bare, "tag", "--list", "v1"); got != "v1" {
		t.Errorf("remote tags %q after push", got)
	}
	covWrite(t, dir, "a.txt", "changed\n")
	covCommit(t, dir, "second")
	aiRun(t, tl, map[string]any{"op": "checkout", "name": "v1"})
	if s := covGit(t, dir, "log", "-1", "--format=%s"); s != "first" {
		t.Errorf("HEAD at %q after checkout of v1", s)
	}
	covGit(t, dir, "checkout", "-q", "main")
	covRefresh(m)
	aiRun(t, tl, map[string]any{"op": "delete_remote", "name": "v1"})
	if !m.confirmOpen || m.confirmKind != confirmKindGitTagDeleteRemote {
		t.Errorf("delete_remote opened no GUI card (%v, kind %v)", m.confirmOpen, m.confirmKind)
	}
	m.confirmOpen = false
	aiRun(t, tl, map[string]any{"op": "delete", "name": "v1"})
	if got := covGit(t, dir, "tag", "--list"); got != "" {
		t.Errorf("tags %q after delete", got)
	}
	if got := covGit(t, bare, "tag", "--list", "v1"); got != "v1" {
		t.Error("delete removed the remote tag too")
	}
}

// Spec: git_remote does the GUI's remote actions: list, add, rename,
// set_url, remove, fetch_all, incoming / outgoing commits, push_to and
// pull_from a remote branch, and force_push. Remove and push_to ask on a
// Cancel-first card; add, rename, set_url, fetch_all and pull_from with
// Always allow git; force_push opens the GUI's force push card, where the
// user types the branch name.
func TestAIGitRemoteOps(t *testing.T) {
	m, dir := covGitRepo(t)
	bare := aiRemoteRepo(t, dir)
	tl := aiGitToolAt(t, m, dir, "git_remote")
	for _, op := range []string{"list", "incoming", "outgoing", "force_push"} {
		aiWantAtOnce(t, tl, map[string]any{"op": op})
	}
	for _, op := range []string{"add", "rename", "set_url", "fetch_all", "pull_from"} {
		aiWantAlways(t, tl, map[string]any{"op": op}, ai.ClassGit)
	}
	for _, op := range []string{"remove", "push_to"} {
		aiWantCancelFirst(t, tl, map[string]any{"op": op})
	}
	if out := aiRun(t, tl, map[string]any{"op": "list"}); !strings.Contains(out, "origin") || !strings.Contains(out, bare) {
		t.Errorf("list %q", out)
	}
	aiRun(t, tl, map[string]any{"op": "add", "name": "up", "url": bare})
	aiRun(t, tl, map[string]any{"op": "rename", "name": "up", "new_name": "upstream"})
	aiRun(t, tl, map[string]any{"op": "set_url", "name": "upstream", "url": bare + "/"})
	if got := covGit(t, dir, "remote", "get-url", "upstream"); got != bare+"/" {
		t.Errorf("upstream url %q", got)
	}
	aiRun(t, tl, map[string]any{"op": "remove", "name": "upstream"})
	if got := covGit(t, dir, "remote"); got != "origin" {
		t.Errorf("remotes %q after remove", got)
	}
	covWrite(t, dir, "a.txt", "ahead\n")
	covCommit(t, dir, "ahead one")
	if out := aiRun(t, tl, map[string]any{"op": "outgoing"}); !strings.Contains(out, "ahead one") {
		t.Errorf("outgoing %q", out)
	}
	aiRun(t, tl, map[string]any{"op": "push_to", "remote": "origin", "branch": "other"})
	if got := covGit(t, bare, "log", "-1", "--format=%s", "other"); got != "ahead one" {
		t.Errorf("origin/other at %q", got)
	}
	aiRun(t, tl, map[string]any{"op": "fetch_all"})
	covGit(t, dir, "reset", "-q", "--hard", "HEAD~1")
	if out := aiRun(t, tl, map[string]any{"op": "pull_from", "remote": "origin", "branch": "other"}); !strings.Contains(out, "pulled") {
		t.Errorf("pull_from %q", out)
	}
	if s := covGit(t, dir, "log", "-1", "--format=%s"); s != "ahead one" {
		t.Errorf("HEAD at %q after pull_from", s)
	}
	covGit(t, dir, "push", "-q", "origin", "main")
	covGit(t, dir, "commit", "-q", "--amend", "-m", "rewritten")
	covRefresh(m)
	aiRun(t, tl, map[string]any{"op": "force_push"})
	if !m.confirmOpen || m.confirmKind != confirmKindGitForcePush {
		t.Errorf("force_push opened no GUI card (%v, kind %v)", m.confirmOpen, m.confirmKind)
	}
	if got := covGit(t, bare, "log", "-1", "--format=%s", "main"); got != "ahead one" {
		t.Errorf("origin/main at %q: it was force pushed without the typed name", got)
	}
}
