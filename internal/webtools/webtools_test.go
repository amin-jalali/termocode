package webtools

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Spec: a page becomes its title and readable text; scripts and styles
// are gone; the text is capped; a loopback address is refused unless
// the test allows it; only GET; non-text pages and bad urls are refused.
func TestFetch(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			io.WriteString(w, `<html><head><title>Docs &amp; more</title><style>p{}</style></head><body><script>evil()</script>
<h1>Hello</h1><p>First &lt;para&gt;.</p><ul><li>one</li><li>two</li></ul><p>key sk-`+`test1234567890abcdefghijkl</p></body></html>`)
		case "/long":
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, strings.Repeat("a", 5000))
		case "/img":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte{0x89, 'P', 'N', 'G'})
		case "/go":
			http.Redirect(w, r, "/page", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	if _, err := Fetch(ctx, srv.URL+"/page", Options{}); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("loopback not refused: %v", err)
	}
	p, err := Fetch(ctx, srv.URL+"/go", Options{AllowPrivate: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Docs & more" || !strings.HasSuffix(p.URL, "/page") {
		t.Errorf("page %+v", p)
	}
	for _, want := range []string{"Hello", "First <para>.", "- one", "- two"} {
		if !strings.Contains(p.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, p.Text)
		}
	}
	if strings.Contains(p.Text, "evil") || strings.Contains(p.Text, "p{}") || strings.Contains(p.Text, "1234567890abcdef") {
		t.Errorf("text kept script, style or a secret:\n%s", p.Text)
	}
	p, err = Fetch(ctx, srv.URL+"/long", Options{AllowPrivate: true, MaxText: 100})
	if err != nil || len(p.Text) != 100 || !p.Cut {
		t.Errorf("cap: %d %v %v", len(p.Text), p.Cut, err)
	}
	p, err = Fetch(ctx, srv.URL+"/long", Options{AllowPrivate: true, MaxBytes: 10})
	if err != nil || len(p.Text) != 10 || !p.Cut {
		t.Errorf("byte cap: %d %v %v", len(p.Text), p.Cut, err)
	}
	if _, err := Fetch(ctx, srv.URL+"/img", Options{AllowPrivate: true}); err == nil || !strings.Contains(err.Error(), "image/png") {
		t.Errorf("image: %v", err)
	}
	if _, err := Fetch(ctx, srv.URL+"/nope", Options{AllowPrivate: true}); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("404: %v", err)
	}
	for _, bad := range []string{"file:///etc/passwd", "ftp://x", "http://", "https://u:p@example.com/"} {
		if _, err := Fetch(ctx, bad, Options{AllowPrivate: true}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, m := range methods {
		if m != http.MethodGet {
			t.Errorf("method %s", m)
		}
	}
}

// Spec: each provider gets the query and the key in its own header and
// its answer becomes title / url / snippet; a 401 says to check the key
// and never shows it.
func TestSearch(t *testing.T) {
	key := "sk-" + "testsearchkey123456"
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/brave":
			got = append(got, "brave:"+r.Header.Get("X-Subscription-Token")+":"+r.URL.Query().Get("q")+":"+r.URL.Query().Get("count"))
			io.WriteString(w, `{"web":{"results":[{"title":"Go","url":"https://go.dev","description":"The <strong>Go</strong> language"}]}}`)
		case "/tavily":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			got = append(got, "tavily:"+r.Header.Get("Authorization")+":"+b["query"].(string))
			io.WriteString(w, `{"results":[{"title":"T","url":"https://t.example","content":"tavily hit"}]}`)
		case "/exa":
			got = append(got, "exa:"+r.Header.Get("x-api-key"))
			io.WriteString(w, `{"results":[{"title":"E","url":"https://e.example","text":"exa hit"}]}`)
		default:
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, `{"error":"bad key `+key+`"}`)
		}
	}))
	defer srv.Close()
	c := Client(true)
	ctx := context.Background()
	for _, p := range Providers {
		p.Endpoint = srv.URL + "/" + p.Name
		rs, err := Search(ctx, c, p, key, "golang", 3)
		if err != nil || len(rs) != 1 || rs[0].URL == "" || rs[0].Snippet == "" {
			t.Errorf("%s: %+v %v", p.Name, rs, err)
		}
		if p.Name == "brave" && rs[0].Snippet != "The Go language" {
			t.Errorf("brave snippet %q", rs[0].Snippet)
		}
	}
	want := []string{"brave:" + key + ":golang:3", "tavily:Bearer " + key + ":golang", "exa:" + key}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("requests %v", got)
	}
	p := Providers[0]
	p.Endpoint = srv.URL + "/bad"
	_, err := Search(ctx, c, p, key, "x", 1)
	if err == nil || !strings.Contains(err.Error(), "check the key") || strings.Contains(err.Error(), key) {
		t.Errorf("401: %v", err)
	}
	if _, err := Search(ctx, c, p, "", "x", 1); err == nil {
		t.Error("no key accepted")
	}
}
