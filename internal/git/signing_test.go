package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// sshSigner sets up ssh signing in r with a new key; allowed also trusts it.
func sshSigner(t *testing.T, r *gitTestRepo, allowed bool) {
	t.Helper()
	if _, err := exec.LookPath("ssh-keygen"); err != nil {
		t.Skip("no ssh-keygen")
	}
	key := filepath.Join(t.TempDir(), "key")
	if out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
		t.Skipf("ssh-keygen: %v %s", err, out)
	}
	r.git(t, "config", "gpg.format", "ssh")
	r.git(t, "config", "user.signingkey", key+".pub")
	if allowed {
		b, err := os.ReadFile(key + ".pub")
		if err != nil {
			t.Fatal(err)
		}
		list := filepath.Join(filepath.Dir(key), "allowed_signers")
		if err := os.WriteFile(list, append([]byte("test@example.com "), b...), 0o600); err != nil {
			t.Fatal(err)
		}
		r.git(t, "config", "gpg.ssh.allowedSignersFile", list)
	}
}

// GITX-38 (B17): Signature says whether a commit is signed and, when git
// can check it, that it is verified and by whom.
func TestSignature(t *testing.T) {
	r := newGitTestRepo(t)
	r.write(t, "a.txt", "a\n")
	r.git(t, "add", "a.txt")
	r.git(t, "commit", "-qm", "plain")
	if s := Signature(r.dir, "HEAD"); s.Signed || s.Label() != "" {
		t.Fatalf("plain commit: %+v %q", s, s.Label())
	}
	sshSigner(t, r, false)
	r.git(t, "-c", "commit.gpgsign=true", "commit", "-q", "--allow-empty", "-m", "signed")
	if s := Signature(r.dir, "HEAD"); !s.Signed || s.Verified() || !strings.Contains(s.Label(), "Signed") {
		t.Fatalf("not checkable: %+v %q", s, s.Label())
	}
	sshSigner(t, r, true)
	r.git(t, "-c", "commit.gpgsign=true", "commit", "-q", "--allow-empty", "-m", "signed 2")
	if s := Signature(r.dir, "HEAD"); !s.Verified() || s.Label() != "✓ Verified · test@example.com" {
		t.Fatalf("verified: %+v %q", s, s.Label())
	}
}

// GITX-38 (B18): SetSigning turns commit.gpgsign on or off for the
// repository; Signing reads it.
func TestSetSigning(t *testing.T) {
	r := newGitTestRepo(t)
	if Signing(r.dir) {
		t.Fatal("on in a test repo")
	}
	if err := SetSigning(r.dir, true); err != nil || !Signing(r.dir) {
		t.Fatalf("on: %v", err)
	}
	if err := SetSigning(r.dir, false); err != nil || Signing(r.dir) {
		t.Fatalf("off: %v", err)
	}
}

// GITX-38: a commit that fails to sign says how to fix it.
func TestSigningFailureReason(t *testing.T) {
	r := newGitTestRepo(t)
	r.git(t, "config", "gpg.format", "openpgp")
	r.git(t, "config", "user.signingkey", "NO-SUCH-KEY")
	r.git(t, "config", "gpg.program", "false")
	r.git(t, "config", "commit.gpgsign", "true")
	r.write(t, "a.txt", "a\n")
	r.git(t, "add", "a.txt")
	err := Commit(r.dir, "x")
	if err == nil {
		t.Fatal("signed with gpg.program=false")
	}
	var ce *CmdError
	if !errors.As(err, &ce) || !strings.Contains(ce.Reason, "Signing the commit failed") || !strings.Contains(ce.Reason, "Toggle Commit Signing") {
		t.Fatalf("reason: %v", err)
	}
}
