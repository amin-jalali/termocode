//go:build unix

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/amin-jalali/termocode/internal/session"
	"github.com/amin-jalali/termocode/internal/webgw"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// `termocode web` (WEB-9) asks the termocode UI of a folder to open or
// stop the web panel. It never starts the gateway itself: the UI does,
// after the user said yes on the laptop's start card (or --yes, from the
// same user), so a command in some script cannot open a door silently.

const webUsage = `Usage: termocode web [start|stop|status] [--yes] [--dir <folder>]

  start    open the web panel of the termocode running in the folder
           (the laptop asks first; --yes skips the question)
  stop     stop the web panel; every browser is disconnected
  status   say whether the web panel runs, and where

The web panel must be turned on in config.json: "web_panel": "on".
`

// Exit codes of `termocode web`.
const (
	webExitOK    = 0
	webExitNo    = 1 // refused, or not running (status)
	webExitUsage = 2 // bad arguments, or no termocode with the web on here
)

// webOffText is what the CLI says when no hub answers (E11).
const webOffText = "termocode: no termocode with the web panel turned on runs in this folder.\n" +
	"Start termocode here, and turn the panel on in config.json: \"web_panel\": \"on\"\n"

func runWeb(args []string) int {
	cwd, _ := os.Getwd()
	return runWebIn(args, os.Stdout, os.Stderr, cwd)
}

func runWebIn(args []string, out, errw io.Writer, cwd string) int {
	cmd, dir, yes := "start", cwd, false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help" || a == "help":
			fmt.Fprint(out, webUsage)
			return webExitOK
		case a == "start" || a == "stop" || a == "status":
			cmd = a
		case a == "--yes" || a == "-y":
			yes = true
		case a == "--dir" && i+1 < len(args):
			i++
			dir = args[i]
		case strings.HasPrefix(a, "--dir="):
			dir = strings.TrimPrefix(a, "--dir=")
		default:
			fmt.Fprintf(errw, "termocode web: unknown argument %q\n\n%s", a, webUsage)
			return webExitUsage
		}
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		fmt.Fprintf(errw, "termocode web: %s is not a folder\n", dir)
		return webExitUsage
	}
	paths, err := session.PathsFor(dir)
	if err != nil {
		fmt.Fprintf(errw, "termocode web: %v\n", err)
		return webExitUsage
	}
	if cmd == "status" {
		return webStatus(paths, out)
	}
	op, a := webproto.OpWebStop, ""
	if cmd == "start" {
		op = webproto.OpWebOpen
		if yes {
			a = `{"yes":true}`
		}
		fmt.Fprintln(out, "Asking termocode to open the web panel. Answer on the laptop…")
	}
	res, code, msg, err := webCtlCall(paths.WebSocket, op, a)
	if err != nil {
		fmt.Fprint(errw, webOffText)
		return webExitUsage
	}
	if code != "" {
		fmt.Fprintf(errw, "termocode web: %s\n", msg)
		return webExitNo
	}
	if cmd == "stop" {
		fmt.Fprintln(out, "Web panel stopped. All browsers were disconnected.")
		return webExitOK
	}
	var r struct {
		Port int `json:"port"`
	}
	_ = json.Unmarshal(res, &r)
	fmt.Fprintf(out, "The web panel is open at http://127.0.0.1:%d/ (the link opened in a browser on this computer).\n", r.Port)
	return webExitOK
}

// webStatus prints the gateway's status file.
func webStatus(paths session.Paths, out io.Writer) int {
	st, err := webgw.ReadStatus(paths.WebStatus)
	if err != nil || !webPIDAlive(st.PID) {
		fmt.Fprintln(out, "The web panel is off.")
		return webExitNo
	}
	fmt.Fprintf(out, "The web panel runs at %s since %s.\n", st.URL, st.Started.Local().Format("15:04"))
	fmt.Fprintf(out, "It stops after %d minutes without use, and at %s at the latest.\n",
		st.IdleS/60, st.Started.Add(time.Duration(st.MaxS)*time.Second).Local().Format("15:04"))
	return webExitOK
}

func webPIDAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

// webCtlCall sends one control request on the hub socket and waits for
// its answer (the user may take a while on the start card). code is the
// err code ("" for res).
func webCtlCall(sock, op, args string) (data json.RawMessage, code, msg string, err error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var d net.Dialer
	dctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	nc, err := d.DialContext(dctx, "unix", sock)
	cancel()
	if err != nil {
		return nil, "", "", err
	}
	defer nc.Close()
	go func() { <-ctx.Done(); _ = nc.Close() }()
	req := `{"t":"ctl"}` + "\n" + `{"t":"req","id":1,"op":` + strconv.Quote(op)
	if args != "" {
		req += `,"args":` + args
	}
	if _, err := io.WriteString(nc, req+"}\n"); err != nil {
		return nil, "", "", err
	}
	br := bufio.NewReader(nc)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return nil, "", "", err
		}
		var m struct {
			T    string          `json:"t"`
			ID   uint64          `json:"id"`
			Data json.RawMessage `json:"data"`
			Code json.RawMessage `json:"code"`
			Msg  string          `json:"msg"`
		}
		if json.Unmarshal(line, &m) != nil || m.ID != 1 {
			continue // events for the gateway
		}
		switch m.T {
		case webproto.KindRes:
			return m.Data, "", "", nil
		case webproto.KindErr:
			var c string
			_ = json.Unmarshal(m.Code, &c)
			return nil, c, cleanLine(m.Msg), nil
		}
	}
}

// cleanLine drops control characters from text for the terminal.
func cleanLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return -1
		}
		return r
	}, s)
}
