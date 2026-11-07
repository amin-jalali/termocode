package dbclient

import (
	"strconv"
	"strings"
)

// ParamRef is one query parameter in a console statement: `?` (named
// "?1", "?2", … by position), `:name`, `$1` (Postgres, SQLite) or
// `@name` (not MySQL, where @name is a server variable). Start and End
// are byte offsets in the SQL.
type ParamRef struct {
	Name       string
	Start, End int
}

func isParamWord(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func isParamStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// FindParams lists the parameters of sql in order. Strings, quoted
// names, comments and Postgres dollar quotes are skipped; so are `::`
// casts, `:=`, array slices (`a[1:n]`), Postgres `?` (a jsonb operator)
// and MySQL `@var` / `@@var`.
func FindParams(sql, driver string) []ParamRef {
	d := normDriver(driver)
	pg := d == "postgres" || d == "postgresql"
	mysql := d == "mysql"
	var out []ParamRef
	pos := 0
	n := len(sql)
	prevWord := func(i int) bool { return i > 0 && (isParamWord(sql[i-1]) || sql[i-1] == '.') }
	for i := 0; i < n; i++ {
		c := sql[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			for i++; i < n; i++ {
				if mysql && sql[i] == '\\' && c != '`' {
					i++
					continue
				}
				if sql[i] == c {
					if i+1 < n && sql[i+1] == c {
						i++
						continue
					}
					break
				}
			}
		case c == '-' && i+1 < n && sql[i+1] == '-', mysql && c == '#':
			for i < n && sql[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && sql[i+1] == '*':
			if j := strings.Index(sql[i+2:], "*/"); j >= 0 {
				i += j + 3
			} else {
				i = n
			}
		case c == '$' && pg && !prevWord(i):
			// $tag$ … $tag$ (and $$ … $$) is a string; $1 a parameter.
			j := i + 1
			for j < n && (isParamWord(sql[j])) && !(sql[j] >= '0' && sql[j] <= '9' && j == i+1) {
				j++
			}
			if j < n && sql[j] == '$' {
				tag := sql[i : j+1]
				if k := strings.Index(sql[j+1:], tag); k >= 0 {
					i = j + k + len(tag)
				} else {
					i = n
				}
				continue
			}
			fallthrough
		case c == '$' && d == "sqlite" && !prevWord(i):
			j := i + 1
			for j < n && sql[j] >= '0' && sql[j] <= '9' {
				j++
			}
			if j > i+1 && (j >= n || !isParamWord(sql[j])) {
				out = append(out, ParamRef{Name: sql[i:j], Start: i, End: j})
				i = j - 1
			}
		case c == '?' && !pg:
			pos++
			out = append(out, ParamRef{Name: "?" + strconv.Itoa(pos), Start: i, End: i + 1})
		case c == ':' || (c == '@' && !mysql):
			if prevWord(i) || i > 0 && (sql[i-1] == ':' || sql[i-1] == '@') || i+1 >= n || !isParamStart(sql[i+1]) {
				continue
			}
			j := i + 1
			for j < n && isParamWord(sql[j]) {
				j++
			}
			out = append(out, ParamRef{Name: sql[i:j], Start: i, End: j})
			i = j - 1
		}
	}
	return out
}

// UniqueParamNames is the names of refs, each once, in first-seen order.
func UniqueParamNames(refs []ParamRef) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range refs {
		if !seen[r.Name] {
			seen[r.Name] = true
			out = append(out, r.Name)
		}
	}
	return out
}

// BindParams replaces each ref with the driver's placeholder and returns
// the values in placeholder order. Postgres reuses one $n for a name
// used twice; `?` dialects bind every use. A value never enters the SQL.
func BindParams(sql string, refs []ParamRef, values map[string]string, driver string) (string, []any) {
	dl := DialectFor(driver)
	if dl == nil || len(refs) == 0 {
		return sql, nil
	}
	d := normDriver(driver)
	reuse := d == "postgres" || d == "postgresql"
	var b strings.Builder
	var args []any
	index := map[string]int{}
	last := 0
	for _, r := range refs {
		b.WriteString(sql[last:r.Start])
		last = r.End
		if k, ok := index[r.Name]; ok && reuse {
			b.WriteString(dl.Placeholder(k))
			continue
		}
		args = append(args, ParamValue(values[r.Name]))
		index[r.Name] = len(args)
		b.WriteString(dl.Placeholder(len(args)))
	}
	b.WriteString(sql[last:])
	return b.String(), args
}

// ParamValue is the value bound for what the user typed: NULL is NULL, a
// whole number (no leading zero) is an integer, the rest is text.
func ParamValue(s string) any {
	if s == "NULL" {
		return nil
	}
	t := strings.TrimPrefix(s, "-")
	if t != "" && (t == "0" || t[0] != '0') {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil {
			return v
		}
	}
	return s
}
