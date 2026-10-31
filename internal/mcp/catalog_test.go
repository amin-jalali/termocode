package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeRegistry serves the registry API: two pages of servers.
func fakeRegistry(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/servers" {
			http.NotFound(w, r)
			return
		}
		hits.Add(1)
		page1 := `{"servers":[
 {"server":{"name":"io.github.acme/notes","title":"Notes","description":"Read and write notes.","version":"1.2.0",
   "repository":{"url":"https://github.com/acme/notes"},
   "packages":[{"registryType":"npm","identifier":"@acme/notes-mcp","version":"1.2.0","transport":{"type":"stdio"},
     "environmentVariables":[{"name":"NOTES_TOKEN","isSecret":true,"isRequired":true},{"name":"NOTES_REGION","default":"eu"}]}]},
  "_meta":{"io.modelcontextprotocol.registry/official":{"status":"active","updatedAt":"2026-10-01T00:00:00Z","isLatest":true}}},
 {"server":{"name":"com.example/old","description":"Old one.","version":"0.1.0",
   "packages":[{"registryType":"pypi","identifier":"old-mcp","version":"0.1.0","transport":{"type":"stdio"}}]},
  "_meta":{"io.modelcontextprotocol.registry/official":{"status":"deprecated"}}}
],"metadata":{"nextCursor":"p2"}}`
		page2 := `{"servers":[
 {"server":{"name":"com.example/remote","description":"A hosted server.","version":"2.0.0",
   "remotes":[{"type":"streamable-http","url":"https://mcp.example.com/mcp","headers":[{"name":"Authorization","isSecret":true}]}]}}
],"metadata":{}}`
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("cursor") == "p2" {
			_, _ = w.Write([]byte(page2))
			return
		}
		_, _ = w.Write([]byte(page1))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCatalogFetchPagesCachesAndMarksOfficial(t *testing.T) {
	var hits atomic.Int32
	srv := fakeRegistry(t, &hits)
	c := Catalog{URL: srv.URL, CacheDir: t.TempDir(), HTTP: srv.Client()}
	if _, _, ok := c.Cached(); ok {
		t.Fatal("cache before any fetch")
	}
	list, err := c.Fetch(context.Background())
	if err != nil || len(list) != 3 || hits.Load() != 2 {
		t.Fatalf("fetch %d entries, %d hits, %v", len(list), hits.Load(), err)
	}
	notes := list[0]
	if notes.Name != "io.github.acme/notes" || notes.Publisher != "github.com/acme" || notes.Status != "active" {
		t.Fatalf("notes = %+v", notes)
	}
	if notes.Verified {
		t.Fatal("a custom catalog is not the official registry: no verified badge")
	}
	if list[1].Status != "deprecated" || !list[1].Deprecated() {
		t.Fatalf("old = %+v", list[1])
	}
	cached, at, ok := c.Cached()
	if !ok || len(cached) != 3 || at.IsZero() {
		t.Fatalf("cache %d %v %v", len(cached), at, ok)
	}
	if hits.Load() != 2 {
		t.Fatal("Cached went to the network")
	}
	off := Catalog{URL: OfficialRegistryURL}
	for i := range cached {
		cached[i].Verified = off.official()
	}
	if !cached[0].Verified {
		t.Fatal("the official registry gives the badge")
	}
}

func TestCatalogRefusesPlainHTTPAwayFromLoopback(t *testing.T) {
	c := Catalog{URL: "http://catalog.example.com"}
	if _, err := c.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("err = %v", err)
	}
}

func TestCatalogEntryToServerEntry(t *testing.T) {
	var hits atomic.Int32
	srv := fakeRegistry(t, &hits)
	list, err := (&Catalog{URL: srv.URL, CacheDir: t.TempDir(), HTTP: srv.Client()}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	name, entry, err := list[0].ServerEntry()
	if err != nil || name != "notes" {
		t.Fatalf("name %q err %v", name, err)
	}
	b, _ := json.Marshal(entry)
	for _, want := range []string{`"command":"npx"`, `"-y","@acme/notes-mcp@1.2.0"`, `"NOTES_TOKEN":"${secret:notes_notes_token}"`, `"NOTES_REGION":"eu"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("entry %s lacks %s", b, want)
		}
	}
	_, entry, _ = list[1].ServerEntry()
	if b, _ := json.Marshal(entry); !strings.Contains(string(b), `"command":"uvx"`) || !strings.Contains(string(b), `old-mcp==0.1.0`) {
		t.Errorf("pypi entry %s", b)
	}
	name, entry, _ = list[2].ServerEntry()
	b, _ = json.Marshal(entry)
	if name != "remote" || !strings.Contains(string(b), `"url":"https://mcp.example.com/mcp"`) || !strings.Contains(string(b), `"Authorization":"${secret:remote_authorization}"`) {
		t.Errorf("remote entry %s %s", name, b)
	}
	oci := CatalogEntry{Name: "com.example/box", Packages: []CatalogPackage{{RegistryType: "oci", Identifier: "example/box", Version: "3", Env: []CatalogVar{{Name: "BOX_KEY", Secret: true}}}}}
	if _, entry, _ := oci.ServerEntry(); entry != nil {
		if b, _ := json.Marshal(entry); !strings.Contains(string(b), `"args":["run","-i","--rm","-e","BOX_KEY","example/box:3"]`) {
			t.Errorf("oci entry %s", b)
		}
	}
	if got := list[0].Secrets(); len(got) != 1 || got[0] != "notes_notes_token" {
		t.Errorf("secrets = %v", got)
	}
	if _, _, err := (CatalogEntry{Name: "x/y"}).ServerEntry(); err == nil {
		t.Error("an entry with no package or remote was turned into a server")
	}
}

func TestCatalogFetchErrorKeepsCache(t *testing.T) {
	var hits atomic.Int32
	srv := fakeRegistry(t, &hits)
	dir := t.TempDir()
	c := Catalog{URL: srv.URL, CacheDir: dir, HTTP: srv.Client()}
	if _, err := c.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	if _, err := c.Fetch(context.Background()); err == nil {
		t.Fatal("a closed server did not fail")
	}
	if list, _, ok := c.Cached(); !ok || len(list) != 3 {
		t.Fatal("a failed fetch dropped the cache")
	}
}
