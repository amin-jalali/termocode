// Package spawn starts child processes. Use it instead of exec.Command:
// on Android it gets past the exec block of the Google Play Termux.
//
// The Google Play Termux targets a new Android SDK, where an app may not
// execve files in its own data dir (W^X). Termux's programs still run
// because libtermux-exec.so (LD_PRELOAD) hooks libc's execve and starts
// them through the system linker. Go does not call libc's execve, so
// exec.Command("nvim") fails there with "permission denied". Android's
// own /system/bin/sh may exec, and it loads the hook; so a program
// outside the system dirs is started as
//
//	/system/bin/sh -c 'exec "$0" "$@"' <path> <args...>
//
// sh execs in place: the pid, process group, Pdeathsig and pipes stay
// the program's own. The F-Droid Termux allows a direct exec, but the
// trampoline works there too, so it is always used on Android.
package spawn

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// systemShell is Android's shell, which may exec app files.
const systemShell = "/system/bin/sh"

// trampolineScript makes sh replace itself with "$0" "$@".
const trampolineScript = `exec "$0" "$@"`

// systemDirs hold files any process may exec on Android.
var systemDirs = []string{"/system/", "/apex/", "/vendor/", "/product/", "/odm/", "/system_ext/"}

// Command is exec.Command, started through the trampoline on Android.
func Command(name string, arg ...string) *exec.Cmd {
	return Fix(exec.Command(name, arg...))
}

// CommandContext is exec.CommandContext, started through the
// trampoline on Android.
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	return Fix(exec.CommandContext(ctx, name, arg...))
}

// Fix rewrites a not yet started cmd for the trampoline on Android. Use
// it when the code changes cmd.Path or cmd.Args after exec.Command.
func Fix(cmd *exec.Cmd) *exec.Cmd {
	if !onAndroid || cmd.Err != nil || len(cmd.Args) == 0 {
		return cmd
	}
	if path, args, ok := trampoline(cmd.Path, cmd.Args); ok {
		cmd.Path, cmd.Args = path, args
	}
	return cmd
}

// Exec is syscall.Exec, through the trampoline on Android.
func Exec(argv0 string, argv []string, envv []string) error {
	if onAndroid {
		if path, args, ok := trampoline(argv0, argv); ok {
			argv0, argv = path, args
		}
	}
	return syscall.Exec(argv0, argv, envv)
}

// trampoline returns the system shell call that execs path with argv[1:],
// or ok=false when path may be exec'd directly.
func trampoline(path string, argv []string) (string, []string, bool) {
	if path == "" || path == systemShell || inSystemDir(path) {
		return "", nil, false
	}
	args := make([]string, 0, len(argv)+3)
	args = append(args, systemShell, "-c", trampolineScript, path)
	if len(argv) > 1 {
		args = append(args, argv[1:]...)
	}
	return systemShell, args, true
}

func inSystemDir(path string) bool {
	for _, d := range systemDirs {
		if strings.HasPrefix(path, d) {
			return true
		}
	}
	return false
}

// Executable is os.Executable, but on Android it gives termocode's own
// file and not the system linker that termux-exec started it with.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil || !onAndroid {
		return exe, err
	}
	return realExecutable(exe, selfExe), nil
}

// selfExe is $TERMUX_EXEC__PROC_SELF_EXE made absolute at start (it can
// be relative to the start dir). termux-exec sets it to the file it ran.
var selfExe = func() string {
	p := os.Getenv("TERMUX_EXEC__PROC_SELF_EXE")
	if p == "" {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}()

// realExecutable picks the env path when /proc/self/exe is the linker.
func realExecutable(exe, self string) string {
	if self != "" && strings.HasPrefix(filepath.Base(exe), "linker") {
		return self
	}
	return exe
}

// linkerArgs undoes a doubled program path. termux-exec starts a program
// in the app's data dir as `linker64 <path> <args>`; the linker hands
// the program argv with <path> twice, so os.Args[1] is the program again
// instead of its first argument. exe is os.Executable (the linker in
// that case) and selfExe what termux-exec says the program is.
func linkerArgs(args []string, exe, selfExe string) []string {
	if b := filepath.Base(exe); b != "linker64" && b != "linker" {
		return args
	}
	if len(args) < 2 || args[0] != args[1] || (selfExe != "" && args[1] != selfExe) {
		return args
	}
	return args[1:]
}
