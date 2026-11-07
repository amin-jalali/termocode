package containers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// CTX-35: catalog browse over the Registry HTTP API v2 (`GET
// /v2/_catalog`), with the same anonymous token exchange as Tags.

func catalogJSON(repos []string) string {
	b, _ := json.Marshal(map[string]any{"repositories": repos})
	return string(b)
}

func TestRegistryCatalogPagesAndToken(t *testing.T) {
	var scope string
	auth := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope = r.URL.Query().Get("scope")
		fmt.Fprintf(w, `{"token":%q}`, testToken)
	}))
	defer auth.Close()
	var reg *httptest.Server
	reg = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/_catalog" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+auth.URL+`/token",service="registry.test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("last") == "" {
			w.Header().Set("Link", `</v2/_catalog?n=100&last=acme%2Fapi>; rel="next"`)
			fmt.Fprint(w, catalogJSON([]string{"acme/api", "BAD NAME"}))
			return
		}
		fmt.Fprint(w, catalogJSON([]string{"acme/web", "library/nginx"}))
	}))
	defer reg.Close()
	pool := certPool(reg)
	pool.AddCert(auth.Certificate())
	u, _ := url.Parse(reg.URL)
	got, err := (&RegistryAPI{RootCAs: pool}).Catalog(context.Background(), u.Host)
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	if strings.Join(got.Repos, ",") != "acme/api,acme/web,library/nginx" || got.Truncated || got.Host != u.Host {
		t.Fatalf("catalog = %+v", got)
	}
	if scope != "registry:catalog:*" {
		t.Fatalf("token scope = %q", scope)
	}
}

func TestRegistryCatalogCapAndErrors(t *testing.T) {
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/_catalog":
			fmt.Fprint(w, catalogJSON([]string{"a", "b", "c"}))
		default:
			http.NotFound(w, r)
		}
	}))
	defer reg.Close()
	u, _ := url.Parse(reg.URL)
	got, err := (&RegistryAPI{RootCAs: certPool(reg), MaxTags: 2}).Catalog(context.Background(), u.Host)
	if err != nil || strings.Join(got.Repos, ",") != "a,b" || !got.Truncated {
		t.Fatalf("capped = %+v %v", got, err)
	}
	if _, err := (&RegistryAPI{}).Catalog(context.Background(), "bad host/x"); err == nil {
		t.Fatal("bad host accepted")
	}
	// Docker Hub has no catalog: a clear error, no network call.
	if _, err := (&RegistryAPI{}).Catalog(context.Background(), "docker.io"); !errors.Is(err, ErrNoCatalog) {
		t.Fatalf("docker.io err = %v", err)
	}
	denied := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="x"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer denied.Close()
	du, _ := url.Parse(denied.URL)
	if _, err := (&RegistryAPI{RootCAs: certPool(denied)}).Catalog(context.Background(), du.Host); !errors.Is(err, ErrNeedsSignIn) {
		t.Fatalf("denied err = %v", err)
	}
}
