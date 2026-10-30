package webproto

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// checkRow wants op's table row to have the role, the card and the audit.
func checkRow(t *testing.T, op string, role Role, card bool, audit Audit) {
	t.Helper()
	p, ok := Lookup(op)
	if !ok || p.Role != role || p.Phase != PhaseV2 || p.Audit != audit || p.LaptopCard != card {
		t.Errorf("%s row %+v", op, p)
	}
	if _, ok := ResultOf(op); !ok {
		t.Errorf("%s has no result type", op)
	}
}

// checkArgs wants each bad args JSON refused and each good one decoded.
func checkArgs(t *testing.T, op string, bad, good []string) {
	t.Helper()
	for _, b := range bad {
		if _, err := DecodeArgs(op, json.RawMessage(b)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s %s: %v, want invalid", op, b, err)
		}
	}
	for _, g := range good {
		if _, err := DecodeArgs(op, json.RawMessage(g)); err != nil {
			t.Errorf("%s %s: %v", op, g, err)
		}
	}
}

// Spec (web Git Graph): git.graph reads one page of the commit graph (all
// branches, or HEAD's history only) with its lanes; a v2 read for
// viewers, not audited. The page starts at most MaxGraphSkip commits in.
func TestGitGraphOp(t *testing.T) {
	checkArgs(t, OpGitGraph, []string{`{"skip":-1}`, `{"skip":5001}`},
		[]string{`{}`, `{"skip":200}`, `{"skip":0,"head":true}`})
	checkRow(t, OpGitGraph, RoleViewer, false, AuditNo)
}

// Spec (blame): git.blame says who last changed each line of one
// workspace file; a v2 read for viewers, not audited.
func TestGitBlameOp(t *testing.T) {
	checkArgs(t, OpGitBlame, []string{`{"path":""}`, `{"path":"../x"}`, `{"path":"dir/"}`, `{"path":"/etc/passwd"}`},
		[]string{`{"path":"src/a.go"}`})
	checkRow(t, OpGitBlame, RoleViewer, false, AuditNo)
}

// Spec (merge editor): git.conflict reads a conflicted file's sides and
// blocks (a read for viewers); git.resolve saves the merged result and
// marks the file resolved (a controller op, audited, with the version
// the page read as its precondition); git.resolve_side keeps one side of
// the whole file, which can delete it: a laptop card.
func TestGitConflictOps(t *testing.T) {
	v := strings.Repeat("ab", 32)
	checkArgs(t, OpGitConflict, []string{`{"path":""}`, `{"path":"../x"}`}, []string{`{"path":"a.go"}`})
	checkRow(t, OpGitConflict, RoleViewer, false, AuditNo)
	checkArgs(t, OpGitResolve, []string{`{"path":"a.go","text":"x","version":""}`, `{"path":"a.go","text":"x","version":"` + v[:63] + `"}`,
		`{"path":"a.go","text":"x","version":"` + strings.Repeat("g", 64) + `"}`, `{"path":"a.go","text":"a\u0000b","version":"` + v + `"}`,
		`{"path":"/x","text":"x","version":"` + v + `"}`},
		[]string{`{"path":"a.go","text":"","version":"` + v + `"}`, `{"path":"a.go","text":"a\nb\r\n\tc","version":"` + v + `"}`})
	checkRow(t, OpGitResolve, RoleController, false, AuditYes)
	checkArgs(t, OpGitResolveSide, []string{`{"path":"a.go","side":"base"}`, `{"path":"a.go","side":""}`}, []string{`{"path":"a.go","side":"ours"}`, `{"path":"a.go","side":"theirs"}`})
	checkRow(t, OpGitResolveSide, RoleController, true, AuditYes)
}

// Spec (amend): git.amend replaces the last commit (with the staged
// changes, and a new message or the old one); it rewrites history, so it
// waits for a laptop card.
func TestGitAmendOp(t *testing.T) {
	checkArgs(t, OpGitAmend, []string{`{"message":"   "}`, `{"message":"\n body only"}`, `{"message":"a\u0007b"}`},
		[]string{`{}`, `{"message":"fix: the subject\n\nthe body"}`})
	checkRow(t, OpGitAmend, RoleController, true, AuditYes)
}
