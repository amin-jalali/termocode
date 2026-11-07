package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// clipHost is what the clipboard code needs from the machine: the OS, the
// environment, PATH and a way to run a command. Tests build a fake one,
// so they never touch the real clipboard or start real processes.
type clipHost struct {
	goos     string
	getenv   func(string) string
	lookPath func(string) (string, error)
	// run starts argv with stdin and returns its stdout.
	run func(ctx context.Context, stdin string, argv []string) (string, error)
}

// realClipHost is the running machine.
func realClipHost() clipHost {
	return clipHost{goos: runtime.GOOS, getenv: os.Getenv, lookPath: exec.LookPath, run: runClipCommand}
}

func runClipCommand(ctx context.Context, stdin string, argv []string) (string, error) {
	c := spawn.CommandContext(ctx, argv[0], argv[1:]...)
	c.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	c.Stdout = &out
	err := c.Run()
	return out.String(), err
}

// clipCommandTimeout caps one clipboard command. termux-clipboard-get
// waits forever when the Termux:API app is missing, so never wait long.
const clipCommandTimeout = 2 * time.Second

// apkClipEnv names the clipboard bridge of the termocode APK shell. An APK
// has no Termux:API, so the shell (Kotlin or Flutter, it does not matter)
// ships one small executable and puts its absolute path here. Contract:
//
//	<bridge> set   reads stdin and puts it on the Android clipboard
//	<bridge> get   prints the Android clipboard to stdout
//
// Exit code 0 means it worked. Anything else falls back to
// termux-clipboard-set/-get, then OSC 52.
const apkClipEnv = "TERMOCODE_APK_CLIP"

// apkBridge is the APK clipboard bridge path, or "" when there is none.
// Only an absolute path counts: a bare name could be any program on PATH.
func (h clipHost) apkBridge() string {
	if p := h.getenv(apkClipEnv); filepath.IsAbs(p) {
		return p
	}
	return ""
}

// termux reports whether termocode runs in Termux on Android: an android
// build, TERMUX_VERSION (set by the Termux app), Termux's PREFIX, or the
// APK shell's clipboard bridge (same Android rules apply).
func (h clipHost) termux() bool {
	return h.goos == "android" || h.apkBridge() != "" ||
		h.getenv("TERMUX_VERSION") != "" ||
		strings.Contains(h.getenv("PREFIX"), "com.termux")
}

// tools are the clipboard commands to try, in order. In Termux,
// termux-clipboard-set (package termux-api) comes first: it reaches the
// Android clipboard. Elsewhere it is a last try (e.g. a proot distro
// inside Termux, where the command is on PATH).
func (h clipHost) tools() [][]string {
	termuxSet := []string{"termux-clipboard-set"}
	var out [][]string
	if b := h.apkBridge(); b != "" {
		out = append(out, []string{b, "set"})
	}
	if h.termux() {
		out = append(out, termuxSet)
	}
	if h.goos == "darwin" {
		out = append(out, []string{"pbcopy"})
	}
	if h.getenv("WAYLAND_DISPLAY") != "" {
		out = append(out, []string{"wl-copy"})
	}
	if h.getenv("DISPLAY") != "" {
		out = append(out, []string{"xclip", "-selection", "clipboard"}, []string{"xsel", "--clipboard", "--input"})
	}
	if !h.termux() {
		if _, err := h.lookPath(termuxSet[0]); err == nil {
			out = append(out, termuxSet)
		}
	}
	return out
}

// toolWrite pipes s into the first clipboard command that works.
func (h clipHost) toolWrite(s string) bool {
	for _, argv := range h.tools() {
		if _, err := h.lookPath(argv[0]); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), clipCommandTimeout)
		_, err := h.run(ctx, s, argv)
		cancel()
		if err == nil {
			return true
		}
	}
	return false
}

// termuxRead reads the Android clipboard: the APK bridge first, then
// termux-clipboard-get. ok is false outside Android or when both are
// missing or fail.
func (h clipHost) termuxRead() (text string, ok bool) {
	if !h.termux() {
		return "", false
	}
	if out, ok := h.apkRead(); ok {
		return out, true
	}
	if _, err := h.lookPath("termux-clipboard-get"); err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipCommandTimeout)
	defer cancel()
	out, err := h.run(ctx, "", []string{"termux-clipboard-get"})
	if err != nil {
		return "", false
	}
	return out, true
}

// apkRead runs "<bridge> get". ok is false with no bridge or on failure.
func (h clipHost) apkRead() (string, bool) {
	b := h.apkBridge()
	if b == "" {
		return "", false
	}
	if _, err := h.lookPath(b); err != nil {
		return "", false
	}
	ctx, cancel := context.WithTimeout(context.Background(), clipCommandTimeout)
	defer cancel()
	out, err := h.run(ctx, "", []string{b, "get"})
	if err != nil {
		return "", false
	}
	return out, true
}

// apkClipHelp is the fix when the APK shell's bridge did not take the copy.
const apkClipHelp = "The termocode app's clipboard bridge did not answer. Restart the app; if it still fails, update it."

// termuxHelp is the fix when Termux has no clipboard command.
const termuxHelp = "Install the Termux:API app (F-Droid or GitHub, same source as Termux) and run `pkg install termux-api`."

// help says how to make copying work on this machine.
func (h clipHost) help() string {
	if h.apkBridge() != "" {
		return apkClipHelp
	}
	if h.termux() {
		if _, err := h.lookPath("termux-clipboard-set"); err != nil {
			return termuxHelp
		}
		return "termux-clipboard-set failed. Open the Termux:API app once, and check that it comes from the same source as Termux."
	}
	return clipboardHelp
}

// watch reports whether to watch the system clipboard for the clipboard
// history. Not in Termux: the only way there is to poll
// termux-clipboard-get, which starts a Java call each time and costs
// battery. The history still gets every yank made in the editor.
func (h clipHost) watch() bool {
	return !h.termux()
}
