package codeindex

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// writeTree writes files (slash paths → text) under a new temp folder.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, s := range files {
		abs := filepath.Join(root, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// listAll is a lister that returns the given paths (no git, no rg).
func listAll(paths ...string) func(string) ([]string, error) {
	return func(string) ([]string, error) { return paths, nil }
}

var sample = map[string]string{
	"auth/login.go": "package auth\n\n// CheckPassword compares the typed password with the stored hash.\n" +
		"func CheckPassword(user, password string) bool {\n\treturn hashOf(password) == storedHash(user)\n}\n",
	"store/remove.go": "package store\n\n// DeleteRow drops one row from the table.\nfunc DeleteRow(id int) error {\n\treturn db.Exec(\"DELETE FROM t WHERE id = ?\", id)\n}\n",
	"ui/render.go":    "package ui\n\n// DrawPanel paints the side panel.\nfunc DrawPanel(w int) string {\n\treturn strings.Repeat(\" \", w)\n}\n",
	".env":            "PASSWORD=hunter2\n",
	"logo.png":        "\x89PNG\x00\x00binary",
}

func newSample(t *testing.T) (*Index, string) {
	t.Helper()
	root := writeTree(t, sample)
	x := New(root, Options{
		List: listAll("auth/login.go", "store/remove.go", "ui/render.go", ".env", "logo.png"),
		Skip: func(rel string) bool { return filepath.Base(rel) == ".env" },
	})
	if _, err := x.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	return x, root
}

// Spec (CMP-69): identifiers split into lower-case words (camelCase,
// snake_case, digits kept), short words and stop words dropped, plural,
// -ing and final-e forms folded to one stem.
func TestTokens(t *testing.T) {
	got := Tokens("func parseHTTPRequest(user_names []string) // Loading the configs")
	want := []string{"pars", "http", "request", "user", "nam", "load", "config"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Tokens = %q, want %q", got, want)
	}
}

// Spec (CMP-69): a query finds the code by meaning, through related
// words: "sign in credentials" finds CheckPassword, "remove record"
// finds DeleteRow — no shared literal text needed.
func TestSearchByMeaning(t *testing.T) {
	x, _ := newSample(t)
	for q, want := range map[string]string{
		"sign in credentials": "auth/login.go",
		"remove a record":     "store/remove.go",
		"render the screen":   "ui/render.go",
	} {
		hits := x.Search(q, 3)
		if len(hits) == 0 || hits[0].Path != want {
			t.Errorf("Search(%q) = %+v, want %s first", q, hits, want)
		}
	}
}

// Spec (CMP-69): a hit names the line of the best match, with a preview
// of it, and scores fall from first to last.
func TestSearchHitLine(t *testing.T) {
	x, _ := newSample(t)
	hits := x.Search("check password", 5)
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	h := hits[0]
	if h.Path != "auth/login.go" || h.Line != 3 && h.Line != 4 || !strings.Contains(h.Preview, "Password") {
		t.Fatalf("hit = %+v", h)
	}
	for i := 1; i < len(hits); i++ {
		if hits[i].Score > hits[i-1].Score {
			t.Fatalf("scores not sorted: %+v", hits)
		}
	}
}

// Spec (CMP-69): secret files (the Skip rule) and binary files are never
// indexed, so their text can never come back in a hit.
func TestSkipsSecretsAndBinaries(t *testing.T) {
	x, _ := newSample(t)
	for _, h := range x.Search("password hunter2 png binary", 10) {
		if h.Path == ".env" || h.Path == "logo.png" {
			t.Fatalf("indexed %s: %+v", h.Path, h)
		}
	}
	if st := x.Stats(); st.Files != 3 {
		t.Fatalf("Files = %d, want 3", st.Files)
	}
}

// Spec (CMP-69): Update reads only changed files again; a deleted file
// leaves the index.
func TestUpdateIncremental(t *testing.T) {
	x, root := newSample(t)
	st, err := x.Update(context.Background())
	if err != nil || st.Reindexed != 0 {
		t.Fatalf("second Update reindexed %d (err %v), want 0", st.Reindexed, err)
	}
	p := filepath.Join(root, "ui", "render.go")
	if err := os.WriteFile(p, []byte("package ui\n\nfunc ScheduleCronJob() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(p, later, later)
	_ = os.Remove(filepath.Join(root, "store", "remove.go"))
	st, err = x.Update(context.Background())
	if err != nil || st.Reindexed != 1 || st.Files != 2 {
		t.Fatalf("Update = %+v (err %v), want 1 reindexed of 2", st, err)
	}
	if h := x.Search("schedule cron", 1); len(h) == 0 || h[0].Path != "ui/render.go" {
		t.Fatalf("changed file not found: %+v", h)
	}
	for _, h := range x.Search("delete row", 5) {
		if h.Path == "store/remove.go" {
			t.Fatal("deleted file still in the index")
		}
	}
}

// Spec (CMP-69): the index saves to a private cache file and loads back
// with the same results, so a restart does not read every file again.
func TestSaveLoad(t *testing.T) {
	x, root := newSample(t)
	path := filepath.Join(t.TempDir(), "sub", "idx.gob")
	if err := x.Save(path); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("cache file mode %v (err %v), want 0600", fi.Mode().Perm(), err)
	}
	y := New(root, Options{List: listAll("auth/login.go", "store/remove.go", "ui/render.go")})
	if err := y.Load(path); err != nil {
		t.Fatal(err)
	}
	st, err := y.Update(context.Background())
	if err != nil || st.Reindexed != 0 {
		t.Fatalf("Update after Load reindexed %d (err %v)", st.Reindexed, err)
	}
	a, b := x.Search("remove a record", 1), y.Search("remove a record", 1)
	if len(a) == 0 || len(b) == 0 || a[0].Path != b[0].Path {
		t.Fatalf("loaded index differs: %+v vs %+v", a, b)
	}
}

// Spec (CMP-69): a cache file of another folder is refused.
func TestLoadOtherRoot(t *testing.T) {
	x, _ := newSample(t)
	path := filepath.Join(t.TempDir(), "idx.gob")
	if err := x.Save(path); err != nil {
		t.Fatal(err)
	}
	if err := New(t.TempDir(), Options{}).Load(path); err == nil {
		t.Fatal("loaded a cache of another folder")
	}
}

// Spec (CMP-69): the cache lives under TERMOCODE_CODEINDEX_DIR when set,
// one file per workspace folder.
func TestCachePath(t *testing.T) {
	d := t.TempDir()
	t.Setenv(CacheDirEnv, d)
	a, b := CachePath("/w/one"), CachePath("/w/two")
	if filepath.Dir(a) != d || a == b || !strings.HasSuffix(a, ".gob") {
		t.Fatalf("CachePath = %q, %q", a, b)
	}
}

// Spec (CMP-69): an empty query or an empty index gives no hits, and a
// file cap is reported.
func TestEdges(t *testing.T) {
	x, _ := newSample(t)
	if h := x.Search("   ", 5); len(h) != 0 {
		t.Fatalf("empty query: %+v", h)
	}
	if h := New(t.TempDir(), Options{List: listAll()}).Search("x", 5); len(h) != 0 {
		t.Fatalf("empty index: %+v", h)
	}
	root := writeTree(t, sample)
	y := New(root, Options{MaxFiles: 1, List: listAll("auth/login.go", "store/remove.go")})
	st, err := y.Update(context.Background())
	if err != nil || st.Files != 1 || !st.Capped {
		t.Fatalf("capped Update = %+v (err %v)", st, err)
	}
}

// Spec (CMP-69): a cancelled context stops Update with the error.
func TestUpdateCancel(t *testing.T) {
	root := writeTree(t, sample)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(root, Options{List: listAll("auth/login.go")}).Update(ctx); err == nil {
		t.Fatal("no error after cancel")
	}
}
