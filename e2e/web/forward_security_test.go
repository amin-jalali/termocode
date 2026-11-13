//go:build browser

package web

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// A forwarded app (WEB-54) runs on its own origin: 127.0.0.1 of the same
// computer, on a port of its own, never a path under the panel. It is
// the same site as the panel (same host, other port), so only the
// origin rules keep it out: its pages can not open the panel's /ws (the
// gateway's exact Origin check refuses the upgrade before the hub), can
// not read the panel's answers (no CORS), its frames or its tab's
// storage, and do not hold the panel's session token.

// startForwardedApp serves page at http://127.0.0.1:<own port>/, as a
// dev server that a port preview forwards would.
func startForwardedApp(t *testing.T, page string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	app := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	app.Listener = ln
	app.Start()
	t.Cleanup(app.Close)
	return app.URL
}

// loginAndLeave logs a page in (its token in this tab's sessionStorage)
// and returns the token and the panel's ws URL.
func loginAndLeave(t *testing.T, s *Server, p *Page) (token, ws string) {
	t.Helper()
	p.Login("c-1")
	token = Eval[string](p, `sessionStorage.getItem("termocode.session") || ""`)
	if token == "" {
		t.Fatal("the logged-in page holds no session token")
	}
	return token, "ws" + strings.TrimPrefix(s.Base, "http") + "/ws"
}

func TestForwardedAppCannotOpenWS(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	_, ws := loginAndLeave(t, s, p)
	before := len(s.Hub.Attempts())
	app := startForwardedApp(t, `<!doctype html><title>app</title><script>
window.__ws = "pending";
const w = new WebSocket(`+jsq(ws)+`, ["termocode.v1"]);
w.onopen = () => { window.__ws = "open"; };
w.onerror = () => { if (window.__ws === "pending") window.__ws = "refused"; };
w.onclose = () => { if (window.__ws === "pending") window.__ws = "refused"; };
</script>`)
	if strings.TrimPrefix(app, "http://") == strings.TrimPrefix(s.Base, "http://") {
		t.Fatalf("the app shares the panel's origin %s", app)
	}
	p.Do(chromedp.Navigate(app + "/"))
	p.WaitJS(`window.__ws && window.__ws !== "pending"`)
	if got := Eval[string](p, `window.__ws`); got != "refused" {
		t.Fatalf("the forwarded app's WebSocket to the panel: %s", got)
	}
	if n := len(s.Hub.Attempts()) - before; n != 0 {
		t.Fatalf("%d upgrade(s) from the forwarded app reached the hub", n)
	}
}

func TestForwardedAppCannotOpenOtherOrigin(t *testing.T) {
	s := StartServer(t)
	p := NewPage(t, s, Desktop)
	token, _ := loginAndLeave(t, s, p)
	panel := jsq(s.Base + "/")
	app := startForwardedApp(t, `<!doctype html><title>app</title><body><script>
window.__read = "pending";
window.__auth = "pending";
fetch(`+panel+`).then((r) => r.text()).then(() => { window.__read = "read"; }, () => { window.__read = "refused"; });
fetch(`+jsq(s.Base+"/auth")+`, {method: "POST", headers: {"Content-Type": "application/json"}, body: "{}"}).then(
  (r) => { window.__auth = "answer " + r.status; }, () => { window.__auth = "refused"; });
const f = document.createElement("iframe");
f.src = `+panel+`;
f.onload = () => { try { window.__frame = String(f.contentDocument && f.contentDocument.body && f.contentDocument.body.innerHTML.length); } catch (e) { window.__frame = "refused"; } };
document.body.appendChild(f);
</script></body>`)
	p.Do(chromedp.Navigate(app + "/"))
	if origin := Eval[string](p, `location.origin`); origin == s.Base {
		t.Fatalf("the app runs on the panel's origin %s", origin)
	}
	p.WaitJS(`window.__read !== "pending" && window.__auth !== "pending" && window.__frame !== undefined`)
	if got := Eval[string](p, `window.__read`); got != "refused" {
		t.Errorf("the forwarded app read the panel's page: %s", got)
	}
	if got := Eval[string](p, `window.__auth`); got != "refused" {
		t.Errorf("the forwarded app reached the panel's /auth: %s", got)
	}
	if got := Eval[string](p, `window.__frame`); got != "refused" && got != "undefined" && got != "null" {
		t.Errorf("the forwarded app read the panel in a frame: %s", got)
	}
	if kept := Eval[string](p, `JSON.stringify([sessionStorage, localStorage, document.cookie])`); strings.Contains(kept, token) {
		t.Errorf("the forwarded app holds the panel's token: %s", kept)
	}
}
