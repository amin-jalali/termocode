package dbclient

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// IntrospectLevel says how much schema detail is loaded.
type IntrospectLevel int

const (
	// LevelNames loads object names only.
	LevelNames IntrospectLevel = iota
	// LevelObjects adds columns.
	LevelObjects
	// LevelFull adds indexes, foreign keys and DDL.
	LevelFull
)

var levelNames = [...]string{"names", "objects", "full"}

func (l IntrospectLevel) String() string {
	if l >= 0 && int(l) < len(levelNames) {
		return levelNames[l]
	}
	return "full"
}

// ParseIntrospectLevel reads a level name ("names", "objects", "full").
func ParseIntrospectLevel(s string) (IntrospectLevel, bool) {
	for i, n := range levelNames {
		if strings.EqualFold(strings.TrimSpace(s), n) {
			return IntrospectLevel(i), true
		}
	}
	return LevelFull, false
}

// AutoIntrospectLevel picks a level from the number of objects: big schemas
// load less detail so the tree stays fast.
func AutoIntrospectLevel(objects int) IntrospectLevel {
	switch {
	case objects > 5000:
		return LevelNames
	case objects > 500:
		return LevelObjects
	}
	return LevelFull
}

// Trim drops the detail this level does not keep.
func (l IntrospectLevel) Trim(r IntrospectionResult) IntrospectionResult {
	if l < LevelFull {
		r.Indexes, r.ForeignKeys, r.DDL = nil, nil, ""
	}
	if l < LevelObjects {
		r.Columns = nil
	}
	return r
}

// SchemaKey names one cached introspection: a whole schema (Object empty)
// or one object.
type SchemaKey struct {
	Conn     string `json:"conn"`
	Database string `json:"database,omitempty"`
	Schema   string `json:"schema,omitempty"`
	Object   string `json:"object,omitempty"`
}

// SchemaCacheEntry is one cached introspection result.
type SchemaCacheEntry struct {
	Key         SchemaKey           `json:"key"`
	Level       IntrospectLevel     `json:"level"`
	Result      IntrospectionResult `json:"result"`
	Fingerprint string              `json:"fingerprint"`
	RefreshedAt time.Time           `json:"refreshed_at"`
}

// SchemaCache keeps introspection results in a file so the tree and
// completion work offline. It stores names and types only, never data or
// credentials.
type SchemaCache struct {
	Path string
	Now  func() time.Time
	mu   sync.Mutex
}

func (c *SchemaCache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// Put stores (or refreshes) one key; other keys stay as they are.
func (c *SchemaCache) Put(k SchemaKey, l IntrospectLevel, r IntrospectionResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, err := c.read()
	if err != nil {
		return err
	}
	r = l.Trim(r)
	e := SchemaCacheEntry{Key: k, Level: l, Result: r, Fingerprint: Fingerprint(r), RefreshedAt: c.now().UTC()}
	for i := range a {
		if a[i].Key == k {
			a[i] = e
			return c.write(a)
		}
	}
	return c.write(append(a, e))
}

// Get returns the cached entry for k.
func (c *SchemaCache) Get(k SchemaKey) (SchemaCacheEntry, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, err := c.read()
	if err != nil {
		return SchemaCacheEntry{}, false, err
	}
	for _, e := range a {
		if e.Key == k {
			return e, true, nil
		}
	}
	return SchemaCacheEntry{}, false, nil
}

// LastRefresh is the newest refresh time of any entry of a connection.
func (c *SchemaCache) LastRefresh(conn string) (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, _ := c.read()
	var t time.Time
	ok := false
	for _, e := range a {
		if e.Key.Conn == conn && (!ok || e.RefreshedAt.After(t)) {
			t, ok = e.RefreshedAt, true
		}
	}
	return t, ok
}

// Outdated reports whether the cached entry is missing or its fingerprint
// differs from a fresh one (for example from a cheap names-level read).
func (c *SchemaCache) Outdated(k SchemaKey, fingerprint string) bool {
	e, ok, err := c.Get(k)
	return err != nil || !ok || e.Fingerprint != fingerprint
}

// Forget drops every entry of a connection.
func (c *SchemaCache) Forget(conn string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, err := c.read()
	if err != nil {
		return err
	}
	o := a[:0]
	for _, e := range a {
		if e.Key.Conn != conn {
			o = append(o, e)
		}
	}
	return c.write(o)
}

// read returns the entries; a missing or broken file is an empty cache.
func (c *SchemaCache) read() ([]SchemaCacheEntry, error) {
	b, err := os.ReadFile(c.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var a []SchemaCacheEntry
	if json.Unmarshal(b, &a) != nil {
		return nil, nil
	}
	return a, nil
}

func (c *SchemaCache) write(a []SchemaCacheEntry) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0700); err != nil {
		return err
	}
	tmp := c.Path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, c.Path)
}

// Fingerprint is a stable hash of a result; the order of lists does not
// matter.
func Fingerprint(r IntrospectionResult) string {
	var parts []string
	add := func(p string, v ...string) {
		for _, s := range v {
			parts = append(parts, p+"\x00"+s)
		}
	}
	add("db", r.Databases...)
	add("schema", r.Schemas...)
	for _, o := range r.Objects {
		add("obj", o.Kind+"\x00"+o.Name)
	}
	for _, col := range r.Columns {
		pk := ""
		if col.PK {
			pk = "pk"
		}
		add("col", col.Table+"\x00"+col.Name+"\x00"+col.Type+"\x00"+pk)
	}
	for _, i := range r.Indexes {
		u := ""
		if i.Unique {
			u = "u"
		}
		add("idx", i.Name+"\x00"+strings.Join(i.Columns, ",")+"\x00"+u)
	}
	for _, f := range r.ForeignKeys {
		add("fk", f.Name+"\x00"+f.Column+"\x00"+f.RefTable+"\x00"+f.RefColumn)
	}
	add("ddl", r.DDL)
	add("key", r.Keys...)
	add("routine", r.Routines...)
	add("seq", r.Sequences...)
	sort.Strings(parts)
	h := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return hex.EncodeToString(h[:])
}

var systemCatalogs = map[string][]string{
	"postgres": {
		"pg_catalog.pg_class", "pg_catalog.pg_namespace", "pg_catalog.pg_attribute",
		"pg_catalog.pg_index", "pg_catalog.pg_constraint", "pg_catalog.pg_proc",
		"pg_catalog.pg_type", "pg_catalog.pg_roles", "pg_catalog.pg_database",
		"pg_catalog.pg_tables", "pg_catalog.pg_views", "pg_catalog.pg_indexes",
		"pg_catalog.pg_settings", "pg_catalog.pg_stat_activity", "pg_catalog.pg_locks",
		"pg_catalog.pg_stat_user_tables", "pg_catalog.pg_extension",
		"information_schema.tables", "information_schema.columns", "information_schema.views",
		"information_schema.routines", "information_schema.schemata",
		"information_schema.table_constraints", "information_schema.key_column_usage",
	},
	"mysql": {
		"information_schema.tables", "information_schema.columns", "information_schema.views",
		"information_schema.routines", "information_schema.schemata", "information_schema.statistics",
		"information_schema.table_constraints", "information_schema.key_column_usage",
		"information_schema.processlist", "information_schema.triggers",
		"performance_schema.threads", "performance_schema.events_statements_current",
		"mysql.user", "mysql.db",
	},
	"sqlite": {"sqlite_master", "sqlite_schema", "sqlite_sequence", "sqlite_temp_master"},
}

// SystemCatalogObjects lists the system catalog tables of a driver, for
// completion. It returns a fresh copy, or nil for a non-SQL driver.
func SystemCatalogObjects(driver string) []SchemaObject {
	d := normDriver(driver)
	if d == "postgresql" {
		d = "postgres"
	}
	names := systemCatalogs[d]
	if names == nil {
		return nil
	}
	out := make([]SchemaObject, len(names))
	for i, n := range names {
		out[i] = SchemaObject{Name: n, Kind: "system"}
	}
	return out
}
