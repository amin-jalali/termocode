package servicestest

import (
	"os"
	"os/exec"
	"testing"
)

// Spec (fix 5): the package builds for Windows too (the fake only has
// to compile there; killing a pid is not supported).
func TestBuildsForWindows(t *testing.T) {
	if testing.Short() {
		t.Skip("cross build")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go tool")
	}
	cmd := exec.Command(goBin, "build", "-o", os.DevNull, ".")
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("GOOS=windows go build: %v\n%s", err, out)
	}
}
