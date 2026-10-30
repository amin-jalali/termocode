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

	"github.com/amin-jalali/termocode/internal/app"
	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/termimg"
)

const sessionsSupported = true

// runServer is the background session server: it owns the PTY the UI
// runs in until the UI exits.
func runServer(dir string) int {
	exe, err := spawn.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "termocode:", err)
		return 1
	}
	key := session.Key(dir)
	return session.Serve(dir, func() *exec.Cmd {
		c := spawn.Command(exe, "--no-session")
		c.Dir = dir
		c.Env = append(os.Environ(), session.EnvSession+"="+key)
		return c
	})
}

// spawnServer starts the session server for p in the background, in its
// own session so the terminal's hangup never reaches it.
func spawnServer(p session.Paths) error {
	exe, err := spawn.Executable()
	if err != nil {
		return err
	}
	logf, err := session.OpenLog(p)
	if err != nil {
		return err
	}
	defer logf.Close()
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		return err
	}
	defer devnull.Close()
	cmd := spawn.Command(exe, serverArg, p.Workspace)
	cmd.Dir = p.Workspace
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// webSSHWait bounds how long the far side waits for its UI's web hub.
const webSSHWait = time.Minute

// runClient attaches this terminal to the workspace session, starting
// it first when none runs (o.create).
func runClient(dir string, o clientOpts) int {
	p, err := session.PathsFor(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "termocode:", err)
		return 1
	}
	if msg := attachRefusal(os.Getenv, p.Key); msg != "" {
		fmt.Fprintln(os.Stderr, msg)
		return 1
	}
	conn, err := session.Dial(p)
	running := err == nil
	if errors.Is(err, session.ErrNoSession) && o.create {
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
	// The UI asks for BDSM explicit mode when it starts; a later attach
	// from another terminal needs it too (internal/rtl).
	rtlEnter, rtlLeave := app.RTLTerminalModes()
	rtlEnter, rtlLeave = rtlEscapes(rtlEnter, rtlLeave, os.Getenv)
	_, _ = os.Stdout.WriteString(rtlEnter)
	defer func() { _, _ = os.Stdout.WriteString(rtlLeave) }()
	caps := clientCaps(running)
	if running {
		caps.Note = o.note
	}
	var announce chan string
	if o.link && o.webPort > 0 {
		announce = make(chan string, 1)
		crash.Go("termocode --link: the web hand-off", func() {
			announce <- webSSHHandoff(p.Workspace, o.webPort, webSSHWait)
		})
	}
	return session.Attach(conn, p, session.AttachOpts{In: os.Stdin, Out: os.Stdout, Caps: caps,
		Reconnect: app.ConfigAutoReconnect(), Link: o.link, Announce: announce})
}

// clientCaps describes this terminal for the session. A running UI keeps
// the caps of the terminal it started in, so for it we also probe the
// graphics support here, before the attach takes the terminal (a new UI
// probes by itself).
func clientCaps(running bool) *session.ClientCaps {
	c := session.EnvCaps(os.Getenv)
	if running {
		switch termimg.ParseMode(app.ConfigDisplay().Images) {
		case termimg.ModeOff, termimg.ModeBlocks:
		default:
			c.Graphics = graphicsToSession(termimg.Probe(os.Stdin, os.Stdout, 250*time.Millisecond, os.Getenv))
		}
	}
	return &c
}

func graphicsToSession(c termimg.Caps) *session.GraphicsCaps {
	return &session.GraphicsCaps{
		Protocol: c.Protocol.String(), CellW: c.CellW, CellH: c.CellH,
		InTmux: c.InTmux, Passthrough: c.Passthrough, Reason: c.Reason,
	}
}

// graphicsFromSession is the inverse of graphicsToSession; an unknown
// protocol (a newer client) is Blocks, which works everywhere.
func graphicsFromSession(g *session.GraphicsCaps) termimg.Caps {
	c := termimg.Caps{Protocol: termimg.Blocks, CellW: g.CellW, CellH: g.CellH,
		InTmux: g.InTmux, Passthrough: g.Passthrough, Reason: g.Reason}
	switch g.Protocol {
	case "kitty":
		c.Protocol = termimg.Kitty
	case "sixel":
		c.Protocol = termimg.Sixel
	case "off":
		c.Protocol = termimg.None
	}
	return c
}

// applyClientEnv copies the terminal variables of the client that attached
// into our environment (a variable it does not set is unset), so colour,
// icon and graphics detection see the terminal that now shows the UI.
func applyClientEnv(c *session.ClientCaps) {
	for _, k := range session.CapsEnvKeys {
		if v, ok := c.Env[k]; ok && v != "" {
			_ = os.Setenv(k, v)
		} else {
			_ = os.Unsetenv(k)
		}
	}
}

// terminalChange builds the message that switches the UI to the client's
// terminal. Without a probe from the client its environment decides.
func terminalChange(c *session.ClientCaps) app.TerminalChangedMsg {
	if c.Graphics != nil {
		return app.TerminalChangedMsg{Graphics: graphicsFromSession(c.Graphics), Gen: c.Gen, Note: c.Note}
	}
	g := termimg.Caps{Protocol: termimg.Blocks, Reason: "no probe from the attaching terminal"}
	switch termimg.ParseMode(app.ConfigDisplay().Images) {
	case termimg.ModeOff, termimg.ModeBlocks:
	default:
		g.Protocol, _ = termimg.DetectEnv(func(k string) string { return c.Env[k] })
	}
	return app.TerminalChangedMsg{Graphics: g, Gen: c.Gen, Note: c.Note}
}

// cmdSSH runs `termocode ssh`: the editor of a far machine in this
// terminal, in its persistent session.
func cmdSSH(args []string) int {
	dir, sshArgs, err := sshCommandArgs(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "termocode:", err)
		return 2
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "termocode: ssh needs a terminal")
		return 1
	}
	interval, dead := linkTiming(os.Getenv(envLinkTiming))
	return session.RunSSH(session.SSHOpts{Args: sshArgs, Dir: dir, In: os.Stdin, Out: os.Stdout,
		Reconnect: app.ConfigAutoReconnect(), Interval: interval, Dead: dead})
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
// draws changed lines. The new terminal may be another program (kitty
// first, a plain xterm later): its caps, stored by the server, replace
// ours before the redraw.
func watchRepaint(p *tea.Program) {
	if os.Getenv(session.EnvSession) == "" {
		return
	}
	capsFile := os.Getenv(session.EnvCapsFile)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, session.RepaintSignal)
	crash.Go("the session repaint", func() {
		for range sig {
			if w, h, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
				p.Send(tea.WindowSizeMsg{Width: w, Height: h})
			}
			if capsFile != "" {
				if c, err := session.ReadCaps(capsFile); err == nil && c != nil {
					applyClientEnv(c)
					p.Send(terminalChange(c))
				}
			}
			p.Send(tea.ClearScreen())
		}
	})
}
