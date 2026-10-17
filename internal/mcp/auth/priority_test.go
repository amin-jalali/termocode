package auth

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

const cimd = "https://docs.example/mcp/client.json"

// Spec (CX-3, MCP spec client registration priority): a pre-registered
// client, then a Client ID Metadata Document, then DCR.
func TestClientPriority(t *testing.T) {
	// CIMD and DCR offered: CIMD wins, nothing is registered.
	f := newFixture(t, mcptest.AuthOptions{CIMD: true})
	fl, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL(), CIMDURL: cimd})
	if err != nil {
		t.Fatal(err)
	}
	fl.Close()
	if reg, _, _ := f.as.Counts(); reg != 0 || !strings.Contains(fl.AuthURL(), "client_id="+urlEsc(cimd)) {
		t.Errorf("CIMD not first: %d registrations, %s", reg, fl.AuthURL())
	}
	// CIMD offered but termocode has no document: DCR.
	f = newFixture(t, mcptest.AuthOptions{CIMD: true})
	fl, err = Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL()})
	if err != nil {
		t.Fatal(err)
	}
	fl.Close()
	if reg, _, _ := f.as.Counts(); reg != 1 {
		t.Errorf("registrations %d", reg)
	}
	// A pre-registered client beats both.
	f = newFixture(t, mcptest.AuthOptions{CIMD: true, PreClientID: "slack-app"})
	fl, err = Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL(), CIMDURL: cimd, ClientID: "slack-app"})
	if err != nil {
		t.Fatal(err)
	}
	fl.Close()
	if reg, _, _ := f.as.Counts(); reg != 0 || !strings.Contains(fl.AuthURL(), "client_id=slack-app") {
		t.Errorf("pre-registered: %d %s", reg, fl.AuthURL())
	}
	// Neither: ErrNeedsApp.
	f = newFixture(t, mcptest.AuthOptions{NoDCR: true})
	if _, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL(), CIMDURL: cimd}); !errors.Is(err, ErrNeedsApp) {
		t.Errorf("neither: %v", err)
	}
}

func urlEsc(s string) string {
	return strings.NewReplacer(":", "%3A", "/", "%2F").Replace(s)
}

func freePorts(t *testing.T) {
	t.Helper()
	for _, p := range LoopbackPorts {
		l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err != nil {
			t.Skipf("port %d is busy on this machine", p)
		}
		l.Close()
	}
}

// Spec: the loopback listener takes the first free port of the fixed
// set 47391–47395 (so CIMD's exact redirect match holds and ssh -L can
// be set up once); all busy is a clear error; mcp_oauth_port overrides.
func TestLoopbackPortSet(t *testing.T) {
	freePorts(t)
	f := newFixture(t, mcptest.AuthOptions{})
	fl, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL()})
	if err != nil {
		t.Fatal(err)
	}
	if fl.Port() != 47391 || fl.RedirectURI() != "http://127.0.0.1:47391/callback" {
		t.Errorf("first: %d %s", fl.Port(), fl.RedirectURI())
	}
	fl2, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL()})
	if err != nil {
		t.Fatal(err)
	}
	if fl2.Port() != 47392 {
		t.Errorf("second: %d", fl2.Port())
	}
	var held []net.Listener
	for _, p := range LoopbackPorts[2:] {
		l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p))
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, l)
	}
	_, err = Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL()})
	if err == nil || !strings.Contains(err.Error(), "47391") || !strings.Contains(err.Error(), "mcp_oauth_port") {
		t.Errorf("all busy: %v", err)
	}
	fl.Close()
	fl2.Close()
	for _, l := range held {
		l.Close()
	}
	// A localhost redirect (Slack's desktop apps) on the same listener.
	fl3, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL(), RedirectHost: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	defer fl3.Close()
	if fl3.RedirectURI() != "http://localhost:47391/callback" {
		t.Errorf("localhost redirect %s", fl3.RedirectURI())
	}
	if page := browse(t, fl3.AuthURL()); !strings.Contains(page, "close this tab") {
		t.Errorf("localhost callback page %q", page)
	}
}

// Spec: docs/mcp/client.json lists exactly the redirect URIs of the
// fixed port set (127.0.0.1 and localhost).
func TestClientJSONRedirects(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "mcp", "client.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		RedirectURIs []string `json:"redirect_uris"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if strings.Join(doc.RedirectURIs, " ") != strings.Join(LoopbackRedirects(), " ") {
		t.Errorf("client.json redirect_uris\n%v\nwant\n%v", doc.RedirectURIs, LoopbackRedirects())
	}
}

// Spec: Revoke posts the tokens to the revocation endpoint; a sign-in
// keeps the endpoint from discovery.
func TestRevoke(t *testing.T) {
	f := newFixture(t, mcptest.AuthOptions{Revocation: true})
	fl, err := Begin(ctx(t), Config{ServerURL: f.mcp.MCPURL()})
	if err != nil {
		t.Fatal(err)
	}
	browse(t, fl.AuthURL())
	tok, err := fl.Wait(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if tok.RevocationEndpoint != f.as.Issuer()+"/revoke" {
		t.Fatalf("revocation endpoint %q", tok.RevocationEndpoint)
	}
	if err := Revoke(ctx(t), nil, tok); err != nil {
		t.Fatal(err)
	}
	if r := f.as.Revoked(); len(r) != 2 || r[0] != tok.RefreshToken || r[1] != tok.AccessToken {
		t.Errorf("revoked %v", r)
	}
	if f.as.Valid(tok.AccessToken) {
		t.Error("still valid")
	}
	// No endpoint: nothing to do, no error.
	if err := Revoke(ctx(t), nil, ai.MCPOAuth{AccessToken: "x"}); err != nil {
		t.Errorf("no endpoint: %v", err)
	}
	// The server refuses: an error for the log.
	if err := Revoke(ctx(t), nil, ai.MCPOAuth{AccessToken: "x", RevocationEndpoint: f.as.Issuer() + "/nope"}); err == nil {
		t.Error("a 404 revoke is not reported")
	}
}
