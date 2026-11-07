//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost/fakedocker"
)

// envFakeDocker makes the e2e test binary play `docker` for one
// container workspace (RC, E2E-OPS): it answers `cp`, `exec` and
// `inspect` with the in-process fake of internal/containerhost, for one
// running container "api-1" whose "/" is $TC_E2E_FAKE_DOCKER/root. Each
// call is one process; the files on disk are the state between calls.
const envFakeDocker = "TC_E2E_FAKE_DOCKER"

// fakeDockerContainer is the one container of the fake.
const fakeDockerContainer = "api-1"

// callTB is the little of testing.TB the fake's Add needs, outside a
// test: temp folders under one call folder, Fatal ends the process.
type callTB struct {
	testing.TB
	base string
}

func (c *callTB) Helper() {}

func (c *callTB) TempDir() string {
	d, err := os.MkdirTemp(c.base, "t-")
	if err != nil {
		c.Fatal(err)
	}
	return d
}

func (c *callTB) Fatal(args ...any) {
	fmt.Fprintln(os.Stderr, append([]any{"fake docker:"}, args...)...)
	os.Exit(125)
}

// fakeDockerMain runs one `docker` call of the fake and returns its exit
// code.
func fakeDockerMain(args []string) int {
	state := os.Getenv(envFakeDocker)
	if f, err := os.OpenFile(filepath.Join(state, "argv.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		fmt.Fprintln(f, strings.Join(args, " "))
		_ = f.Close()
	}
	base, err := os.MkdirTemp(state, "call-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake docker:", err)
		return 125
	}
	defer os.RemoveAll(base)
	root, err := filepath.EvalSymlinks(filepath.Join(state, "root")) // macOS: /var → /private/var
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake docker:", err)
		return 125
	}
	d := fakedocker.New()
	c := d.Add(&callTB{base: base}, fakeDockerContainer)
	c.Root = root
	var in []byte
	for _, a := range args {
		if a == "-" || a == "-i" || a == "--interactive" {
			in, _ = io.ReadAll(os.Stdin)
			break
		}
	}
	out, errText, code, err := d.CaptureInput(context.Background(), "", in, append([]string{"docker"}, args...)...)
	_, _ = io.WriteString(os.Stdout, out)
	_, _ = io.WriteString(os.Stderr, errText)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake docker:", err)
		return 125
	}
	return code
}

// fakeDockerWorkspace puts the fake `docker` first on PATH for this test
// and returns the container's "/" on this machine (write the files of the
// container there) and the state folder (argv.log: one call per line).
func fakeDockerWorkspace(t *testing.T) (root, state string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	state = t.TempDir()
	root = filepath.Join(state, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\n%s=%s exec %s \"$@\"\n", envFakeDocker, shQuote(state), shQuote(exe))
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return root, state
}

// shQuote quotes s for /bin/sh.
func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
