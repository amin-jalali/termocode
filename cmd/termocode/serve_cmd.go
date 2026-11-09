//go:build unix

package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/session"
)

const serveUsage = `Usage: termocode serve [--web] [dir]

Start termocode for a folder (default: the current one) without a
terminal: the UI runs in its own session (as at login with termocode
service) and the command returns at once, so a script can start it.
Use it from the web panel or the phone; see it here with termocode
attach <dir>, stop it with termocode kill <dir>. A dev build reloads
itself when you rebuild it (reload_on_new_build).

  --web   also start its web panel (needs "web_panel": "on" in config.json)
`

// serveHubWait bounds the wait for the UI's web hub (--web).
const serveHubWait = 45 * time.Second

// serveDeps are what `termocode serve` starts (tests swap them).
type serveDeps struct {
	// start makes sure dir's session runs with its UI and no terminal;
	// started is false when it ran already.
	start func(dir string) (started bool, err error)
	// waitHub waits for the UI's web hub (made at or after since).
	waitHub func(dir string, since time.Time) error
	// web runs `termocode web` with args.
	web func(args []string) int
}

func defaultServeDeps() serveDeps {
	return serveDeps{
		start: func(dir string) (bool, error) {
			p, err := session.PathsFor(dir)
			if err != nil {
				return false, err
			}
			return session.StartHeadless(p, 0, func() error { return spawnServer(p) })
		},
		waitHub: func(dir string, since time.Time) error {
			p, err := session.PathsFor(dir)
			if err != nil {
				return err
			}
			return session.WaitWebHub(p, since, serveHubWait)
		},
		web: func(args []string) int {
			cwd, _ := os.Getwd()
			return runWebIn(args, os.Stdout, os.Stderr, cwd)
		},
	}
}

func runServe(args []string) int {
	cwd, _ := os.Getwd()
	return runServeIn(args, os.Stdout, os.Stderr, cwd, defaultServeDeps())
}

func runServeIn(args []string, out, errw io.Writer, cwd string, d serveDeps) int {
	dir, web := cwd, false
	for _, a := range args {
		switch {
		case a == "-h" || a == "--help" || a == "help":
			fmt.Fprint(out, serveUsage)
			return 0
		case a == "--web":
			web = true
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(errw, "termocode serve: unknown flag %q\n\n%s", a, serveUsage)
			return 2
		default:
			dir = a
		}
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(cwd, dir)
	}
	dir = filepath.Clean(dir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		fmt.Fprintf(errw, "termocode: %s is not a folder\n", dir)
		return 1
	}
	since := time.Now()
	started, err := d.start(dir)
	if err != nil {
		fmt.Fprintln(errw, "termocode serve:", err)
		return 1
	}
	if started {
		fmt.Fprintf(out, "termocode runs for %s without a terminal.\n", dir)
	} else {
		since = time.Time{}
		fmt.Fprintf(out, "termocode already runs for %s.\n", dir)
	}
	fmt.Fprintf(out, "See it here: termocode attach %s\nStop it: termocode kill %s\n", dir, dir)
	if !web {
		return 0
	}
	if err := d.waitHub(dir, since); err != nil {
		fmt.Fprintln(errw, "termocode serve: no web panel:", err)
		return 1
	}
	return d.web([]string{"start", "--yes", "--dir", dir})
}
