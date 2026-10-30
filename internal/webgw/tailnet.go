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
	"strings"

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
