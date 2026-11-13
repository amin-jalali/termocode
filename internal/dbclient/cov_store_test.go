package dbclient

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSavedQueriesFileFailures(t *testing.T) {
	dir := t.TempDir()
	s := &SavedQueries{Path: filepath.Join(dir, "q.json")}
	if _, err := s.Save(SavedQuery{Name: "a", SQL: strings.Repeat("x", SavedQueryMax+1)}); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("a long query = %v", err)
	}
	if _, err := s.Save(SavedQuery{Name: " ", SQL: "SELECT 1"}); err == nil {
		t.Error("an empty name: want error")
	}
	if _, err := s.Save(SavedQuery{Name: "n", SQL: " "}); err == nil {
		t.Error("an empty query: want error")
	}
	// A broken file is reported, and nothing is overwritten.
	if err := os.WriteFile(s.Path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(SavedQuery{Name: "n", SQL: "SELECT 1"}); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Errorf("save over a broken file = %v", err)
	}
	if _, err := s.List(""); err == nil {
		t.Error("list of a broken file: want error")
	}
	if err := s.Delete("x"); err == nil {
		t.Error("delete in a broken file: want error")
	}
	if b, _ := os.ReadFile(s.Path); string(b) != "{not json" {
		t.Errorf("the broken file must be left as it is: %q", b)
	}
	// The path is a folder: reading fails.
	folder := &SavedQueries{Path: dir}
	if _, err := folder.Save(SavedQuery{Name: "n", SQL: "SELECT 1"}); err == nil {
		t.Error("a folder as the file: want error")
	}
	// The parent cannot be made.
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	under := &SavedQueries{Path: filepath.Join(blocker, "sub", "q.json")}
	if _, err := under.Save(SavedQuery{Name: "n", SQL: "SELECT 1"}); err == nil {
		t.Error("a parent that is a file: want error")
	}
	if err := under.write(nil); err == nil {
		t.Error("write under a file: want error")
	}
	ok := &SavedQueries{Path: filepath.Join(dir, "new", "q.json")}
	if err := ok.write(nil); err != nil {
		t.Errorf("an empty list is written as []: %v", err)
	}
	if b, _ := os.ReadFile(ok.Path); !strings.Contains(string(b), "[]") {
		t.Errorf("empty list file = %q", b)
	}
	// List sorts by name then connection.
	for _, q := range []SavedQuery{{Name: "b", SQL: "1", Conn: "z"}, {Name: "B", SQL: "1", Conn: "a"}, {Name: "a", SQL: "1", Conn: "z"}} {
		if _, err := ok.Save(q); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ok.List("")
	if err != nil || len(got) != 3 || got[0].Name != "a" || got[1].Conn != "a" {
		t.Errorf("list = %+v, %v", got, err)
	}
}

func TestSchemaCacheFileFailuresAndFingerprint(t *testing.T) {
	dir := t.TempDir()
	c := &SchemaCache{Path: filepath.Join(dir, "c.json"), Now: func() time.Time { return time.Unix(100, 0) }}
	k := SchemaKey{Conn: "a"}
	res := IntrospectionResult{
		Columns:     []Column{{Name: "id", Type: "int", PK: true}, {Name: "n", Type: "text"}},
		Indexes:     []SchemaIndex{{Name: "i", Columns: []string{"n"}, Unique: true}, {Name: "j", Columns: []string{"id"}}},
		ForeignKeys: []SchemaForeignKey{{Name: "f", Column: "n", RefTable: "t", RefColumn: "id"}},
	}
	if err := c.Put(k, LevelFull, res); err != nil {
		t.Fatal(err)
	}
	if err := c.Put(k, LevelFull, res); err != nil {
		t.Fatal(err)
	}
	e, ok, err := c.Get(k)
	if err != nil || !ok || e.Fingerprint == "" || !e.RefreshedAt.Equal(time.Unix(100, 0).UTC()) {
		t.Errorf("get = %+v %v %v", e, ok, err)
	}
	flipped := res
	flipped.Columns = []Column{{Name: "id", Type: "int"}, {Name: "n", Type: "text"}}
	if Fingerprint(flipped) == Fingerprint(res) {
		t.Error("a primary key change changes the fingerprint")
	}
	flipped = res
	flipped.Indexes = []SchemaIndex{res.Indexes[1], {Name: "i", Columns: []string{"n"}}}
	if Fingerprint(flipped) == Fingerprint(res) {
		t.Error("a unique change changes the fingerprint")
	}
	if Fingerprint(IntrospectionResult{Indexes: []SchemaIndex{res.Indexes[1], res.Indexes[0]}}) != Fingerprint(IntrospectionResult{Indexes: res.Indexes}) {
		t.Error("the order of lists does not matter")
	}
	if !c.Outdated(k, "other") || c.Outdated(k, e.Fingerprint) {
		t.Error("Outdated")
	}
	if _, ok := c.LastRefresh("a"); !ok {
		t.Error("LastRefresh")
	}
	if err := c.Forget("a"); err != nil {
		t.Error(err)
	}
	// A broken file reads as an empty cache; a folder as the file is an error.
	if err := os.WriteFile(c.Path, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := c.Get(k); ok || err != nil {
		t.Errorf("broken file = %v %v", ok, err)
	}
	folder := &SchemaCache{Path: dir}
	if err := folder.Put(k, LevelFull, res); err == nil {
		t.Error("put over a folder: want error")
	}
	if _, _, err := folder.Get(k); err == nil {
		t.Error("get over a folder: want error")
	}
	if err := folder.Forget("a"); err == nil {
		t.Error("forget over a folder: want error")
	}
	if !folder.Outdated(k, "x") {
		t.Error("an unreadable cache is outdated")
	}
	blocker := filepath.Join(dir, "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	under := &SchemaCache{Path: filepath.Join(blocker, "x", "c.json")}
	if err := under.Put(k, LevelFull, res); err == nil {
		t.Error("a parent that is a file: want error")
	}
	if len(SystemCatalogObjects("postgresql")) == 0 || SystemCatalogObjects("redis") != nil {
		t.Error("SystemCatalogObjects")
	}
}

func TestExportToFileFailuresLeaveNothingBehind(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	rows := func(_ context.Context, sink RowSink) error {
		if err := sink.Columns([]Column{{Name: "a"}}); err != nil {
			return err
		}
		return sink.Row([]Cell{{Text: "1"}})
	}
	path := filepath.Join(dir, "out.csv")
	if _, err := ExportToFile(ctx, filepath.Join(dir, "missing", "x.csv"), FormatCSV, ExportOptions{}, false, rows, nil); err == nil {
		t.Error("a folder that is not there: want error")
	}
	if _, err := ExportToFile(ctx, dir, FormatCSV, ExportOptions{}, true, rows, nil); err == nil || !strings.Contains(err.Error(), "folder") {
		t.Errorf("a folder as the target = %v", err)
	}
	if n, err := ExportToFile(ctx, path, FormatCSV, ExportOptions{}, false, rows, nil); err != nil || n != 1 {
		t.Fatalf("export = %d, %v", n, err)
	}
	if _, err := ExportToFile(ctx, path, FormatCSV, ExportOptions{}, false, rows, nil); !errors.Is(err, ErrExportExists) {
		t.Errorf("no overwrite = %v", err)
	}
	if _, err := ExportToFile(ctx, path, ExportFormat("nonsense"), ExportOptions{}, true, rows, nil); err == nil {
		t.Error("an unknown format: want error")
	}
	boom := errors.New("source failed")
	if _, err := ExportToFile(ctx, path, FormatCSV, ExportOptions{}, true, func(context.Context, RowSink) error { return boom }, nil); !errors.Is(err, boom) {
		t.Errorf("a failing source = %v", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := ExportToFile(cancelled, path, FormatCSV, ExportOptions{}, true, func(context.Context, RowSink) error { return nil }, nil); err == nil {
		t.Error("a cancelled export: want error")
	}
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "1") {
		t.Errorf("the older file is kept: %q", b)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a temporary file is left: %s", e.Name())
		}
	}
}
