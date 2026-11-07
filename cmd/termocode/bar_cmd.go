package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/traybar"
)

// `termocode bar` (CMP-50): the running sessions and their agents as a
// short summary for an OS menu bar, tray or status bar.

const barUsage = `termocode bar: the running sessions and their agents, for a menu bar

  termocode bar [--format text|json|menu]
        text  the title ("tc 2 waiting · 1 working") and one line per session
        json  one line with text, tooltip, class (none, idle, working,
              waiting) and the sessions, for a status-bar module
        menu  the plugin format of menu-bar tools: the title, then per
              session its agents and an Attach item

Run it every few seconds from your menu bar or status bar tool. It only
reads; nothing in the menu ends a session.
`

// barGather finds the sessions (a seam for tests).
var barGather = barSessions

func runBar(args []string) int { return runBarIn(args, os.Stdout, os.Stderr) }

func runBarIn(args []string, out, errw io.Writer) int {
	format := traybar.FormatText
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-h" || a == "--help" || a == "help":
			fmt.Fprint(out, barUsage)
			return 0
		case a == "--format" && i+1 < len(args):
			i++
			format = args[i]
		case strings.HasPrefix(a, "--format="):
			format = strings.TrimPrefix(a, "--format=")
		default:
			fmt.Fprint(errw, barUsage)
			return 2
		}
	}
	if _, err := traybar.Render(format, nil, "", ""); err != nil {
		fmt.Fprintf(errw, "termocode bar: %v\n", err)
		return 2
	}
	ss, err := barGather()
	if err != nil {
		fmt.Fprintf(errw, "termocode bar: %v\n", err)
		return 1
	}
	home, _ := os.UserHomeDir()
	exe, err := spawn.Executable()
	if err != nil || exe == "" {
		exe = "termocode"
	}
	s, err := traybar.Render(format, ss, home, exe)
	if err != nil {
		fmt.Fprintf(errw, "termocode bar: %v\n", err)
		return 1
	}
	fmt.Fprint(out, s)
	return 0
}
