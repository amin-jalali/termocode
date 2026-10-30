package webproto

import (
	"strings"
	"testing"
)

// Spec (WEB-41): the file ops. New file, new folder and rename are
// controller ops with an audit line; delete waits for a laptop card. A
// path is never the root and never has a ".." part; a new name is one
// plain name (no slash, not "." or "..", no control character).
func TestFileOpsArgs(t *testing.T) {
	for _, op := range []string{OpTreeNewFile, OpTreeNewDir, OpTreeRename} {
		p, ok := Lookup(op)
		if !ok || p.Role != RoleController || p.Audit != AuditYes || p.LaptopCard {
			t.Errorf("%s: %+v", op, p)
		}
	}
	good := map[string]string{
		OpTreeNewFile: `{"path":"src/new.go"}`,
		OpTreeNewDir:  `{"path":"src/pkg"}`,
		OpTreeRename:  `{"path":"src/a.go","name":"b.go"}`,
	}
	if p, _ := Lookup(OpTreeDelete); p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes {
		t.Errorf("tree.delete: %+v", p)
	}
	good[OpTreeDelete] = `{"path":"src/a.go"}`
	for op, args := range good {
		if _, err := DecodeArgs(op, []byte(args)); err != nil {
			t.Errorf("%s %s: %v", op, args, err)
		}
	}
	for _, c := range []struct{ op, args string }{
		{OpTreeNewFile, `{"path":""}`},
		{OpTreeNewFile, `{"path":"../x"}`},
		{OpTreeNewFile, `{"path":"/etc/x"}`},
		{OpTreeNewDir, `{"path":"a/../b"}`},
		{OpTreeDelete, `{"path":""}`},
		{OpTreeRename, `{"path":"a.go","name":""}`},
		{OpTreeRename, `{"path":"a.go","name":"x/y"}`},
		{OpTreeRename, `{"path":"a.go","name":".."}`},
		{OpTreeRename, `{"path":"a.go","name":"a\\b"}`},
		{OpTreeRename, `{"path":"a.go","name":"a\u0007"}`},
		{OpTreeRename, `{"path":"","name":"b"}`},
		{OpTreeRename, `{"path":"a.go","name":"` + strings.Repeat("n", 256) + `"}`},
	} {
		if _, err := DecodeArgs(c.op, []byte(c.args)); err == nil {
			t.Errorf("%s %s: accepted", c.op, c.args)
		}
	}
}

// Spec (WEB-42): push, pull, checkout and stash wait for a laptop card;
// the branch list is a read. A branch name that git would read as an
// option, or that is not a plain ref name, is refused on the wire.
func TestGitMoreOps(t *testing.T) {
	for _, op := range []string{OpGitPush, OpGitPull, OpGitCheckout, OpGitStash, OpGitStashPop} {
		if p, ok := Lookup(op); !ok || p.Role != RoleController || !p.LaptopCard || p.Audit != AuditYes {
			t.Errorf("%s: %+v", op, p)
		}
	}
	if p, _ := Lookup(OpGitBranches); p.Role != RoleViewer || p.LaptopCard {
		t.Errorf("git.branches: %+v", p)
	}
	for _, b := range []string{`{"branch":"main"}`, `{"branch":"feat/web-v2"}`} {
		if _, err := DecodeArgs(OpGitCheckout, []byte(b)); err != nil {
			t.Errorf("%s: %v", b, err)
		}
	}
	for _, b := range []string{`{"branch":""}`, `{"branch":"-f"}`, `{"branch":"--orphan=x"}`, `{"branch":"a b"}`, `{"branch":"a..b"}`, `{"branch":"x\u0000"}`, `{"branch":"HEAD~1"}`} {
		if _, err := DecodeArgs(OpGitCheckout, []byte(b)); err == nil {
			t.Errorf("%s accepted", b)
		}
	}
	if _, err := DecodeArgs(OpGitStash, []byte(`{"message":"wip\u001b[2J"}`)); err == nil {
		t.Error("a stash message with a control character")
	}
}
