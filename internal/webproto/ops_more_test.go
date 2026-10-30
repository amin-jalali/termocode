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
