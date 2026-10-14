package transfer

import (
	"path/filepath"
	"strings"
)

// Chooser is a program that opens the desktop's own "open files" window
// and prints the chosen paths, one per line. It exits with status 1 when
// the user cancels.
type Chooser struct {
	Name string   // zenity, kdialog, yad, osascript
	Argv []string // Argv[0] is the program's full path
}

// PickChooser finds a file chooser for a local upload: osascript on
// macOS; on Linux and the BSDs, with a display, kdialog first on KDE,
// else zenity, kdialog, yad. lookPath is exec.LookPath (tests stub it).
// Without one the app asks for a path itself.
//
// The xdg-desktop-portal FileChooser is not used: it answers with a
// D-Bus signal to the caller, and a one-shot `gdbus call` has gone (and
// the request with it) before the user picks anything.
func PickChooser(goos string, getenv func(string) string, lookPath func(string) (string, error), title, startDir string) (Chooser, bool) {
	find := func(names ...string) (string, string, bool) {
		for _, n := range names {
			if p, err := lookPath(n); err == nil {
				return n, p, true
			}
		}
		return "", "", false
	}
	start := strings.TrimSuffix(startDir, "/") + "/"
	switch goos {
	case "darwin":
		_, p, ok := find("osascript")
		if !ok {
			return Chooser{}, false
		}
		return Chooser{Name: "osascript", Argv: []string{p,
			"-e", "on run argv",
			"-e", "set picked to choose file with prompt (item 1 of argv) default location (POSIX file (item 2 of argv)) with multiple selections allowed",
			"-e", "set out to \"\"",
			"-e", "repeat with f in picked",
			"-e", "set out to out & POSIX path of f & linefeed",
			"-e", "end repeat",
			"-e", "return out",
			"-e", "end run",
			title, start}}, true
	case "windows", "android", "ios", "plan9", "js", "wasip1":
		return Chooser{}, false
	}
	if getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
		return Chooser{}, false
	}
	order := []string{"zenity", "kdialog", "yad"}
	if strings.Contains(strings.ToUpper(getenv("XDG_CURRENT_DESKTOP")), "KDE") {
		order = []string{"kdialog", "zenity", "yad"}
	}
	name, p, ok := find(order...)
	if !ok {
		return Chooser{}, false
	}
	var argv []string
	switch name {
	case "zenity":
		argv = []string{p, "--file-selection", "--multiple", "--separator=\n", "--title=" + title, "--filename=" + start}
	case "kdialog":
		argv = []string{p, "--title", title, "--getopenfilename", start, "--multiple", "--separate-output"}
	case "yad":
		argv = []string{p, "--file", "--multiple", "--separator=\n", "--title=" + title, "--filename=" + start}
	}
	return Chooser{Name: name, Argv: argv}, true
}

// ParseChosen splits a chooser's output into absolute paths.
func ParseChosen(out []byte) []string {
	var paths []string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimRight(l, "\r")
		if l != "" && filepath.IsAbs(l) {
			paths = append(paths, filepath.Clean(l))
		}
	}
	return paths
}
