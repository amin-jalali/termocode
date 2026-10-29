package git

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

// rebaseRepo makes a repo with commits c1..cN (one file each, c1 first)
// and returns it with the full hashes, oldest first.
func rebaseRepo(t *testing.T, n int) (*gitTestRepo, []string) {
	t.Helper()
	r := newGitTestRepo(t)
	var hashes []string
	for i := 1; i <= n; i++ {
		r.write(t, fmt.Sprintf("f%d.txt", i), fmt.Sprintf("file %d\n", i))
		r.git(t, "add", "-A")
		r.git(t, "commit", "-q", "-m", fmt.Sprintf("c%d", i), "-m", fmt.Sprintf("body %d", i))
		hashes = append(hashes, strings.TrimSpace(r.git(t, "rev-parse", "HEAD")))
	}
	return r, hashes
}

// subjects lists HEAD's history subjects, oldest first.
func subjects(t *testing.T, r *gitTestRepo) []string {
	t.Helper()
	return strings.Fields(r.git(t, "log", "--reverse", "--format=%s"))
}

// G-RBS: the todo lists base..HEAD oldest first, every row a pick with its
// subject; base "" means from the root commit.
func TestLoadRebaseTodo(t *testing.T) {
	r, h := rebaseRepo(t, 4)
	todo, err := LoadRebaseTodo(r.dir, h[0])
	if err != nil {
		t.Fatal(err)
	}
	if todo.Base != h[0] || len(todo.Items) != 3 {
		t.Fatalf("todo %+v", todo)
	}
	for i, it := range todo.Items {
		if it.Action != RebasePick || it.FullHash != h[i+1] || it.Subject != fmt.Sprintf("c%d", i+2) {
			t.Errorf("row %d %+v", i, it)
		}
		if !strings.HasPrefix(it.FullHash, it.Hash) || len(it.Hash) < 7 || it.Body != fmt.Sprintf("body %d", i+2) {
			t.Errorf("row %d hash/body %+v", i, it)
		}
	}
	root, err := LoadRebaseTodo(r.dir, "")
	if err != nil || len(root.Items) != 4 || root.Items[0].Subject != "c1" {
		t.Fatalf("root todo %+v %v", root, err)
	}
}

// G-RBS: a merge commit in the range is refused with a clear message.
func TestLoadRebaseTodoRefusesMerges(t *testing.T) {
	r, h := rebaseRepo(t, 2)
	r.git(t, "checkout", "-q", "-b", "side")
	r.write(t, "side.txt", "side\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "side")
	r.git(t, "checkout", "-q", "main")
	r.write(t, "main.txt", "main\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "main")
	r.git(t, "merge", "-q", "--no-ff", "-m", "merge side", "side")
	_, err := LoadRebaseTodo(r.dir, h[0])
	if !errors.Is(err, ErrRebaseMerge) || !strings.Contains(err.Error(), "merge commit") {
		t.Fatalf("err %v", err)
	}
}

// G-RBS: an empty range (base is HEAD) has nothing to rebase.
func TestLoadRebaseTodoEmpty(t *testing.T) {
	r, h := rebaseRepo(t, 2)
	if _, err := LoadRebaseTodo(r.dir, h[1]); !errors.Is(err, ErrRebaseEmpty) {
		t.Fatalf("err %v", err)
	}
}

// todoOf is a todo of rows named a, b, c … (no git needed).
func todoOf(names ...string) RebaseTodo {
	var t RebaseTodo
	for _, n := range names {
		t.Items = append(t.Items, RebaseItem{Action: RebasePick, Hash: n, FullHash: n, Subject: n})
	}
	return t
}

func order(t RebaseTodo) string {
	var s []string
	for _, it := range t.Items {
		s = append(s, it.Hash)
	}
	return strings.Join(s, "")
}

// G-RBS: Move swaps a row with its neighbour and returns its new index;
// at either end it stays put.
func TestRebaseTodoMove(t *testing.T) {
	todo := todoOf("a", "b", "c")
	if i := todo.Move(0, 1); i != 1 || order(todo) != "bac" {
		t.Fatalf("down: %d %s", i, order(todo))
	}
	if i := todo.Move(1, -1); i != 0 || order(todo) != "abc" {
		t.Fatalf("up: %d %s", i, order(todo))
	}
	if i := todo.Move(0, -1); i != 0 || order(todo) != "abc" {
		t.Fatalf("top: %d %s", i, order(todo))
	}
	if i := todo.Move(2, 1); i != 2 || order(todo) != "abc" {
		t.Fatalf("bottom: %d %s", i, order(todo))
	}
	if i := todo.Move(7, 1); i != 7 || order(todo) != "abc" {
		t.Fatalf("out of range: %d %s", i, order(todo))
	}
}

// G-RBS: the preview counts the commits left: squash and fixup meld into
// the row above, drop removes one.
func TestRebaseTodoSummary(t *testing.T) {
	todo := todoOf("a", "b", "c", "d", "e")
	if got := todo.Summary(); got != "5 commits → 5 commits" {
		t.Errorf("all picks: %q", got)
	}
	todo.SetAction(1, RebaseSquash)
	todo.SetAction(2, RebaseFixup)
	todo.SetAction(4, RebaseDrop)
	if got := todo.Summary(); got != "5 commits → 2 commits" {
		t.Errorf("mixed: %q", got)
	}
	todo.SetAction(3, RebaseDrop)
	if got := todo.Summary(); got != "5 commits → 1 commit" {
		t.Errorf("one left: %q", got)
	}
	todo.SetAction(9, RebaseDrop) // out of range: ignored
}

// G-RBS: Validate refuses a todo git cannot run or that loses everything.
func TestRebaseTodoValidate(t *testing.T) {
	todo := todoOf("a", "b")
	if err := todo.Validate(); err != nil {
		t.Errorf("picks: %v", err)
	}
	todo.SetAction(0, RebaseFixup)
	if err := todo.Validate(); err == nil || !strings.Contains(err.Error(), "nothing above") {
		t.Errorf("fixup first: %v", err)
	}
	todo.SetAction(0, RebaseDrop)
	todo.SetAction(1, RebaseSquash)
	if err := todo.Validate(); err == nil || !strings.Contains(err.Error(), "nothing above") {
		t.Errorf("squash after only drops: %v", err)
	}
	todo.SetAction(1, RebaseDrop)
	if err := todo.Validate(); err == nil || !strings.Contains(err.Error(), "every commit") {
		t.Errorf("all dropped: %v", err)
	}
}

// G-RBS: the todo file has one "<action> <full hash> <subject>" line per
// row, in the todo's order; drop is written out, not left away.
func TestRebaseTodoFormat(t *testing.T) {
	todo := RebaseTodo{Items: []RebaseItem{
		{Action: RebasePick, FullHash: "aaa", Subject: "first"},
		{Action: RebaseReword, FullHash: "bbb", Subject: "second", Message: "new"},
		{Action: RebaseFixup, FullHash: "ccc", Subject: "third # not a comment"},
		{Action: RebaseDrop, FullHash: "ddd", Subject: "fourth"},
	}}
	want := "pick aaa first\nreword bbb second\nfixup ccc third # not a comment\ndrop ddd fourth\n"
	if got := todo.Format(); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// testRebaseOpts runs the helper from this test binary (TestMain).
func testRebaseOpts(t *testing.T) RebaseOptions {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return RebaseOptions{Exe: exe}
}

// G-RBS: RunRebase replays the prepared todo: rows reordered, one squashed
// into the row above (both messages kept), one fixup (its message
// dropped), one dropped. The work tree ends clean and ORIG_HEAD is the
// old HEAD.
func TestRunRebaseAppliesTodo(t *testing.T) {
	r, h := rebaseRepo(t, 5)
	todo, err := LoadRebaseTodo(r.dir, h[0]) // c2 c3 c4 c5
	if err != nil {
		t.Fatal(err)
	}
	todo.Move(2, 1)                 // c2 c3 c5 c4
	todo.SetAction(1, RebaseSquash) // c3 into c2
	todo.SetAction(2, RebaseDrop)   // c5 gone
	todo.SetAction(3, RebaseFixup)  // c4 into c2 (no message)
	out, err := RunRebase(r.dir, todo, testRebaseOpts(t))
	if err != nil || out.Stopped {
		t.Fatalf("rebase: %+v %v", out, err)
	}
	if got := strings.Join(subjects(t, r), " "); got != "c1 c2" {
		t.Fatalf("history %q", got)
	}
	msg := r.git(t, "log", "-1", "--format=%B")
	if !strings.Contains(msg, "body 2") || !strings.Contains(msg, "c3") || strings.Contains(msg, "c4") {
		t.Errorf("squashed message %q", msg)
	}
	files := r.git(t, "ls-files")
	if !strings.Contains(files, "f4.txt") || strings.Contains(files, "f5.txt") {
		t.Errorf("files %q", files)
	}
	if st := r.git(t, "status", "--porcelain"); st != "" {
		t.Errorf("dirty after rebase: %q", st)
	}
	if orig := strings.TrimSpace(r.git(t, "rev-parse", "ORIG_HEAD")); orig != h[4] {
		t.Errorf("ORIG_HEAD %s, want %s", orig, h[4])
	}
}

// G-RBS: a todo git cannot run is refused before git starts.
func TestRunRebaseRefusesInvalidTodo(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	todo, _ := LoadRebaseTodo(r.dir, h[0])
	todo.SetAction(0, RebaseFixup)
	if _, err := RunRebase(r.dir, todo, testRebaseOpts(t)); err == nil {
		t.Fatal("no error")
	}
	if head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD")); head != h[2] {
		t.Errorf("HEAD moved to %s", head)
	}
}

// G-RBS: a reword row gets the message typed in termocode. The message
// reaches git through the plan file, never a shell: quotes, $( ) and
// backticks stay as typed. A fixup into the reworded row keeps the new
// message; a squash elsewhere keeps git's combined message.
func TestRunRebaseRewords(t *testing.T) {
	r, h := rebaseRepo(t, 5)
	todo, err := LoadRebaseTodo(r.dir, h[0]) // c2 c3 c4 c5
	if err != nil {
		t.Fatal(err)
	}
	newMsg := "new \"c2\" $(touch pwned) `id` it's\n\nnew body"
	todo.SetAction(0, RebaseReword)
	todo.Items[0].Message = newMsg
	todo.SetAction(1, RebaseFixup)  // c3 into the reworded c2
	todo.SetAction(3, RebaseSquash) // c5 into c4
	todo.SetAction(2, RebaseReword) // c4, no message typed: keeps its own
	if out, err := RunRebase(r.dir, todo, testRebaseOpts(t)); err != nil || out.Stopped {
		t.Fatalf("rebase: %+v %v", out, err)
	}
	log := r.git(t, "log", "--reverse", "--format=%B%x1e")
	msgs := strings.Split(strings.TrimSuffix(strings.TrimSpace(log), "\x1e"), "\x1e")
	if len(msgs) != 3 {
		t.Fatalf("messages %q", msgs)
	}
	if got := strings.TrimSpace(msgs[1]); got != newMsg {
		t.Errorf("reworded %q, want %q", got, newMsg)
	}
	if got := msgs[2]; !strings.Contains(got, "c4") || !strings.Contains(got, "c5") {
		t.Errorf("squash message %q", got)
	}
	if _, err := os.Stat(r.dir + "/pwned"); err == nil {
		t.Error("the message ran in a shell")
	}
}

// G-RBS: with uncommitted changes the rebase is refused (ErrRebaseDirty)
// unless Autostash is on; then git stashes them and puts them back.
// Untracked files do not count.
func TestRunRebaseDirtyTree(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	r.write(t, "untracked.txt", "new\n")
	if WorkTreeDirty(r.dir) {
		t.Fatal("an untracked file made the tree dirty")
	}
	r.write(t, "f1.txt", "edited\n")
	if !WorkTreeDirty(r.dir) {
		t.Fatal("an edit did not make the tree dirty")
	}
	todo, _ := LoadRebaseTodo(r.dir, h[0])
	todo.SetAction(1, RebaseDrop)
	opt := testRebaseOpts(t)
	if _, err := RunRebase(r.dir, todo, opt); !errors.Is(err, ErrRebaseDirty) {
		t.Fatalf("dirty: %v", err)
	}
	if head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD")); head != h[2] {
		t.Fatalf("HEAD moved")
	}
	opt.Autostash = true
	if out, err := RunRebase(r.dir, todo, opt); err != nil || out.Stopped {
		t.Fatalf("autostash: %+v %v", out, err)
	}
	if got := strings.Join(subjects(t, r), " "); got != "c1 c2" {
		t.Errorf("history %q", got)
	}
	if b, _ := os.ReadFile(r.dir + "/f1.txt"); string(b) != "edited\n" {
		t.Errorf("stashed edit not back: %q", b)
	}
}

// G-RBS: RebasePushed counts the commits of the range that are on the
// branch's upstream already (a rebase rewrites them: force-push needed).
func TestRebasePushed(t *testing.T) {
	r, h := rebaseRepo(t, 3)
	if up, n := RebasePushed(r.dir, h[0]); up != "" || n != 0 {
		t.Fatalf("no upstream: %q %d", up, n)
	}
	remote := t.TempDir()
	r.git(t, "init", "-q", "--bare", remote)
	r.git(t, "remote", "add", "origin", remote)
	r.git(t, "push", "-q", "-u", "origin", "main")
	r.write(t, "f4.txt", "4\n")
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "c4")
	if up, n := RebasePushed(r.dir, h[0]); up != "origin/main" || n != 2 {
		t.Errorf("c2 c3 pushed: %q %d", up, n)
	}
	if up, n := RebasePushed(r.dir, h[2]); up != "origin/main" || n != 0 {
		t.Errorf("only c4, not pushed: %q %d", up, n)
	}
	if _, n := RebasePushed(r.dir, ""); n != 3 {
		t.Errorf("from the root: %d", n)
	}
}
