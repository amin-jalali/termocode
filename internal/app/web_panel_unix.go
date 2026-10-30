//go:build unix

package app

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/spawn"
	"github.com/amin-jalali/termocode/internal/webgw"
)

func webNewLinkSecret() (string, [sha256.Size]byte, error) { return webgw.NewLinkSecret() }

func webWriteRedirect(path, link string) error { return webgw.WriteRedirect(path, link) }

func webRedirectPath() (string, error) {
	p, err := session.PathsFor(aiWorkspaceRoot())
	return p.WebOpen, err
}

// webHandoffWait bounds the gateway's start (listen, hub, first link).
const webHandoffWait = 15 * time.Second

// webGatewayExe is the binary that runs the gateway. Tests swap it.
var webGatewayExe = spawn.Executable

// webGatewayErrMax caps what is kept of the gateway's stderr.
const webGatewayErrMax = 4 << 10

// startWebGatewayProcess runs `termocode __web-gateway` in its own
// process group (terminal signals stay with the UI). Its argv holds only
// paths and limits; it writes {"port":N} on fd 3 once it serves.
func startWebGatewayProcess(h *webHub, args []string) (*webGatewayProc, error) {
	exe, err := webGatewayExe()
	if err != nil {
		return nil, err
	}
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	cmd := spawn.Command(exe, append([]string{"__web-gateway"}, args...)...)
	cmd.ExtraFiles = []*os.File{pw}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	er, ew, err := os.Pipe()
	if err != nil {
		_ = pr.Close()
		_ = pw.Close()
		return nil, err
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, ew
	if err := cmd.Start(); err != nil {
		_ = pr.Close()
		_ = pw.Close()
		_ = er.Close()
		_ = ew.Close()
		return nil, err
	}
	_ = pw.Close()
	_ = ew.Close()
	stderr := &webTail{max: webGatewayErrMax}
	errDone := make(chan struct{})
	crash.Go("web: the gateway stderr", func() {
		defer close(errDone)
		defer er.Close()
		_, _ = io.Copy(stderr, er)
	})
	done := make(chan struct{})
	proc := &webGatewayProc{
		terminate: func() { _ = cmd.Process.Signal(syscall.SIGTERM) },
		alive: func() bool {
			select {
			case <-done:
				return false
			default:
				return true
			}
		},
	}
	crash.Go("web: the gateway handoff", func() {
		defer pr.Close()
		_ = pr.SetReadDeadline(time.Now().Add(webHandoffWait))
		var hand struct {
			Port int `json:"port"`
		}
		if err := json.NewDecoder(pr).Decode(&hand); err != nil || hand.Port == 0 {
			select { // the reason is on stderr once the process ended
			case <-errDone:
			case <-time.After(2 * time.Second):
			}
			_ = h.send(h.ctx, webPanelFailMsg{hub: h, proc: proc, err: errors.New(webGatewayReason(stderr.String()))})
			return
		}
		_ = h.send(h.ctx, webPanelUpMsg{hub: h, proc: proc, port: hand.Port})
	})
	crash.Go("web: the gateway process", func() {
		_ = cmd.Wait()
		close(done)
		_ = h.send(h.ctx, webPanelExitMsg{hub: h, proc: proc})
	})
	return proc, nil
}
