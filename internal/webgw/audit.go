//go:build unix

package webgw

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
)

// EventKind names what happened. The set is closed: every kind is listed
// in AllEventKinds.
type EventKind string

// The audit events of the gateway.
const (
	EvStart         EventKind = "start"          // the gateway began listening
	EvStop          EventKind = "stop"           // it stopped (Why says how)
	EvAuthOK        EventKind = "auth_ok"        // a launch secret became a session
	EvAuthFail      EventKind = "auth_fail"      // /auth refused (Why: the rule)
	EvSecretBurned  EventKind = "secret_burned"  // too many wrong secrets
	EvRateLimited   EventKind = "rate_limited"   // a request over a rate limit
	EvConnect       EventKind = "connect"        // a WebSocket session began
	EvDisconnect    EventKind = "disconnect"     // it ended (Code: close code)
	EvProtoError    EventKind = "proto_error"    // a bad message (Why: the rule)
	EvOriginBlocked EventKind = "origin_blocked" // the guard refused an Origin
	EvHostBlocked   EventKind = "host_blocked"   // the guard refused a Host
	EvIdleStop      EventKind = "idle_stop"      // stopped after the idle time
	EvMaxStop       EventKind = "max_stop"       // stopped at the max lifetime
	EvKillSwitch    EventKind = "kill_switch"    // the user stopped the panel
	EvOp            EventKind = "op"             // a browser request (Op, OK, Summary)
)

// AllEventKinds lists every kind, for tests and docs.
var AllEventKinds = []EventKind{
	EvStart, EvStop, EvAuthOK, EvAuthFail, EvSecretBurned, EvRateLimited,
	EvConnect, EvDisconnect, EvProtoError, EvOriginBlocked, EvHostBlocked,
	EvIdleStop, EvMaxStop, EvKillSwitch, EvOp,
}

// Event is one audit line. It has no field for a secret, a token, a
// request body, chat, file or terminal text: only ids, fixed names and a
// short summary built by the gateway's policy (a path, a label, a count).
type Event struct {
	T       time.Time `json:"t"`
	Kind    EventKind `json:"ev"`
	Conn    string    `json:"conn,omitempty"`    // session id (Sess.ID)
	Device  string    `json:"device,omitempty"`  // device id
	Op      string    `json:"op,omitempty"`      // request name, for EvOp
	OK      *bool     `json:"ok,omitempty"`      // the result, for EvOp
	Code    int       `json:"code,omitempty"`    // HTTP status or close code
	Why     string    `json:"why,omitempty"`     // fixed reason name
	Summary string    `json:"summary,omitempty"` // short, cleaned
}

// Audit is the gateway's append-only JSONL log, mode 0600. A nil *Audit
// logs nothing, so code runs the same with no log configured.
type Audit struct {
	path string
	now  func() time.Time

	mu   sync.Mutex
	f    *os.File
	size int64
}

// OpenAudit opens (or creates) the log at path for appending. It refuses
// a symlink or anything but a regular file we own, and makes a looser
// file 0600. now is the clock (nil: time.Now).
func OpenAudit(path string, now func() time.Time) (*Audit, error) {
	if now == nil {
		now = time.Now
	}
	a := &Audit{path: path, now: now}
	if err := a.open(); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Audit) open() error {
	f, err := os.OpenFile(a.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("open web audit log: %w", err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !fi.Mode().IsRegular() || !ok || int(st.Uid) != os.Getuid() {
		_ = f.Close()
		return fmt.Errorf("web audit log %s is not a regular file of yours", a.path)
	}
	if fi.Mode().Perm() != 0o600 {
		if err := f.Chmod(0o600); err != nil {
			_ = f.Close()
			return err
		}
	}
	a.f, a.size = f, fi.Size()
	return nil
}

// Log appends e as one JSON line. A zero e.T is set to now.
func (a *Audit) Log(e Event) error {
	if a == nil {
		return nil
	}
	if e.T.IsZero() {
		e.T = a.now()
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return os.ErrClosed
	}
	n, err := a.f.Write(line)
	a.size += int64(n)
	return err
}

// Close closes the log.
func (a *Audit) Close() error {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return nil
	}
	err := a.f.Close()
	a.f = nil
	return err
}
