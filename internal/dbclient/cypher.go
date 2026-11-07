package dbclient

import "strings"

// cypherTok is one Cypher token: a word (afterDot when it follows '.', ':'
// or '$', so it is a name, not a keyword) or a single symbol byte.
type cypherTok struct {
	word     string
	afterDot bool
	sym      byte
}

var cypherWriteWords = map[string]bool{
	"CREATE": true, "MERGE": true, "DELETE": true, "DETACH": true, "SET": true,
	"REMOVE": true, "DROP": true, "LOAD": true, "FOREACH": true, "START": true,
	"STOP": true, "ALTER": true, "GRANT": true, "DENY": true, "REVOKE": true,
	"TERMINATE": true, "RENAME": true, "ENABLE": true, "DEALLOCATE": true,
	"REALLOCATE": true, "FINISH": true,
}

var cypherReadStarts = map[string]bool{
	"MATCH": true, "OPTIONAL": true, "RETURN": true, "WITH": true, "UNWIND": true,
	"SHOW": true, "EXPLAIN": true, "PROFILE": true, "CALL": true, "USE": true,
}

// cypherReadProcs are built-in procedures that only read the catalog.
var cypherReadProcs = map[string]bool{
	"db.labels": true, "db.relationshiptypes": true, "db.propertykeys": true,
	"db.schema.visualization": true, "db.schema.nodetypeproperties": true,
	"db.schema.reltypeproperties": true, "db.indexes": true, "db.constraints": true,
	"db.info": true, "dbms.components": true,
}

// ClassifyCypher is a conservative read/write hint for a Neo4j Cypher query,
// used like Classify for the read-only and production rules. It fails
// closed: unterminated strings or comments, unknown first keywords and
// procedures outside a small read-only list are writes. An empty query is
// unknown.
func ClassifyCypher(q string) Kind {
	toks, ok := cypherTokens(q)
	if !ok {
		return KindWrite
	}
	start := true
	seen := false
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t.sym == ';' {
			start = true
			continue
		}
		if t.word == "" || t.afterDot {
			start = false
			continue
		}
		if start {
			seen = true
			if !cypherReadStarts[t.word] {
				return KindWrite
			}
		}
		start = false
		if cypherWriteWords[t.word] {
			return KindWrite
		}
		if t.word == "CALL" && i+1 < len(toks) && toks[i+1].word != "" {
			name := strings.ToLower(toks[i+1].word)
			j := i + 2
			for j+1 < len(toks) && toks[j].sym == '.' && toks[j+1].afterDot {
				name += "." + strings.ToLower(toks[j+1].word)
				j += 2
			}
			if !cypherReadProcs[name] {
				return KindWrite
			}
			i = j - 1
		}
	}
	if !seen {
		return KindUnknown
	}
	return KindRead
}

// cypherTokens splits q into words and symbols, skipping strings, quoted
// `names` and comments. ok is false for an unterminated string or comment.
func cypherTokens(q string) (toks []cypherTok, ok bool) {
	isWord := func(c byte) bool {
		return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
	}
	prevDot := false
	for i := 0; i < len(q); {
		c := q[i]
		switch {
		case c == '/' && i+1 < len(q) && q[i+1] == '/':
			for i < len(q) && q[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(q) && q[i+1] == '*':
			end := strings.Index(q[i+2:], "*/")
			if end < 0 {
				return nil, false
			}
			i += end + 4
		case c == '\'' || c == '"' || c == '`':
			j := i + 1
			for ; j < len(q) && q[j] != c; j++ {
				if q[j] == '\\' && c != '`' {
					j++
				}
			}
			if j >= len(q) {
				return nil, false
			}
			// A quoted `name` is a name, never a keyword.
			toks = append(toks, cypherTok{sym: c})
			prevDot = false
			i = j + 1
		case isWord(c):
			j := i
			for j < len(q) && isWord(q[j]) {
				j++
			}
			toks = append(toks, cypherTok{word: strings.ToUpper(q[i:j]), afterDot: prevDot})
			prevDot = false
			i = j
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		default:
			toks = append(toks, cypherTok{sym: c})
			// A word after '.', ':' or '$' is a property, label, type or
			// parameter name, never a keyword.
			prevDot = c == '.' || c == ':' || c == '$'
			i++
		}
	}
	return toks, true
}
