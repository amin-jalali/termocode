//go:build e2e && browser

package e2e

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// signinShotsEnv names a folder for the test's screenshots (before and
// after each step); unset: none are kept.
const signinShotsEnv = "WEB_E2E_SIGNIN_SHOTS"

// shot keeps a screenshot of the page as name.png in $WEB_E2E_SIGNIN_SHOTS.
func (p *webPage) shot(name string) {
	p.t.Helper()
	dir := os.Getenv(signinShotsEnv)
	if dir == "" {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		p.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(p.ctx, webWait)
	defer cancel()
	png, err := chromedp.Run(ctx, chromedp.CaptureScreenshot())
	if err != nil {
		p.t.Fatalf("screenshot %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name+".png"), png, 0o644); err != nil {
		p.t.Fatal(err)
	}
}

// offline cuts (or gives back) the page's network, its open WebSocket
// included: a network drop the gateway knows nothing of.
func (p *webPage) offline(on bool) {
	p.t.Helper()
	p.do(chromedp.Func(func(ctx context.Context, t *chromedp.Target) error {
		if _, err := cdp.Call(ctx, t, network.Enable, network.EnableParams{}); err != nil {
			return err
		}
		_, err := cdp.Call(ctx, t, network.EmulateNetworkConditionsByRule, network.EmulateNetworkConditionsByRuleParams{
			MatchedNetworkConditions: []*network.Conditions{{Offline: on, DownloadThroughput: -1, UploadThroughput: -1}}})
		return err
	}))
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

const (
	pageLost   = `document.documentElement.dataset.link === "offline" && document.body.innerText.includes("Connection lost")`
	pageOnline = `document.documentElement.dataset.link === "online"`
	// pageSignIn is what a new tab shows on this site with no session: on
	// loopback (no passkeys on an IP address) the connect screen that says
	// to open the page from termocode.
	pageSignIn = `document.documentElement.dataset.signin === "none" && document.body.innerText.includes("Open this page from termocode")`
)

// The owner's bug (2026-11-15): after `termocode web stop` (the kill
// switch) and a new `web start`, a refresh of the open page showed
// "Connection lost. Trying again…" and a skeleton for ever, and the
// audit log showed no attempt at all. The gateway refuses the old token
// before the upgrade; the browser reports that as a dropped network.
// Now the page asks /ws/check and shows the sign-in a new tab would
// show, and the log has a reject line. A real network drop still shows
// the retry and comes back by itself.
func TestWebSignInAfterKillSwitch(t *testing.T) {
	port := freePort(t)
	r := newWebRig(t)
	writeFile(t, filepath.Join(r.home, ".config", "termocode", "config.json"), fmt.Sprintf(`{"web_panel": "on", "web_port": %d}`, port))
	tm := Start(t, StartOpts{Cols: 140, Rows: 40, Home: r.home, Workspace: r.ws, Env: r.env})
	tm.WaitFor("status bar", bootTimeout, func(ls []string) bool {
		return mainFrame.Top(ls[0]) && mainFrame.Bottom(ls[len(ls)-1])
	})
	link := r.startPanel()
	if !strings.HasPrefix(link, fmt.Sprintf("http://127.0.0.1:%d/", port)) {
		t.Fatalf("link %q is not on port %d", link, port)
	}
	tm.WaitText("Web panel is on", uiTimeout)
	p := newWebPage(t, webViewport{Name: "phone", W: 390, H: 664, Scale: 3, Mobile: true})
	p.do(chromedp.Navigate(link))
	p.tap(".connect-foot button")
	p.waitJS(pageOnline)
	p.shot("1-signed-in")

	// A network drop: the gateway and the session live on. The page says
	// the connection is lost, tries again, and is back once the network is.
	p.offline(true)
	p.waitJS(pageLost)
	p.shot("2-network-drop-retrying")
	p.offline(false)
	p.waitJS(pageOnline)
	if webEval[bool](p, pageSignIn) {
		t.Fatal("a network drop ended the sign-in")
	}
	p.shot("3-network-back")

	// The kill switch, then a new start on the same port.
	if out, code := r.cli("web", "stop"); code != 0 {
		t.Fatalf("web stop: exit %d: %s", code, out)
	}
	p.waitJS(`document.body.innerText.includes("The web session was stopped.")`)
	p.shot("4-kill-switch")
	waitUntil(t, "web status to say off", uiTimeout, func() bool {
		out, code := r.cli("web", "status")
		return code == 1 && strings.Contains(out, "off")
	})
	_ = os.Remove(r.opened)
	if again := r.startPanel(); !strings.HasPrefix(again, fmt.Sprintf("http://127.0.0.1:%d/", port)) {
		t.Fatalf("second link %q is not on port %d", again, port)
	}

	// The refresh: the old token is dead. The sign-in shows within a few
	// seconds, never the retry loop.
	p.do(chromedp.Reload())
	p.waitJSFor(pageSignIn, 5*time.Second)
	if webEval[bool](p, `document.body.innerText.includes("Connection lost")`) {
		t.Error("the sign-in page also says Connection lost")
	}
	if webEval[string](p, `sessionStorage.getItem("termocode.session") ?? ""`) != "" {
		t.Error("the dead token is still kept")
	}
	p.shot("5-reload-after-restart-signin")

	audit, err := os.ReadFile(filepath.Join(r.home, ".config", "termocode", "web-audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(audit), `"ev":"reject"`) || !strings.Contains(string(audit), `"why":"signed_out"`) {
		t.Errorf("no reject line for the refused page in the audit log:\n%s", audit)
	}
	tm.Quit()
}
