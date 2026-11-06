package testkit

import (
	"strings"
	"testing"
)

// Source Control: commit signing (GITX-38).

// Spec: Toggle Commit Signing (This Repository) turns commit.gpgsign on
// for the repository and says so; the second run turns it off.
func TestSCMToggleSigning(t *testing.T) {
	d, r := scmDriver(t, nil)
	d.Palette("git-signing-toggle")
	if !until(d, func() bool { return strings.Contains(text(d), "Commit signing on") }) {
		t.Fatalf("no toast:%s", dump(d))
	}
	if got := strings.TrimSpace(r.git("config", "--local", "commit.gpgsign")); got != "true" {
		t.Fatalf("commit.gpgsign = %q", got)
	}
	d.Palette("git-signing-toggle")
	if !until(d, func() bool { return strings.Contains(text(d), "Commit signing off") }) {
		t.Fatalf("no toast:%s", dump(d))
	}
	if got := strings.TrimSpace(r.git("config", "--local", "commit.gpgsign")); got != "false" {
		t.Errorf("commit.gpgsign = %q", got)
	}
}
