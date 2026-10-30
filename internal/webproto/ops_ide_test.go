package webproto

import (
	"encoding/json"
	"errors"
	"testing"
)

// Spec (explorer TIMELINE): git.file_log lists the commits that changed
// one workspace file; a v2 read for viewers, not audited. git.show takes
// an optional path to show only that file of the commit.
func TestGitFileLogOp(t *testing.T) {
	for _, bad := range []string{`{"path":""}`, `{"path":"/etc/passwd"}`, `{"path":"a/../../x"}`} {
		if _, err := DecodeArgs(OpGitFileLog, json.RawMessage(bad)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v, want invalid", bad, err)
		}
	}
	if _, err := DecodeArgs(OpGitFileLog, json.RawMessage(`{"path":"src/a.go"}`)); err != nil {
		t.Error(err)
	}
	p, ok := Lookup(OpGitFileLog)
	if !ok || p.Role != RoleViewer || p.Phase != PhaseV2 || p.Audit != AuditNo || p.LaptopCard {
		t.Errorf("git.file_log row %+v", p)
	}
	if _, ok := ResultOf(OpGitFileLog); !ok {
		t.Error("git.file_log has no result type")
	}
	if _, err := DecodeArgs(OpGitShow, json.RawMessage(`{"hash":"abcd1234","path":"src/a.go"}`)); err != nil {
		t.Errorf("git.show with a path: %v", err)
	}
	if _, err := DecodeArgs(OpGitShow, json.RawMessage(`{"hash":"abcd1234","path":"../x"}`)); !errors.Is(err, ErrInvalid) {
		t.Errorf("git.show with a bad path: %v", err)
	}
}
