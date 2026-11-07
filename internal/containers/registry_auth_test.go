package containers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// CTX-35: private tag lists with the user's saved login (opt-in), the
// manifest digest of a tag, and deleting a manifest.

const testDigest = "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func privateRegistry(t *testing.T) (*httptest.Server, *httptest.Server, *[]string) {
	var calls []string
	auth := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "amin" || p != "s3cret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		calls = append(calls, "token "+r.URL.Query().Get("scope"))
		fmt.Fprintf(w, `{"token":%q}`, testToken)
	}))
	reg := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+testToken {
			w.Header().Set("WWW-Authenticate", `Bearer realm="`+auth.URL+`/token",service="reg"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/v2/acme/api/tags/list":
			fmt.Fprint(w, `{"tags":["1.0","2.0"]}`)
		case r.Method == http.MethodHead && r.URL.Path == "/v2/acme/api/manifests/1.0":
			if !strings.Contains(r.Header.Get("Accept"), "application/vnd.oci.image.index.v1+json") {
				w.WriteHeader(http.StatusNotAcceptable)
				return
			}
			w.Header().Set("Docker-Content-Digest", testDigest)
		case r.Method == http.MethodDelete && r.URL.Path == "/v2/acme/api/manifests/"+testDigest:
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() { auth.Close(); reg.Close() })
	return reg, auth, &calls
}

func TestRegistryTagsWithSavedLogin(t *testing.T) {
	reg, auth, _ := privateRegistry(t)
	pool := certPool(reg)
	pool.AddCert(auth.Certificate())
	u, _ := url.Parse(reg.URL)
	repo := RemoteRepo{Host: u.Host, APIHost: u.Host, Name: "acme/api"}
	if _, err := (&RegistryAPI{RootCAs: pool}).Tags(context.Background(), repo); err != ErrNeedsSignIn {
		t.Fatalf("anonymous: %v", err)
	}
	api := &RegistryAPI{RootCAs: pool, Login: &RegistryLogin{User: "amin", Secret: "s3cret"}}
	got, err := api.Tags(context.Background(), repo)
	if err != nil || strings.Join(got.Tags, ",") != "1.0,2.0" {
		t.Fatalf("with login: %+v %v", got, err)
	}
}

func TestRegistryManifestDigestAndDelete(t *testing.T) {
	reg, auth, calls := privateRegistry(t)
	pool := certPool(reg)
	pool.AddCert(auth.Certificate())
	u, _ := url.Parse(reg.URL)
	repo := RemoteRepo{Host: u.Host, APIHost: u.Host, Name: "acme/api"}
	api := &RegistryAPI{RootCAs: pool, Login: &RegistryLogin{User: "amin", Secret: "s3cret"}}
	d, err := api.ManifestDigest(context.Background(), repo, "1.0")
	if err != nil || d != testDigest {
		t.Fatalf("digest %q %v", d, err)
	}
	if err := api.DeleteManifest(context.Background(), repo, d); err != nil {
		t.Fatalf("delete: %v", err)
	}
	joined := strings.Join(*calls, "|")
	if !strings.Contains(joined, "token repository:acme/api:pull,delete") || !strings.Contains(joined, "DELETE /v2/acme/api/manifests/"+testDigest) {
		t.Fatalf("calls %q", joined)
	}
	if err := api.DeleteManifest(context.Background(), repo, "1.0"); err == nil {
		t.Fatal("delete must take a digest, never a tag")
	}
	if err := (&RegistryAPI{RootCAs: pool}).DeleteManifest(context.Background(), repo, d); err == nil {
		t.Fatal("delete needs a login")
	}
}

func TestCloudRegistryLoginHint(t *testing.T) {
	for host, want := range map[string]string{
		"123456789012.dkr.ecr.eu-west-1.amazonaws.com": "aws ecr get-login-password --region eu-west-1",
		"acme.azurecr.io":             "az acr login --name acme",
		"europe-west4-docker.pkg.dev": "gcloud auth configure-docker europe-west4-docker.pkg.dev",
		"gcr.io":                      "gcloud auth configure-docker gcr.io",
	} {
		h, ok := CloudLoginHint(host)
		if !ok || !strings.Contains(h.Command, want) {
			t.Fatalf("%s: %+v", host, h)
		}
	}
	if h, ok := CloudLoginHint("ghcr.io"); !ok || h.Command != "" || !strings.Contains(h.Note, "read:packages") {
		t.Fatalf("ghcr: %+v", h)
	}
	if _, ok := CloudLoginHint("registry.example.com"); ok {
		t.Fatal("no hint for a plain host")
	}
}
