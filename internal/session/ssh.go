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
	// Reconnect: when the link drops, run ssh again with the same
	// arguments until it is back or the user quits (auto_reconnect).
	Reconnect bool
	// Interval and Dead time the heartbeat: a ping every Interval, the
	// link is gone after Dead without a byte (0: 5 s and 15 s).
	Interval, Dead time.Duration
}

// msgSSHLeft is what termocode ssh prints when the user leaves while the
// link is down.
const msgSSHLeft = "termocode: left; the session keeps running on the far side. Run the same termocode ssh command to go back."

// msgSSHDropped is printed when the link dropped and reconnecting is off.
const msgSSHDropped = "termocode: the connection dropped; the session keeps running on the far side. Run the same termocode ssh command to go back."

// sshAttempt is how one ssh connection ended.
type sshAttempt struct {
	code int  // ssh's exit code
	quit bool // the user or a signal ended termocode ssh: stop here
	dead bool // the heartbeat gave the link up and ssh was killed
}

// lost reports whether the link was lost (ssh says 255, or the heartbeat
// gave it up) rather than the far termocode ending (its own exit code).
func (a sshAttempt) lost() bool { return a.dead || a.code == 255 }

// sshTerm is the local terminal side of RunSSH, shared by its attempts.
type sshTerm struct {
	o    SSHOpts
	out  *lockedWriter
	keys <-chan []byte
	sigs <-chan os.Signal
	seen *Modes // the far UI's terminal modes, to undo them on a lost link

	everUp  bool      // a far termocode answered once: reconnecting makes sense
	offline bool      // the link is down; reconnecting
	since   time.Time // when it went down
	dropped bool      // keys typed while offline were dropped
	tries   int       // the reconnect attempt shown
	unicode bool      // the local terminal takes UTF-8
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
	if o.Interval <= 0 {
		o.Interval = 5 * time.Second
	}
	if o.Dead <= 0 {
		o.Dead = 15 * time.Second
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
	local := EnvCaps(os.Getenv)
	t := &sshTerm{o: o, out: &lockedWriter{w: o.Out}, keys: readKeys(o.In, stop), sigs: sigs, seen: NewModes(),
		unicode: clientUnicode(&local)}
	reconnected := ""
	for {
		a := t.attempt(reconnected)
		if a.quit || !a.lost() || !t.everUp {
			return a.code
		}
		if !o.Reconnect {
			t.leave(msgSSHDropped)
			return a.code
		}
		if !t.offline {
			t.offline, t.since, t.dropped, t.tries = true, time.Now(), false, 0
		}
		t.tries++
		t.drawOffline()
		if t.tries > 1 && t.wait(reconnectDelay(t.tries-1)) {
			t.leave(msgSSHLeft)
			return 0
		}
		reconnected = fmt.Sprintf("%ds", int(time.Since(t.since).Round(time.Second)/time.Second))
		if t.dropped {
			reconnected += ",dropped"
		}
	}
}

// leave puts the terminal back to normal and prints msg.
func (t *sshTerm) leave(msg string) {
	_, _ = t.out.Write(append(t.seen.Reset(), plainReset+msg+"\r\n"...))
}

// drawOffline draws the offline status line.
func (t *sshTerm) drawOffline() {
	cols, rows := t.size()
	_, _ = t.out.WriteString(offlineLine(t.tries, cols, rows, t.unicode))
}

// offlineKey handles keys typed while the link is down: Ctrl+C asks to
// quit, the rest is dropped (and noted).
func (t *sshTerm) offlineKey(k []byte) (quit bool) {
	if offlineQuit(k) {
		return true
	}
	t.dropped = t.dropped || offlineKeys(k)
	return false
}

// wait waits d before the next attempt; it reports whether the user
// quit meanwhile.
func (t *sshTerm) wait(d time.Duration) (quit bool) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			return false
		case k, ok := <-t.keys:
			if !ok || t.offlineKey(k) {
				return true
			}
		case s := <-t.sigs:
			if s != syscall.SIGWINCH {
				return true
			}
			t.drawOffline()
		}
	}
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
	// The heartbeat runs once the far termocode said hello (an older one
	// never does, and never gets a ping).
	start := time.Now()
	var mon *linkMonitor
	tick := time.NewTicker(min(time.Second, t.o.Interval/4))
	defer tick.Stop()
	dead := false
	// While reconnecting, ssh may talk before the far termocode does: an
	// error, or a question (password, passphrase, host key). Its text
	// goes on the bottom row; a question (a line without its newline)
	// gets the keys until it is answered.
	shown, asking := false, false

	for {
		select {
		case <-tick.C:
			if mon == nil || dead {
				continue
			}
			ping, gone, _ := mon.Tick(realClock(start))
			if ping {
				_, _ = ptmx.Write([]byte(linkPing))
			}
			if gone {
				dead = true
				kill()
			}
		case k, ok := <-keys:
			if !ok { // the terminal is gone
				keys = nil
				kill()
				continue
			}
			if t.offline && !asking {
				if t.offlineKey(k) {
					kill()
					<-exited
					t.leave(msgSSHLeft)
					return sshAttempt{code: 0, quit: true}
				}
				continue
			}
			_, _ = ptmx.Write(k)
		case b, ok := <-output:
			if !ok {
				output = nil
				continue
			}
			b, found := link.Filter(b)
			for _, i := range found {
				if i == 0 { // linkHello: the far termocode is there
					t.everUp, t.offline = true, false
					mon = newLinkMonitor(t.o.Interval, t.o.Dead, realClock(start))
				}
			}
			if mon != nil {
				mon.Received(realClock(start))
			}
			if len(b) > 0 && t.offline {
				if !shown {
					_, rows := t.size()
					_, _ = fmt.Fprintf(t.out, "\x1b[%d;1H\x1b[0m\x1b[2K", rows)
					shown = true
				}
				_, _ = t.out.Write(b)
				asking = b[len(b)-1] != '\n'
			} else if len(b) > 0 {
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
			if !dead {
				t.drain(output, link)
			}
			return sshAttempt{code: code, dead: dead}
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
