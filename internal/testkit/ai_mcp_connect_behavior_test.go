//go:build unix

package testkit

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

// "Connect a service" (CX-5): the wizard against fakes only. Every test
// shadows gh with mcptest.FakeGH (the real gh would talk to GitHub) and
// points the presets at httptest servers with mcp.OverridePresets.

var ghAccounts = []string{"amin-jalali", "perkino-deals", "huisstart"}

// noGH puts a gh on PATH that is not signed in (it hides a real one).
func noGH(t *testing.T) {
	mcptest.FakeGH(t, nil, "")
	t.Setenv("GH_FAKE_NOT_LOGGED_IN", "1")
}

// pointPreset swaps one preset's endpoints for the test.
func pointPreset(t *testing.T, name string, fn func(p *mcp.Preset)) {
	t.Cleanup(mcp.OverridePresets(func(ps []mcp.Preset) []mcp.Preset {
		for i := range ps {
			if ps[i].Name == name {
				fn(&ps[i])
			}
		}
		return ps
	}))
}

func userMCPJSON(t *testing.T) string {
	b, _ := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "termocode", "mcp.json"))
	return string(b)
}

// configTree returns every file under the sandbox's config and cache
// dirs (to look for leaked tokens).
func configTree(t *testing.T) map[string]string {
	out := map[string]string{}
	for _, root := range []string{os.Getenv("XDG_CONFIG_HOME"), os.Getenv("XDG_CACHE_HOME")} {
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				b, _ := os.ReadFile(p)
				out[p] = string(b)
			}
			return nil
		})
	}
	return out
}

// pickService opens AI: Connect a service… and picks a row.
func pickService(d *Driver, title string) {
	d.T.Helper()
	runPalette(d, "AI: Connect a Service")
	if !d.WaitForText("Connect a service", 10*time.Second) {
		d.T.Fatalf("no wizard:\n%s", screen(d))
	}
	if !d.WaitForText(title, 5*time.Second) {
		d.T.Fatalf("no %s row:\n%s", title, screen(d))
	}
	d.Type(title)
	d.Key("enter")
}

func waitConnected(t *testing.T, d *Driver, want string) {
	t.Helper()
	if !d.WaitForText(want, 20*time.Second) {
		t.Fatalf("no %q:\n%s", want, screen(d))
	}
}

// Spec (§4.2): gh signed in with three accounts → the GitHub screen lists
// them (active first) → Use gh → Done says "connected as … · read only";
// mcp.json has auth.gh and the /readonly URL; the chat can call a tool;
// no gh token is on disk.
func TestConnectGitHubWithGH(t *testing.T) {
	mcptest.FakeGH(t, ghAccounts, "amin-jalali")
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Bearer: mcptest.GHToken("perkino-deals"), Login: "perkino-deals"})
	pointPreset(t, "github", func(p *mcp.Preset) { p.URL, p.ReadOnlyURL = srv.MCPURL(), srv.MCPURL()+"/readonly" })
	s, model := fakeToolModel(t, toolCall{"mcp__github__get_me", map[string]any{}})
	d := remoteDriver(t, model.URL, `{"mcpServers":{}}`, "", false)

	pickService(d, "GitHub")
	if !d.WaitForText("You are signed in to GitHub with the gh command", 10*time.Second) {
		t.Fatalf("no gh screen:\n%s", screen(d))
	}
	sc := screen(d)
	if strings.Index(sc, "amin-jalali (active)") < 0 || strings.Index(sc, "amin-jalali") > strings.Index(sc, "perkino-deals") ||
		!strings.Contains(sc, "huisstart") || !strings.Contains(sc, "Read only") {
		t.Fatalf("accounts:\n%s", sc)
	}
	d.Key("right") // perkino-deals
	d.Key("enter") // Enter on a choice = Use gh
	waitConnected(t, d, "connected as perkino-deals")
	if sc := screen(d); !strings.Contains(sc, "read only") || !strings.Contains(sc, "show my open pull requests") {
		t.Errorf("done screen:\n%s", sc)
	}
	var f struct {
		MCPServers map[string]struct {
			URL  string `json:"url"`
			Auth struct {
				GH string `json:"gh"`
			} `json:"auth"`
		} `json:"mcpServers"`
	}
	_ = json.Unmarshal([]byte(userMCPJSON(t)), &f)
	if e := f.MCPServers["github"]; e.URL != srv.MCPURL()+"/readonly" || e.Auth.GH != "perkino-deals" {
		t.Errorf("mcp.json %s", userMCPJSON(t))
	}
	d.Key("tab", "enter") // Done
	askAI(d, "who am i")
	waitDone(t, d)
	if r := s.toolResults(); len(r) != 1 || !strings.Contains(r[0], "perkino-deals") {
		t.Errorf("tool results %q", r)
	}
	for p, body := range configTree(t) {
		if strings.Contains(body, "gho_fake") {
			t.Errorf("%s holds the gh token", p)
		}
	}
}

// Spec (§4.3): no gh, over SSH → the device screen shows the user code
// and the server's page; the test approves at the fake AS; Done;
// credentials.json (0600) holds the tokens.
func TestConnectGitHubDeviceFlow(t *testing.T) {
	noGH(t)
	clip := sinkClipboard(t)
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{Device: true, PreClientID: "Ov23liFAKE", DeviceMode: "manual", GitHubStyle: true, NoRefresh: true})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, Login: "amin-jalali"})
	pointPreset(t, "github", func(p *mcp.Preset) {
		p.URL, p.ReadOnlyURL = srv.MCPURL(), srv.MCPURL()+"/readonly"
		p.Device = &mcp.DeviceApp{ClientID: func() string { return "Ov23liFAKE" }, DeviceEndpoint: as.Issuer() + "/device/code",
			TokenEndpoint: as.Issuer() + "/token", Issuer: as.Issuer(), Scopes: []string{"read:user"}, Resource: srv.MCPURL(), NoResourceParam: true}
	})
	_, model := fakeToolModel(t)
	d := remoteDriver(t, model.URL, `{"mcpServers":{}}`, "", true)

	pickService(d, "GitHub")
	if !d.WaitForText("Enter this code there", 10*time.Second) {
		t.Fatalf("no device screen:\n%s", screen(d))
	}
	sc := screen(d)
	code := ""
	for _, f := range strings.Fields(sc) {
		if strings.HasPrefix(f, "WDJB-") {
			code = f
		}
	}
	if code == "" || !strings.Contains(sc, as.Issuer()+"/device") || !strings.Contains(sc, "left") || !strings.Contains(sc, "laptop or phone") {
		t.Fatalf("device screen:\n%s", sc)
	}
	if got, _ := clip.get(); got != code {
		t.Errorf("clipboard %q, want the code %q", got, code)
	}
	as.ApproveDevice(code)
	waitConnected(t, d, "connected as amin-jalali")
	c, mode := credentials(t)
	if !signedIn(t, "github") || mode != 0o600 {
		t.Errorf("credentials (mode %o): %v", mode, c)
	}
	if o := c["mcp_oauth"].(map[string]any)["github"].(map[string]any); o["account"] != "amin-jalali" {
		t.Errorf("account not stored: %v", o)
	}
}

// Spec: without a registered GitHub OAuth App the device step is
// skipped and the wizard says so; the token way still works.
func TestConnectGitHubNoClientID(t *testing.T) {
	noGH(t)
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Bearer: "ghp_testtoken_1234567890", Login: "amin-jalali"})
	pointPreset(t, "github", func(p *mcp.Preset) {
		p.URL, p.ReadOnlyURL = srv.MCPURL(), srv.MCPURL()+"/readonly"
		p.Device.ClientID = func() string { return "" }
	})
	_, model := fakeToolModel(t)
	d := remoteDriver(t, model.URL, `{"mcpServers":{}}`, "", false)
	pickService(d, "GitHub")
	if !d.WaitForText("Connect GitHub with a token", 10*time.Second) {
		t.Fatalf("no token screen:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "sign-in with a code is not set up in this build") {
		t.Errorf("no note about the missing app:\n%s", screen(d))
	}
	d.Type("ghp_testtoken_1234567890")
	if strings.Contains(screen(d), "ghp_testtoken") {
		t.Error("the token is shown")
	}
	d.Key("enter")
	waitConnected(t, d, "connected as amin-jalali")
	if !strings.Contains(userMCPJSON(t), "${secret:github_token}") || strings.Contains(userMCPJSON(t), "ghp_") {
		t.Errorf("mcp.json %s", userMCPJSON(t))
	}
	c, _ := credentials(t)
	if c["mcp_secrets"].(map[string]any)["github_token"] != "ghp_testtoken_1234567890" {
		t.Errorf("secret not saved: %v", c)
	}
}

// Spec (§4.4): Linear on a laptop: the browser screen with a countdown
// (the sandbox has no browser: the "could not open" line and c to copy);
// the test plays the browser; Done with the tool count.
func TestConnectLinearBrowser(t *testing.T) {
	noGH(t)
	clip := sinkClipboard(t)
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer()})
	pointPreset(t, "linear", func(p *mcp.Preset) { p.URL, p.ReadOnlyURL = srv.MCPURL(), srv.MCPURL()+"/readonly" })
	_, model := fakeToolModel(t)
	d := remoteDriver(t, model.URL, `{"mcpServers":{}}`, "", false)

	pickService(d, "Linear")
	if !d.WaitForText("Waiting for Linear", 10*time.Second) {
		t.Fatalf("no browser screen:\n%s", screen(d))
	}
	sc := screen(d)
	if !strings.Contains(sc, "left") || !strings.Contains(sc, "Could not open a browser") || !strings.Contains(sc, "Use a token instead") {
		t.Errorf("browser screen:\n%s", sc)
	}
	var authURL string
	if !d.WaitFor(func() bool { authURL, _ = clip.get(); return strings.HasPrefix(authURL, as.Issuer()+"/authorize?") }, 5*time.Second) {
		t.Fatalf("authorize URL not copied: %q", authURL)
	}
	if !strings.Contains(authURL, "scope=read") || !strings.Contains(authURL, "47391") {
		t.Errorf("authorize URL %s", authURL)
	}
	resp, err := http.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	waitConnected(t, d, "Linear — connected")
	if sc := screen(d); !strings.Contains(sc, "tools") || !strings.Contains(sc, "Allow writes") {
		t.Errorf("done:\n%s", sc)
	}
	if !signedIn(t, "linear") || !strings.Contains(userMCPJSON(t), "/mcp/readonly") {
		t.Errorf("not saved: %s", userMCPJSON(t))
	}
}

// Spec (§4.5): Linear over SSH: the paste screen (masked); a wrong state
// gives the error screen with Try again; the right address connects.
func TestConnectLinearOverSSHPaste(t *testing.T) {
	noGH(t)
	clip := sinkClipboard(t)
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer()})
	pointPreset(t, "linear", func(p *mcp.Preset) { p.URL, p.ReadOnlyURL = srv.MCPURL(), srv.MCPURL()+"/readonly" })
	_, model := fakeToolModel(t)
	d := remoteDriver(t, model.URL, `{"mcpServers":{}}`, "", true)

	pickService(d, "Linear")
	if !d.WaitForText("over SSH", 10*time.Second) {
		t.Fatalf("no SSH screen:\n%s", screen(d))
	}
	if sc := screen(d); !strings.Contains(sc, "ssh -L 47391:127.0.0.1:47391 10.9.8.7") || !strings.Contains(sc, "Paste it here") {
		t.Errorf("ssh screen:\n%s", sc)
	}
	authURL, _ := clip.get()
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	callback := resp.Header.Get("Location")
	bad := strings.Replace(callback, "state=", "state=x", 1)
	d.Type(bad)
	if strings.Contains(screen(d), "state=x") {
		t.Error("the pasted address is shown")
	}
	d.Key("enter")
	if !d.WaitForText("belongs to another sign-in", 10*time.Second) {
		t.Fatalf("no wrong-state error:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "Try again") {
		t.Errorf("no Try again:\n%s", screen(d))
	}
	d.Key("enter") // Try again (the primary button)
	if !d.WaitForText("Paste it here", 10*time.Second) {
		t.Fatalf("no new SSH screen:\n%s", screen(d))
	}
	authURL, _ = clip.get()
	resp, err = c.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	d.Type(resp.Header.Get("Location"))
	d.Key("enter")
	waitConnected(t, d, "Linear — connected")
}

// Spec (§4.8): Use a token instead → masked token screen → the entry gets
// a ${secret:…} header → test call → Done; a refused token names the
// scopes.
func TestConnectTokenFallback(t *testing.T) {
	noGH(t)
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Bearer: "sntrys_good_token_123", BearerScheme: "Sentry-Bearer", AuthServer: as.Issuer()})
	pointPreset(t, "sentry", func(p *mcp.Preset) { p.URL = srv.MCPURL(); p.Identity = "" })
	_, model := fakeToolModel(t)
	d := remoteDriver(t, model.URL, `{"mcpServers":{}}`, "", false)

	pickService(d, "Sentry")
	if !d.WaitForText("Use a token instead", 10*time.Second) {
		t.Fatalf("no browser screen:\n%s", screen(d))
	}
	d.Key("tab") // the buttons
	d.Key("enter")
	if !d.WaitForText("Connect Sentry with a token", 10*time.Second) {
		t.Fatalf("no token screen:\n%s", screen(d))
	}
	if sc := screen(d); !strings.Contains(sc, "org:read") || !strings.Contains(sc, "credentials.json") {
		t.Errorf("token screen:\n%s", sc)
	}
	d.Type("sntrys_bad_token_999")
	d.Key("enter")
	if !d.WaitForText("refused the token", 15*time.Second) {
		t.Fatalf("no refused error:\n%s", screen(d))
	}
	if !strings.Contains(screen(d), "org:read") {
		t.Errorf("the error does not name the scopes:\n%s", screen(d))
	}
	d.Key("enter") // Edit token
	if !d.WaitForText("Connect Sentry with a token", 5*time.Second) {
		t.Fatalf("no token screen again:\n%s", screen(d))
	}
	d.Type("sntrys_good_token_123")
	d.Key("enter")
	waitConnected(t, d, "Sentry — connected")
	if j := userMCPJSON(t); !strings.Contains(j, "Sentry-Bearer ${secret:sentry_token}") || strings.Contains(j, "sntrys_") {
		t.Errorf("mcp.json %s", j)
	}
	for p, body := range configTree(t) {
		if strings.Contains(body, "sntrys_good") && !strings.HasSuffix(p, "credentials.json") {
			t.Errorf("%s holds the token", p)
		}
	}
}

// Spec (§3 Slack): the recipe copies the manifest; the pasted client id
// goes into mcp.json and the browser sign-in follows with the fixed
// localhost port.
func TestConnectSlackRecipe(t *testing.T) {
	noGH(t)
	clip := sinkClipboard(t)
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{NoDCR: true, PreClientID: "1234.5678"})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer()})
	pointPreset(t, "slack", func(p *mcp.Preset) { p.URL = srv.MCPURL() })
	_, model := fakeToolModel(t)
	d := remoteDriver(t, model.URL, `{"mcpServers":{}}`, "", false)

	pickService(d, "Slack")
	if !d.WaitForText("Client ID", 10*time.Second) {
		t.Fatalf("no recipe:\n%s", screen(d))
	}
	if m, _ := clip.get(); !strings.Contains(m, "http://localhost:47391/callback") {
		t.Errorf("manifest not copied: %q", m)
	}
	d.Type("1234.5678")
	d.Key("enter")
	var authURL string
	if !d.WaitFor(func() bool { authURL, _ = clip.get(); return strings.HasPrefix(authURL, as.Issuer()+"/authorize?") }, 10*time.Second) {
		t.Fatalf("no sign-in after the client id:\n%s", screen(d))
	}
	if !strings.Contains(authURL, "client_id=1234.5678") || !strings.Contains(authURL, "localhost%3A47391") {
		t.Errorf("authorize URL %s", authURL)
	}
	if !strings.Contains(userMCPJSON(t), `"client_id": "1234.5678"`) {
		t.Errorf("mcp.json %s", userMCPJSON(t))
	}
	// The browser: the fake redirects to http://localhost:47391/callback.
	resp, err := http.Get(authURL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	waitConnected(t, d, "Slack — connected")
}

// Spec (§4.7): access_denied and an expired code give their own text and
// buttons; DCR 403 says only approved apps may sign in.
func TestConnectErrors(t *testing.T) {
	noGH(t)
	for _, c := range []struct {
		mode, want, button string
	}{{"denied", "cancelled on the web page", "Try again"}, {"expired", "The code expired", "Use a token"}} {
		t.Run(c.mode, func(t *testing.T) {
			as := mcptest.NewAuthServer(t, mcptest.AuthOptions{Device: true, PreClientID: "Ov23liFAKE", DeviceMode: c.mode, GitHubStyle: true})
			srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid})
			pointPreset(t, "github", func(p *mcp.Preset) {
				p.URL, p.ReadOnlyURL = srv.MCPURL(), srv.MCPURL()+"/readonly"
				p.Device = &mcp.DeviceApp{ClientID: func() string { return "Ov23liFAKE" }, DeviceEndpoint: as.Issuer() + "/device/code",
					TokenEndpoint: as.Issuer() + "/token", Issuer: as.Issuer(), Resource: srv.MCPURL(), NoResourceParam: true}
			})
			_, model := fakeToolModel(t)
			d := remoteDriver(t, model.URL, `{"mcpServers":{}}`, "", true)
			pickService(d, "GitHub")
			if !d.WaitForText(c.want, 15*time.Second) {
				t.Fatalf("no %q:\n%s", c.want, screen(d))
			}
			if !strings.Contains(screen(d), c.button) {
				t.Errorf("no %s button:\n%s", c.button, screen(d))
			}
		})
	}
	t.Run("dcr-403", func(t *testing.T) {
		as := mcptest.NewAuthServer(t, mcptest.AuthOptions{DCRStatus: 403})
		srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer()})
		pointPreset(t, "notion", func(p *mcp.Preset) { p.URL = srv.MCPURL() })
		_, model := fakeToolModel(t)
		d := remoteDriver(t, model.URL, `{"mcpServers":{}}`, "", false)
		pickService(d, "Notion")
		if !d.WaitForText("lets only approved apps sign in", 15*time.Second) {
			t.Fatalf("no DCR error:\n%s", screen(d))
		}
	})
}

// Spec: Esc on a waiting screen ends the sign-in (the listener closes).
func TestConnectCancel(t *testing.T) {
	noGH(t)
	clip := sinkClipboard(t)
	as := mcptest.NewAuthServer(t, mcptest.AuthOptions{})
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Validate: as.Valid, AuthServer: as.Issuer()})
	pointPreset(t, "linear", func(p *mcp.Preset) { p.URL, p.ReadOnlyURL = srv.MCPURL(), srv.MCPURL()+"/readonly" })
	_, model := fakeToolModel(t)
	d := remoteDriver(t, model.URL, `{"mcpServers":{}}`, "", false)
	pickService(d, "Linear")
	if !d.WaitForText("Waiting for Linear", 10*time.Second) {
		t.Fatalf("no browser screen:\n%s", screen(d))
	}
	authURL, _ := clip.get()
	d.Key("esc")
	if !d.WaitFor(func() bool { return !strings.Contains(screen(d), "Waiting for Linear") }, 5*time.Second) {
		t.Fatal("Esc did not close the wizard")
	}
	if !d.WaitFor(func() bool {
		c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		resp, err := c.Get(authURL)
		if err != nil {
			return false
		}
		resp.Body.Close()
		_, err = http.Get(resp.Header.Get("Location"))
		return err != nil // the loopback listener is closed
	}, 5*time.Second) {
		t.Error("the loopback listener is still open")
	}
	if signedIn(t, "linear") {
		t.Error("signed in after Esc")
	}
}
