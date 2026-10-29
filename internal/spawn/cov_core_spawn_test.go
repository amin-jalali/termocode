package spawn

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func TestCovCoreFixForAndroid(t *testing.T) {
	prog := "/data/data/com.termux/files/usr/bin/git"
	cmd := &exec.Cmd{Path: prog, Args: []string{"git", "status"}}
	fixFor(cmd, true)
	want := []string{systemShell, "-c", trampolineScript, prog, "status"}
	if cmd.Path != systemShell || !reflect.DeepEqual(cmd.Args, want) {
		t.Errorf("android fix: %q %q", cmd.Path, cmd.Args)
	}
	// A system program runs as is.
	sys := &exec.Cmd{Path: "/system/bin/toybox", Args: []string{"toybox"}}
	fixFor(sys, true)
	if sys.Path != "/system/bin/toybox" {
		t.Errorf("system program trampolined: %q", sys.Path)
	}
	// A failed lookup and an empty argv are left alone.
	bad := &exec.Cmd{Path: prog, Args: []string{"git"}, Err: errors.New("not found")}
	fixFor(bad, true)
	empty := &exec.Cmd{Path: prog}
	fixFor(empty, true)
	if bad.Path != prog || empty.Path != prog {
		t.Errorf("broken cmds changed: %q %q", bad.Path, empty.Path)
	}
	// Not Android: nothing changes.
	plain := &exec.Cmd{Path: prog, Args: []string{"git"}}
	fixFor(plain, false)
	if plain.Path != prog || len(plain.Args) != 1 {
		t.Errorf("non-android fix changed the cmd: %q %q", plain.Path, plain.Args)
	}
}

func TestCovCoreCommandContextIsPlainHere(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := CommandContext(ctx, "sh", "-c", "exit 0")
	if filepath.Base(cmd.Path) != "sh" || !reflect.DeepEqual(cmd.Args, []string{"sh", "-c", "exit 0"}) {
		t.Errorf("CommandContext = %q %q", cmd.Path, cmd.Args)
	}
}

func TestCovCoreExecArgs(t *testing.T) {
	prog := "/data/data/com.termux/files/usr/bin/nvim"
	p, a := execArgs(prog, []string{"nvim", "-u"}, true)
	if p != systemShell || !reflect.DeepEqual(a, []string{systemShell, "-c", trampolineScript, prog, "-u"}) {
		t.Errorf("android: %q %q", p, a)
	}
	p, a = execArgs("/system/bin/sh", []string{"sh"}, true)
	if p != "/system/bin/sh" || !reflect.DeepEqual(a, []string{"sh"}) {
		t.Errorf("android system shell: %q %q", p, a)
	}
	p, a = execArgs(prog, []string{"nvim"}, false)
	if p != prog || !reflect.DeepEqual(a, []string{"nvim"}) {
		t.Errorf("not android: %q %q", p, a)
	}
}

func TestCovCoreExecMissingProgramFails(t *testing.T) {
	// A program that is not there: Exec returns the error and this
	// process goes on (it is not replaced).
	missing := filepath.Join(t.TempDir(), "no-such-program")
	err := Exec(missing, []string{missing}, os.Environ())
	if !errors.Is(err, syscall.ENOENT) {
		t.Errorf("Exec(missing) = %v, want ENOENT", err)
	}
}

func TestCovCoreExecutableHere(t *testing.T) {
	got, err := Executable()
	want, werr := os.Executable()
	if got != want || (err == nil) != (werr == nil) {
		t.Errorf("Executable = %q, %v; os.Executable = %q, %v", got, err, want, werr)
	}
}

func TestCovCoreAbsPath(t *testing.T) {
	if absPath("") != "" {
		t.Error(`absPath("") not empty`)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if got := absPath("bin/termocode"); got != filepath.Join(wd, "bin", "termocode") {
		t.Errorf("relative: %q", got)
	}
	if got := absPath("/usr/bin/termocode"); got != "/usr/bin/termocode" {
		t.Errorf("absolute: %q", got)
	}
}
