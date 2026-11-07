package httpclient

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestParseAuth(t *testing.T) {
	cases := []struct {
		spec string
		want Auth
		err  bool
	}{
		{"basic alice s3cret", Auth{Scheme: AuthBasic, User: "alice", Pass: "s3cret"}, false},
		{"Basic alice", Auth{Scheme: AuthBasic, User: "alice"}, false},
		{"bearer tok-1", Auth{Scheme: AuthBearer, Token: "tok-1"}, false},
		{"apikey header X-API-Key k1", Auth{Scheme: AuthAPIKey, In: "header", Name: "X-API-Key", Token: "k1"}, false},
		{"apikey query api_key k2", Auth{Scheme: AuthAPIKey, In: "query", Name: "api_key", Token: "k2"}, false},
		{"digest bob pw", Auth{Scheme: AuthDigest, User: "bob", Pass: "pw"}, false},
		{"none", Auth{Scheme: AuthNone}, false},
		{"", Auth{}, true},
		{"bearer", Auth{}, true},
		{"apikey cookie a b", Auth{}, true},
		{"apikey header X", Auth{}, true},
		{"magic x", Auth{}, true},
		{"digest", Auth{}, true},
	}
	for _, c := range cases {
		got, err := ParseAuth(c.spec)
		if (err != nil) != c.err {
			t.Fatalf("ParseAuth(%q) err = %v, want err %v", c.spec, err, c.err)
		}
		if err == nil && got != c.want {
			t.Fatalf("ParseAuth(%q) = %+v, want %+v", c.spec, got, c.want)
		}
	}
}

func TestParseAuthErrorHidesValues(t *testing.T) {
	_, err := ParseAuth("magic sekrit-value-123")
	if err == nil || strings.Contains(err.Error(), "sekrit") {
		t.Fatalf("err = %v, must not echo the values", err)
	}
}

func resolveWith(t *testing.T, src string, envs *Envs, env string) (Resolved, []Unresolved) {
	t.Helper()
	f := Parse(src)
	if len(f.Requests) != 1 {
		t.Fatalf("want one request, got %d", len(f.Requests))
	}
	rv := &Resolver{FileVars: f.Vars, Env: env, Envs: envs, Secret: func(n string) (string, bool) {
		if n == "pw" {
			return "from-" + "store", true
		}
		return "", false
	}}
	return rv.Resolve(f.Requests[0])
}

func TestResolveAuthDirectiveBasic(t *testing.T) {
	rr, un := resolveWith(t, "@user = alice\n# @auth basic {{user}} {{$secret pw}}\nGET http://x/a\n", nil, "")
	if len(un) != 0 {
		t.Fatalf("unresolved = %+v", un)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:from-store"))
	if got := rr.Header("Authorization"); got != want {
		t.Fatalf("Authorization = %q, want %q", got, want)
	}
	if rr.Template.HasHeader("Authorization") {
		t.Fatal("the template must stay as written")
	}
	found := false
	for _, s := range rr.Secrets {
		found = found || s == "from-store"
	}
	if !found {
		t.Fatalf("secret not listed: %v", rr.Secrets)
	}
}

func TestResolveAuthBearerAndAPIKey(t *testing.T) {
	rr, _ := resolveWith(t, "# @auth bearer t-1\nGET http://x/a\n", nil, "")
	if got := rr.Header("Authorization"); got != "Bearer t-1" {
		t.Fatalf("bearer = %q", got)
	}
	rr, _ = resolveWith(t, "# @auth apikey header X-Key k-1\nGET http://x/a\n", nil, "")
	if got := rr.Header("X-Key"); got != "k-1" {
		t.Fatalf("apikey header = %q", got)
	}
	rr, _ = resolveWith(t, "# @auth apikey query api_key k 2\nGET http://x/a?b=1#f\n", nil, "")
	if rr.URL != "http://x/a?b=1&api_key=k+2#f" {
		t.Fatalf("apikey query URL = %q", rr.URL)
	}
	rr, _ = resolveWith(t, "# @auth apikey query key k3\nGET http://x/a\n", nil, "")
	if rr.URL != "http://x/a?key=k3" {
		t.Fatalf("apikey query URL = %q", rr.URL)
	}
}

func TestResolveAuthExplicitHeaderWins(t *testing.T) {
	rr, _ := resolveWith(t, "# @auth bearer t-1\nGET http://x/a\nAuthorization: Bearer mine\n", nil, "")
	if got := rr.Header("Authorization"); got != "Bearer mine" || len(rr.Headers) != 1 {
		t.Fatalf("headers = %+v", rr.Headers)
	}
}

func TestResolveAuthFromEnvAndNone(t *testing.T) {
	envs := &Envs{public: map[string]map[string]string{
		"dev":     {"$auth": "bearer {{token}}", "token": "env-tok"},
		SharedEnv: {"$auth": "basic s s"},
	}, private: map[string]map[string]string{}}
	rr, _ := resolveWith(t, "GET http://x/a\n", envs, "dev")
	if got := rr.Header("Authorization"); got != "Bearer env-tok" {
		t.Fatalf("env auth = %q", got)
	}
	rr, _ = resolveWith(t, "GET http://x/a\n", envs, "prod")
	if got := rr.Header("Authorization"); !strings.HasPrefix(got, "Basic ") {
		t.Fatalf("$shared auth = %q", got)
	}
	rr, _ = resolveWith(t, "# @auth none\nGET http://x/a\n", envs, "dev")
	if rr.HasHeader("Authorization") {
		t.Fatalf("@auth none must drop env auth: %+v", rr.Headers)
	}
	rr, _ = resolveWith(t, "# @auth bearer own\nGET http://x/a\n", envs, "dev")
	if got := rr.Header("Authorization"); got != "Bearer own" {
		t.Fatalf("the request directive must win over env: %q", got)
	}
}

func TestResolveAuthUnresolvedAndBad(t *testing.T) {
	_, un := resolveWith(t, "# @auth bearer {{nope}}\nGET http://x/a\n", nil, "")
	if len(un) != 1 || un[0].Name != "nope" || un[0].Field != "auth" {
		t.Fatalf("unresolved = %+v", un)
	}
	rr, un := resolveWith(t, "# @auth magic x\nGET http://x/a\n", nil, "")
	if rr.HasHeader("Authorization") || len(un) != 1 || un[0].Field != "auth" {
		t.Fatalf("bad spec: headers %+v unresolved %+v", rr.Headers, un)
	}
}

func TestSendDigestAuth(t *testing.T) {
	const realm, nonce, user, pass = "r1", "n0nce", "bob", "pw-" + "digest"
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Digest ") {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Digest realm=%q, nonce=%q, qop="auth", algorithm=MD5`, realm, nonce))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		p := parseDigestParams(strings.TrimPrefix(h, "Digest "))
		md := func(s string) string { x := md5.Sum([]byte(s)); return hex.EncodeToString(x[:]) }
		ha1 := md(user + ":" + realm + ":" + pass)
		ha2 := md(r.Method + ":" + p["uri"])
		want := md(ha1 + ":" + nonce + ":" + p["nc"] + ":" + p["cnonce"] + ":auth:" + ha2)
		if p["username"] != user || p["response"] != want || p["uri"] != r.URL.RequestURI() {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		b := make([]byte, 16)
		n, _ := r.Body.Read(b)
		fmt.Fprintf(w, "ok %s", b[:n])
	}))
	defer srv.Close()
	f := Parse("# @auth digest " + user + " " + pass + "\nPOST " + srv.URL + "/p?q=1\n\nhello\n")
	rr, _ := (&Resolver{}).Resolve(f.Requests[0])
	if rr.HasHeader("Authorization") {
		t.Fatal("digest must not add a header before the challenge")
	}
	resp := New(Options{}).Send(context.Background(), rr, Hooks{})
	if resp.Err != "" || resp.Status != 200 || string(resp.Body) != "ok hello" {
		t.Fatalf("status %d err %q body %q", resp.Status, resp.Err, resp.Body)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d, want 2 (challenge + answer)", hits.Load())
	}
}

func TestSendDigestNoChallengeKeeps401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	f := Parse("# @auth digest u p\nGET " + srv.URL + "/\n")
	rr, _ := (&Resolver{}).Resolve(f.Requests[0])
	resp := New(Options{}).Send(context.Background(), rr, Hooks{})
	if resp.Status != 401 {
		t.Fatalf("status = %d, want 401", resp.Status)
	}
}

func TestResolvedJSONHidesDigest(t *testing.T) {
	f := Parse("# @auth digest u secret-pw-1\nGET http://x/\n")
	rr, _ := (&Resolver{}).Resolve(f.Requests[0])
	if rr.Digest == nil || rr.Digest.Pass != "secret-pw-1" {
		t.Fatalf("digest = %+v", rr.Digest)
	}
	b, _ := json.Marshal(rr)
	if strings.Contains(string(b), `"Digest"`) || strings.Contains(string(b), `"Pass"`) {
		t.Fatalf("digest creds leak to JSON: %s", b)
	}
}
