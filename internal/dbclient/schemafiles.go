package dbclient

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SchemaObjectFiles is s as canonical DDL with one file per object:
// tables/<name>.sql (the table with its keys, foreign keys and indexes),
// views/<name>.sql and routines/<name>.sql (not SQLite). Names are made
// safe for any disk: only letters, digits, "_", "-" and "." (never at the
// start) stay, and names that differ only in case get "-2", "-3" … The
// order and the text are stable, so a folder dump diffs well in git.
func SchemaObjectFiles(s CanonSchema) []CanonFile {
	s = sortCanon(s)
	w := newCanonWriter(s.Driver, "")
	used := map[string]int{}
	name := func(dir, obj string) string {
		base := dir + "/" + schemaFileBase(obj)
		key := strings.ToLower(base)
		used[key]++
		if n := used[key]; n > 1 {
			base = fmt.Sprintf("%s-%d", base, n)
			used[strings.ToLower(base)]++
		}
		return base + ".sql"
	}
	var out []CanonFile
	for _, t := range s.Tables {
		blocks := []string{w.table(t, true)}
		for _, ix := range t.Indexes {
			blocks = append(blocks, w.index(ix))
		}
		out = append(out, CanonFile{Name: name("tables", t.Name), Text: joinBlocks(blocks, "")})
	}
	for _, v := range s.Views {
		out = append(out, CanonFile{Name: name("views", v.Name), Text: joinBlocks([]string{w.view(v)}, "")})
	}
	if !w.sqlite() {
		for _, r := range s.Routines {
			out = append(out, CanonFile{Name: name("routines", r.Name), Text: joinBlocks([]string{w.routine(r)}, "")})
		}
	}
	return out
}

// schemaFileBase is an object name as a safe file name (no extension).
func schemaFileBase(obj string) string {
	var b strings.Builder
	for _, c := range obj {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '-':
			b.WriteRune(c)
		case c == '.' && b.Len() > 0:
			b.WriteRune(c)
		case c == '.':
		default:
			b.WriteByte('_')
		}
	}
	base := strings.TrimRight(b.String(), ".")
	if base == "" {
		base = "unnamed"
	}
	return base
}

// WriteSchemaFolder writes files under dir (made when missing). A file
// whose text is already on disk is not touched; it returns how many
// files it wrote. A name that would leave dir is refused before anything
// is written. Each file goes to a temporary name first, so a failure
// never leaves half a file.
func WriteSchemaFolder(dir string, files []CanonFile) (int, error) {
	paths := make([]string, len(files))
	for i, f := range files {
		rel := filepath.Clean(filepath.FromSlash(f.Name))
		if f.Name == "" || filepath.IsAbs(rel) || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return 0, fmt.Errorf("schema file %q is outside the folder", f.Name)
		}
		paths[i] = filepath.Join(dir, rel)
	}
	written := 0
	for i, f := range files {
		p := paths[i]
		if old, err := os.ReadFile(p); err == nil && bytes.Equal(old, []byte(f.Text)) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return written, err
		}
		tmp, err := os.CreateTemp(filepath.Dir(p), ".schema-*.tmp")
		if err != nil {
			return written, err
		}
		_, werr := tmp.WriteString(f.Text)
		cerr := tmp.Close()
		if werr == nil {
			werr = cerr
		}
		if werr == nil {
			werr = os.Chmod(tmp.Name(), 0o644)
		}
		if werr == nil {
			werr = os.Rename(tmp.Name(), p)
		}
		if werr != nil {
			_ = os.Remove(tmp.Name())
			return written, werr
		}
		written++
	}
	return written, nil
}
