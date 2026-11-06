package git

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Commit signing (GITX-38): the signature of a commit, the repository's
// commit.gpgsign switch and help when signing fails.

// Sig is a commit's signature.
type Sig struct {
	Signed bool
	Status byte   // git's %G?: G good, U good (key trust unknown), B bad, X/Y expired, R revoked, E cannot check, N none
	Signer string // %GS: the signer's name or e-mail
}

// Verified reports a good signature.
func (s Sig) Verified() bool { return s.Status == 'G' }

// Label is the commit view's badge ("" for an unsigned commit).
func (s Sig) Label() string {
	if !s.Signed {
		return ""
	}
	who := ""
	if s.Signer != "" {
		who = " · " + s.Signer
	}
	switch s.Status {
	case 'G':
		return "✓ Verified" + who
	case 'U':
		return "✓ Signed, key not trusted" + who
	case 'B':
		return "✗ Bad signature" + who
	case 'X', 'Y':
		return "Signed, expired" + who
	case 'R':
		return "✗ Signed, key revoked" + who
	}
	return "Signed (not checked: no key or allowed signers here)"
}

// Signature reads the signature of rev. The commit object is read first
// (cheap); only a signed commit runs the check (gpg or ssh-keygen).
func Signature(dir, rev string) Sig {
	obj, err := runOut(dir, "git", "cat-file", "commit", rev)
	if err != nil {
		return Sig{}
	}
	head, _, _ := strings.Cut(obj, "\n\n")
	if !strings.Contains(head, "\ngpgsig ") && !strings.Contains(head, "\ngpgsig-sha256 ") {
		return Sig{}
	}
	s := Sig{Signed: true, Status: 'E'}
	out, err := runOut(dir, "git", "log", "-1", "--format=%G?%x1f%GS", rev, "--") // gpg's stderr is dropped
	if err != nil {
		return s
	}
	st, signer, _ := strings.Cut(strings.TrimSpace(out), "\x1f")
	if st != "" {
		s.Status = st[0]
	}
	s.Signer = signer
	return s
}

// Signing reports whether commits are signed here (commit.gpgsign).
func Signing(dir string) bool {
	out, err := runOut(dir, "git", "config", "--bool", "commit.gpgsign")
	return err == nil && strings.TrimSpace(out) == "true"
}

// SetSigning sets commit.gpgsign for this repository.
func SetSigning(dir string, on bool) error {
	v := "false"
	if on {
		v = "true"
	}
	cmd := spawn.Command("git", "config", "--local", "commit.gpgsign", v)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return cmdError(err, out)
	}
	return nil
}

// signFailed reports git output of a commit or tag that could not be
// signed.
func signFailed(out string) bool {
	return strings.Contains(out, "failed to sign the data") || strings.Contains(out, "gpg failed to sign") ||
		strings.Contains(out, "signing failed")
}

// signReason is the toast reason for a failed signature.
func signReason(out string) string {
	why := ""
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "gpg: ") || strings.HasPrefix(l, "error: Load key") || strings.Contains(l, "No such file") {
			why = l
		}
	}
	r := "Signing the commit failed"
	if why != "" {
		r += " (" + why + ")"
	}
	return r + ". Check user.signingkey and gpg.format, unlock your key, or turn signing off here: Git: Toggle Commit Signing."
}
