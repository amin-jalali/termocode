//go:build unix

package webgw

import (
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// lanBrowser is one browser on the LAN page: it keeps its cookies and
// sends the page's own Origin, as the tablet's WebView does.
type lanBrowser struct {
	t      *testing.T
	origin string
	c      *http.Client
}

func (b *lanBrowser) post(path, body string) (int, map[string]any) {
	b.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, b.origin+path, strings.NewReader(body))
	req.Header.Set("Origin", b.origin)
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.c.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

// Spec (LAN mode, "Remember this browser"): a tablet that paired on the
// LAN page (no passkeys there, so the remembered key is its way back)
// signs straight in again with that key after the app reopens: the
// session is gone, the cookie is not. Before the fix /login/device said
// 404 (the sign-in had no device list without passkeys) and the page
// fell back to "Open this page from termocode on the laptop".
func TestLANRememberedBrowserSignsBackIn(t *testing.T) {
	ip, ok := PickLANAddr(LANAddrs())
	if !ok {
		t.Skip("no private LAN address here")
	}
	devs := filepath.Join(t.TempDir(), "web")
	pr, pw := io.Pipe()
	r := startGateway(t, func(c *RunConfig) {
		c.Handoff = pw
		c.DevicesDir = devs
		c.LAN = &LANConfig{CertDir: filepath.Join(devs, "lan"), IP: ip}
	})
	r.link()
	r.handoff(pr)
	jar, _ := cookiejar.New(nil)
	b := &lanBrowser{t: t, origin: "https://" + net.JoinHostPort(ip.String(), strconv.Itoa(r.port)),
		c: &http.Client{Jar: jar, Timeout: 10 * time.Second, Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}}}} //nolint:gosec // the test's own self-signed cert

	plain := r.openPairing(false)
	var code int
	var m map[string]any
	for i := 0; i < 100; i++ { // the event may come after the first try
		if code, m = b.post("/pair/start", `{"p":"`+plain+`","name":"Android · Chrome"}`); code == http.StatusOK {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if code != http.StatusOK {
		t.Fatalf("start: %d %v", code, m)
	}
	ask := r.ctl.line(t)
	r.ctl.send(t, `{"t":"res","id":`+jsonNum(ask["id"])+`,"data":{"role":"control"}}`)
	id := m["pair_id"].(string)
	for i := 0; i < 100; i++ {
		if _, w := b.post("/pair/wait", `{"pair_id":"`+id+`"}`); w["state"] == "approved" {
			break
		}
	}
	code, m = b.post("/pair/finish", `{"pair_id":"`+id+`","remember":true}`)
	if code != http.StatusOK || m["remembered"] != true {
		t.Fatalf("finish: %d %v", code, m)
	}

	// The app reopens: no session token, only the remembered cookie.
	code, m = b.post("/login/device", `{}`)
	if code != http.StatusOK || m["token"] == nil || m["remembered"] != true {
		t.Fatalf("sign in again: %d %v", code, m)
	}
	// And once more with the next key (it rotates on every use).
	if code, m = b.post("/login/device", `{}`); code != http.StatusOK || m["token"] == nil {
		t.Fatalf("second reopen: %d %v", code, m)
	}
}
