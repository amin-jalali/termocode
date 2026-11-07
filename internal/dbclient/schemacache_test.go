package dbclient

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAutoIntrospectLevel(t *testing.T) {
	cases := []struct {
		n    int
		want IntrospectLevel
	}{{0, LevelFull}, {500, LevelFull}, {501, LevelObjects}, {5000, LevelObjects}, {5001, LevelNames}}
	for _, c := range cases {
		if got := AutoIntrospectLevel(c.n); got != c.want {
			t.Fatalf("AutoIntrospectLevel(%d) = %v, want %v", c.n, got, c.want)
		}
	}
	if l, ok := ParseIntrospectLevel("objects"); !ok || l != LevelObjects || l.String() != "objects" {
		t.Fatalf("parse objects: %v %v", l, ok)
	}
	if _, ok := ParseIntrospectLevel("bogus"); ok {
		t.Fatal("bogus level parsed")
	}
}

func TestIntrospectLevelTrim(t *testing.T) {
	r := IntrospectionResult{
		Objects:     []SchemaObject{{Name: "t", Kind: "table"}},
		Columns:     []Column{{Name: "id"}},
		Indexes:     []SchemaIndex{{Name: "i"}},
		ForeignKeys: []SchemaForeignKey{{Name: "f"}},
		DDL:         "create table t()",
	}
	n := LevelNames.Trim(r)
	if len(n.Objects) != 1 || n.Columns != nil || n.Indexes != nil || n.ForeignKeys != nil || n.DDL != "" {
		t.Fatalf("names trim kept detail: %+v", n)
	}
	o := LevelObjects.Trim(r)
	if len(o.Columns) != 1 || o.Indexes != nil || o.ForeignKeys != nil || o.DDL != "" {
		t.Fatalf("objects trim: %+v", o)
	}
	f := LevelFull.Trim(r)
	if len(f.Indexes) != 1 || f.DDL == "" {
		t.Fatalf("full trim lost data: %+v", f)
	}
}

func TestSchemaCachePersistsOffline(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cache", "schema.json")
	now := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	c := &SchemaCache{Path: p, Now: func() time.Time { return now }}
	k := SchemaKey{Conn: "prod", Database: "app", Schema: "public"}
	r := IntrospectionResult{Objects: []SchemaObject{{Name: "users", Kind: "table"}}}
	if err := c.Put(k, LevelFull, r); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("cache file mode: %v %v", fi, err)
	}
	// A new cache on the same file works with no connection (offline).
	c2 := &SchemaCache{Path: p}
	e, ok, err := c2.Get(k)
	if err != nil || !ok {
		t.Fatalf("get offline: %v %v", ok, err)
	}
	if len(e.Result.Objects) != 1 || e.Result.Objects[0].Name != "users" || e.Level != LevelFull {
		t.Fatalf("entry: %+v", e)
	}
	if !e.RefreshedAt.Equal(now) {
		t.Fatalf("refreshed at %v", e.RefreshedAt)
	}
	if lr, ok := c2.LastRefresh("prod"); !ok || !lr.Equal(now) {
		t.Fatalf("last refresh %v %v", lr, ok)
	}
	if _, ok := c2.LastRefresh("other"); ok {
		t.Fatal("last refresh for unknown conn")
	}
}

func TestSchemaCacheRefreshOneObject(t *testing.T) {
	p := filepath.Join(t.TempDir(), "schema.json")
	t0 := time.Date(2026, 11, 7, 10, 0, 0, 0, time.UTC)
	now := t0
	c := &SchemaCache{Path: p, Now: func() time.Time { return now }}
	a := SchemaKey{Conn: "c", Schema: "s", Object: "a"}
	b := SchemaKey{Conn: "c", Schema: "s", Object: "b"}
	_ = c.Put(a, LevelFull, IntrospectionResult{Columns: []Column{{Name: "x"}}})
	_ = c.Put(b, LevelFull, IntrospectionResult{Columns: []Column{{Name: "y"}}})
	now = t0.Add(time.Hour)
	if err := c.Put(a, LevelFull, IntrospectionResult{Columns: []Column{{Name: "x2"}}}); err != nil {
		t.Fatal(err)
	}
	ea, _, _ := c.Get(a)
	eb, _, _ := c.Get(b)
	if ea.Result.Columns[0].Name != "x2" || !ea.RefreshedAt.Equal(now) {
		t.Fatalf("a not refreshed: %+v", ea)
	}
	if eb.Result.Columns[0].Name != "y" || !eb.RefreshedAt.Equal(t0) {
		t.Fatalf("b changed by refresh of a: %+v", eb)
	}
	if lr, _ := c.LastRefresh("c"); !lr.Equal(now) {
		t.Fatalf("last refresh %v", lr)
	}
	if err := c.Forget("c"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := c.Get(a); ok {
		t.Fatal("forget kept entries")
	}
}

func TestSchemaCacheOutdated(t *testing.T) {
	c := &SchemaCache{Path: filepath.Join(t.TempDir(), "s.json")}
	k := SchemaKey{Conn: "c", Schema: "s"}
	r := IntrospectionResult{Objects: []SchemaObject{{Name: "t", Kind: "table"}}}
	if !c.Outdated(k, Fingerprint(r)) {
		t.Fatal("missing entry must be outdated")
	}
	_ = c.Put(k, LevelObjects, r)
	if c.Outdated(k, Fingerprint(r)) {
		t.Fatal("same fingerprint reported outdated")
	}
	r2 := IntrospectionResult{Objects: []SchemaObject{{Name: "t", Kind: "table"}, {Name: "u", Kind: "table"}}}
	if !c.Outdated(k, Fingerprint(r2)) {
		t.Fatal("changed schema not outdated")
	}
	// Order of objects does not change the fingerprint.
	r3 := IntrospectionResult{Objects: []SchemaObject{{Name: "u", Kind: "table"}, {Name: "t", Kind: "table"}}}
	if Fingerprint(r2) != Fingerprint(r3) {
		t.Fatal("fingerprint depends on order")
	}
}

func TestSchemaCacheCorruptFileIsEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(p, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	c := &SchemaCache{Path: p}
	if _, ok, err := c.Get(SchemaKey{Conn: "c"}); ok || err != nil {
		t.Fatalf("corrupt cache: %v %v", ok, err)
	}
	if err := c.Put(SchemaKey{Conn: "c"}, LevelNames, IntrospectionResult{}); err != nil {
		t.Fatalf("put over corrupt: %v", err)
	}
}

func TestSystemCatalogObjects(t *testing.T) {
	has := func(a []SchemaObject, name string) bool {
		for _, o := range a {
			if o.Name == name {
				return true
			}
		}
		return false
	}
	if a := SystemCatalogObjects("postgres"); !has(a, "pg_catalog.pg_class") || !has(a, "information_schema.columns") {
		t.Fatalf("pg catalogs: %v", a)
	}
	if a := SystemCatalogObjects("mysql"); !has(a, "information_schema.tables") || !has(a, "performance_schema.threads") {
		t.Fatalf("mysql catalogs: %v", a)
	}
	if a := SystemCatalogObjects("sqlite"); !has(a, "sqlite_master") {
		t.Fatalf("sqlite catalogs: %v", a)
	}
	if a := SystemCatalogObjects("redis"); a != nil {
		t.Fatalf("redis catalogs: %v", a)
	}
	a := SystemCatalogObjects("postgres")
	a[0].Name = "changed"
	if SystemCatalogObjects("postgres")[0].Name == "changed" {
		t.Fatal("catalog list is shared, not copied")
	}
}
