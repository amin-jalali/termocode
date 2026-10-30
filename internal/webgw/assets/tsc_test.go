package assets

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestTypeScriptTypeChecks runs the TypeScript compiler over src/ with the
// strict tsconfig.json. It needs node tooling, which CI does not have, so
// it only runs on a dev machine that asks for it:
//
//	TERMOCODE_TSC=1 go test -run TestTypeScriptTypeChecks ./internal/webgw/assets
//
// TERMOCODE_TSC_BIN names the tsc to use (default: tsc on PATH, then
// ~/.local/bin/tsc).
func TestTypeScriptTypeChecks(t *testing.T) {
	if os.Getenv("TERMOCODE_TSC") != "1" {
		t.Skip("set TERMOCODE_TSC=1 to type check the web sources with tsc")
	}
	bin := os.Getenv("TERMOCODE_TSC_BIN")
	if bin == "" {
		if p, err := exec.LookPath("tsc"); err == nil {
			bin = p
		} else if home, err := os.UserHomeDir(); err == nil {
			bin = filepath.Join(home, ".local", "bin", "tsc")
		}
	}
	if _, err := os.Stat(bin); bin == "" || err != nil {
		t.Fatalf("tsc not found (set TERMOCODE_TSC_BIN): %q", bin)
	}
	out, err := exec.Command(bin, "-p", "tsconfig.json").CombinedOutput()
	if err != nil {
		t.Fatalf("tsc found type errors:\n%s", out)
	}
}
