package ledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec (plan §4.3): seen.json per workflow: key → {run, at, result};
// Seen(key) survives a restart; Forget (CLI --again) drops one key.
func TestSeenItems(t *testing.T) {
	root := t.TempDir()
	s, err := OpenSeen(root, "weekday-bugs")
	if err != nil {
		t.Fatal(err)
	}
	if s.Has("LIN-228") {
		t.Fatal("seen before marked")
	}
	if err := s.Mark("LIN-228", "r1", "done"); err != nil {
		t.Fatal(err)
	}
	re, _ := OpenSeen(root, "weekday-bugs")
	got, ok := re.Get("LIN-228")
	if !ok || got.Run != "r1" || got.Result != "done" || got.AtMs == 0 {
		t.Fatalf("after reopen: %+v %v", got, ok)
	}
	if other, _ := OpenSeen(root, "other"); other.Has("LIN-228") {
		t.Fatal("seen leaks to another workflow")
	}
	if err := re.Forget("LIN-228"); err != nil {
		t.Fatal(err)
	}
	if again, _ := OpenSeen(root, "weekday-bugs"); again.Has("LIN-228") {
		t.Fatal("Forget did not stick")
	}
	if _, err := os.Stat(filepath.Join(root, "weekday-bugs.seen.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenSeen(root, "../x"); err == nil {
		t.Fatal("bad name accepted")
	}
	// The key is outside data: it is stored redacted.
	tok := "sk-" + "test0123456789abcdefghijklmnop"
	_ = re.Mark("k", "r1", "failed: key="+tok)
	b, _ := os.ReadFile(filepath.Join(root, "weekday-bugs.seen.json"))
	if strings.Contains(string(b), tok) {
		t.Fatalf("secret in seen.json: %s", b)
	}
}
