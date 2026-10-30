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

// Spec (interactive rebase): git.rebase_plan lists a commit and every
// newer one of HEAD as picks (a read), git.rebase_state says where a
// stopped rebase is (a read); git.rebase runs the user's list and
// git.rebase_ctl continues, skips or aborts a stopped one: both rewrite
// history and wait for a laptop card.
func TestGitRebaseOps(t *testing.T) {
	checkArgs(t, OpGitRebasePlan, []string{`{"from":""}`, `{"from":"HEAD~3"}`, `{"from":"abc"}`, `{"from":"--all"}`}, []string{`{"from":"60ed1c8"}`})
	checkRow(t, OpGitRebasePlan, RoleViewer, false, AuditNo)
	checkArgs(t, OpGitRebaseState, nil, []string{`{}`})
	checkRow(t, OpGitRebaseState, RoleViewer, false, AuditNo)
	checkArgs(t, OpGitRebase, []string{`{"base":"60ed1c8","steps":[]}`, `{"base":"main","steps":[{"hash":"60ed1c8","action":"pick"}]}`,
		`{"base":"","steps":[{"hash":"60ed1c8","action":"exec"}]}`, `{"base":"","steps":[{"hash":"x","action":"pick"}]}`,
		`{"base":"","steps":[{"hash":"60ed1c8","action":"reword","message":"  "}]}`, `{"base":"","steps":[{"hash":"60ed1c8","action":"pick","message":"m"}]}`},
		[]string{`{"base":"","steps":[{"hash":"60ed1c8","action":"pick"}]}`, `{"base":"4db6cc1f","steps":[{"hash":"60ed1c8","action":"reword","message":"new"},{"hash":"1a5f196a","action":"fixup"}]}`})
	checkRow(t, OpGitRebase, RoleController, true, AuditYes)
	checkArgs(t, OpGitRebaseCtl, []string{`{"do":"edit"}`, `{"do":""}`}, []string{`{"do":"continue"}`, `{"do":"skip"}`, `{"do":"abort"}`})
	checkRow(t, OpGitRebaseCtl, RoleController, true, AuditYes)
}

// Spec (clone): git.clone takes a remote's https or ssh URL only (never a
// local path, a file:// or helper transport, or a URL with a password or
// token in it) and talks to a remote, so it waits for a laptop card.
func TestGitCloneOp(t *testing.T) {
	checkArgs(t, OpGitClone, []string{`{"url":""}`, `{"url":"/home/u/repo"}`, `{"url":"file:///etc"}`, `{"url":"ext::sh -c touch% /tmp/x"}`,
		`{"url":"https://user:tok@github.com/a/b.git"}`, `{"url":"https://x-access-token@github.com/a/b"}`, `{"url":"-u https://h/a"}`,
		`{"url":"https://github.com/a/b c"}`, `{"url":"git@github.com:"}`, `{"url":"http://github.com/a/b"}`, `{"url":"ssh://h:22"}`,
		`{"url":"https://github.com"}`, `{"url":"git@-oProxyCommand=x:a/b"}`},
		[]string{`{"url":"https://github.com/amin-jalali/termocode.git"}`, `{"url":"git@github.com:amin-jalali/termocode.git"}`, `{"url":"ssh://git@host.example:2222/a/b.git"}`})
	checkRow(t, OpGitClone, RoleController, true, AuditYes)
}
