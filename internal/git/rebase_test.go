package git

import (
	"errors"
	"fmt"
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
