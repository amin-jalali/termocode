package update

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func TestVerifyChecksum(t *testing.T) {
	dir := t.TempDir()
	data := []byte("tarball bytes")
	f := filepath.Join(dir, "t.tar.gz")
	if err := os.WriteFile(f, data, 0o600); err != nil {
		t.Fatal(err)
	}
	other := strings.Repeat("0", 64)
	sums := other + "  termocode-v0.2.0-linux-arm64.tar.gz\n" +
		sha(data) + "  termocode-v0.2.0-linux-amd64.tar.gz\n"
	if err := VerifyChecksum(f, "termocode-v0.2.0-linux-amd64.tar.gz", []byte(sums)); err != nil {
		t.Fatalf("good sum: %v", err)
	}
	// sha256sum -b writes "*name".
	if err := VerifyChecksum(f, "termocode-v0.2.0-linux-amd64.tar.gz", []byte(sha(data)+" *termocode-v0.2.0-linux-amd64.tar.gz\n")); err != nil {
		t.Fatalf("binary-mode line: %v", err)
	}
}

func TestVerifyChecksumRefuses(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "t.tar.gz")
	if err := os.WriteFile(f, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "termocode-v0.2.0-linux-amd64.tar.gz"
	for label, sums := range map[string]string{
		"mismatch":     sha([]byte("original")) + "  " + name + "\n",
		"missing line": sha([]byte("tampered")) + "  termocode-v0.2.0-linux-arm64.tar.gz\n",
		"empty":        "",
		"short sum":    "abc  " + name + "\n",
		"suffix name":  sha([]byte("tampered")) + "  x" + name + "\n",
	} {
		err := VerifyChecksum(f, name, []byte(sums))
		if err == nil {
			t.Errorf("%s: want an error", label)
		}
	}
}
