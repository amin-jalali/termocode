//go:build unix

package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/mcp/mcptest"
)

var threeAccounts = []string{"amin-jalali", "perkino-deals", "huisstart"}

// Spec (CX-2): GHAccounts parses `gh auth status` (the fake prints the
// real format): three accounts, the active one first, with scopes.
func TestGHAccounts(t *testing.T) {
	mcptest.FakeGH(t, threeAccounts, "perkino-deals")
	acc, err := GHAccounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(acc) != 3 || acc[0].Login != "perkino-deals" || !acc[0].Active || acc[1].Active ||
		acc[1].Login != "amin-jalali" || acc[2].Login != "huisstart" {
		t.Fatalf("accounts %+v", acc)
	}
	if strings.Join(acc[0].Scopes, ",") != "gist,read:org,repo,workflow" {
		t.Errorf("scopes %v", acc[0].Scopes)
	}
}

// Spec: GHTokenFor returns the account's token and registers it as a
// secret; the active account when user is "".
func TestGHTokenFor(t *testing.T) {
	mcptest.FakeGH(t, threeAccounts, "amin-jalali")
	tok, err := GHTokenFor(context.Background(), "huisstart")
	if err != nil || tok != mcptest.GHToken("huisstart") {
		t.Fatalf("%q %v", tok, err)
	}
	if strings.Contains(ai.Redact("t="+tok), tok) {
		t.Error("gh token not registered as a secret")
	}
	if tok, _ := GHTokenFor(context.Background(), ""); tok != mcptest.GHToken("amin-jalali") {
		t.Errorf("active: %q", tok)
	}
	if _, err := GHTokenFor(context.Background(), "nobody"); err == nil {
		t.Error("unknown account gave a token")
	}
}

// Spec: no gh → ErrNoGH; gh not signed in → ErrGHNotLoggedIn.
func TestGHErrors(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := GHAccounts(context.Background()); !errors.Is(err, ErrNoGH) {
		t.Errorf("no gh: %v", err)
	}
	if _, err := GHTokenFor(context.Background(), "x"); !errors.Is(err, ErrNoGH) {
		t.Errorf("no gh token: %v", err)
	}
	mcptest.FakeGH(t, threeAccounts, "amin-jalali")
	t.Setenv("GH_FAKE_NOT_LOGGED_IN", "1")
	if _, err := GHAccounts(context.Background()); !errors.Is(err, ErrGHNotLoggedIn) {
		t.Errorf("not logged in: %v", err)
	}
	if _, err := GHTokenFor(context.Background(), ""); !errors.Is(err, ErrGHNotLoggedIn) {
		t.Errorf("not logged in token: %v", err)
	}
}

// Spec: a gh that hangs is cut off after the timeout.
func TestGHTimeout(t *testing.T) {
	mcptest.FakeGH(t, threeAccounts, "amin-jalali")
	t.Setenv("GH_FAKE_SLEEP", "5")
	defer func(d time.Duration) { ghTimeout = d }(ghTimeout)
	ghTimeout = 200 * time.Millisecond
	start := time.Now()
	_, err := GHTokenFor(context.Background(), "")
	if err == nil || time.Since(start) > 2*time.Second {
		t.Errorf("err %v after %v", err, time.Since(start))
	}
}

// Spec: "auth": {"gh": "<login>"} is read from the user file; in a
// project file it is invalid (a project can not make termocode run gh).
func TestConfigAuthGH(t *testing.T) {
	body := []byte(`{"mcpServers":{"github":{"type":"http","url":"https://api.githubcopilot.com/mcp/readonly","auth":{"gh":"amin-jalali"}}}}`)
	list, _, err := ParseFile(body, SourceUser, "/u/mcp.json")
	if err != nil || len(list) != 1 || list[0].Problem != "" || list[0].Auth == nil || list[0].Auth.GH != "amin-jalali" {
		t.Fatalf("user: %+v %v", list, err)
	}
	list, _, _ = ParseFile(body, SourceProject, "/p/.termocode/mcp.json")
	if !strings.Contains(list[0].Problem, "auth.gh is only allowed in your own mcp.json") {
		t.Errorf("project: %q", list[0].Problem)
	}
	bad := []byte(`{"mcpServers":{"g":{"url":"https://x.example/mcp","auth":{"gh":"a b;rm"}}}}`)
	list, _, _ = ParseFile(bad, SourceUser, "/u/mcp.json")
	if list[0].Problem == "" {
		t.Error("a bad login is accepted")
	}
}

// Spec: a server with auth.gh connects with the gh token as a bearer;
// the token is never written to credentials.json or the tool cache.
func TestManagerGHAuth(t *testing.T) {
	mcptest.FakeGH(t, threeAccounts, "amin-jalali")
	srv := mcptest.NewHTTPServer(t, mcptest.HTTPOptions{Bearer: mcptest.GHToken("perkino-deals"), Login: "perkino-deals"})
	dir := t.TempDir()
	user := filepath.Join(dir, "mcp.json")
	_ = os.WriteFile(user, []byte(`{"mcpServers":{"gh":{"type":"http","url":"`+srv.MCPURL()+`","auth":{"gh":"perkino-deals"}}}}`), 0o644)
	cred := filepath.Join(dir, "ai", "credentials.json")
	cache := filepath.Join(dir, "cache")
	m := NewManager(ManagerOptions{UserPath: user, CacheDir: cache, Tokens: CredStore{Path: cred}})
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.Connect(ctx, nil, "gh"); err != nil {
		t.Fatal(err)
	}
	res, err := m.Call(ctx, nil, "gh", "get_me", nil)
	if err != nil || !strings.Contains(res.Text(), "perkino-deals") {
		t.Fatalf("get_me: %v %q", err, res.Text())
	}
	for _, root := range []string{dir} {
		_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			b, _ := os.ReadFile(p)
			if strings.Contains(string(b), "gho_fake") {
				t.Errorf("%s holds the gh token", p)
			}
			return nil
		})
	}
}
