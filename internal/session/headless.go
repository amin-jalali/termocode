//go:build unix

package session

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// A project the phone opens (MOB-51, mobile plan §3.7): the door's home UI
// starts that folder's session with its UI but no terminal, waits for its
// web hub socket, and the door gateway then joins that hub. `termocode
// attach` in the folder shows it on the laptop later, as for any session.

// StartHeadless makes sure the session of p runs with its UI started and
// no terminal attached; spawn starts a server when none runs. started is
// false when the UI ran already (then idle changes nothing).
func StartHeadless(p Paths, idle int, spawn func() error) (started bool, err error) {
	before, serr := Status(p)
	c, err := Ensure(p, spawn)
	if err != nil {
		return false, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	if err := writeFrame(c, frameStart, encodeCode(idle)); err != nil {
		return false, err
	}
	typ, b, err := readFrame(c)
	if err != nil {
		return false, err
	}
	switch typ {
	case frameStatus:
		return serr != nil || before.UIPID == 0, nil
	case frameDetached:
		return false, errors.New(strings.TrimPrefix(string(b), "termocode: "))
	}
	return false, fmt.Errorf("unexpected reply %q", typ)
}

// WaitWebHub waits until the UI of p serves its web hub: the socket is
// there and, when fresh is set, was made at or after it (a socket a
// crashed run left behind does not count).
func WaitWebHub(p Paths, fresh time.Time, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		if fi, err := os.Stat(p.WebSocket); err == nil && fi.Mode()&os.ModeSocket != 0 &&
			(fresh.IsZero() || !fi.ModTime().Before(fresh.Add(-time.Second))) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the project's web hub did not start within %s (see %s)", d, p.Log)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// UIRunning reports whether the session of p runs with its UI started
// and its web hub socket in place (nothing is dialed on the hub).
func UIRunning(p Paths) bool {
	in, err := Status(p)
	if err != nil || in.UIPID == 0 {
		return false
	}
	fi, err := os.Stat(p.WebSocket)
	return err == nil && fi.Mode()&os.ModeSocket != 0
}
