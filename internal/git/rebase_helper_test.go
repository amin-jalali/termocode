package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// G-RBS: the sequence-editor helper writes the prepared todo over the
// file git hands it, and git reaches it through a shell string that holds
// only quoted paths (here the test binary, re-run like termocode).
func TestRebaseHelperWritesTodo(t *testing.T) {
	dir := t.TempDir()
	plan, cleanup, err := writeRebasePlan(rebasePlan{Todo: "pick abc it's \"quoted\" $HOME\n"})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if fi, err := os.Stat(filepath.Dir(plan)); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("plan dir mode %v %v", fi.Mode(), err)
	}
	file := filepath.Join(dir, "git-rebase-todo")
	if err := os.WriteFile(file, []byte("pick old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// git runs an editor as: sh -c '<editor> "$@"' <editor> <file>
	cmdline := helperCommand(exe, "seq", plan)
	sh := exec.Command("sh", "-c", cmdline+` "$@"`, cmdline, file)
	if out, err := sh.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if b, _ := os.ReadFile(file); string(b) != "pick abc it's \"quoted\" $HOME\n" {
		t.Fatalf("todo %q", b)
	}
	cleanup()
	if _, err := os.Stat(plan); !os.IsNotExist(err) {
		t.Errorf("plan left behind: %v", err)
	}
}

// G-RBS: with an empty todo (autosquash) the helper keeps git's own list.
func TestRebaseHelperKeepsGitsTodo(t *testing.T) {
	plan, cleanup, err := writeRebasePlan(rebasePlan{})
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	file := filepath.Join(t.TempDir(), "git-rebase-todo")
	_ = os.WriteFile(file, []byte("pick a\nfixup b\n"), 0o644)
	if code := runRebaseHelper([]string{"seq", plan, file}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if b, _ := os.ReadFile(file); string(b) != "pick a\nfixup b\n" {
		t.Fatalf("todo %q", b)
	}
	if code := runRebaseHelper([]string{"nope"}); code == 0 {
		t.Error("bad args exit 0")
	}
}
