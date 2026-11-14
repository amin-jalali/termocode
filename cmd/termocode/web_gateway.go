//go:build unix

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/relay"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/webgw"
)

// webGatewayArg is the hidden first argument of the web gateway process,
// which the UI starts (WEB-9). Its argv holds only paths and limits;
// the launch secret never: the UI sends the gateway its digest over the
// hub socket, and the gateway hands back the port on fd 3.
const webGatewayArg = "__web-gateway"

// webAssets serves the browser UI's page and files; tests may swap it.
var webAssets = gatewayAssets

// gatewayAssets is the embedded browser UI (index.html, app.js, app.css).
func gatewayAssets() http.Handler { return webgw.Assets() }

func runWebGateway(args []string) int {
	// fd 3 is taken only once the arguments are termocode's own: an unused
	// *os.File would close the process's fd 3 when it is collected.
	if _, ok := webGatewayConfig(args, os.Stderr); !ok {
		return 2
	}
	return runWebGatewayIn(args, os.NewFile(3, "web-handoff"), os.Stderr)
}

func runWebGatewayIn(args []string, handoff io.WriteCloser, errw io.Writer) int {
	cfg, ok := webGatewayConfig(args, errw)
	if !ok {
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if handoff != nil {
		cfg.Handoff = handoff
	}
	if _, err := webgw.Run(ctx, cfg); err != nil {
		fmt.Fprintf(errw, "termocode web gateway: %v\n", err)
		return 1
	}
	return 0
}

// webGatewayConfig reads the gateway's arguments; ok is false (and why
// is on errw) when they are not what termocode itself passes.
func webGatewayConfig(args []string, errw io.Writer) (webgw.RunConfig, bool) {
	fs := flag.NewFlagSet(webGatewayArg, flag.ContinueOnError)
	fs.SetOutput(errw)
	dir := fs.String("dir", "", "workspace folder")
	audit := fs.String("audit", "", "audit log path")
	idle := fs.Int("idle-min", 15, "minutes without use")
	maxH := fs.Int("max-hours", 12, "hours at most")
	viewers := fs.Int("viewers", webgw.DefaultMaxViewers, "browsers at once")
	port := fs.Int("port", 0, "port (0: random)")
	sshPort := fs.Int("ssh-port", 0, "SSH mode: serve on the folder's unix socket for this forwarded port")
	tailnet := fs.Bool("tailnet", false, "tailnet mode: tailscale serve in front of 127.0.0.1")
	devices := fs.String("devices", "", "the paired-device folder (pairing and passkeys on)")
	lan := fs.Bool("lan", false, "LAN mode: https on a private LAN address, pinned self-signed certificate")
	totp := fs.Bool("totp", false, "a paired device also needs a one-time code")
	appKey := fs.Bool("app-key", false, "the Android app may sign in with its keystore key")
	phone := fs.Bool("phone-watch", false, "a paired phone that watches keeps the gateway up (web_phone)")
	relayURL := fs.String("relay", "", "web_relay: also wait for paired phones on this relay (https://host[:port])")
	resume := fs.Bool("resume", false, "take over the browser sessions the last gateway handed over (a reload into a new build)")
	if err := fs.Parse(args); err != nil || *dir == "" || *audit == "" || fs.NArg() != 0 ||
		(*sshPort != 0 && (*port != 0 || *sshPort < 1024 || *sshPort > 65535 || *tailnet)) ||
		(*lan && (*devices == "" || *tailnet || *sshPort != 0)) ||
		(*relayURL != "" && *devices == "") {
		fmt.Fprintln(errw, "termocode: the web gateway is started by termocode itself")
		return webgw.RunConfig{}, false
	}
	paths, err := session.PathsFor(*dir)
	if err != nil {
		fmt.Fprintf(errw, "termocode web gateway: %v\n", err)
		return webgw.RunConfig{}, false
	}
	cfg := webgw.RunConfig{
		Dial: func(ctx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", paths.WebSocket)
		},
		AuditPath: *audit, StatusPath: paths.WebStatus, LockPath: paths.WebLock,
		Port: *port, Idle: time.Duration(*idle) * time.Minute, Max: time.Duration(*maxH) * time.Hour,
		MaxViewers: *viewers, Assets: webAssets(),
	}
	if *sshPort != 0 {
		cfg.UnixPath, cfg.PortHint = paths.WebHTTPSocket, *sshPort
	}
	if *tailnet {
		cfg.Tailnet = &webgw.TailnetConfig{}
	}
	if *lan {
		cfg.LAN = &webgw.LANConfig{CertDir: filepath.Join(*devices, "lan")}
	}
	cfg.DevicesDir = *devices
	if *devices != "" {
		// MOB-50: the paired gateway is the door to this user's other
		// running workspaces on the machine.
		home := paths.Key
		cfg.Home = home
		cfg.DialWS = func(ctx context.Context, ws string) (net.Conn, error) { return webDoorDial(ctx, home, ws) }
	}
	cfg.RequireTOTP = *totp
	cfg.AppKeyLogin = *appKey
	cfg.PhoneWatch = *phone
	if *relayURL != "" {
		if _, err := relay.CheckURL(*relayURL); err != nil {
			fmt.Fprintf(errw, "termocode web gateway: %v\n", err)
			return webgw.RunConfig{}, false
		}
		cfg.Relay = &webgw.RelayConfig{URL: *relayURL} // its key comes from termocode over the hub socket
	}
	// A reload into a new build: the folder's own hand-over file, never a
	// path from argv. Written on a restart bye, read (and removed) once.
	cfg.ResumePath = paths.WebResume
	if *resume {
		cfg.ResumeFrom = paths.WebResume // read once the gateway holds the lock
	}
	return cfg, true
}
