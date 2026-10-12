//go:build unix

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"

	"github.com/amin-jalali/termocode/internal/session"
)

const sessionsSupported = true

// runServer is the background session server: it owns the PTY the UI
// runs in until the UI exits.
func runServer(dir string) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "termocode:", err)
		return 1
	}
	key := session.Key(dir)
	return session.Serve(dir, func() *exec.Cmd {
		c := exec.Command(exe, "--no-session")
		c.Dir = dir
		c.Env = append(os.Environ(), session.EnvSession+"="+key)
		return c
	})
}

// spawnServer starts the session server for p in the background, in its
// own session so the terminal's hangup never reaches it.
func spawnServer(p session.Paths) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logf, err := os.OpenFile(p.Log, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer devnull.Close()
	cmd := exec.Command(exe, serverArg, p.Workspace)
	cmd.Dir = p.Workspace
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// runClient attaches this terminal to the workspace session, starting
// it first when none runs (create = true).
func runClient(dir string, create bool) int {
	p, err := session.PathsFor(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "termocode:", err)
		return 1
	}
	conn, err := session.Dial(p)
	if errors.Is(err, session.ErrNoSession) && create {
		conn, err = session.Ensure(p, func() error { return spawnServer(p) })
	}
	if err != nil {
		if errors.Is(err, session.ErrNoSession) {
			fmt.Fprintf(os.Stderr, "termocode: no session for %s\n", p.Workspace)
		} else {
			fmt.Fprintln(os.Stderr, "termocode:", err)
		}
		return 1
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "termocode: attach needs a terminal")
		_ = conn.Close()
		return 1
	}
	return session.Attach(conn, p, os.Stdin, os.Stdout)
}

// cmdSessions prints the running sessions (`termocode sessions`).
func cmdSessions() int {
	list, err := session.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, "termocode:", err)
		return 1
	}
	if len(list) == 0 {
		fmt.Println("no running sessions")
		return 0
	}
	for _, in := range list {
		state := "detached"
		if in.Attached {
			state = "attached"
		}
		fmt.Printf("%-9s  pid %-7d  since %s  %s\n", state, in.PID, in.Started.Local().Format(time.DateTime), in.Workspace)
	}
	return 0
}

// cmdKill ends the session of a folder (`termocode kill [dir]`).
func cmdKill(dir string) int {
	p, err := session.PathsFor(dir)
	if err == nil {
		err = session.Kill(p)
	}
	if errors.Is(err, session.ErrNoSession) {
		fmt.Fprintf(os.Stderr, "termocode: no session for %s\n", p.Workspace)
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "termocode:", err)
		return 1
	}
	fmt.Printf("ended the session for %s\n", p.Workspace)
	return 0
}

// watchRepaint redraws the whole UI when the session server asks (a
// client attached): the new terminal starts empty, and Bubble Tea only
// draws changed lines.
func watchRepaint(p *tea.Program) {
	if os.Getenv(session.EnvSession) == "" {
		return
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, session.RepaintSignal)
	go func() {
		for range sig {
			if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
				p.Send(tea.WindowSizeMsg{Width: w, Height: h})
			}
			p.Send(tea.ClearScreen())
		}
	}()
}
