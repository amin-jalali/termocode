package httpclient

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixedNow() time.Time { return time.Date(2026, 10, 16, 9, 12, 33, 0, time.UTC) }

func TestResolvePrecedence(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "http-client.env.json"), `{
  "$shared": {"version": "v1", "shared_only": "s"},
  "dev": {"host": "http://root-dev", "who": "root"},
  "staging": {"host": "https://staging.example"}
}`)
	writeFile(t, filepath.Join(root, "api", "http-client.env.json"), `{"dev": {"host": "http://localhost:8080", "port": 8080, "flag": true}}`)
	writeFile(t, filepath.Join(root, "api", "http-client.private.env.json"), `{"dev": {"token": "dev-secret-token-1"}, "$shared": {"version": "v2"}}`)
	writeFile(t, filepath.Join(root, "api", ".env"), "DB_USER=ada\n# comment\nexport DB_PASS=\"pw with spaces\"\n")
	envs := LoadEnvs(filepath.Join(root, "api"), root)
	if len(envs.Errs) != 0 {
		t.Fatalf("errs: %v", envs.Errs)
	}
	if got := envs.Names(); !reflect.DeepEqual(got, []string{"dev", "staging"}) {
		t.Fatalf("names = %v", got)
	}
	var registered []string
	rv := &Resolver{
		FileVars: []Var{{Name: "base", Value: "{{host}}/{{version}}"}, {Name: "who", Value: "file"}},
		Env:      "dev",
		Envs:     envs,
		LookupEnv: func(k string) (string, bool) {
			if k == "HOME_X" {
				return "/home/x", true
			}
			return "", false
		},
		Secret: func(k string) (string, bool) {
			if k == "gh" {
				return "ghp_secretvalue123", true
			}
			return "", false
		},
		OnSecret: func(v string) { registered = append(registered, v) },
		Now:      fixedNow,
	}
	cases := map[string]string{
		"{{who}}":                   "file",                     // file var beats env
		"{{host}}":                  "http://localhost:8080",    // nearest folder wins
		"{{base}}":                  "http://localhost:8080/v2", // nested + private $shared beats public $shared
		"{{shared_only}}":           "s",
		"{{port}}":                  "8080",
		"{{flag}}":                  "true",
		"{{token}}":                 "dev-secret-token-1",
		"{{$dotenv DB_USER}}":       "ada",
		"{{$dotenv DB_PASS}}":       "pw with spaces",
		"{{$processEnv HOME_X}}":    "/home/x",
		"{{$secret gh}}":            "ghp_secretvalue123",
		"{{ host }}":                "http://localhost:8080",
		"{{nope}}":                  "{{nope}}",
		"{{$timestamp}}":            strconv.FormatInt(fixedNow().Unix(), 10),
		"{{$timestamp -1 d}}":       strconv.FormatInt(fixedNow().Add(-24*time.Hour).Unix(), 10),
		"{{$timestamp 2 h}}":        strconv.FormatInt(fixedNow().Add(2*time.Hour).Unix(), 10),
		"{{$datetime iso8601}}":     "2026-10-16T09:12:33Z",
		"{{$datetime rfc1123}}":     "Fri, 16 Oct 2026 09:12:33 GMT",
		"{{$datetime iso8601 1 d}}": "2026-10-17T09:12:33Z",
	}
	for in, want := range cases {
		got, _ := rv.Resolve(Request{Method: "GET", URL: in})
		if got.URL != want {
			t.Errorf("%s → %q, want %q", in, got.URL, want)
		}
	}
	joined := strings.Join(registered, ",")
	if !strings.Contains(joined, "dev-secret-token-1") || !strings.Contains(joined, "ghp_secretvalue123") {
		t.Fatalf("private and $secret values must be registered, got %v", registered)
	}
	if strings.Contains(joined, "localhost") || strings.Contains(joined, "/home/x") {
		t.Fatalf("public values must not be registered: %v", registered)
	}
	if !envs.IsPrivate("dev", "token") || envs.IsPrivate("dev", "host") {
		t.Fatal("IsPrivate wrong")
	}
	// staging: no private file entry, nearest dir has no staging → root.
	rv.Env = "staging"
	if got, _ := rv.Resolve(Request{URL: "{{host}}"}); got.URL != "https://staging.example" {
		t.Fatalf("staging host = %q", got.URL)
	}
	// no environment: only $shared.
	rv.Env = ""
	if got, _ := rv.Resolve(Request{URL: "{{version}}|{{host}}"}); got.URL != "v2|{{host}}" {
		t.Fatalf("no env = %q", got.URL)
	}
}

func TestResolveDynamicOncePerSend(t *testing.T) {
	rv := &Resolver{Now: fixedNow}
	body := `{"a":"{{$guid}}","b":"{{$guid}}","c":"{{$uuid}}","n":{{$randomInt 5 6}}}`
	got, un := rv.Resolve(Request{URL: "http://x/{{$guid}}", Body: &body})
	if len(un) != 0 {
		t.Fatalf("unresolved: %v", un)
	}
	re := regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}`)
	ids := re.FindAllString(*got.Body, -1)
	if len(ids) != 3 || ids[0] != ids[1] {
		t.Fatalf("guid twice must be equal: %v (%s)", ids, *got.Body)
	}
	if !strings.HasSuffix(got.URL, ids[0]) {
		t.Fatalf("same $guid in URL and body: %s vs %s", got.URL, ids[0])
	}
	if !strings.Contains(*got.Body, `"n":5`) {
		t.Fatalf("randomInt [5,6) must be 5: %s", *got.Body)
	}
	got2, _ := rv.Resolve(Request{URL: "{{$guid}}"})
	if got2.URL == ids[0] {
		t.Fatal("a new send gets a new $guid")
	}
}

func TestResolveUnresolvedPositions(t *testing.T) {
	rv := &Resolver{FileVars: []Var{{Name: "a", Value: "{{b}}"}}}
	body := "x {{c}}"
	_, un := rv.Resolve(Request{
		URL:     "http://h/{{a}}/{{missing}}",
		Headers: []Header{{Name: "Authorization", Value: "Bearer {{token}}"}},
		Body:    &body,
	})
	want := []Unresolved{
		{Name: "b", Field: "url", Offset: 9},
		{Name: "missing", Field: "url", Offset: 15},
		{Name: "token", Field: "header:Authorization", Offset: 7},
		{Name: "c", Field: "body", Offset: 2},
	}
	if !reflect.DeepEqual(un, want) {
		t.Fatalf("unresolved\n got  %+v\n want %+v", un, want)
	}
	if !rv.Known("a") || rv.Known("missing") || !rv.Known("$guid") || rv.Known("$dotenv NOPE") {
		t.Fatal("Known wrong")
	}
}

func TestResolveNestingStopsAtFive(t *testing.T) {
	rv := &Resolver{FileVars: []Var{{Name: "loop", Value: "x{{loop}}"}}}
	got, un := rv.Resolve(Request{URL: "{{loop}}"})
	if got.URL != "xxxxx{{loop}}" {
		t.Fatalf("got %q", got.URL)
	}
	if len(un) != 1 || un[0].Name != "loop" {
		t.Fatalf("a cycle is reported as unresolved: %+v", un)
	}
}

func TestLoadEnvsMalformedKeepsOtherFile(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "http-client.env.json"), `{"dev": {"host": "h"}`)
	writeFile(t, filepath.Join(root, "http-client.private.env.json"), `{"dev": {"token": "tok-12345678"}}`)
	envs := LoadEnvs(root, root)
	if len(envs.Errs) != 1 || !strings.Contains(envs.Errs[0].Error(), "http-client.env.json") {
		t.Fatalf("want one error naming the file, got %v", envs.Errs)
	}
	rv := &Resolver{Env: "dev", Envs: envs}
	if got, _ := rv.Resolve(Request{URL: "{{token}}"}); got.URL != "tok-12345678" {
		t.Fatalf("the private file still loads: %q", got.URL)
	}
	if len(envs.Files) != 2 {
		t.Fatalf("files = %v", envs.Files)
	}
}

func TestLoadEnvsStopsAtRoot(t *testing.T) {
	outer := t.TempDir()
	root := filepath.Join(outer, "ws")
	writeFile(t, filepath.Join(outer, "http-client.env.json"), `{"dev": {"host": "outside"}}`)
	writeFile(t, filepath.Join(root, "a", "b", "x.http"), "GET {{host}}\n")
	envs := LoadEnvs(filepath.Join(root, "a", "b"), root)
	if len(envs.Names()) != 0 {
		t.Fatalf("env files above the workspace root must not load: %v", envs.Names())
	}
}

func TestStatePerWorkspace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "http")
	path := filepath.Join(dir, "state.json")
	var s State
	s.SetEnv("/ws/a", "dev")
	s.SetEnv("/ws/b", "staging")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil || got.Env("/ws/a") != "dev" || got.Env("/ws/b") != "staging" || got.Env("/ws/c") != "" {
		t.Fatalf("state = %+v err %v", got, err)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("state mode %v", st.Mode())
	}
	dst, _ := os.Stat(dir)
	if dst.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", dst.Mode())
	}
	got.SetEnv("/ws/a", "")
	if got.Env("/ws/a") != "" {
		t.Fatal("clearing failed")
	}
}

func BenchmarkResolve(b *testing.B) {
	rv := &Resolver{FileVars: []Var{{Name: "host", Value: "http://localhost:8080"}, {Name: "token", Value: "abc"}}, Now: fixedNow}
	body := `{"name": "{{$guid}}", "x": "{{host}}"}`
	req := Request{Method: "POST", URL: "{{host}}/users/{{$randomInt 1 100}}", Headers: []Header{{Name: "Authorization", Value: "Bearer {{token}}"}, {Name: "Accept", Value: "application/json"}}, Body: &body}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		rv.Resolve(req)
	}
}

// PERF-8: the undercurl check (Known, once per placeholder on every
// buffer refresh) neither runs a regexp nor makes a UUID.
func TestKnownAllocs(t *testing.T) {
	rv := &Resolver{FileVars: []Var{{Name: "host", Value: "h"}}, Env: "dev", Envs: &Envs{public: map[string]map[string]string{"dev": {"token": "t"}}}}
	for _, expr := range []string{"host", "token", "$guid", "$uuid", "$isoTimestamp", "nope"} {
		if n := testing.AllocsPerRun(20, func() { rv.Known(expr) }); n != 0 {
			t.Errorf("Known(%q): %.0f allocs", expr, n)
		}
	}
}

func TestIsPlainNameMatchesRegexp(t *testing.T) {
	re := regexp.MustCompile(`^[\w.-]+$`)
	for _, s := range []string{"", "a", "host", "a.b-c_D9", "a b", "a{", "é", "$guid", "x/y", "-", ".", "_", "a\n"} {
		if got, want := isPlainName(s), re.MatchString(s); got != want {
			t.Errorf("%q: %v, regexp says %v", s, got, want)
		}
	}
}

// Known agrees with evaluating the dynamic function.
func TestKnownDynamicMatchesValue(t *testing.T) {
	rv := &Resolver{}
	now := time.Now()
	for _, expr := range []string{"$guid", "$uuid", "$random.uuid", "$timestamp", "$timestamp -1 d", "$timestamp x", "$timestamp 1 q",
		"$isoTimestamp", "$datetime iso8601", "$datetime rfc1123 1 h", "$datetime", "$datetime bad", "$localDatetime iso8601",
		"$randomInt 1 10", "$randomInt 10 1", "$randomInt", "$nope"} {
		fn, arg, _ := strings.Cut(expr, " ")
		_, want := dynamicValue(fn, strings.TrimSpace(arg), now)
		if got := rv.Known(expr); got != want {
			t.Errorf("Known(%q) = %v, evaluating says %v", expr, got, want)
		}
	}
}
