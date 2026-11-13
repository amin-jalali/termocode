package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCovSchemaChecks(t *testing.T) {
	cases := []struct {
		name, schema, val, wantErr string
	}{
		{"int ok", `{"type":"integer"}`, `3`, ""},
		{"int fraction", `{"type":"integer"}`, `3.5`, "want integer"},
		{"int is string", `{"type":"integer"}`, `"3"`, "is string, want integer"},
		{"number ok", `{"type":"number"}`, `3.5`, ""},
		{"number is bool", `{"type":"number"}`, `true`, "is boolean"},
		{"type list ok", `{"type":["string","null"]}`, `null`, ""},
		{"type list bad", `{"type":["string","null"]}`, `[1]`, "is array"},
		{"odd type unchecked", `{"type":5}`, `"x"`, ""},
		{"enum miss", `{"enum":["a","b"]}`, `"c"`, "not one of"},
		{"enum hit", `{"enum":["a","b"]}`, `"b"`, ""},
		{"required", `{"type":"object","required":["id"]}`, `{}`, `has no "id"`},
		{"nested", `{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"string"}}}}}`, `{"a":{"b":1}}`, "a.b is number, want string"},
		{"array item", `{"type":"array","items":{"type":"string"}}`, `["x", 2]`, "[1] is number"},
		{"object type", `{"type":"object"}`, `{"a":1}`, ""},
		{"unknown json kind", `{"type":"object"}`, `"x"`, "is string, want object"},
		{"not json", `{}`, `{`, "not JSON"},
	}
	for _, c := range cases {
		var sc map[string]any
		if err := json.Unmarshal([]byte(c.schema), &sc); err != nil {
			t.Fatal(err)
		}
		err := checkSchema(sc, json.RawMessage(c.val))
		if c.wantErr == "" && err != nil || c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)) {
			t.Errorf("%s: got %v, want %q", c.name, err, c.wantErr)
		}
	}
	if jsonType(struct{}{}) != "unknown" || jsonType(nil) != "null" {
		t.Error("jsonType kinds")
	}
}

func TestCovGlobAndPolicyRules(t *testing.T) {
	yes := [][2]string{{"git*", "github"}, {"*://x.com/*", "https://x.com/a/b"}, {"a*b*c", "aXXbYYc"}, {"npx", "npx"}}
	no := [][2]string{{"git*", "hub"}, {"a*b*c", "aXXcYYb"}, {"a*c", "abd"}, {"npx", "npm"}, {"[", "x"}, {"*x*y", "zzx"}}
	for _, p := range yes {
		if !globMatch(p[0], p[1]) {
			t.Errorf("globMatch(%q,%q) = false", p[0], p[1])
		}
	}
	for _, p := range no {
		if globMatch(p[0], p[1]) {
			t.Errorf("globMatch(%q,%q) = true", p[0], p[1])
		}
	}
	if (PolicyRule{}).matches(Server{Name: "x"}) {
		t.Error("an empty rule must match nothing")
	}
	if (PolicyRule{URL: "https://*"}).matches(Server{Command: "npx"}) {
		t.Error("a url rule must not match a command server")
	}
	if !(PolicyRule{Command: "npx*"}).matches(Server{Command: "/usr/bin/npx", Args: []string{"-y"}}) {
		t.Error("a command rule matches the base name")
	}
	if (PolicyRule{Command: "npx"}).matches(Server{URL: "https://x"}) {
		t.Error("a command rule must not match a url server")
	}
	if UserPolicyPath("") != "" || UserPolicyPath("/a/b/mcp.json") != filepath.Join("/a/b", "mcp-policy.json") {
		t.Error("UserPolicyPath")
	}
}

func TestCovSystemPolicyPathPerOS(t *testing.T) {
	p := defaultSystemPolicyPath()
	switch runtime.GOOS {
	case "darwin":
		if !strings.HasPrefix(p, "/Library/") {
			t.Errorf("darwin path %q", p)
		}
	case "windows":
	default:
		if p != "/etc/termocode/mcp-policy.json" {
			t.Errorf("path %q", p)
		}
	}
}

func TestCovSmallHelpers(t *testing.T) {
	if (BundleField{}).DefaultText() != "" || (BundleField{Default: "x"}).DefaultText() != "x" ||
		(BundleField{Default: 3.0}).DefaultText() != "3" || (BundleField{Default: []any{1.0}}).DefaultText() != "[1]" {
		t.Error("DefaultText")
	}
	if got := (Catalog{URL: "http://127.0.0.1:9/"}).Host(); got != "127.0.0.1:9" {
		t.Errorf("Host = %q", got)
	}
	if got := (Catalog{}).Host(); got == "" {
		t.Error("the official host is not empty")
	}
	if got := (Catalog{URL: "::bad"}).Host(); got != "::bad" {
		t.Errorf("a bad url is shown as it is, got %q", got)
	}
	if (Catalog{}).cachePath() != "" {
		t.Error("no cache dir, no cache path")
	}
	if (Catalog{CacheDir: "/c", URL: "http://a/"}).cachePath() == (Catalog{CacheDir: "/c", URL: "http://b/"}).cachePath() {
		t.Error("two catalogs share one cache file")
	}
	if got := (CatalogEntry{Name: "io.github/My Server!"}).ShortName(); got != "my-server" {
		t.Errorf("ShortName = %q", got)
	}
	if got := (CatalogEntry{Name: "plain"}).ShortName(); got != "plain" {
		t.Errorf("ShortName = %q", got)
	}
	if UserPath("/c") != filepath.Join("/c", "mcp.json") {
		t.Error("UserPath")
	}
	if serverOfDisplay("mcp.git.hub.status") != "git.hub" || serverOfDisplay("mcp.solo") != "solo" {
		t.Error("serverOfDisplay")
	}
	if rawBool(nil, "x") || rawBool(json.RawMessage(`{`), "x") || rawBool(json.RawMessage(`{"x":"yes"}`), "x") || !rawBool(json.RawMessage(`{"x":true}`), "x") {
		t.Error("rawBool")
	}
	if (Server{}).AutostartOff() || (Server{raw: json.RawMessage(`{`)}).AutostartOff() || (Server{raw: json.RawMessage(`{"autostart":true}`)}).AutostartOff() || !(Server{raw: json.RawMessage(`{"autostart":false}`)}).AutostartOff() {
		t.Error("AutostartOff")
	}
	if (Resource{URI: "u"}).Label() != "u" || (Resource{URI: "u", Name: "n"}).Label() != "n" || (Resource{URI: "u", Name: "n", Title: "t"}).Label() != "t" {
		t.Error("Resource.Label")
	}
	if (&CallError{Code: 1, Message: "boom"}).Error() != "boom" {
		t.Error("CallError.Error")
	}
	for s, want := range map[string]bool{"": false, "abc.D_1-2": true, "a b": false, "a/b": false, strings.Repeat("a", 101): false} {
		if ValidClientID(s) != want {
			t.Errorf("ValidClientID(%q) != %v", s, want)
		}
	}
	r := AppRecipe{RedirectHost: "127.0.0.1", Port: 4242}
	if r.RedirectURI() != "http://127.0.0.1:4242/callback" {
		t.Errorf("RedirectURI = %q", r.RedirectURI())
	}
}

func TestCovGitHubClientIDOverride(t *testing.T) {
	SetGitHubClientID("  my-id ")
	if GitHubDeviceClientID() != "my-id" {
		t.Errorf("got %q", GitHubDeviceClientID())
	}
	SetGitHubClientID("")
	if GitHubDeviceClientID() != GitHubClientID {
		t.Error("an empty override falls back to the build-time id")
	}
	if _, ok := PresetByName("github"); !ok {
		t.Error("github preset")
	}
	if _, ok := PresetByName("nope"); ok {
		t.Error("unknown preset found")
	}
}

func TestCovProfilesSummaryFind(t *testing.T) {
	if got := (Profile{}).Summary(); got != "all servers · all tools" {
		t.Errorf("got %q", got)
	}
	if got := (Profile{Servers: []string{"a"}, Tools: []string{"x.*"}}).Summary(); got != "1 server: a · x.*" {
		t.Errorf("got %q", got)
	}
	if got := (Profile{Servers: []string{"a", "b"}}).Summary(); got != "2 servers: a, b · all tools" {
		t.Errorf("got %q", got)
	}
	ps := []Profile{{Name: "one"}, {Name: "two"}}
	if p, ok := FindProfile(ps, "two"); !ok || p.Name != "two" {
		t.Error("FindProfile hit")
	}
	if _, ok := FindProfile(ps, "x"); ok {
		t.Error("FindProfile miss")
	}
}

func TestCovStateTextAndCounts(t *testing.T) {
	if got := (Status{State: StateReconnecting, RetryIn: 3500 * time.Millisecond}).StateText(); got != "reconnecting in 4s" {
		t.Errorf("got %q", got)
	}
	if got := (Status{State: StateReconnecting}).StateText(); got != "reconnecting in 1s" {
		t.Errorf("got %q", got)
	}
	if got := (Status{State: StateRunning}).StateText(); got != StateRunning {
		t.Errorf("got %q", got)
	}
	m := &Manager{servers: map[string]*serverState{"b": {state: StateSignIn}, "a": {state: StateSignIn}, "c": {state: StateRunning}}}
	if m.Count() != 3 {
		t.Error("Count")
	}
	if got := strings.Join(m.NeedSignIn(), ","); got != "a,b" {
		t.Errorf("NeedSignIn = %q", got)
	}
}

func TestCovCredStoreForget(t *testing.T) {
	cs := CredStore{Path: filepath.Join(t.TempDir(), "credentials.json")}
	if err := cs.SetAccount("svc", "me@x"); err != nil {
		t.Fatal(err)
	}
	if err := cs.SetSecret("TOK", "v"); err != nil {
		t.Fatal(err)
	}
	if cs.Account("svc") != "me@x" {
		t.Fatal("account not stored")
	}
	if err := cs.Forget("svc", []string{"TOK"}); err != nil {
		t.Fatal(err)
	}
	if cs.Account("svc") != "" {
		t.Error("account label survived Forget")
	}
	if _, ok := cs.Load("svc"); ok {
		t.Error("sign-in survived Forget")
	}
}

func TestCovElicitWithoutAsker(t *testing.T) {
	if got := Elicit(context.Background(), ElicitRequest{}); got.Action != ElicitDecline {
		t.Errorf("no elicitor must decline, got %q", got.Action)
	}
	if _, err := objectKeys(json.RawMessage(`[1]`)); err == nil {
		t.Error("an array is not an object")
	}
	if k, err := objectKeys(nil); k != nil || err != nil {
		t.Error("empty is no keys")
	}
	k, err := objectKeys(json.RawMessage(`{"b":1,"a":{"x":2}}`))
	if err != nil || strings.Join(k, ",") != "b,a" {
		t.Errorf("keys %v %v", k, err)
	}
}

func TestCovCompareAndAutoUpdate(t *testing.T) {
	if compareVersions("1.10", "1.9") != 1 || compareVersions("1.0.a", "1.0.b") >= 0 || compareVersions("1.0", "1.0.1") >= 0 {
		t.Error("compareVersions")
	}
	if ParseAutoUpdate(" Notify ") != AutoUpdateNotify || ParseAutoUpdate("ON") != AutoUpdateOn || ParseAutoUpdate("x") != AutoUpdateOff {
		t.Error("ParseAutoUpdate")
	}
}
