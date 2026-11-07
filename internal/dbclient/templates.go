package dbclient

import (
	"sort"
	"strings"
)

// sqlTemplates are the console live templates (DBX-6); "|" marks where
// the cursor goes.
var sqlTemplates = map[string]string{
	"sel": "SELECT * FROM |;",
	"ins": "INSERT INTO | () VALUES ();",
	"upd": "UPDATE | SET  WHERE ;",
	"del": "DELETE FROM | WHERE ;",
	"jn":  "JOIN | ON ",
	"cnt": "SELECT COUNT(*) FROM |;",
}

// ExpandTemplate is the SQL of live template abbr (any case) and the
// byte offset of the cursor in it.
func ExpandTemplate(abbr string) (text string, cursor int, ok bool) {
	t, ok := sqlTemplates[strings.ToLower(abbr)]
	if !ok {
		return "", 0, false
	}
	cursor = strings.IndexByte(t, '|')
	return t[:cursor] + t[cursor+1:], cursor, true
}

// Templates are the live template names, sorted.
func Templates() []string {
	out := make([]string, 0, len(sqlTemplates))
	for k := range sqlTemplates {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
