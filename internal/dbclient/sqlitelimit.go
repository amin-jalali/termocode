package dbclient

import (
	"strconv"
	"strings"
)

// sqliteRowLimit adds "LIMIT limit" to a plain read when it has no LIMIT
// of its own, so sqlite3 stops after the rows the caller keeps instead of
// printing (and the session reading) a whole big table. ok is false, and
// the statement is sent unchanged, for anything else: a write, a PRAGMA,
// a top-level LIMIT, SQL this reader does not follow.
func sqliteRowLimit(stmt string, limit int) (string, bool) {
	if limit <= 0 {
		return stmt, false
	}
	toks, ok := selectTokens(stmt, "sqlite")
	if !ok || len(toks) == 0 || toks[0].kind != tokWord {
		return stmt, false
	}
	switch strings.ToUpper(toks[0].text) {
	case "SELECT", "WITH":
	default:
		return stmt, false
	}
	depth := 0
	for _, t := range toks {
		switch {
		case t.kind == tokPunct && t.text == "(":
			depth++
		case t.kind == tokPunct && t.text == ")":
			depth--
			if depth < 0 {
				return stmt, false
			}
		case t.kind == tokPunct && t.text == ";":
			return stmt, false // Split leaves none; be safe if one is there
		case t.kind == tokPunct && t.text == "?":
			return stmt, false // a numbered parameter could follow a LIMIT
		case t.kind == tokWord && depth == 0:
			switch strings.ToUpper(t.text) {
			case "LIMIT", "INSERT", "UPDATE", "DELETE", "REPLACE", "RETURNING", "VALUES":
				// VALUES: sqlite3 takes no LIMIT after a top-level VALUES list.
				return stmt, false
			}
		}
	}
	if depth != 0 {
		return stmt, false
	}
	// On its own line: a trailing "-- comment" must not swallow it.
	return stmt + "\nLIMIT " + strconv.Itoa(limit), true
}
