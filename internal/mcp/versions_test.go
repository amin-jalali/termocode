package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func catalogServer(name, version, status string) CatalogEntry {
	return CatalogEntry{Name: name, Version: version, Status: status, Publisher: publisherOf(name),
		Packages: []CatalogPackage{{RegistryType: "npm", Identifier: "@acme/notes-mcp", Version: version}}}
}

func loadUser(t *testing.T, path string) []Server {
	t.Helper()
	list, _ := Load(path, "")
	return list
}

func TestFindUpdatesAndDeprecated(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "mcp.json")
	_, entry, _ := catalogServer("io.github.acme/notes", "1.2.0", "").ServerEntry()
	if err := AddServer(user, "notes", entry); err != nil {
		t.Fatal(err)
	}
	_ = AddServer(user, "hand", map[string]any{"command": "my-server"})
	servers := loadUser(t, user)
	var notes Server
	for _, s := range servers {
		if s.Name == "notes" {
			notes = s
		}
	}
	if ref, ok := RegistryOf(notes); !ok || ref.Name != "io.github.acme/notes" || ref.Version != "1.2.0" {
		var names []string
		for _, s := range servers {
			names = append(names, s.Name)
		}
		t.Fatalf("ref %+v ok=%v (%v)", ref, ok, names)
	}
	cat := []CatalogEntry{catalogServer("io.github.acme/notes", "1.3.0", "deprecated")}
	ups := FindUpdates(servers, cat)
	u, ok := ups["notes"]
	if !ok || u.Current != "1.2.0" || u.Latest != "1.3.0" || !u.Deprecated || !u.Newer() {
		t.Fatalf("updates %+v", ups)
	}
	if _, ok := ups["hand"]; ok {
		t.Fatal("a hand entry got an update")
	}
	same := FindUpdates(servers, []CatalogEntry{catalogServer("io.github.acme/notes", "1.2.0", "")})
	if u := same["notes"]; u.Newer() {
		t.Fatal("the same version is not newer")
	}
	older := FindUpdates(servers, []CatalogEntry{catalogServer("io.github.acme/notes", "1.10.0", "")})
	if u := older["notes"]; !u.Newer() {
		t.Fatal("1.10.0 is newer than 1.2.0")
	}
}

func TestApplyUpdateKeepsUserChanges(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "mcp.json")
	_, entry, _ := catalogServer("io.github.acme/notes", "1.2.0", "").ServerEntry()
	entry["env"] = map[string]any{"NOTES_REGION": "us"}
	entry["autoApprove"] = []any{"find_*"}
	_ = AddServer(user, "notes", entry)
	if err := ApplyUpdate(user, "notes", catalogServer("io.github.acme/notes", "1.3.0", "")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(user)
	s := string(b)
	for _, want := range []string{"@acme/notes-mcp@1.3.0", `"NOTES_REGION": "us"`, `"find_*"`, `"version": "1.3.0"`} {
		if !strings.Contains(s, want) {
			t.Errorf("after update lacks %s:\n%s", want, s)
		}
	}
	if strings.Contains(s, "1.2.0") {
		t.Errorf("old version kept:\n%s", s)
	}
}

func TestCatalogFetchSinceMergesIntoCache(t *testing.T) {
	var since []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		since = append(since, r.URL.Query().Get("updated_since"))
		v := "1.2.0"
		if r.URL.Query().Get("updated_since") != "" {
			v = "1.3.0"
		}
		body, _ := json.Marshal(map[string]any{"servers": []any{map[string]any{"server": map[string]any{
			"name": "io.github.acme/notes", "version": v}}}})
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	c := Catalog{URL: srv.URL, CacheDir: t.TempDir(), HTTP: srv.Client()}
	if _, err := c.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, at, _ := c.Cached()
	changed, err := c.FetchSince(context.Background(), at)
	if err != nil || len(changed) != 1 || changed[0].Version != "1.3.0" {
		t.Fatalf("changed %+v %v", changed, err)
	}
	if since[1] == "" {
		t.Fatal("updated_since was not sent")
	}
	if _, err := time.Parse(time.RFC3339, since[1]); err != nil {
		t.Fatalf("updated_since %q", since[1])
	}
	list, at2, _ := c.Cached()
	if len(list) != 1 || list[0].Version != "1.3.0" || !at2.After(at.Add(-time.Second)) {
		t.Fatalf("cache %+v", list)
	}
}

func TestLoadSettingsAutoUpdate(t *testing.T) {
	p := t.TempDir() + "/config.json"
	if s := LoadSettings(p); s.AutoUpdate != AutoUpdateOff {
		t.Fatalf("default %q", s.AutoUpdate)
	}
	_ = SaveSetting(p, "mcp_auto_update", "notify")
	if s := LoadSettings(p); s.AutoUpdate != AutoUpdateNotify {
		t.Fatalf("saved %q", s.AutoUpdate)
	}
}
