//go:build unix

package webgw

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The gateway process (WEB-9): `termocode __web-gateway`. The UI starts
// it when the user opens the web panel. It lives while the UI does (the
// control connection) and stops on the first of: the kill switch on the
// laptop (4009), the UI going away (4012), no use for Idle (4006), Max
// after it began (4007), or SIGTERM from the UI (4009).

// RunConfig sets up one gateway run.
type RunConfig struct {
	// Dial opens a connection to the hub (<key>.web.sock).
	Dial func(ctx context.Context) (net.Conn, error)
	// AuditPath is the JSONL audit log (0600). A log that cannot be
	// opened stops the start.
	AuditPath string
	// StatusPath gets the status file (0600) while the gateway runs.
	StatusPath string
	// LockPath is held (flock) while the gateway runs: one per workspace.
	LockPath string
	// Port is 0 for a random free port.
	Port int
	// Idle, Max and MaxViewers are the owner's limits (0: the defaults).
	Idle, Max  time.Duration
	MaxViewers int
	// Handoff gets {"port":N} once the gateway serves and the first
	// launch link is set; it is closed then when it is an io.Closer.
	Handoff io.Writer
	// Assets serves the page (nil: everything but /auth and /ws is 404).
	Assets http.Handler
	// LinkWait is how long the start waits for the first launch link.
	LinkWait time.Duration
	// Now is the clock of tokens and links (nil: time.Now).
	Now func() time.Time
}

// Status is what the gateway writes to its status file. It holds no
// secret: `termocode web status` and the UI read it.
type Status struct {
	PID     int       `json:"pid"`
	Port    int       `json:"port"`
	URL     string    `json:"url"`
	Started time.Time `json:"started"`
	IdleS   int       `json:"idle_s"`
	MaxS    int       `json:"max_s"`
}

// ErrGatewayBusy: another gateway already runs for this workspace.
var ErrGatewayBusy = errors.New("the web panel already runs for this folder")

// ErrNoLink: the UI did not send the launch link in time.
var ErrNoLink = errors.New("no launch link from termocode")

// Run serves until the gateway stops and says why (a close code). It
// returns an error only when the start failed.
func Run(ctx context.Context, cfg RunConfig) (webproto.CloseCode, error) {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	if cfg.Idle <= 0 {
		cfg.Idle = DefaultIdle
	}
	if cfg.Max <= 0 {
		cfg.Max = DefaultMax
	}
	if cfg.LinkWait <= 0 {
		cfg.LinkWait = 5 * time.Second
	}
	unlock, err := lockFile(cfg.LockPath)
	if err != nil {
		return 0, err
	}
	defer unlock()
	audit, err := OpenAudit(cfg.AuditPath, now)
	if err != nil {
		return 0, err
	}
	defer audit.Close()
	ln, site, err := Listen(ListenConfig{Port: cfg.Port})
	if err != nil {
		return 0, err
	}
	defer ln.Close()

	tokens := NewTokens(cfg.Idle, cfg.Max)
	auth := NewAuth(tokens, now, audit)
	linked := make(chan struct{}, 1)
	ctl, err := DialCtl(ctx, cfg.Dial, func(d [sha256.Size]byte) {
		auth.SetSecretDigest(d)
		select {
		case linked <- struct{}{}:
		default:
		}
	})
	if err != nil {
		return 0, fmt.Errorf("reach termocode: %w", err)
	}
	defer ctl.Close()

	var last atomic.Int64 // unix nanos of the last use
	last.Store(time.Now().UnixNano())
	ws := NewWS(WSConfig{Site: site, Tokens: tokens, Audit: audit, Dial: cfg.Dial, Now: now,
		MaxViewers: cfg.MaxViewers, Activity: func() { last.Store(time.Now().UnixNano()) }})
	mux := http.NewServeMux()
	mux.Handle("/auth", auth)
	mux.Handle("/ws", ws)
	if cfg.Assets != nil {
		mux.Handle("/", cfg.Assets)
	}
	srv := NewServer(site, mux, DefaultLimits, audit)
	crash.Go("web gateway: the server", func() { _ = srv.Serve(LimitConns(ln, DefaultLimits.MaxConns)) })
	defer func() { _ = srv.Close() }()

	select {
	case <-linked:
	case <-ctl.Done():
		return 0, fmt.Errorf("reach termocode: %w", ErrNoLink)
	case <-time.After(cfg.LinkWait):
		return 0, ErrNoLink
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	port := ln.Addr().(*net.TCPAddr).Port
	started := time.Now()
	st := Status{PID: os.Getpid(), Port: port, URL: "http://" + site.Hosts[0] + "/", Started: started,
		IdleS: int(cfg.Idle / time.Second), MaxS: int(cfg.Max / time.Second)}
	if err := writeStatus(cfg.StatusPath, st); err != nil {
		return 0, err
	}
	defer func() { _ = os.Remove(cfg.StatusPath) }()
	if err := audit.Log(Event{Kind: EvStart, Summary: "127.0.0.1:" + strconv.Itoa(port)}); err != nil {
		return 0, err
	}
	if cfg.Handoff != nil {
		_ = json.NewEncoder(cfg.Handoff).Encode(map[string]int{"port": port})
		if c, ok := cfg.Handoff.(io.Closer); ok {
			_ = c.Close()
		}
	}

	code, kind, why := waitStop(ctx, ctl, &last, cfg.Idle, started.Add(cfg.Max))
	if code == webproto.CloseStopped {
		tokens.RevokeAll()
	}
	_ = audit.Log(Event{Kind: kind, Code: int(code)})
	ws.CloseAll(code, time.Second)
	_ = srv.Close()
	ws.Wait()
	_ = audit.Log(Event{Kind: EvStop, Code: int(code), Why: why})
	return code, nil
}

// waitStop waits for the first reason to stop. The idle timer is one
// timer moved forward only when it fires early: no ticking while idle.
func waitStop(ctx context.Context, ctl *Ctl, last *atomic.Int64, idle time.Duration, maxAt time.Time) (webproto.CloseCode, EventKind, string) {
	idleT := time.NewTimer(idle)
	defer idleT.Stop()
	maxT := time.NewTimer(time.Until(maxAt))
	defer maxT.Stop()
	for {
		select {
		case <-ctl.Done():
			if ctl.Code() == webproto.CloseStopped {
				return webproto.CloseStopped, EvKillSwitch, "kill_switch"
			}
			return webproto.CloseHubGone, EvStop, "hub_gone"
		case <-ctx.Done():
			return webproto.CloseStopped, EvKillSwitch, "signal"
		case <-maxT.C:
			return webproto.CloseMaxLifetime, EvMaxStop, "max"
		case <-idleT.C:
			since := time.Since(time.Unix(0, last.Load()))
			if since >= idle {
				return webproto.CloseIdle, EvIdleStop, "idle"
			}
			idleT.Reset(idle - since)
		}
	}
}

// lockFile takes an exclusive flock on path (0600) or says
// ErrGatewayBusy. The returned func lets go.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrGatewayBusy
		}
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}

// writeStatus writes the status file atomically with mode 0600.
func writeStatus(path string, st Status) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".web-status-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ReadStatus reads a gateway's status file.
func ReadStatus(path string) (Status, error) {
	var st Status
	b, err := os.ReadFile(path)
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(b, &st)
}
