package devpreview

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in, want string
		port     int
		ok       bool
	}{
		{"http://localhost:5173", "http://localhost:5173/", 5173, true},
		{"http://LOCALHOST:3000/app/.", "http://localhost:3000/app/", 3000, true},
		{"http://0.0.0.0:8000/", "http://localhost:8000/", 8000, true},
		{"http://[::]:4000/x#top", "http://localhost:4000/x", 4000, true},
		{"http://[::1]:4000/", "http://[::1]:4000/", 4000, true},
		{"http://127.0.0.1:8080/api?q=1", "http://127.0.0.1:8080/api?q=1", 8080, true},
		{"https://localhost", "https://localhost/", 443, true},
		{"http://localhost", "http://localhost/", 80, true},
		{"http://192.168.1.5:5173/", "", 0, false},
		{"http://example.com/", "", 0, false},
		{"http://localhost:0/", "", 0, false},
		{"http://localhost:70000/", "", 0, false},
		{"ftp://localhost:21/", "", 0, false},
		{"http://user:pw@localhost:3000/", "", 0, false},
	}
	for _, c := range cases {
		got, port, ok := Normalize(c.in)
		if got != c.want || port != c.port || ok != c.ok {
			t.Errorf("Normalize(%q) = %q %d %v, want %q %d %v", c.in, got, port, ok, c.want, c.port, c.ok)
		}
	}
}

func TestFindURLsInDevServerOutput(t *testing.T) {
	out := "\x1b[32m  VITE v5.0.0  ready in 300 ms\x1b[0m\n" +
		"  ➜  Local:   \x1b[36mhttp://localhost:\x1b[1m5173\x1b[22m/\x1b[39m\n" +
		"  ➜  Network: http://192.168.1.5:5173/\n" +
		"Listening on http://0.0.0.0:8000.\n" +
		"see https://vitejs.dev/guide for help\n" +
		"(server at http://127.0.0.1:9229/json)\n" +
		"again: http://localhost:5173/\n"
	got := FindURLs(out)
	want := []string{"http://localhost:8000/", "http://127.0.0.1:9229/json", "http://localhost:5173/"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("FindURLs = %q, want %q (newest last, local only, once each)", got, want)
	}
}

func TestFindURLsOSCLink(t *testing.T) {
	out := "\x1b]8;;http://localhost:3000/\x07http://localhost:3000/\x1b]8;;\x07 ready"
	if got := FindURLs(out); !reflect.DeepEqual(got, []string{"http://localhost:3000/"}) {
		t.Errorf("FindURLs = %q", got)
	}
}

func TestMergeNewestFirst(t *testing.T) {
	in := []Target{
		{URL: "http://localhost:5432/", From: "service db"},
		{URL: "http://localhost:3000", From: "terminal tab 1 (web)"},
		{URL: "http://example.com/", From: "terminal tab 1 (web)"},
		{URL: "http://localhost:5432/", From: "terminal tab 2 (agent)"},
	}
	got := Merge(in)
	want := []Target{
		{URL: "http://localhost:5432/", Port: 5432, From: "terminal tab 2 (agent)"},
		{URL: "http://localhost:3000/", Port: 3000, From: "terminal tab 1 (web)"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Merge = %+v, want %+v", got, want)
	}
}

func TestFromPortAndText(t *testing.T) {
	if tg, ok := FromPort(8080, "service api"); !ok || tg.URL != "http://localhost:8080/" || tg.Port != 8080 {
		t.Errorf("FromPort = %+v %v", tg, ok)
	}
	if _, ok := FromPort(0, "x"); ok {
		t.Error("port 0 accepted")
	}
	got := FromText("up at http://localhost:4321", "terminal tab 3 (docs)")
	if len(got) != 1 || got[0].Port != 4321 || got[0].From != "terminal tab 3 (docs)" {
		t.Errorf("FromText = %+v", got)
	}
}

func TestPageTitle(t *testing.T) {
	if got := PageTitle([]byte("<html><head><TITLE lang=en>\n  My &amp; App  </TITLE>")); got != "My & App" {
		t.Errorf("title = %q", got)
	}
	if got := PageTitle([]byte("<p>no title</p>")); got != "" {
		t.Errorf("title = %q", got)
	}
	long := "<title>" + strings.Repeat("a", 100) + "</title>"
	if got := PageTitle([]byte(long)); len([]rune(got)) != 60 || !strings.HasSuffix(got, "…") {
		t.Errorf("long title = %q", got)
	}
}

func TestCheckHint(t *testing.T) {
	if h := (Check{Up: true, Status: 200, Title: "Shop"}).Hint(); h != "200 · Shop" {
		t.Errorf("hint = %q", h)
	}
	if h := (Check{Err: "refused"}).Hint(); h != "not answering (refused)" {
		t.Errorf("hint = %q", h)
	}
}

func TestProbeReadsStatusAndTitle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><title>Dev Shop</title>"))
	}))
	defer srv.Close()
	c := Probe(context.Background(), srv.URL)
	if !c.Up || c.Status != 200 || c.Title != "Dev Shop" {
		t.Errorf("Probe = %+v", c)
	}
}

func TestProbeRefusesNonLocal(t *testing.T) {
	c := Probe(context.Background(), "http://example.com/")
	if c.Up || c.Err == "" {
		t.Errorf("Probe of a public host = %+v, want refused without a request", c)
	}
}

func TestProbeDoesNotFollowRedirectAway(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://example.com/login", http.StatusFound)
	}))
	defer srv.Close()
	c := Probe(context.Background(), srv.URL)
	if c.Up || !strings.Contains(c.Err, "away") {
		t.Errorf("Probe = %+v, want the redirect away refused", c)
	}
}

func TestProbeClosedPort(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	if c := Probe(context.Background(), url); c.Up || c.Err == "" {
		t.Errorf("Probe of a closed port = %+v", c)
	}
}

func TestProbeAllKeepsOrder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()
	got := ProbeAll(context.Background(), []Target{{URL: "http://example.com/"}, {URL: srv.URL}})
	if len(got) != 2 || got[0].Up || !got[1].Up || got[1].Status != http.StatusTeapot {
		t.Errorf("ProbeAll = %+v", got)
	}
}
