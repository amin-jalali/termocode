package testkit

import (
	"strings"
	"testing"
	"time"
)

// SCN-37: the doctor on a machine where the tools termocode uses are
// missing. Each missing tool gets its own row with an install hint; only
// nvim counts as required.

// Spec: Given none of nvim, git, rg, node, docker or kubectl is on PATH,
// when the user runs Help: Run Doctor, then the report marks nvim as
// missing (✗), the other tools as optional (?), and says one required
// tool is missing.
func TestDoctorWithMissingTools(t *testing.T) {
	d := New(t, Options{W: 140, H: 80})
	t.Setenv("PATH", t.TempDir())
	d.Palette("run-doctor")
	if !d.WaitForText("required tool(s) missing", 5*time.Second) {
		t.Fatalf("no doctor report:\n%s", screen(d))
	}
	s := screen(d)
	for _, want := range []string{
		"✗ neovim (editor engine)",
		"? git (Source Control)",
		"? ripgrep (workspace search)",
		"? node (JS debugger)",
		"? docker (Containers)",
		"? kubectl (Kubernetes)",
		"1 required tool(s) missing.",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("doctor report has no %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "All required tools found") {
		t.Errorf("doctor says all required tools are found without nvim:\n%s", s)
	}
}
