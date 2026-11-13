package app

import (
	"runtime"
	"strings"
	"testing"
)

// RC-6: a local gopls reading a Linux container's code on another OS or
// CPU gets GOOS/GOARCH of the container, so build tags and
// platform files match what the container builds; nothing else does.
func TestLocalGoplsGetsTheContainersGOOS(t *testing.T) {
	in := testLSPInput(containerLSPSettings{Where: "local"})
	in.Arch = "riscv64" // never this machine's
	p := planContainerLSP(in, testSpecs)
	env := p.Servers["gopls"].Env
	if env["GOOS"] != "linux" || env["GOARCH"] != "riscv64" {
		t.Fatalf("gopls env = %v", env)
	}
	if len(p.Servers["ts_ls"].Env) != 0 {
		t.Errorf("ts_ls env = %v, want none", p.Servers["ts_ls"].Env)
	}
	// The same OS and CPU as here: no hint.
	in.Arch = runtime.GOARCH
	if runtime.GOOS == "linux" {
		if env := planContainerLSP(in, testSpecs).Servers["gopls"].Env; len(env) != 0 {
			t.Errorf("same platform env = %v", env)
		}
	}
	// In the image the server is the container's own: no hint.
	in = testLSPInput(containerLSPSettings{})
	in.Arch = "riscv64"
	if env := planContainerLSP(in, testSpecs).Servers["gopls"].Env; len(env) != 0 {
		t.Errorf("in-image env = %v", env)
	}
	if !strings.Contains(lspSetupLua, "cmd_env = plan.env") {
		t.Error("lspSetupLua does not pass the plan's env")
	}
}
