package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/svcinstall"
)

const serviceUsage = `Usage: termocode service install|uninstall|status [--dir d] [--yes]

Start the session of a folder at login (and so the workflows in it).
  install    show the service file, ask, then write and start it
  uninstall  stop it and remove the file termocode wrote
  status     show whether it is installed and running

Linux: a systemd user unit. macOS: a launchd LaunchAgent. Only files under
your home folder are written; no sudo. --yes skips the question.
`

func runService(args []string) int {
	cwd, _ := os.Getwd()
	home, _ := os.UserHomeDir()
	exe, err := spawn.Executable()
	if err != nil {
		exe = os.Args[0]
	}
	return runServiceIn(args, os.Stdin, os.Stdout, os.Stderr, cwd, home, exe)
}

func runServiceIn(args []string, in io.Reader, out, errw io.Writer, cwd, home, exe string) int {
	cmd, dir, yes := "", cwd, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help" || a == "help":
			fmt.Fprint(out, serviceUsage)
			return 0
		case a == "install" || a == "uninstall" || a == "status":
			cmd = a
		case a == "--yes" || a == "-y":
			yes = true
		case a == "--dir" && i+1 < len(args):
			i++
			dir = args[i]
		case strings.HasPrefix(a, "--dir="):
			dir = strings.TrimPrefix(a, "--dir=")
		default:
			fmt.Fprintf(errw, "termocode service: unknown argument %q\n\n%s", a, serviceUsage)
			return 2
		}
	}
	if cmd == "" {
		fmt.Fprint(errw, serviceUsage)
		return 2
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		fmt.Fprintln(errw, "termocode service:", err)
		return 1
	}
	s := svcinstall.Spec{GOOS: runtime.GOOS, Home: home, Exe: exe, Workspace: abs, UID: os.Getuid(), Termux: svcinstall.InTermux(os.Getenv)}
	switch cmd {
	case "install":
		confirm := func() bool { return yes }
		if !yes {
			confirm = func() bool {
				fmt.Fprint(out, "Write this file and start the service? [y/N] ")
				line, _ := bufio.NewReader(in).ReadString('\n')
				a := strings.ToLower(strings.TrimSpace(line))
				return a == "y" || a == "yes"
			}
		}
		err = svcinstall.Install(s, out, confirm)
	case "uninstall":
		err = svcinstall.Uninstall(s, out)
	default:
		err = svcinstall.Status(s, out)
	}
	if errors.Is(err, svcinstall.ErrCancelled) {
		fmt.Fprintln(out, "Nothing was written.")
		return 1
	}
	if err != nil {
		fmt.Fprintln(errw, "termocode service:", err)
		return 1
	}
	return 0
}
