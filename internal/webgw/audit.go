//go:build unix

package webgw

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
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
	EvFunnelStop    EventKind = "funnel_stop"    // stopped: the tailnet port became public
	EvServeGone     EventKind = "serve_gone"     // stopped: tailscale serve ended
	EvPairStart     EventKind = "pair_start"     // a phone used a pairing link (Device: its name)
	EvPairOK        EventKind = "pair_ok"        // the owner said yes: a device was paired
	EvPairFail      EventKind = "pair_fail"      // a pairing was refused (Why: the rule or denied)
)

// AllEventKinds lists every kind, for tests and docs.
var AllEventKinds = []EventKind{
	EvStart, EvStop, EvAuthOK, EvAuthFail, EvSecretBurned, EvRateLimited,
	EvConnect, EvDisconnect, EvProtoError, EvOriginBlocked, EvHostBlocked,
	EvIdleStop, EvMaxStop, EvKillSwitch, EvOp, EvFunnelStop, EvServeGone,
	EvPairStart, EvPairOK, EvPairFail,
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
	By      string    `json:"by,omitempty"`      // who asked, for EvOp: "web:<device>"
}

// Rotation: the log moves to <path>.1 before it would pass AuditMaxBytes;
// AuditKeep old files are kept.
const (
	AuditMaxBytes = 5 << 20
	AuditKeep     = 2
)

// Audit is the gateway's append-only JSONL log, mode 0600. A nil *Audit
// logs nothing, so code runs the same with no log configured.
type Audit struct {
	path string
	now  func() time.Time
	max  int64 // rotate before a write would pass this size

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
	a := &Audit{path: path, now: now, max: AuditMaxBytes}
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

// Log appends e as one JSON line. A zero e.T is set to now. Every text
// field passes cleanAuditText first.
func (a *Audit) Log(e Event) error {
	if a == nil {
		return nil
	}
	if e.T.IsZero() {
		e.T = a.now()
	}
	for _, f := range []*string{&e.Conn, &e.Device, &e.Op, &e.Why, &e.Summary, &e.By} {
		*f = cleanAuditText(*f)
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
	if a.size > 0 && a.size+int64(len(line)) > a.max {
		if err := a.rotate(); err != nil {
			return err
		}
	}
	n, err := a.f.Write(line)
	a.size += int64(n)
	return err
}

// rotate moves path to path.1 (path.1 to path.2, …), drops the oldest and
// opens a fresh path. a.mu is held. Renames keep the 0600 mode.
func (a *Audit) rotate() error {
	if err := a.f.Close(); err != nil {
		return err
	}
	a.f = nil
	old := func(i int) string { return fmt.Sprintf("%s.%d", a.path, i) }
	if err := os.Remove(old(AuditKeep)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for i := AuditKeep - 1; i >= 1; i-- {
		if err := os.Rename(old(i), old(i+1)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Rename(a.path, old(1)); err != nil {
		return err
	}
	return a.open()
}

// Audit text limits: a run of maskRunLen or more token characters looks
// like a key or token and is masked; a field is at most maxAuditText runes.
const (
	maskRunLen   = 24
	maxAuditText = 200
)

// cleanAuditText makes one text field safe for the log, even if a caller
// passed something it should not have: invalid UTF-8 is replaced; control
// and bidi characters are dropped; any long token-like run (keys, tokens,
// hex digests) becomes "[masked]"; the result is capped.
func cleanAuditText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	var out []rune
	var run []rune
	flush := func() {
		if len(run) >= maskRunLen {
			out = append(out, []rune("[masked]")...)
		} else {
			out = append(out, run...)
		}
		run = run[:0]
	}
	for _, r := range s {
		switch {
		case unicode.IsControl(r) || isBidiControl(r):
			continue
		case isTokenRune(r):
			run = append(run, r)
		default:
			flush()
			out = append(out, r)
		}
	}
	flush()
	if len(out) > maxAuditText {
		out = append(out[:maxAuditText-1], '…')
	}
	return string(out)
}

// isTokenRune: the characters keys and tokens are made of (base64, base64url,
// hex, and the dashes of prefixed keys). Not '/' or '.', so paths stay.
func isTokenRune(r rune) bool {
	return r < 0x80 && (unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-' || r == '+' || r == '=')
}

// isBidiControl: marks, embeddings, overrides and isolates that can make
// a log line read differently from what it holds.
func isBidiControl(r rune) bool {
	switch {
	case r == 0x061C, r == 0x200E, r == 0x200F:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
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
