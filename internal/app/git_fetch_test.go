package app

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/amin-jalali/termocode/internal/git"
)

// The full git read (fetchGitCmd) runs its reads at once; it must give
// what reading them one after another gives, and IsRepo false (nothing
// else) outside a repository.
func TestFetchGitCmdSameAsSequentialReads(t *testing.T) {
	m, run := gitIdleRepo(t)
	_ = m
	_ = os.WriteFile(filepath.Join("sub", "a.txt"), []byte("changed\n"), 0o644)
	_ = os.WriteFile("new.txt", []byte("n\n"), 0o644)
	run("tag", "v1")
	cwd, _ := os.Getwd()
	got, ok := fetchGitCmd()().(GitMsg)
	got.Seq = 0 // the read order number is new each call (FIX-46); the snapshot is what counts
	if !ok {
		t.Fatal("no GitMsg")
	}
	branch, _ := git.GetBranch(cwd)
	files, _ := git.Status(cwd)
	graph, more, _ := git.Commits(cwd, 0, gitGraphLimit)
	want := GitMsg{IsRepo: true, Branch: branch, Files: files, Graph: graph, More: more, Rebase: git.RebaseStatus(cwd)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fetchGitCmd:\n got %+v\nwant %+v", got, want)
	}
	if len(got.Files) != 2 || len(got.Graph) == 0 || got.Branch.Name != "main" {
		t.Fatalf("unexpected snapshot %+v", got)
	}

	t.Chdir(t.TempDir())
	out, _ := fetchGitCmd()().(GitMsg)
	out.Seq = 0
	if got := out; !reflect.DeepEqual(got, GitMsg{IsRepo: false}) {
		t.Fatalf("outside a repo: %+v", got)
	}
}
