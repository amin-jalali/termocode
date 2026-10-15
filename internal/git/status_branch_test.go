package git

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// StatusBranch must give the same answer as GetBranch + Status (which run
// three or four git processes) in every state the Source Control view shows.
func TestStatusBranchMatchesSeparateCalls(t *testing.T) {
	r := newGitTestRepo(t)
	check := func(name string) {
		t.Helper()
		wantB, err := GetBranch(r.dir)
		if err != nil {
			t.Fatalf("%s: GetBranch: %v", name, err)
		}
		wantF, _ := Status(r.dir)
		got, err := StatusBranch(r.dir)
		if err != nil {
			t.Fatalf("%s: StatusBranch: %v", name, err)
		}
		if got.Branch != wantB {
			t.Errorf("%s: branch = %+v, want %+v", name, got.Branch, wantB)
		}
		if !reflect.DeepEqual(got.Files, wantF) {
			t.Errorf("%s: files = %q, want %q", name, got.Files, wantF)
		}
	}
	check("empty repo")
	r.write(t, "a.txt", "a\n")
	r.write(t, "old name.txt", "o\n")
	r.write(t, "d.txt", "d\n")
	r.git(t, "add", ".")
	r.git(t, "commit", "-q", "-m", "one")
	check("clean")
	head := strings.TrimSpace(r.git(t, "rev-parse", "HEAD"))
	if got, _ := StatusBranch(r.dir); got.OID != head {
		t.Errorf("OID = %q, want %q", got.OID, head)
	}

	r.write(t, "a.txt", "changed\n")
	r.write(t, "new file ü.txt", "n\n")
	r.write(t, "sub/x.txt", "x\n")
	r.git(t, "mv", "old name.txt", "renamed.txt")
	_ = os.Remove(filepath.Join(r.dir, "d.txt"))
	r.write(t, "staged.txt", "s\n")
	r.git(t, "add", "staged.txt")
	r.write(t, "staged.txt", "s2\n")
	check("dirty")

	// Upstream with ahead/behind counts.
	r.git(t, "add", "-A")
	r.git(t, "commit", "-q", "-m", "two")
	r.git(t, "branch", "up", "HEAD~1")
	r.git(t, "branch", "--set-upstream-to=up")
	check("ahead")
	r.git(t, "checkout", "-q", "up")
	r.write(t, "u.txt", "u\n")
	r.git(t, "add", "u.txt")
	r.git(t, "commit", "-q", "-m", "up")
	r.git(t, "checkout", "-q", "main")
	check("ahead and behind")

	r.git(t, "checkout", "-q", "--detach", "HEAD")
	check("detached")
}

func TestStatusBranchNotARepo(t *testing.T) {
	if _, err := StatusBranch(t.TempDir()); err == nil {
		t.Fatal("want an error outside a repo")
	}
}
