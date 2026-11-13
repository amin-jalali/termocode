//go:build unix

package webgw

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/webdevices"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The gateway process (WEB-9): `termocode __web-gateway`. The UI starts
// it when the user opens the web panel. It lives while the UI does (the
// control connection) and stops on the first of: the kill switch on the
// laptop (4009), the UI going away (4012), no use for Idle (4006), Max
// after it began (4007), or SIGTERM from the UI (4009). With PhoneWatch
// a paired phone that watches is use (MOB-31); nothing else changes.

// RunConfig sets up one gateway run.
type RunConfig struct {
	// Dial opens a connection to the hub (<key>.web.sock).
	Dial func(ctx context.Context) (net.Conn, error)
	// Home and DialWS make the gateway the door to the user's other
	// running workspaces (MOB-50): Home is this workspace's key, DialWS
	// opens another workspace's hub by key (nil: home only). MaxHubs caps
	// them per browser (0: webproto.MaxHubs).
	Home    string
	DialWS  func(ctx context.Context, ws string) (net.Conn, error)
	MaxHubs int
	// AuditPath is the JSONL audit log (0600). A log that cannot be
	// opened stops the start.
	AuditPath string
	// StatusPath gets the status file (0600) while the gateway runs.
	StatusPath string
	// LockPath is held (flock) while the gateway runs: one per workspace.
	LockPath string
	// Port is 0 for a random free port.
	Port int
	// UnixPath, when set, is SSH mode (WEB-29): the gateway serves on this
	// 0600 unix socket instead of TCP, and answers to 127.0.0.1:PortHint,
	// the port the user's own ssh forwards to it on their computer.
	UnixPath string
	PortHint int
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
	// DevicesDir, when set, holds the paired devices (WEB-32) and turns
	// on the pairing endpoints.
	DevicesDir string
	// RequireTOTP asks a paired device for a one-time code after its
	// passkey (WEB-48); the UI checks it (web.totp_check).
	RequireTOTP bool
	// AppKeyLogin lets the Android app pair and sign in with its keystore
	// key when passkeys fail in its WebView (web_app_key_login, MOB-27).
	AppKeyLogin bool
	// PhoneWatch (web_phone "on", MOB-31): a paired, non-guest device
	// whose link answers the pings counts as use, so a phone that only
	// watches keeps the gateway up. Max still ends it.
	PhoneWatch bool
	// Tailnet, when set, is tailnet mode (WEB-31): still 127.0.0.1 only,
	// reached from the user's tailnet through tailscale serve on
	// https://<machine>.<tailnet>.ts.net/; Funnel is refused.
	Tailnet *TailnetConfig
	// LAN, when set, is LAN mode: https on one private LAN address with a
	// stored self-signed certificate; the pairing link pins its
	// fingerprint. No passkeys (an IP address has no name for them).
	LAN *LANConfig
	// ResumePath gets the hand-over file when the hub's bye is 1012
	// (termocode reloads into a new build: resume.go); "" writes none.
	ResumePath string
	// Resume, when set, is the last gateway's hand-over: its browser
	// sessions go on here, and Max counts from its start. ResumeFrom is
	// a hand-over file read (and removed) once the lock is held.
	Resume     *Resume
	ResumeFrom string
	// Relay, when set, is web_relay (ADR 0022): the gateway also waits in
	// its room on this relay and serves paired phones through it. It
	// needs DevicesDir. The key and room come later, from the UI
	// (EvRelayKey).
	Relay *RelayConfig
}

// Status is what the gateway writes to its status file. It holds no
// secret: `termocode web status` and the UI read it.
type Status struct {
	PID     int       `json:"pid"`
	Port    int       `json:"port"`
	URL     string    `json:"url"`
	Sock    string    `json:"sock,omitempty"` // SSH mode: the unix socket
	Started time.Time `json:"started"`
	IdleS   int       `json:"idle_s"`
	MaxS    int       `json:"max_s"`
	// PhoneWatch: a watching phone keeps the gateway up (MOB-31).
	PhoneWatch bool `json:"phone_watch,omitempty"`
	// Relay is the relay's address with web_relay on (ADR 0022).
	Relay string `json:"relay,omitempty"`
}

// handoff is what the gateway writes on fd 3 once it serves: the port
// and the address a browser uses (the tailnet URL in tailnet mode).
type handoff struct {
	Port int    `json:"port"`
	URL  string `json:"url"`
	// FP is LAN mode's certificate fingerprint (base64url SHA-256).
	FP string `json:"fp,omitempty"`
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
	if cfg.Resume == nil && cfg.ResumeFrom != "" {
		// Only now, with the lock: a start that fails on a gateway still
		// ending leaves the hand-over for the next try.
		if r, err := ReadResume(cfg.ResumeFrom, now()); err == nil {
			cfg.Resume = &r
		}
	}
	audit, err := OpenAudit(cfg.AuditPath, now)
	if err != nil {
		return 0, err
	}
	defer audit.Close()
	var tn *tailnetRun
	var tnSite Site
	if cfg.Tailnet != nil && cfg.UnixPath == "" {
		if tn, tnSite, err = prepareTailnet(ctx, *cfg.Tailnet); err != nil {
			return 0, err
		}
	}
	var ln net.Listener
	var site Site
	port := cfg.Port
	var lanFP string
	switch {
	case cfg.UnixPath != "":
		ln, site, err = ListenUnix(cfg.UnixPath, cfg.PortHint)
		port = cfg.PortHint
	case cfg.LAN != nil:
		ln, site, lanFP, err = listenLAN(*cfg.LAN, cfg.Port, now())
	default:
		ln, site, err = Listen(ListenConfig{Port: cfg.Port, Tailnet: tn != nil})
	}
	if err != nil {
		return 0, err
	}
	if tn != nil {
		site = tnSite
	}
	defer ln.Close()
	if cfg.UnixPath != "" {
		defer func() { _ = os.Remove(cfg.UnixPath) }()
	} else {
		port = ln.Addr().(*net.TCPAddr).Port
	}

	tokens := NewTokens(cfg.Idle, cfg.Max)
	if cfg.Resume != nil {
		tokens.Import(cfg.Resume.Tokens, now())
	}
	auth := NewAuth(tokens, now, audit)
	linked := make(chan struct{}, 1)
	var ctlRef atomic.Pointer[Ctl] // set once the control connection is up
	var pairing *Pairing
	var devices *webdevices.Store
	var wsRef atomic.Pointer[WS]
	var relayRef atomic.Pointer[relayLink]
	var watchRef atomic.Pointer[Watch]
	var login *Login
	var watchToks *WatchTokens
	// The merged inbox the watch socket reads (MOB-23, MOB-53). The home
	// key only has to be a stable workspace key of this gateway: the lock
	// file is one per workspace.
	lockSum := sha256.Sum256([]byte(cfg.LockPath))
	inbox := NewInboxMux(hex.EncodeToString(lockSum[:webproto.InboxWSLen/2]))
	var push *UnifiedPush // MOB-32: paired phones' wakes
	pushCtx, stopPush := context.WithCancel(ctx)
	defer stopPush()
	if cfg.DevicesDir != "" {
		devices = webdevices.Open(cfg.DevicesDir)
		var werr error
		if watchToks, werr = OpenWatchTokens(cfg.DevicesDir); werr != nil {
			f := false
			_ = audit.Log(Event{Kind: EvOp, Op: webproto.OpAppWatchToken, OK: &f, Why: "damaged_file"})
		}
		var pk *Passkeys
		if cfg.LAN == nil {
			if pk, err = NewPasskeys(site, devices, now); err != nil {
				return 0, fmt.Errorf("passkeys: %w", err)
			}
		}
		pairing = NewPairing(PairingConfig{Tokens: tokens, Store: devices, Audit: audit, Now: now, Passkeys: pk, AppKeys: cfg.AppKeyLogin,
			Ask: func(ctx context.Context, a webproto.PairAskArgs) (string, error) {
				return askPair(ctx, ctlRef.Load(), a)
			}})
		// The device list itself, not only through the passkeys: LAN mode has
		// none, and its remembered browsers sign in with their key alone.
		login = NewLogin(LoginConfig{Tokens: tokens, Store: devices, Passkeys: pk, Audit: audit, Now: now, RequireTOTP: cfg.RequireTOTP, AppKeys: cfg.AppKeyLogin,
			CheckTOTP: func(ctx context.Context, device, code string) error {
				c := ctlRef.Load()
				if c == nil {
					return ErrCtlGone
				}
				_, err := c.Call(ctx, webproto.OpWebTotpCheck, webproto.TotpCheckArgs{Code: code, Device: device})
				return err
			}})
		push = NewUnifiedPush(UnifiedPushConfig{Now: now, Audit: audit,
			Live: func(device string) bool {
				w := wsRef.Load()
				return w != nil && w.HasDevice(device)
			},
			Watch: func(ctx context.Context, on bool) error {
				c := ctlRef.Load()
				if c == nil {
					return ErrCtlGone
				}
				_, err := c.Call(ctx, webproto.OpWebPushWatch, webproto.PushWatchArgs{On: on})
				return err
			}})
		crash.Go("web gateway: the push watch", func() { push.Run(pushCtx) })
		defer func() { stopPush(); push.Wait() }()
	}
	ctl, err := DialCtlWith(ctx, cfg.Dial, CtlHandlers{Link: func(d [sha256.Size]byte) {
		auth.SetSecretDigest(d)
		select {
		case linked <- struct{}{}:
		default:
		}
	}, Audit: func(n webproto.AuditNote) {
		ok := n.OK
		_ = audit.Log(Event{Kind: EvOp, Conn: n.Conn, Device: n.Device, Op: n.Op, OK: &ok,
			Why: n.Why, Summary: n.Summary, By: "web:" + n.Device})
	}, Event: func(kind string, data json.RawMessage) {
		onCtlEvent(kind, data, ctlTargets{pairing: pairing, tokens: tokens, ws: wsRef.Load(), audit: audit,
			watchToks: watchToks, watch: watchRef.Load(), inbox: inbox, push: push, ctx: pushCtx, relay: relayRef.Load()})
	}})
	if err != nil {
		return 0, fmt.Errorf("reach termocode: %w", err)
	}
	ctlRef.Store(ctl)
	defer ctl.Close()

	var last atomic.Int64 // unix nanos of the last use
	last.Store(time.Now().UnixNano())
	var control *Control
	if pairing != nil {
		control = &Control{Passkeys: pairing.cfg.Passkeys, Tokens: tokens, Store: devices, Audit: audit, Now: now, Laptop: laptopName(),
			Ask: func(ctx context.Context, a webproto.GrantAskArgs) error {
				c := ctlRef.Load()
				if c == nil {
					return ErrCtlGone
				}
				_, err := c.Call(ctx, webproto.OpWebGrantAsk, a)
				return err
			}}
	}
	ws := NewWS(WSConfig{Site: site, Tokens: tokens, Audit: audit, Dial: cfg.Dial, Now: now, Devices: devices, Control: control,
		Push: push, WatchTokens: watchToks, MaxViewers: cfg.MaxViewers, PhoneWatch: cfg.PhoneWatch, Activity: func() { last.Store(time.Now().UnixNano()) },
		Home: cfg.Home, DialWS: cfg.DialWS, MaxHubs: cfg.MaxHubs})
	wsRef.Store(ws)
	mux := siteMux(site, auth, ws, cfg.Assets)
	if pairing != nil {
		mux.Handle("/pair/", pairing)
		mux.Handle("/login/", login)
		watch := &Watch{Tokens: watchToks, Store: devices, Inbox: inbox, Audit: audit, Now: now,
			Watching: func(ctx context.Context, on bool) error {
				c := ctlRef.Load()
				if c == nil {
					return ErrCtlGone
				}
				_, err := c.Call(ctx, webproto.OpWebWatching, webproto.WatchingArgs{On: on})
				return err
			}}
		watchRef.Store(watch)
		mux.Handle("/watch", watch)
	}
	srv := NewServer(site, mux, DefaultLimits, audit)
	crash.Go("web gateway: the server", func() { _ = srv.Serve(LimitConns(ln, DefaultLimits.MaxConns)) })
	defer func() { _ = srv.Close() }()
	var rl *relayLink
	if cfg.Relay != nil {
		if pairing == nil {
			return 0, errors.New("the relay needs the paired-device folder")
		}
		if _, err := CheckRelayURL(cfg.Relay.URL); err != nil {
			return 0, err
		}
		rl = newRelayLink(*cfg.Relay, site, mux, audit, devices, pairing)
		relayRef.Store(rl)
		defer rl.stop()
	}

	url := site.Scheme + "://" + site.Hosts[0] + "/"
	where := "127.0.0.1:" + strconv.Itoa(port)
	if cfg.LAN != nil {
		where = site.Hosts[0] + " on the LAN (https, pinned certificate)"
	}
	tailCh := make(chan tailStop, 1)
	if tn != nil {
		if err := tn.serve(ctx, port); err != nil {
			return 0, err
		}
		defer tn.stop()
		url = tn.URL()
		where += " via tailscale serve " + url
		wctx, stopWatch := context.WithCancel(ctx)
		defer stopWatch()
		crash.Go("web gateway: the tailnet watch", func() { tn.watch(wctx, tailCh) })
	}
	select {
	case <-linked:
	case <-ctl.Done():
		return 0, fmt.Errorf("reach termocode: %w", ErrNoLink)
	case <-time.After(cfg.LinkWait):
		return 0, ErrNoLink
	case <-ctx.Done():
		return 0, ctx.Err()
	}
	started := time.Now()
	if cfg.Resume != nil && !cfg.Resume.Started.IsZero() && cfg.Resume.Started.Before(started) {
		started = cfg.Resume.Started // a reload does not make the panel's Max longer
		where += ", after a reload"
	}
	st := Status{PID: os.Getpid(), Port: port, URL: url, Sock: cfg.UnixPath, Started: started,
		IdleS: int(cfg.Idle / time.Second), MaxS: int(cfg.Max / time.Second), PhoneWatch: cfg.PhoneWatch}
	if cfg.Relay != nil {
		st.Relay = cfg.Relay.URL
	}
	if err := writeStatus(cfg.StatusPath, st); err != nil {
		return 0, err
	}
	defer func() { _ = os.Remove(cfg.StatusPath) }()
	if cfg.UnixPath != "" {
		where += " over ssh"
	}
	if cfg.PhoneWatch {
		where += ", a watching phone keeps it up"
	}
	if err := audit.Log(Event{Kind: EvStart, Summary: where}); err != nil {
		return 0, err
	}
	if cfg.Handoff != nil {
		_ = json.NewEncoder(cfg.Handoff).Encode(handoff{Port: port, URL: url, FP: lanFP})
		if c, ok := cfg.Handoff.(io.Closer); ok {
			_ = c.Close()
		}
	}

	code, kind, why := waitStop(ctx, ctl, &last, cfg.Idle, started.Add(cfg.Max), tailCh)
	if code == webproto.CloseStopped {
		tokens.RevokeAll()
	}
	if code == webproto.CloseRestarting && cfg.ResumePath != "" {
		res := Resume{At: now(), Port: port, Started: started, Tokens: tokens.Export(now())}
		if cfg.UnixPath != "" {
			res.SSHPort = cfg.PortHint
		}
		if err := WriteResume(cfg.ResumePath, res); err != nil {
			why = "restart: " + err.Error() // the pages need a new link then
		}
	}
	_ = audit.Log(Event{Kind: kind, Code: int(code)})
	ws.CloseAll(code, time.Second)
	_ = srv.Close()
	if rl != nil {
		rl.stop() // its sessions' WebSockets are closed above, with the code
	}
	ws.Wait()
	_ = audit.Log(Event{Kind: EvStop, Code: int(code), Why: why})
	return code, nil
}

// listenLAN opens LAN mode's listener: the stored certificate (made on
// the first start) on the configured or picked private address.
func listenLAN(cfg LANConfig, port int, now time.Time) (net.Listener, Site, string, error) {
	if cfg.CertDir == "" {
		return nil, Site{}, "", errors.New("LAN mode needs the paired-device folder")
	}
	ip := cfg.IP
	if ip == nil {
		var ok bool
		if ip, ok = PickLANAddr(LANAddrs()); !ok {
			return nil, Site{}, "", ErrNoLAN
		}
	}
	cert, fp, err := LoadOrCreateLANCert(cfg.CertDir, now)
	if err != nil {
		return nil, Site{}, "", fmt.Errorf("LAN certificate: %w", err)
	}
	ln, site, err := ListenLAN(ip, port, cert)
	if err != nil {
		return nil, Site{}, "", err
	}
	return ln, site, fp, nil
}

// askPair asks the laptop to pair a device and returns the owner's role.
func askPair(ctx context.Context, ctl *Ctl, a webproto.PairAskArgs) (string, error) {
	if ctl == nil {
		return "", ErrCtlGone
	}
	data, err := ctl.Call(ctx, webproto.OpWebPairAsk, a)
	if err != nil {
		return "", err
	}
	var r struct {
		Role string `json:"role"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return "", err
	}
	return r.Role, nil
}

// ctlTargets are what the hub's events act on.
type ctlTargets struct {
	pairing *Pairing
	tokens  *Tokens
	ws      *WS // nil until the WebSocket endpoint is made
	audit   *Audit
	// The watch socket (MOB-23); nil without paired devices.
	watchToks *WatchTokens
	watch     *Watch
	inbox     *InboxMux
	push      *UnifiedPush    // nil: no paired devices here
	ctx       context.Context // the wakes' context (ends with the run)
	relay     *relayLink      // nil: no web_relay
}

// onCtlEvent handles the hub's other events: a new pairing link
// ({sha256, ttl_s, guest}), its end (pair_close), a removed device
// (revoke {device}).
func onCtlEvent(kind string, data json.RawMessage, t ctlTargets) {
	pairing := t.pairing
	switch kind {
	case EvRevokeDevice:
		var d struct {
			Device string `json:"device"`
		}
		if json.Unmarshal(data, &d) != nil || !strings.HasPrefix(d.Device, "dev-") {
			return
		}
		n := t.tokens.RevokeDevice(d.Device)
		if t.push != nil {
			t.push.Forget(d.Device)
		}
		if t.ws != nil {
			n += t.ws.CloseDevice(d.Device, webproto.CloseDeviceRevoked)
		}
		if t.watchToks != nil {
			t.watchToks.RevokeDevice(d.Device)
		}
		if t.watch != nil {
			n += t.watch.CloseDevice(d.Device)
		}
		if t.relay != nil {
			t.relay.recheck()
		}
		_ = t.audit.Log(Event{Kind: EvRevoke, Device: d.Device, Summary: fmt.Sprintf("%d sessions closed", n)})
	case EvRoleChange:
		var d struct {
			Device string `json:"device"`
			Role   string `json:"role"`
		}
		if json.Unmarshal(data, &d) != nil || !strings.HasPrefix(d.Device, "dev-") ||
			(d.Role != webdevices.RoleViewer && d.Role != webdevices.RoleControl) {
			return
		}
		t.tokens.SetDeviceRole(d.Device, sessionRole(d.Role))
		if t.ws != nil {
			t.ws.CloseDevice(d.Device, webproto.CloseRoleChanged)
		}
		_ = t.audit.Log(Event{Kind: EvRole, Device: d.Device, Summary: d.Role, By: "laptop"})
	case EvPairLink:
		var d struct {
			SHA256 string `json:"sha256"`
			TTLS   int    `json:"ttl_s"`
			Guest  bool   `json:"guest"`
		}
		if json.Unmarshal(data, &d) != nil || pairing == nil {
			return
		}
		ttl := time.Duration(d.TTLS) * time.Second
		if ttl <= 0 || ttl > ShareTTL {
			ttl = PairTTL
		}
		if b, err := hex.DecodeString(d.SHA256); err == nil && len(b) == sha256.Size {
			pairing.SetSecret([sha256.Size]byte(b), ttl, d.Guest)
		}
	case EvRelayKey:
		if t.relay != nil {
			t.relay.setKey(data)
		}
	case EvPairClose:
		if pairing != nil {
			pairing.CloseSecret()
		}
	case webproto.EvInboxWatch:
		if t.inbox != nil {
			_ = t.inbox.Set("", data) // a bad snap keeps the last good inbox
		}
	case webproto.EvWake: // {count}: more cards wait; the count stays here
		var d webproto.WakeNote
		if json.Unmarshal(data, &d) != nil || d.Count <= 0 || t.push == nil || t.ctx == nil {
			return
		}
		t.push.Wake(t.ctx)
	}
}

// Control events of v2.
const (
	EvPairLink  = "pair"       // {sha256, ttl_s, guest}: a new pairing link
	EvPairClose = "pair_close" // the pairing link was closed on the laptop
	// EvRevokeDevice: {device}: the device was removed on the laptop.
	EvRevokeDevice = "revoke"
	// EvRoleChange: {device, role}: the laptop changed a device's role.
	EvRoleChange = "role"
)

// waitStop waits for the first reason to stop. The idle timer is one
// timer moved forward only when it fires early: no ticking while idle.
func waitStop(ctx context.Context, ctl *Ctl, last *atomic.Int64, idle time.Duration, maxAt time.Time, tail <-chan tailStop) (webproto.CloseCode, EventKind, string) {
	idleT := time.NewTimer(idle)
	defer idleT.Stop()
	maxT := time.NewTimer(time.Until(maxAt))
	defer maxT.Stop()
	for {
		select {
		case <-ctl.Done():
			switch ctl.Code() {
			case webproto.CloseStopped:
				return webproto.CloseStopped, EvKillSwitch, "kill_switch"
			case webproto.CloseRestarting: // a reload into a new build (resume.go)
				return webproto.CloseRestarting, EvStop, "restart"
			}
			return webproto.CloseHubGone, EvStop, "hub_gone"
		case <-ctx.Done():
			return webproto.CloseStopped, EvKillSwitch, "signal"
		case <-maxT.C:
			return webproto.CloseMaxLifetime, EvMaxStop, "max"
		case ts := <-tail:
			return webproto.CloseStopped, ts.kind, ts.why
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

// siteMux routes the gateway's paths: /auth, /ws, the page (assets may
// be nil: then only /auth and /ws exist) and, on an installable site,
// the app manifest (WEB-35) and the push-only service worker (MOB-30).
func siteMux(site Site, auth, ws, assets http.Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/auth", auth)
	mux.Handle("/ws", ws)
	if site.Installable() {
		mux.Handle(ManifestPath, Manifest())
		mux.Handle(SWPath, ServiceWorker())
	}
	if assets != nil {
		mux.Handle("/", assets)
	}
	return mux
}
