package dbclient

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/amin-jalali/termocode/internal/textdiff"
)

// ObjectKind is a kind of database object whose source text can be edited
// (DBX-32): views, routines and triggers.
type ObjectKind string

const (
	ObjectView      ObjectKind = "view"
	ObjectFunction  ObjectKind = "function"
	ObjectProcedure ObjectKind = "procedure"
	ObjectTrigger   ObjectKind = "trigger"
)

// SourceQuery reads the stored source of one object: run SQL with Args and
// take the text in column Column of the first row.
type SourceQuery struct {
	SQL    string
	Args   []any
	Column int
}

// ObjectSourceQuery is the read-only query that returns the stored source
// (a full CREATE statement) of an object.
func ObjectSourceQuery(driver string, kind ObjectKind, name string) (SourceQuery, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return SourceQuery{}, errors.New("object name is empty")
	}
	switch normDriver(driver) {
	case "postgres", "postgresql":
		switch kind {
		case ObjectView:
			return SourceQuery{SQL: `SELECT 'CREATE OR REPLACE VIEW ' || $1::regclass::text || E' AS\n' || pg_get_viewdef($1::regclass, true)`, Args: []any{name}}, nil
		case ObjectFunction, ObjectProcedure:
			cast := "regproc"
			if strings.Contains(name, "(") {
				cast = "regprocedure" // a signature picks one overload
			}
			return SourceQuery{SQL: `SELECT pg_get_functiondef($1::` + cast + `)`, Args: []any{name}}, nil
		case ObjectTrigger:
			return SourceQuery{SQL: `SELECT pg_get_triggerdef(t.oid, true) FROM pg_trigger t WHERE NOT t.tgisinternal AND t.tgname = $1 ORDER BY t.oid LIMIT 1`, Args: []any{name}}, nil
		}
	case "mysql":
		q := MySQLDialect{}.QuoteIdent(name)
		switch kind {
		case ObjectView:
			return SourceQuery{SQL: "SHOW CREATE VIEW " + q, Column: 1}, nil
		case ObjectFunction, ObjectProcedure, ObjectTrigger:
			return SourceQuery{SQL: "SHOW CREATE " + strings.ToUpper(string(kind)) + " " + q, Column: 2}, nil
		}
	case "sqlite":
		switch kind {
		case ObjectView, ObjectTrigger:
			return SourceQuery{SQL: `SELECT sql FROM sqlite_master WHERE type = ? AND name = ?`, Args: []any{string(kind), name}}, nil
		}
	}
	return SourceQuery{}, fmt.Errorf("editing the source of a %s is not supported for %s", kind, driver)
}

// SourcePlan is the script that replaces an object's stored source.
// Transactional is false when the engine commits DDL at once (MySQL): the
// preview card must say the old object is gone if the CREATE fails.
type SourcePlan struct {
	Statements    []string
	Transactional bool
}

// SubmitObjectSource checks an edited source and builds the script that
// stores it. The source must be exactly one CREATE statement for the same
// kind and name, so a stray edit can never run other SQL.
func SubmitObjectSource(driver string, kind ObjectKind, name, source string) (SourcePlan, error) {
	src := strings.TrimSpace(source)
	if src == "" {
		return SourcePlan{}, errors.New("the source is empty")
	}
	if _, err := ObjectSourceQuery(driver, kind, name); err != nil {
		return SourcePlan{}, err
	}
	stmts, err := Split(src, driver)
	if err != nil {
		return SourcePlan{}, err
	}
	n := 0
	for _, s := range stmts {
		if strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), ";")) != "" {
			n++
		}
	}
	if n != 1 {
		return SourcePlan{}, fmt.Errorf("the source must be one CREATE statement, found %d statements", n)
	}
	if err := checkSourceHeader(src, kind, name); err != nil {
		return SourcePlan{}, err
	}
	switch normDriver(driver) {
	case "postgres", "postgresql":
		return SourcePlan{Statements: []string{orReplace(src)}, Transactional: true}, nil
	case "mysql":
		if kind == ObjectView {
			return SourcePlan{Statements: []string{orReplace(src)}}, nil
		}
		drop := "DROP " + strings.ToUpper(string(kind)) + " IF EXISTS " + MySQLDialect{}.QuoteIdent(bareName(name))
		return SourcePlan{Statements: []string{drop, src}}, nil
	default: // sqlite: no CREATE OR REPLACE
		drop := "DROP " + strings.ToUpper(string(kind)) + " IF EXISTS " + SQLiteDialect{}.QuoteIdent(bareName(name))
		return SourcePlan{Statements: []string{drop, src}, Transactional: true}, nil
	}
}

func bareName(name string) string {
	if i := strings.IndexByte(name, '('); i >= 0 {
		name = name[:i]
	}
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		name = name[i+1:]
	}
	return strings.Trim(strings.TrimSpace(name), "\"`[]")
}

// checkSourceHeader makes sure src is CREATE ... <kind> <name>.
func checkSourceHeader(src string, kind ObjectKind, name string) error {
	f := strings.Fields(src)
	if len(f) == 0 || !strings.EqualFold(f[0], "CREATE") {
		return errors.New("the source must start with CREATE")
	}
	for i := 1; i < len(f); i++ {
		w := strings.ToLower(f[i])
		switch ObjectKind(w) {
		case ObjectView, ObjectFunction, ObjectProcedure, ObjectTrigger:
		default:
			continue
		}
		if ObjectKind(w) != kind {
			return fmt.Errorf("the source creates a %s, not a %s", w, kind)
		}
		j := i + 1
		for j < len(f) && (strings.EqualFold(f[j], "IF") || strings.EqualFold(f[j], "NOT") || strings.EqualFold(f[j], "EXISTS")) {
			j++
		}
		if j < len(f) && strings.EqualFold(bareName(f[j]), bareName(name)) {
			return nil
		}
		return fmt.Errorf("the source does not create %s", bareName(name))
	}
	return fmt.Errorf("the source does not create a %s", kind)
}

// orReplace turns CREATE into CREATE OR REPLACE when it is not already.
func orReplace(src string) string {
	rest := strings.TrimSpace(src[len("CREATE"):])
	f := strings.Fields(rest)
	if len(f) >= 2 && strings.EqualFold(f[0], "OR") && strings.EqualFold(f[1], "REPLACE") {
		return src
	}
	return "CREATE OR REPLACE " + rest
}

// SourceEdit is one object source opened for editing: Stored is the text
// read at open time, Text the buffer.
type SourceEdit struct {
	Conn   string
	Kind   ObjectKind
	Name   string
	Stored string
	Text   string
}

// NewSourceEdit opens an edit with the buffer equal to the stored source.
func NewSourceEdit(conn string, kind ObjectKind, name, stored string) *SourceEdit {
	return &SourceEdit{Conn: conn, Kind: kind, Name: name, Stored: stored, Text: stored}
}

// Dirty reports whether the buffer differs from the stored source.
func (e *SourceEdit) Dirty() bool { return normSource(e.Text) != normSource(e.Stored) }

// Diff is the line diff stored → buffer.
func (e *SourceEdit) Diff() *textdiff.Diff { return textdiff.Compute(e.Stored, e.Text) }

// Preview is a unified diff for the submit card.
func (e *SourceEdit) Preview() string {
	var b strings.Builder
	b.WriteString("--- stored " + string(e.Kind) + " " + e.Name + "\n")
	b.WriteString("+++ edited " + string(e.Kind) + " " + e.Name + "\n")
	for _, r := range e.Diff().Rows {
		switch r.Kind {
		case textdiff.Added:
			b.WriteByte('+')
		case textdiff.Removed:
			b.WriteByte('-')
		default:
			b.WriteByte(' ')
		}
		b.WriteString(r.Text)
		b.WriteByte('\n')
	}
	return b.String()
}

// Outdated reports whether the source stored now (read again just before
// submit) differs from the one the edit started from: someone else changed
// the object, and submitting would overwrite their change.
func (e *SourceEdit) Outdated(storedNow string) bool {
	return normSource(storedNow) != normSource(e.Stored)
}

// normSource ignores line endings and trailing blanks.
func normSource(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// PendingSources is the pending-changes list: edited sources not yet
// submitted. Safe for use from several goroutines.
type PendingSources struct {
	mu sync.Mutex
	m  map[string]SourceEdit
}

func pendingKey(conn string, kind ObjectKind, name string) string {
	return conn + "\x00" + string(kind) + "\x00" + name
}

// Put records the edit when it is dirty and removes it when it is clean.
func (p *PendingSources) Put(e *SourceEdit) {
	p.mu.Lock()
	defer p.mu.Unlock()
	k := pendingKey(e.Conn, e.Kind, e.Name)
	if !e.Dirty() {
		delete(p.m, k)
		return
	}
	if p.m == nil {
		p.m = map[string]SourceEdit{}
	}
	p.m[k] = *e
}

// Drop removes one entry (submitted or discarded).
func (p *PendingSources) Drop(conn string, kind ObjectKind, name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, pendingKey(conn, kind, name))
}

// Len is the number of pending edits.
func (p *PendingSources) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.m)
}

// List is the pending edits sorted by connection, name and kind.
func (p *PendingSources) List() []SourceEdit {
	p.mu.Lock()
	out := make([]SourceEdit, 0, len(p.m))
	for _, e := range p.m {
		out = append(out, e)
	}
	p.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Conn != b.Conn {
			return a.Conn < b.Conn
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Kind < b.Kind
	})
	return out
}
