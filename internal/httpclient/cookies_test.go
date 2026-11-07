package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func cookieURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestCookieJarIsPerEnvironment(t *testing.T) {
	s := NewCookieStore("")
	u := cookieURL(t, "https://api.example.com/v1/login")
	s.Jar("dev").SetCookies(u, []*http.Cookie{{Name: "sid", Value: "d1"}})
	s.Jar("prod").SetCookies(u, []*http.Cookie{{Name: "sid", Value: "p1"}})

	if got := s.Jar("dev").Cookies(cookieURL(t, "https://api.example.com/v1/me")); len(got) != 1 || got[0].Value != "d1" {
		t.Fatalf("dev cookies = %v, want sid=d1", got)
	}
	if got := s.Jar("prod").Cookies(cookieURL(t, "https://api.example.com/v1/me")); len(got) != 1 || got[0].Value != "p1" {
		t.Fatalf("prod cookies = %v, want sid=p1", got)
	}
	if got := s.Jar("").Cookies(u); len(got) != 0 {
		t.Fatalf("no-env jar has %v, want none", got)
	}
}

func TestCookieJarMatchesDomainPathAndSecure(t *testing.T) {
	s := NewCookieStore("")
	j := s.Jar("dev")
	j.SetCookies(cookieURL(t, "https://www.example.com/app/login"), []*http.Cookie{
		{Name: "host", Value: "1"},                                   // host only, path /app
		{Name: "wide", Value: "2", Domain: "example.com", Path: "/"}, // all subdomains
		{Name: "sec", Value: "3", Path: "/", Secure: true},
		{Name: "evil", Value: "4", Domain: "other.com"}, // not our domain: dropped
	})
	names := func(u string) map[string]bool {
		out := map[string]bool{}
		for _, c := range j.Cookies(cookieURL(t, u)) {
			out[c.Name] = true
		}
		return out
	}
	if got := names("https://www.example.com/app/x"); !got["host"] || !got["wide"] || !got["sec"] || got["evil"] {
		t.Fatalf("same host = %v", got)
	}
	if got := names("https://api.example.com/app/x"); got["host"] || !got["wide"] {
		t.Fatalf("subdomain = %v, want only wide", got)
	}
	if got := names("http://www.example.com/other"); got["host"] || got["sec"] || !got["wide"] {
		t.Fatalf("plain http other path = %v, want only wide", got)
	}
	if got := s.List("dev"); len(got) != 3 {
		t.Fatalf("List = %v, want 3 cookies (evil dropped)", got)
	}
}

func TestCookieJarExpiredAndMaxAgeDelete(t *testing.T) {
	s := NewCookieStore("")
	j := s.Jar("dev")
	u := cookieURL(t, "https://example.com/")
	j.SetCookies(u, []*http.Cookie{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}})
	j.SetCookies(u, []*http.Cookie{{Name: "a", Value: "", MaxAge: -1}})
	j.SetCookies(u, []*http.Cookie{{Name: "old", Value: "x", Expires: time.Now().Add(-time.Hour)}})
	got := s.List("dev")
	if len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("List = %v, want only b", got)
	}
}

func TestCookieStoreEditAndDelete(t *testing.T) {
	s := NewCookieStore("")
	u := cookieURL(t, "https://example.com/")
	s.Jar("dev").SetCookies(u, []*http.Cookie{{Name: "sid", Value: "old"}, {Name: "theme", Value: "dark"}})
	c := s.List("dev")[0]
	if c.Name != "sid" {
		t.Fatalf("List not sorted by name: %v", s.List("dev"))
	}
	c.Value = "new"
	s.Set("dev", c)
	if got := s.Jar("dev").Cookies(u); len(got) != 2 || got[0].Value != "new" && got[1].Value != "new" {
		t.Fatalf("after edit = %v", got)
	}
	if !s.Delete("dev", c.Domain, c.Path, c.Name) {
		t.Fatal("Delete returned false")
	}
	if s.Delete("dev", c.Domain, c.Path, c.Name) {
		t.Fatal("second Delete returned true")
	}
	if got := s.List("dev"); len(got) != 1 || got[0].Name != "theme" {
		t.Fatalf("after delete = %v", got)
	}
	s.Clear("dev")
	if got := s.List("dev"); len(got) != 0 {
		t.Fatalf("after clear = %v", got)
	}
}

func TestCookieStorePersistsPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "http", "cookies.json")
	s := NewCookieStore(path)
	s.Jar("dev").SetCookies(cookieURL(t, "https://example.com/"), []*http.Cookie{
		{Name: "sid", Value: "v1", Expires: time.Now().Add(time.Hour)},
		{Name: "session", Value: "v2"},
	})
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("cookies not saved: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", st.Mode().Perm())
	}
	back, err := LoadCookieStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.List("dev"); len(got) != 2 {
		t.Fatalf("reloaded = %v, want 2", got)
	}
	back.Delete("dev", "example.com", "/", "sid")
	again, _ := LoadCookieStore(path)
	if got := again.List("dev"); len(got) != 1 || got[0].Name != "session" {
		t.Fatalf("delete not saved: %v", got)
	}
}

func TestLoadCookieStoreMissingOrBadFile(t *testing.T) {
	dir := t.TempDir()
	s, err := LoadCookieStore(filepath.Join(dir, "none.json"))
	if err != nil || s == nil || len(s.Envs()) != 0 {
		t.Fatalf("missing file: %v %v", s, err)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o600)
	s, err = LoadCookieStore(bad)
	if err == nil || s == nil {
		t.Fatalf("bad file: want an error and an empty store, got %v %v", s, err)
	}
}

func TestClientUsesCookieStoreOfTheEnvironment(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: r.URL.Query().Get("v"), Path: "/"})
			return
		}
		c, _ := r.Cookie("sid")
		if c != nil {
			seen = append(seen, c.Value)
		} else {
			seen = append(seen, "")
		}
		io.WriteString(w, "ok")
	}))
	defer srv.Close()
	store := NewCookieStore("")
	c := New(Options{CookieJar: true, Cookies: store})
	do := func(env, path string, dirs map[string]string) {
		r := Request{Method: "GET", URL: srv.URL + path, Directives: dirs}
		if resp := c.Send(context.Background(), Resolved{Request: r, Template: r, Env: env}, Hooks{}); resp.Err != "" {
			t.Fatal(resp.Err)
		}
	}
	do("dev", "/login?v=d", nil)
	do("prod", "/login?v=p", nil)
	do("dev", "/me", nil)
	do("prod", "/me", nil)
	do("", "/me", nil)
	do("dev", "/me", map[string]string{"no-cookie-jar": ""})
	want := []string{"d", "p", "", ""}
	if len(seen) != len(want) {
		t.Fatalf("seen = %q, want %q", seen, want)
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Fatalf("seen = %q, want %q", seen, want)
		}
	}
	if got := store.List("dev"); len(got) != 1 {
		t.Fatalf("store dev = %v", got)
	}
}
