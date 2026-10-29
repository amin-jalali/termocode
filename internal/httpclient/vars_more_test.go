package httpclient

import (
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Every dynamic variable form: good ones give a value, a bad one stays
// as written (and is listed as unresolved) so the user sees it.
func TestResolveDynamicForms(t *testing.T) {
	rv := &Resolver{Now: fixedNow}
	now := fixedNow()
	good := map[string]string{
		"{{$isoTimestamp}}":          "2026-10-16T09:12:33Z",
		"{{$timestamp 1 y}}":         strconv.FormatInt(now.AddDate(1, 0, 0).Unix(), 10),
		"{{$timestamp 1 M}}":         strconv.FormatInt(now.AddDate(0, 1, 0).Unix(), 10),
		"{{$timestamp 2 w}}":         strconv.FormatInt(now.AddDate(0, 0, 14).Unix(), 10),
		"{{$timestamp 30 m}}":        strconv.FormatInt(now.Add(30*time.Minute).Unix(), 10),
		"{{$timestamp -5 s}}":        strconv.FormatInt(now.Add(-5*time.Second).Unix(), 10),
		"{{$timestamp 1500 ms}}":     strconv.FormatInt(now.Add(1500*time.Millisecond).Unix(), 10),
		"{{$localDatetime iso8601}}": now.Local().Format(time.RFC3339),
		"{{$localDatetime rfc1123}}": now.Local().Format(time.RFC1123Z),
		"{{$randomInt -3 -2}}":       "-3",
	}
	for in, want := range good {
		got, un := rv.Resolve(Request{URL: in})
		if got.URL != want || len(un) != 0 {
			t.Errorf("%s → %q (%v), want %q", in, got.URL, un, want)
		}
	}
	for _, in := range []string{
		"{{$timestamp 1}}", "{{$timestamp x d}}", "{{$timestamp 1 fortnight}}",
		"{{$datetime}}", "{{$datetime unix}}", "{{$datetime iso8601 1 q}}",
		"{{$randomInt 5}}", "{{$randomInt 6 5}}", "{{$randomInt a b}}",
		"{{$nope}}", "{{$secret gh}}", "{{not a name}}", "{{$processEnv}}",
	} {
		got, un := rv.Resolve(Request{URL: in})
		if got.URL != in || len(un) == 0 {
			t.Errorf("%s → %q (%v), want it left as written", in, got.URL, un)
		}
	}
	if rv.Known("$secret gh") || rv.Known("$processEnv TERMOCODE_SURELY_UNSET_X") {
		t.Error("Known without a store or an env value")
	}
}

// $processEnv reads the real environment when no lookup is given.
func TestResolveProcessEnvDefault(t *testing.T) {
	t.Setenv("TERMOCODE_HTTP_TEST_VAR", "from-env")
	rv := &Resolver{}
	if got, _ := rv.Resolve(Request{URL: "{{$processEnv TERMOCODE_HTTP_TEST_VAR}}"}); got.URL != "from-env" {
		t.Fatalf("%q", got.URL)
	}
	if !rv.Known("$processEnv TERMOCODE_HTTP_TEST_VAR") {
		t.Error("Known")
	}
	sec := &Resolver{Secret: func(k string) (string, bool) { return "s3cr3t-value", k == "gh" }}
	if !sec.Known("$secret gh") || sec.Known("$secret other") {
		t.Error("Known $secret")
	}
}

// A secret value never reaches a copied curl command: URL, header and
// body fall back to their templates.
func TestToCurlHidesSecretsEverywhere(t *testing.T) {
	body := `{"token":"{{$secret gh}}"}`
	req := Request{Method: "POST", URL: "https://api.test/{{$secret gh}}", Body: &body,
		Headers: []Header{{Name: "X-Key", Value: "{{$secret gh}}"}, {Name: "X-Plain", Value: "ok"}}}
	rv := &Resolver{Secret: func(k string) (string, bool) { return "ghp_secretvalue123", true }}
	rr, _ := rv.Resolve(req)
	shared := ToCurl(rr, false)
	if strings.Contains(shared, "ghp_secretvalue123") || !strings.Contains(shared, "{{$secret gh}}") || !strings.Contains(shared, "X-Plain: ok") {
		t.Fatalf("shared curl leaks or lost text:\n%s", shared)
	}
	// A header added after resolving has no template: it is redacted.
	rr.Headers = append(rr.Headers, Header{Name: "Authorization", Value: "Bearer ghp_secretvalue123"})
	if got := ToCurl(rr, false); strings.Contains(got, "ghp_secretvalue123") {
		t.Fatalf("extra header leaks:\n%s", got)
	}
	if got := ToCurl(rr, true); !strings.Contains(got, "ghp_secretvalue123") {
		t.Fatalf("with secrets on, the value is there:\n%s", got)
	}
}

// .env lines: quotes, broken quotes and lines without "=".
func TestLoadEnvsDotenvForms(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".env"), "NOEQUALS\nA='single quoted'\nB=\"bad \\q escape\"\nC = spaced \n")
	envs := LoadEnvs(root, root)
	want := map[string]string{"A": "single quoted", "B": `bad \q escape`, "C": "spaced"}
	if !reflect.DeepEqual(envs.Dotenv, want) {
		t.Fatalf("dotenv = %#v", envs.Dotenv)
	}
}

// Env files: an unreadable one is an error; a folder outside the root
// reads only itself; a nil set is safe to ask.
func TestLoadEnvsEdges(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "http-client.env.json"), 0o755); err != nil { // a folder where the file goes
		t.Fatal(err)
	}
	envs := LoadEnvs(root, root)
	if len(envs.Errs) != 1 {
		t.Fatalf("errs = %v", envs.Errs)
	}
	other := t.TempDir()
	writeFile(t, filepath.Join(other, "http-client.env.json"), `{"dev": {"h": "1"}, "$shared": {"s": "2"}}`)
	writeFile(t, filepath.Join(other, "http-client.private.env.json"), `{"dev": {"p": "3"}}`)
	envs = LoadEnvs(other, root)
	if !reflect.DeepEqual(envs.Names(), []string{"dev"}) {
		t.Fatalf("outside root: %v", envs.Names())
	}
	if got := envs.VarNames("dev"); !reflect.DeepEqual(got, []string{"h", "p", "s"}) {
		t.Fatalf("VarNames = %v", got)
	}
	var none *Envs
	if none.Names() != nil || none.VarNames("dev") != nil {
		t.Error("nil envs")
	}
	if _, _, ok := none.Lookup("dev", "h"); ok {
		t.Error("nil lookup")
	}
}

func TestStateLoadAndSaveErrors(t *testing.T) {
	s, err := LoadState(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || s.Env("/ws") != "" {
		t.Fatalf("missing = %+v %v", s, err)
	}
	if _, err := LoadState(t.TempDir()); err == nil {
		t.Error("a folder read as state")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	writeFile(t, blocker, "")
	s.SetEnv("/ws", "dev")
	if err := s.Save(filepath.Join(blocker, "state.json")); err == nil {
		t.Error("saved under a file")
	}
}
