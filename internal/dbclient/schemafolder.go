package dbclient

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DBX-46: a folder written by SchemaObjectFiles, read back and compared
// with the live schema, and the migration tool a project uses.

// schemaFolderKinds are the subfolders SchemaObjectFiles writes.
var schemaFolderKinds = []string{"tables", "views", "routines"}

// schemaFolderMaxFile caps one object file read from the folder.
const schemaFolderMaxFile = 4 << 20

// ReadSchemaFolder reads dir's tables/*.sql, views/*.sql and
// routines/*.sql by slash name ("tables/users.sql"). Other files are
// left out; a missing folder is empty. Links are not followed.
func ReadSchemaFolder(dir string) (map[string]string, error) {
	out := map[string]string{}
	for _, kind := range schemaFolderKinds {
		entries, err := os.ReadDir(filepath.Join(dir, kind))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".sql") || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				return nil, err
			}
			if info.Size() > schemaFolderMaxFile {
				return nil, fmt.Errorf("%s/%s is larger than 4 MB", kind, e.Name())
			}
			b, err := os.ReadFile(filepath.Join(dir, kind, e.Name()))
			if err != nil {
				return nil, err
			}
			out[kind+"/"+e.Name()] = string(b)
		}
	}
	return out, nil
}

// SchemaFileState is how one object file compares.
type SchemaFileState int

const (
	SchemaFileChanged SchemaFileState = iota
	SchemaFileLiveOnly
	SchemaFileFolderOnly
	SchemaFileIdentical
)

// SchemaFileDiff is one object file: its live text and its folder text.
type SchemaFileDiff struct {
	Name         string
	State        SchemaFileState
	Live, Folder string
}

// CompareSchemaFolder sorts every object of live and folder: changed
// first, then only in the database, only in the folder, identical; by
// name inside each.
func CompareSchemaFolder(live []CanonFile, folder map[string]string) []SchemaFileDiff {
	var out []SchemaFileDiff
	seen := map[string]bool{}
	for _, f := range live {
		seen[f.Name] = true
		old, ok := folder[f.Name]
		switch {
		case !ok:
			out = append(out, SchemaFileDiff{Name: f.Name, State: SchemaFileLiveOnly, Live: f.Text})
		case old == f.Text:
			out = append(out, SchemaFileDiff{Name: f.Name, State: SchemaFileIdentical, Live: f.Text, Folder: old})
		default:
			out = append(out, SchemaFileDiff{Name: f.Name, State: SchemaFileChanged, Live: f.Text, Folder: old})
		}
	}
	for name, text := range folder {
		if !seen[name] {
			out = append(out, SchemaFileDiff{Name: name, State: SchemaFileFolderOnly, Folder: text})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].State != out[j].State {
			return out[i].State < out[j].State
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ApplySchemaFolder makes the folder match the database for diffs: a
// changed or database-only object is written, a folder-only one is
// removed, an identical one is left. Every name is checked before
// anything changes.
func ApplySchemaFolder(dir string, diffs []SchemaFileDiff) (wrote, removed int, err error) {
	var files []CanonFile
	var gone []string
	for _, d := range diffs {
		rel := filepath.Clean(filepath.FromSlash(d.Name))
		if d.Name == "" || filepath.IsAbs(rel) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return 0, 0, fmt.Errorf("schema file %q is outside the folder", d.Name)
		}
		switch d.State {
		case SchemaFileChanged, SchemaFileLiveOnly:
			files = append(files, CanonFile{Name: d.Name, Text: d.Live})
		case SchemaFileFolderOnly:
			gone = append(gone, filepath.Join(dir, rel))
		}
	}
	wrote, err = WriteSchemaFolder(dir, files)
	if err != nil {
		return wrote, 0, err
	}
	for _, p := range gone {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return wrote, removed, err
		}
		removed++
	}
	return wrote, removed, nil
}

// DetectMigrationTool names the migration tool root uses, "" for none
// found: a schema folder is then a snapshot next to the migrations.
func DetectMigrationTool(root string) string {
	exists := func(rel string) bool {
		_, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
		return err == nil
	}
	glob := func(pattern string) bool {
		m, _ := filepath.Glob(filepath.Join(root, filepath.FromSlash(pattern)))
		return len(m) > 0
	}
	switch {
	case exists("flyway.conf") || exists("flyway.toml") || exists("conf/flyway.conf"):
		return "Flyway"
	case exists("liquibase.properties") || glob("db/changelog/*"):
		return "Liquibase"
	case exists("alembic.ini"):
		return "Alembic"
	case exists("prisma/migrations"):
		return "Prisma"
	case exists("db/migrate"):
		return "Rails"
	case glob("migrations/*.up.sql") || glob("db/migrations/*.up.sql"):
		return "golang-migrate"
	case exists("dbmate") || glob("db/migrations/*.sql"):
		return "dbmate"
	}
	return ""
}
