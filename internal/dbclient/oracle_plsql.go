package dbclient

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Oracle PL/SQL source edit (DBX-63): packages (spec and body),
// procedures, functions and triggers are read from all_source, stored
// with CREATE OR REPLACE, and their compile errors read from all_errors.

const (
	ObjectPackage     ObjectKind = "package"
	ObjectPackageBody ObjectKind = "package body"
)

// oracleSourceTypes is the all_source / all_errors type of each kind.
var oracleSourceTypes = map[ObjectKind]string{
	ObjectPackage:     "'PACKAGE'",
	ObjectPackageBody: "'PACKAGE BODY'",
	ObjectFunction:    "'FUNCTION', 'PROCEDURE'",
	ObjectProcedure:   "'PROCEDURE', 'FUNCTION'",
	ObjectTrigger:     "'TRIGGER'",
}

// oracleOwner splits "SCHEMA.NAME" into the bound name and the owner
// condition (the current schema when there is none).
func oracleOwner(name string, n int) (string, []any) {
	schema, obj := "", name
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		schema, obj = name[:i], name[i+1:]
	}
	obj = strings.Trim(obj, `"`)
	if schema = strings.Trim(schema, `"`); schema == "" {
		return "SYS_CONTEXT('USERENV', 'CURRENT_SCHEMA')", []any{obj}
	}
	return ":" + strconv.Itoa(n), []any{obj, schema}
}

func oracleSourceQuery(kind ObjectKind, name string) (SourceQuery, error) {
	owner, args := oracleOwner(name, 2)
	if kind == ObjectView {
		return SourceQuery{SQL: "SELECT DBMS_METADATA.GET_DDL('VIEW', :1, " + owner + ") FROM dual", Args: args}, nil
	}
	types := oracleSourceTypes[kind]
	if types == "" {
		return SourceQuery{}, fmt.Errorf("editing the source of a %s is not supported for oracle", kind)
	}
	q := "SELECT text FROM all_source WHERE name = :1 AND owner = " + owner + " AND type IN (" + types + ") ORDER BY type, line"
	return SourceQuery{SQL: q, Args: args, Lines: true, Prefix: "CREATE OR REPLACE "}, nil
}

// Text is the source a query's result holds: one cell, or (Lines) the
// cells of every row joined, after Prefix. ok is false when there is none.
func (q SourceQuery) Text(r Result) (string, bool) {
	if len(r.Rows) == 0 {
		return "", false
	}
	if !q.Lines {
		if q.Column >= len(r.Rows[0]) || r.Rows[0][q.Column].Null {
			return "", false
		}
		return r.Rows[0][q.Column].Text, true
	}
	var b strings.Builder
	b.WriteString(q.Prefix)
	for _, row := range r.Rows {
		if q.Column < len(row) {
			b.WriteString(row[q.Column].Text)
		}
	}
	return b.String(), true
}

// SourceHeaderKind is the kind of object a CREATE statement makes.
func SourceHeaderKind(src string) (ObjectKind, bool) {
	f := strings.Fields(src)
	if len(f) == 0 || !strings.EqualFold(f[0], "CREATE") {
		return "", false
	}
	for i := 1; i < len(f); i++ {
		switch w := strings.ToLower(f[i]); w {
		case "package":
			if i+1 < len(f) && strings.EqualFold(f[i+1], "body") {
				return ObjectPackageBody, true
			}
			return ObjectPackage, true
		case string(ObjectView), string(ObjectFunction), string(ObjectProcedure), string(ObjectTrigger):
			return ObjectKind(w), true
		case "or", "replace", "editionable", "noneditionable", "editioning", "force", "no", "algorithm", "definer", "sql", "security", "invoker", "temp", "temporary", "materialized", "recursive", "constraint", "aggregate", "unique":
			continue
		default:
			if strings.Contains(w, "=") || strings.HasPrefix(w, "definer") {
				continue
			}
			return "", false
		}
	}
	return "", false
}

// oraclePLSQLUnit reports a statement that is a PL/SQL unit: an anonymous
// block or a CREATE of a package, routine, trigger or type. Its inner
// semicolons belong to it.
func oraclePLSQLUnit(stmt string) bool {
	words := tokenWordsDialect(stmt, "")
	if len(words) == 0 {
		return false
	}
	switch strings.ToUpper(words[0]) {
	case "BEGIN", "DECLARE":
		return true
	case "CREATE":
	default:
		return false
	}
	for _, w := range words[1:] {
		switch strings.ToUpper(w) {
		case "OR", "REPLACE", "EDITIONABLE", "NONEDITIONABLE":
			continue
		case "PACKAGE", "PROCEDURE", "FUNCTION", "TRIGGER", "TYPE":
			return true
		}
		return false
	}
	return false
}

// splitOracle splits a script the SQL*Plus way: a "/" line ends a
// statement, and a PL/SQL unit runs to that line (or the end) with its
// semicolons and its final ';'. Other statements split at ';'.
func splitOracle(sql string) ([]string, error) {
	var out []string
	var chunks []string
	start := 0
	lines := strings.SplitAfter(sql, "\n")
	pos := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == "/" {
			chunks = append(chunks, sql[start:pos])
			start = pos + len(l)
		}
		pos += len(l)
	}
	chunks = append(chunks, sql[start:])
	for _, chunk := range chunks {
		parts, err := Split(chunk, "")
		if err != nil {
			return nil, err
		}
		at := 0
		for _, p := range parts {
			i := strings.Index(chunk[at:], p)
			if i < 0 {
				return nil, errors.New("SQL script could not be split")
			}
			if oraclePLSQLUnit(p) {
				out = append(out, strings.TrimSpace(chunk[at+i:]))
				break
			}
			out = append(out, p)
			at += i + len(p)
		}
	}
	return out, nil
}

// CompileError is one line of all_errors, on the buffer's line and column.
type CompileError struct {
	Line, Col int
	Text      string
	Warning   bool
}

// OracleCompileErrorsQuery reads the compile errors of a PL/SQL unit.
func OracleCompileErrorsQuery(kind ObjectKind, name string) (SourceQuery, error) {
	t := strings.Trim(strings.SplitN(oracleSourceTypes[kind], ",", 2)[0], "' ")
	if t == "" {
		return SourceQuery{}, fmt.Errorf("a %s has no compile errors", kind)
	}
	owner, args := oracleOwner(name, 2)
	if len(args) == 1 {
		return SourceQuery{SQL: "SELECT line, position, text, attribute FROM all_errors WHERE name = :1 AND owner = " + owner + " AND type = :2 ORDER BY sequence", Args: []any{args[0], t}}, nil
	}
	return SourceQuery{SQL: "SELECT line, position, text, attribute FROM all_errors WHERE name = :1 AND owner = :2 AND type = :3 ORDER BY sequence", Args: append(args, t)}, nil
}

// IsPLSQLKind reports a kind whose submit is compiled (and may fail to).
func IsPLSQLKind(k ObjectKind) bool { return k != ObjectView && oracleSourceTypes[k] != "" }

// OracleCompileErrors maps all_errors rows (line, position, text,
// attribute) onto source, the text that was submitted: Oracle counts
// lines from the unit's first line (after CREATE OR REPLACE), so blank
// lines before it move them down, and columns on that line move right.
func OracleCompileErrors(r Result, source string) []CompileError {
	lines := strings.Split(source, "\n")
	first, shift := 0, 0
	for first < len(lines) && strings.TrimSpace(lines[first]) == "" {
		first++
	}
	if first < len(lines) {
		if k, ok := SourceHeaderKind(lines[first]); ok {
			word := strings.Fields(string(k))[0]
			if i := strings.Index(strings.ToLower(lines[first]), word); i >= 0 {
				shift = i
			}
		}
	}
	var out []CompileError
	for _, row := range r.Rows {
		if len(row) < 3 {
			continue
		}
		line, _ := strconv.Atoi(strings.TrimSpace(row[0].Text))
		col, _ := strconv.Atoi(strings.TrimSpace(row[1].Text))
		if line < 1 {
			line = 1
		}
		if line == 1 {
			col += shift
		}
		e := CompileError{Line: line + first, Col: col, Text: strings.TrimSpace(row[2].Text)}
		if len(row) > 3 && strings.EqualFold(strings.TrimSpace(row[3].Text), "WARNING") {
			e.Warning = true
		}
		out = append(out, e)
	}
	return out
}
