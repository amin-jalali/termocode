//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// envFakeForward makes the e2e test binary play `ssh -N -L
// 127.0.0.1:<port>:<socket> …`: it forwards the TCP port to the unix
// socket on this machine, like sshd would on the far side.
const envFakeForward = "TC_E2E_FAKE_FWD"

// fakeForward runs the forward until it is killed; it writes its pid to
// $TC_E2E_FWD_PID.
func fakeForward(args []string) int {
	var port, sock string
	for i, a := range args {
		if a == "-L" && i+1 < len(args) {
			parts := strings.SplitN(args[i+1], ":", 3)
			if len(parts) == 3 && parts[0] == "127.0.0.1" {
				port, sock = parts[1], parts[2]
			}
		}
	}
	if port == "" {
		fmt.Fprintln(os.Stderr, "fake forward: no -L 127.0.0.1:port:socket")
		return 255
	}
	ln, err := net.Listen("tcp4", "127.0.0.1:"+port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake forward:", err)
		return 255
	}
	_ = os.WriteFile(os.Getenv("TC_E2E_FWD_PID"), []byte(strconv.Itoa(os.Getpid())), 0o644)
	for {
		c, err := ln.Accept()
		if err != nil {
			return 255
		}
		go func() {
			defer c.Close()
			u, err := net.Dial("unix", sock)
			if err != nil {
				return
			}
			defer u.Close()
			go func() { _, _ = io.Copy(u, c) }()
			_, _ = io.Copy(c, u)
		}()
	}
}

// fakeSSHWebScript is fakeSSHScript plus the forward: `ssh -N …` runs
// the fake forward (this test binary) instead of a remote command.
const fakeSSHWebScript = `#!/bin/sh
d='%DIR%'
echo "$*" >> "$d/argv"
for a in "$@"; do
  if [ "$a" = "-N" ]; then
    echo "$*" >> "$d/fwd-argv"
    exec env TC_E2E_FAKE_FWD=1 TC_E2E_FWD_PID="$d/fwd.pid" '%BIN%' "$@"
  fi
done
while [ "$1" != "--" ]; do shift; done; shift
exec sh -c "exec $*"
`

// sshWebOpener is the browser opener of the user's computer here: it
// records its arguments and copies the page it got.
const sshWebOpener = `#!/bin/sh
printf '%s\n' "$*" >> '%DIR%/open-argv'
cat "$1" > '%DIR%/opened.tmp' && mv '%DIR%/opened.tmp' '%DIR%/opened.html'
`

// Spec (WEB-29, TestSSHWebForward): `termocode ssh --web box` opens the
// far termocode as usual; its web panel starts in SSH mode (a unix
// socket, no TCP port on the far side), the hand-off never reaches the
// terminal, a second ssh forwards 127.0.0.1:<port> to the socket, and
// the one-time link opens in the browser here through a page (never
// argv). The link works through the forward; leaving termocode ssh
// stops the forward.
func TestSSHWebForward(t *testing.T) {
	sr := newSessionRig(t)
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r := strings.NewReplacer("%DIR%", dir, "%BIN%", self)
	writeFile(t, filepath.Join(dir, "ssh"), r.Replace(fakeSSHWebScript))
	writeFile(t, filepath.Join(dir, "xdg-open"), r.Replace(sshWebOpener))
	writeFile(t, filepath.Join(dir, "open"), r.Replace(sshWebOpener))
	for _, n := range []string{"ssh", "xdg-open", "open"} {
		if err := os.Chmod(filepath.Join(dir, n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	bin, err := filepath.Abs(binPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bin, filepath.Join(dir, "termocode")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(sr.home, ".config", "termocode", "config.json"), `{"web_panel": "on"}`)
	env := append(append([]string{}, sr.env...), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Cleanup(func() { // a failed test leaves no forward behind
		if b, err := os.ReadFile(filepath.Join(dir, "fwd.pid")); err == nil {
			if pid, _ := strconv.Atoi(strings.TrimSpace(string(b))); pid > 0 {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})
	tm := Start(t, StartOpts{Home: sr.home, Workspace: sr.ws, Env: env, Args: []string{"ssh", "--web", "--dir", sr.ws, "box"}})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})

	// The link opened here, through a page.
	opened := filepath.Join(dir, "opened.html")
	waitUntil(t, "the browser here", 30*time.Second, func() bool { _, err := os.Stat(opened); return err == nil })
	page, _ := os.ReadFile(opened)
	m := regexp.MustCompile(`url=([^"]+)"`).FindSubmatch(page)
	if m == nil {
		t.Fatalf("no link in the page: %s", page)
	}
	link := html.UnescapeString(string(m[1]))
	base, secret, ok := strings.Cut(link, "/#s=")
	if !ok || !strings.HasPrefix(base, "http://127.0.0.1:") {
		t.Fatalf("link %q", link)
	}
	port := strings.TrimPrefix(base, "http://127.0.0.1:")
	if args, _ := os.ReadFile(filepath.Join(dir, "open-argv")); strings.Contains(string(args), secret) {
		t.Fatalf("the secret is in the opener's argv: %q", args)
	}
	fwd, _ := os.ReadFile(filepath.Join(dir, "fwd-argv"))
	if !strings.Contains(string(fwd), "-L 127.0.0.1:"+port+":") || !strings.Contains(string(fwd), ".webhttp.sock") ||
		!strings.Contains(string(fwd), "ExitOnForwardFailure=yes") || !strings.HasSuffix(strings.TrimSpace(string(fwd)), " box") {
		t.Fatalf("forward argv %q", fwd)
	}
	if argv, _ := os.ReadFile(filepath.Join(dir, "argv")); !strings.Contains(string(argv), "--link --web="+port) {
		t.Fatalf("the far side was not told the port: %q", argv)
	}

	// The far gateway: SSH mode, no TCP port.
	st, _ := os.ReadFile(filepath.Join(sr.runtime, "termocode", hashKey(sr.ws)+".web.json"))
	var status struct {
		Port int    `json:"port"`
		Sock string `json:"sock"`
	}
	if err := json.Unmarshal(st, &status); err != nil || strconv.Itoa(status.Port) != port || !strings.HasSuffix(status.Sock, ".webhttp.sock") {
		t.Fatalf("gateway status %s: %v", st, err)
	}

	// The link works through the forward, once.
	auth := func() int {
		req, _ := http.NewRequest(http.MethodPost, base+"/auth", strings.NewReader(`{"secret":"`+secret+`"}`))
		req.Header.Set("Origin", base)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if code := auth(); code != http.StatusOK {
		t.Fatalf("auth through the forward: %d", code)
	}
	if code := auth(); code == http.StatusOK {
		t.Fatal("the link worked twice")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatalf("the page through the forward: %d, CSP %q", resp.StatusCode, resp.Header.Get("Content-Security-Policy"))
	}

	// The hand-off never reached the terminal.
	tm.mu.Lock()
	raw := tm.raw.String()
	tm.mu.Unlock()
	if strings.Contains(raw, "termocode:web") || strings.Contains(raw, secret) {
		t.Fatal("the web hand-off reached the terminal")
	}

	// Leaving ends the forward.
	b, _ := os.ReadFile(filepath.Join(dir, "fwd.pid"))
	pid, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	if pid == 0 {
		t.Fatal("no forward pid")
	}
	tm.Quit()
	waitUntil(t, "the forward to end", uiTimeout, func() bool { return syscall.Kill(pid, 0) != nil })
}
