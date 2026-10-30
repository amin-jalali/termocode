//go:build unix

package webgw

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// Tailnet mode (WEB-31, owner: Tailscale in v2, a relay later). The
// gateway still listens on 127.0.0.1 only; Tailscale Serve, run by the
// gateway in the foreground for exactly its own lifetime, gives it an
// https name inside the user's tailnet. There is never a public port: a
// machine whose Serve port is under Funnel (public on the internet) is
// refused at the start and stopped when Funnel shows up later (T16).

// TailscaleRun runs the tailscale CLI with args and returns its stdout.
type TailscaleRun func(ctx context.Context, args ...string) ([]byte, error)

// Errors of tailnet mode. Each says what to do, in one short sentence.
var (
	ErrNoTailscale    = errors.New("tailscale is not installed here. Install it and log in, or use \"web_listen\": \"loopback\"")
	ErrNotOnTailnet   = errors.New("this computer is not on a tailnet. Run: tailscale up")
	ErrNoTailnetHTTPS = errors.New("your tailnet gives this computer no HTTPS name. Turn on HTTPS in the Tailscale admin console (DNS page)")
)

// TailnetInfo is what the gateway needs to know about this machine.
type TailnetInfo struct {
	// Host is the machine's tailnet name, e.g. box.tail1234.ts.net.
	Host string
}

// tsStatus is the part of `tailscale status --json` the gateway reads.
type tsStatus struct {
	BackendState string
	Self         *struct {
		DNSName string
	}
	CertDomains []string
}

// DetectTailnet asks tailscale who this machine is. It refuses unless
// Tailscale runs, is logged in, and can give this machine an https
// certificate for its tailnet name (needed for passkeys and for Serve).
func DetectTailnet(ctx context.Context, run TailscaleRun) (TailnetInfo, error) {
	out, err := run(ctx, "status", "--json")
	if errors.Is(err, ErrNoTailscale) {
		return TailnetInfo{}, ErrNoTailscale
	}
	var st tsStatus
	if err != nil || json.Unmarshal(out, &st) != nil {
		return TailnetInfo{}, ErrNotOnTailnet
	}
	if st.BackendState != "Running" || st.Self == nil || st.Self.DNSName == "" {
		return TailnetInfo{}, ErrNotOnTailnet
	}
	host := strings.TrimSuffix(st.Self.DNSName, ".")
	if !isTailnetName(host) {
		return TailnetInfo{}, fmt.Errorf("%w: %s", ErrNotTailnetName, cleanAuditText(host))
	}
	if !slices.Contains(st.CertDomains, host) {
		return TailnetInfo{}, ErrNoTailnetHTTPS
	}
	return TailnetInfo{Host: host}, nil
}

// tailscaleBins are tried in order: the PATH, then the macOS app's CLI.
var tailscaleBins = []string{"tailscale", "/Applications/Tailscale.app/Contents/MacOS/Tailscale"}

// tailscaleBin finds the tailscale CLI, or ErrNoTailscale.
func tailscaleBin() (string, error) {
	for _, b := range tailscaleBins {
		if strings.HasPrefix(b, "/") {
			if fi, err := os.Stat(b); err == nil && !fi.IsDir() {
				return b, nil
			}
			continue
		}
		if p, err := exec.LookPath(b); err == nil {
			return p, nil
		}
	}
	return "", ErrNoTailscale
}

// Tailscale is the real CLI: stdout of `tailscale <args>`; on a failure
// the error holds the last line it printed on stderr (cleaned).
func Tailscale(ctx context.Context, args ...string) ([]byte, error) {
	bin, err := tailscaleBin()
	if err != nil {
		return nil, err
	}
	cmd := spawn.CommandContext(ctx, bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("tailscale %s: %s", args[0], lastLine(stderr.String(), err))
	}
	return stdout.Bytes(), nil
}

// lastLine is the last non-empty line of s, cleaned, or err's text.
func lastLine(s string, err error) string {
	var last string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			last = l
		}
	}
	if last == "" {
		return err.Error()
	}
	return cleanAuditText(last)
}

// More errors of tailnet mode.
var (
	ErrFunnel       = errors.New("this port is public on the internet (Tailscale Funnel). The web panel will not start. Run: tailscale funnel off")
	ErrServeBusy    = errors.New("Tailscale Serve already uses port 443 on this computer. See: tailscale serve status")
	ErrServeUnknown = errors.New("could not read tailscale serve status")
)

// TailnetPort is the https port Serve answers on for the panel.
const TailnetPort = 443

// tsServe is the part of `tailscale serve status --json` (ipn.ServeConfig)
// the gateway reads. Foreground sessions carry their own config.
type tsServe struct {
	TCP map[string]struct {
		HTTPS      bool
		TCPForward string
	}
	Web map[string]struct {
		Handlers map[string]struct {
			Proxy string
		}
	}
	AllowFunnel map[string]bool
	Foreground  map[string]tsServe
}

// CheckServe reads the Serve config: ErrFunnel when the panel's port on
// host is public anywhere in it, ErrServeBusy when something else uses
// that port, else ours says whether it already proxies to target alone.
func CheckServe(ctx context.Context, run TailscaleRun, host, target string) (ours bool, err error) {
	out, err := run(ctx, "serve", "status", "--json")
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrServeUnknown, err)
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return false, nil // nothing served at all
	}
	var cfg tsServe
	if json.Unmarshal(out, &cfg) != nil {
		return false, ErrServeUnknown
	}
	key := host + ":" + fmt.Sprint(TailnetPort)
	busy := false
	var walk func(c tsServe)
	walk = func(c tsServe) {
		if c.AllowFunnel[key] {
			err = ErrFunnel
		}
		if tcp, ok := c.TCP[fmt.Sprint(TailnetPort)]; ok && tcp.TCPForward != "" {
			busy = true
		}
		if w, ok := c.Web[key]; ok {
			for path, h := range w.Handlers {
				if path == "/" && h.Proxy == target {
					ours = true
				} else {
					busy = true
				}
			}
		}
		for _, f := range c.Foreground {
			walk(f)
		}
	}
	walk(cfg)
	switch {
	case err != nil:
		return false, err
	case busy:
		return false, ErrServeBusy
	}
	return ours, nil
}

// ServeProc is `tailscale serve` running in the foreground for the
// gateway. Its config lives exactly as long as the process: nothing is
// left behind in Tailscale when the panel stops.
type ServeProc interface {
	Stop()                 // end it; its Serve config goes with it
	Done() <-chan struct{} // closed once it ended
	Reason() string        // the last line it printed (after Done)
}

// ServeStart starts Serve for https://<host>/ → target.
type ServeStart func(target string) (ServeProc, error)

// TailnetConfig turns on tailnet mode in RunConfig.
type TailnetConfig struct {
	Run   TailscaleRun  // nil: Tailscale
	Start ServeStart    // nil: StartTailscaleServe
	Every time.Duration // the Funnel check (0: DefaultFunnelCheck)
	Wait  time.Duration // how long Serve may take to come up (0: DefaultServeWait)
}

// Tailnet defaults.
const (
	DefaultFunnelCheck = time.Minute
	DefaultServeWait   = 15 * time.Second
)

// errOperatorHint is added when Tailscale refused a user who is not its
// operator (Linux).
const operatorHint = ". Run once: sudo tailscale set --operator=$USER"

// tailnetRun is tailnet mode while the gateway runs.
type tailnetRun struct {
	cfg  TailnetConfig
	host string
	proc ServeProc
}

// prepareTailnet checks the machine before anything listens.
func prepareTailnet(ctx context.Context, cfg TailnetConfig) (*tailnetRun, Site, error) {
	if cfg.Run == nil {
		cfg.Run = Tailscale
	}
	if cfg.Start == nil {
		cfg.Start = StartTailscaleServe
	}
	if cfg.Every <= 0 {
		cfg.Every = DefaultFunnelCheck
	}
	if cfg.Wait <= 0 {
		cfg.Wait = DefaultServeWait
	}
	info, err := DetectTailnet(ctx, cfg.Run)
	if err != nil {
		return nil, Site{}, err
	}
	site, err := TailnetSite(info.Host)
	if err != nil {
		return nil, Site{}, err
	}
	return &tailnetRun{cfg: cfg, host: info.Host}, site, nil
}

// URL is the panel's address in the tailnet.
func (t *tailnetRun) URL() string { return "https://" + t.host + "/" }

// serve starts Serve for the gateway on 127.0.0.1:port and waits until
// Tailscale shows it, refusing Funnel and a busy port first.
func (t *tailnetRun) serve(ctx context.Context, port int) error {
	target := "http://127.0.0.1:" + strconv.Itoa(port)
	ours, err := CheckServe(ctx, t.cfg.Run, t.host, target)
	if err != nil {
		return err
	}
	if !ours {
		p, err := t.cfg.Start(target)
		if err != nil {
			return serveErr(err.Error())
		}
		t.proc = p
	}
	deadline := time.NewTimer(t.cfg.Wait)
	defer deadline.Stop()
	for {
		ours, err := CheckServe(ctx, t.cfg.Run, t.host, target)
		switch {
		case err != nil && !errors.Is(err, ErrServeUnknown):
			t.stop()
			return err
		case ours:
			return nil
		}
		var done <-chan struct{}
		if t.proc != nil {
			done = t.proc.Done()
		}
		select {
		case <-done:
			return serveErr(t.proc.Reason())
		case <-deadline.C:
			t.stop()
			return errors.New("tailscale serve did not start in time")
		case <-ctx.Done():
			t.stop()
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// serveErr names a failed Serve start, with the operator hint when
// Tailscale refused this user.
func serveErr(reason string) error {
	msg := "tailscale serve did not start: " + cleanAuditText(reason)
	low := strings.ToLower(reason)
	if strings.Contains(low, "access denied") || strings.Contains(low, "operator") || strings.Contains(low, "permission") {
		msg += operatorHint
	}
	return errors.New(msg)
}

func (t *tailnetRun) stop() {
	if t != nil && t.proc != nil {
		t.proc.Stop()
	}
}

// tailStop is why tailnet mode ends the panel.
type tailStop struct {
	kind EventKind
	why  string
}

// watch checks for Funnel every cfg.Every and watches Serve; the first
// reason to stop goes to out. It ends with ctx.
func (t *tailnetRun) watch(ctx context.Context, out chan<- tailStop) {
	tick := time.NewTicker(t.cfg.Every)
	defer tick.Stop()
	var done <-chan struct{}
	if t.proc != nil {
		done = t.proc.Done()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			out <- tailStop{kind: EvServeGone, why: "serve_gone"}
			return
		case <-tick.C:
			if _, err := CheckServe(ctx, t.cfg.Run, t.host, ""); errors.Is(err, ErrFunnel) {
				out <- tailStop{kind: EvFunnelStop, why: "funnel"}
				return
			}
		}
	}
}

// errServeEnded: Serve ended without a word.
var errServeEnded = errors.New("tailscale serve ended")
