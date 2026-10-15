package app

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
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
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	c.Stdout = &out
	err := c.Run()
	return out.String(), err
}

// clipCommandTimeout caps one clipboard command. termux-clipboard-get
// waits forever when the Termux:API app is missing, so never wait long.
const clipCommandTimeout = 2 * time.Second

// termux reports whether termocode runs in Termux on Android: an android
// build, TERMUX_VERSION (set by the Termux app) or Termux's PREFIX.
func (h clipHost) termux() bool {
	return h.goos == "android" ||
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

// termuxRead reads the Android clipboard with termux-clipboard-get. ok is
// false outside Termux or when the command is missing or fails.
func (h clipHost) termuxRead() (text string, ok bool) {
	if !h.termux() {
		return "", false
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

// termuxHelp is the fix when Termux has no clipboard command.
const termuxHelp = "Install the Termux:API app (F-Droid or GitHub, same source as Termux) and run `pkg install termux-api`."

// help says how to make copying work on this machine.
func (h clipHost) help() string {
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
