//go:build unix

package session

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// SSHOpts configures RunSSH (`termocode ssh`).
type SSHOpts struct {
	SSH    string   // the ssh program ("" = ssh)
	Args   []string // the user's ssh arguments, destination included
	Remote string   // termocode on the far side ("" = termocode)
	Dir    string   // folder on the far side ("" = the login folder)
	In     *os.File
	Out    *os.File
}

// sshAttempt is how one ssh connection ended.
type sshAttempt struct {
	code int  // ssh's exit code
	quit bool // the user or a signal ended termocode ssh: stop here
}

// sshTerm is the local terminal side of RunSSH, shared by its attempts.
type sshTerm struct {
	o    SSHOpts
	out  *lockedWriter
	keys <-chan []byte
	sigs <-chan os.Signal
	seen *Modes // the far UI's terminal modes, to undo them on a lost link
}

// RunSSH runs ssh to the far side's termocode session in a PTY and relays
// this terminal to it. ssh's own questions (password, passphrase, host
// key) pass through as they are; nothing is read or kept. It returns the
// exit code of ssh (that of the far termocode when the link held).
func RunSSH(o SSHOpts) int {
	if o.SSH == "" {
		o.SSH = "ssh"
	}
	if o.Remote == "" {
		o.Remote = "termocode"
	}
	if term.IsTerminal(int(o.In.Fd())) {
		if st, err := term.MakeRaw(int(o.In.Fd())); err == nil {
			defer func() { _ = term.Restore(int(o.In.Fd()), st) }()
		}
	}
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, syscall.SIGWINCH, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	defer signal.Stop(sigs)
	stop := make(chan struct{})
	defer close(stop)
	t := &sshTerm{o: o, out: &lockedWriter{w: o.Out}, keys: readKeys(o.In, stop), sigs: sigs, seen: NewModes()}
	return t.attempt("").code
}

// readKeys reads the terminal until it ends (the channel closes) or stop.
func readKeys(in *os.File, stop <-chan struct{}) <-chan []byte {
	keys := make(chan []byte, 16)
	go func() {
		defer close(keys)
		buf := make([]byte, 4096)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				select {
				case keys <- append([]byte(nil), buf[:n]...):
				case <-stop:
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return keys
}

// size is the local terminal's size (80x24 when it has none).
func (t *sshTerm) size() (cols, rows int) {
	cols, rows, err := term.GetSize(int(t.o.Out.Fd()))
	if err != nil || cols <= 0 || rows <= 0 {
		return 80, 24
	}
	return cols, rows
}

// attempt runs one ssh connection until ssh exits.
func (t *sshTerm) attempt(reconnected string) sshAttempt {
	cols, rows := t.size()
	cmd := spawn.Command(t.o.SSH, sshArgv(t.o.Args, t.o.Remote, t.o.Dir, reconnected)...)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		fmt.Fprintf(os.Stderr, "termocode: %s: %v\r\n", t.o.SSH, err)
		return sshAttempt{code: 1, quit: true}
	}
	defer ptmx.Close()
	exited := make(chan int, 1)
	go func() { exited <- exitCode(cmd.Wait()) }()
	output := make(chan []byte, 16)
	go func() {
		defer close(output)
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				output <- append([]byte(nil), buf[:n]...)
			}
			if err != nil {
				return
			}
		}
	}()
	kill := func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	link := newSeqFilter(linkHello, linkPong)
	keys := t.keys

	for {
		select {
		case k, ok := <-keys:
			if !ok { // the terminal is gone
				keys = nil
				kill()
				continue
			}
			_, _ = ptmx.Write(k)
		case b, ok := <-output:
			if !ok {
				output = nil
				continue
			}
			b, _ = link.Filter(b)
			if len(b) > 0 {
				t.seen.Feed(b)
				_, _ = t.out.Write(b)
			}
		case s := <-t.sigs:
			switch s {
			case syscall.SIGWINCH:
				cols, rows = t.size()
				_ = pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
			default:
				kill()
				<-exited
				if s != syscall.SIGHUP {
					_, _ = t.out.Write(append(t.seen.Reset(), plainReset...))
				}
				return sshAttempt{code: 0, quit: true}
			}
		case code := <-exited:
			t.drain(output, link)
			return sshAttempt{code: code}
		}
	}
}

// drain shows what ssh wrote just before it exited (its last message),
// without waiting long for a child that keeps the PTY open.
func (t *sshTerm) drain(output <-chan []byte, link *seqFilter) {
	if output == nil {
		return
	}
	deadline := time.After(time.Second)
	for {
		select {
		case b, ok := <-output:
			if !ok {
				return
			}
			b, _ = link.Filter(b)
			t.seen.Feed(b)
			_, _ = t.out.Write(b)
		case <-deadline:
			return
		}
	}
}

// exitCode is the exit code of a finished command (-1: killed by a
// signal).
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	if err != nil {
		return 1
	}
	return 0
}
