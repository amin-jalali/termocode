package sidecartools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func repoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// RC-8: the tools volume is built from source: tc-sleep, tc-dap-bridge
// and dlv (pinned in tools/sidecar/go.mod) static for Linux, then copied
// into termocode-tools-<arch>-<libc> with the target's own image (no
// other image is pulled).
func TestToolsVolumeBuildScript(t *testing.T) {
	s := repoFile(t, "scripts/sidecar-tools/build.sh")
	for _, want := range []string{
		"CGO_ENABLED=0", "GOOS=linux", "-trimpath",
		"./cmd/tc-sleep", "./cmd/tc-dap-bridge", "github.com/go-delve/delve/cmd/dlv",
		"-mod=readonly", "termocode-tools-", "volume create", "docker cp", "fetch-rg.sh",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("build.sh has no %q", want)
		}
	}
	mod := repoFile(t, "tools/sidecar/go.mod")
	if !strings.Contains(mod, "tool github.com/go-delve/delve/cmd/dlv") || !strings.Contains(mod, "github.com/go-delve/delve v1.27.2") {
		t.Errorf("tools/sidecar/go.mod does not pin dlv:\n%s", mod)
	}
	if !strings.Contains(repoFile(t, "tools/sidecar/go.sum"), "github.com/go-delve/delve v1.27.2 h1:") {
		t.Error("tools/sidecar/go.sum has no hash for delve v1.27.2")
	}
}

// rg (a Rust program) is never built here: fetch-rg.sh downloads one
// pinned release and refuses to install it without a matching sha256.
// An arch with no pinned hash stops before any download.
func TestFetchRgNeedsAPinnedHash(t *testing.T) {
	s := repoFile(t, "scripts/sidecar-tools/fetch-rg.sh")
	for _, want := range []string{"RG_VERSION=", "sha256sum", "x86_64-unknown-linux-musl", "aarch64-unknown-linux"} {
		if !strings.Contains(s, want) {
			t.Errorf("fetch-rg.sh has no %q", want)
		}
	}
	out := t.TempDir()
	cmd := exec.Command("sh", filepath.Join("..", "..", "scripts", "sidecar-tools", "fetch-rg.sh"), "riscv64", out)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	b, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("an arch without a pinned hash succeeded: %s", b)
	}
	if !strings.Contains(string(b), "sha256") {
		t.Fatalf("message %q", b)
	}
	if ents, _ := os.ReadDir(out); len(ents) != 0 {
		t.Fatalf("files written: %v", ents)
	}
}
