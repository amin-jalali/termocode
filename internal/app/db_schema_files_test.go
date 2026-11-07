package app

import (
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// DBX-46: F on a schema dumps it to a folder or compares it with one.

func TestDBSchemaFolderPath(t *testing.T) {
	if got := dbSchemaFolderPath("/work", "schema/local"); got != "/work/schema/local" {
		t.Errorf("relative: %s", got)
	}
	if got := dbSchemaFolderPath("/work", "/abs/dir"); got != "/abs/dir" {
		t.Errorf("absolute: %s", got)
	}
	if got := dbSchemaFolderDefault(dbCompareSide{conn: "my pg", schema: "public"}); got != "schema/my_pg-public" {
		t.Errorf("default: %s", got)
	}
}

func TestDBSchemaCompareItems(t *testing.T) {
	diffs := []dbclient.SchemaFileDiff{
		{Name: "tables/b.sql", State: dbclient.SchemaFileChanged},
		{Name: "views/c.sql", State: dbclient.SchemaFileLiveOnly},
		{Name: "routines/old.sql", State: dbclient.SchemaFileFolderOnly},
		{Name: "tables/a.sql", State: dbclient.SchemaFileIdentical},
	}
	items := dbSchemaCompareItems(diffs, map[string]bool{"tables/b.sql": true})
	var heads, rows []string
	for _, it := range items {
		if it.Header {
			heads = append(heads, it.Title)
		} else {
			rows = append(rows, it.ID+"="+it.Title)
		}
	}
	if strings.Join(heads, "|") != "Changed|Only in the database|Only in the folder|Identical" {
		t.Errorf("headers: %v", heads)
	}
	if len(rows) != 4 || !strings.HasPrefix(rows[0], "tables/b.sql=[x] ") || !strings.HasPrefix(rows[1], "views/c.sql=[ ] ") {
		t.Errorf("rows: %v", rows)
	}
	if strings.Contains(rows[3], "[") {
		t.Errorf("an identical row cannot be selected: %s", rows[3])
	}
}
