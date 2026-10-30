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
	"syscall"
	"time"

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
	if err := fs.Parse(args); err != nil || *dir == "" || *audit == "" || fs.NArg() != 0 ||
		(*sshPort != 0 && (*port != 0 || *sshPort < 1024 || *sshPort > 65535 || *tailnet)) {
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
	return cfg, true
}
