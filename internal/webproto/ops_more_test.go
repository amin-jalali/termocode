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

// Spec (WEB-44): the container reads are viewer ops; start and restart
// are controller ops; stop and remove wait for the laptop. A container is
// named only by its hex id, which no engine reads as an option.
func TestContainerOps(t *testing.T) {
	want := map[string]struct {
		role Role
		card bool
	}{
		OpContainersList: {RoleViewer, false}, OpContainersLogs: {RoleViewer, false},
		OpContainersStart: {RoleController, false}, OpContainersRestart: {RoleController, false},
		OpContainersStop: {RoleController, true}, OpContainersRemove: {RoleController, true},
	}
	for op, w := range want {
		if p, ok := Lookup(op); !ok || p.Role != w.role || p.LaptopCard != w.card {
			t.Errorf("%s: %+v", op, p)
		}
	}
	if _, err := DecodeArgs(OpContainersStop, []byte(`{"id":"9f3c2e1a1234"}`)); err != nil {
		t.Error(err)
	}
	for _, a := range []string{`{"id":""}`, `{"id":"-f"}`, `{"id":"nginx"}`, `{"id":"9F3C2E1A1234"}`, `{"id":"9f3c"}`} {
		if _, err := DecodeArgs(OpContainersStop, []byte(a)); err == nil {
			t.Errorf("%s accepted", a)
		}
	}
	if _, err := DecodeArgs(OpContainersLogs, []byte(`{"id":"9f3c2e1a1234","lines":5000}`)); err == nil {
		t.Error("5000 log lines accepted")
	}
}

// Spec (WEB-43): browsing a database is a read; a typed query is a
// controller op; a write waits for the laptop. SQL is one non-empty text
// without NUL, capped.
func TestDBOps(t *testing.T) {
	for _, op := range []string{OpDBConns, OpDBTables, OpDBRows} {
		if p, _ := Lookup(op); p.Role != RoleViewer || p.LaptopCard {
			t.Errorf("%s: %+v", op, p)
		}
	}
	if p, _ := Lookup(OpDBQuery); p.Role != RoleController || p.LaptopCard || p.Audit != AuditYes {
		t.Errorf("db.query: %+v", p)
	}
	if p, _ := Lookup(OpDBExec); p.Role != RoleController || !p.LaptopCard {
		t.Errorf("db.exec: %+v", p)
	}
	for _, a := range []string{`{"conn":"local","sql":""}`, `{"conn":"local","sql":"  "}`, `{"conn":"","sql":"select 1"}`, `{"conn":"local","sql":"select\u0000 1"}`} {
		if _, err := DecodeArgs(OpDBQuery, []byte(a)); err == nil {
			t.Errorf("%s accepted", a)
		}
	}
	if _, err := DecodeArgs(OpDBRows, []byte(`{"conn":"local","table":"users","from":-1}`)); err == nil {
		t.Error("a negative page accepted")
	}
}

// Spec (WEB-49): transfers are controller ops; replacing a file with an
// upload waits for the laptop; a chunk is valid base64 of at most
// TransferChunk bytes (decoded on the wire, before the hub); sizes and
// offsets stay under MaxTransfer; an upload id is short hex.
func TestTransferOps(t *testing.T) {
	for _, op := range []string{OpFileDownload, OpFileUploadBegin, OpFileUploadChunk, OpFileUploadSave, OpFileUploadReplace, OpFileUploadCancel} {
		p, _ := Lookup(op)
		if p.Role != RoleController || p.LaptopCard != (op == OpFileUploadReplace) {
			t.Errorf("%s: %+v", op, p)
		}
	}
	a, err := DecodeArgs(OpFileUploadChunk, []byte(`{"id":"0a1b","off":0,"data":"aGVsbG8="}`))
	if err != nil || string(a.(*UploadChunkArgs).Bytes()) != "hello" {
		t.Fatalf("chunk %v %v", a, err)
	}
	big := strings.Repeat("A", (TransferChunk+3)/3*4+4)
	for _, bad := range []string{`{"id":"0a1b","off":0,"data":"!!"}`, `{"id":"0a1b","off":0,"data":""}`, `{"id":"XYZ","off":0,"data":"aGk="}`,
		`{"id":"0a1b","off":-1,"data":"aGk="}`, `{"id":"0a1b","off":0,"data":"` + big + `"}`} {
		if _, err := DecodeArgs(OpFileUploadChunk, []byte(bad)); err == nil {
			t.Errorf("%.80s accepted", bad)
		}
	}
	for _, bad := range []string{`{"path":"a/","size":1}`, `{"path":"x","size":-1}`, `{"path":"x","size":268435457}`, `{"path":"../x","size":1}`} {
		if _, err := DecodeArgs(OpFileUploadBegin, []byte(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
