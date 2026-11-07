package dbclient

import (
	"path/filepath"
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// SQL language helpers, part 2 (DBX-41): where a name is used in SQL
// code, the CTE names of a statement, and SQL written inside the strings
// of Go, Python and JavaScript/TypeScript files. Pure text work, own
// scanner, no connection.

// Span is a byte range of a text.
type Span struct{ Start, End int }

func identByte(c byte) bool {
	return c == '_' || c == '$' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 0x80
}

// NameUsages lists where name (any case) stands as a whole identifier in
// src: plain words and "double quoted", `back quoted` or [bracketed]
// names (the span is the name without its quotes). Strings and comments
// do not count.
func NameUsages(src, name string) []Span {
	if name == "" {
		return nil
	}
	code := blankLiterals(src)
	var out []Span
	for i := 0; i < len(code); {
		c := code[i]
		switch {
		case c == '"' || c == '`' || c == '[':
			closer := c
			if c == '[' {
				closer = ']'
			}
			j := strings.IndexByte(code[i+1:], closer)
			if j < 0 {
				return out
			}
			if strings.EqualFold(code[i+1:i+1+j], name) {
				out = append(out, Span{i + 1, i + 1 + j})
			}
			i += j + 2
		case identByte(c):
			j := i
			for j < len(code) && identByte(code[j]) {
				j++
			}
			if strings.EqualFold(code[i:j], name) {
				out = append(out, Span{i, j})
			}
			i = j
		default:
			i++
		}
	}
	return out
}

// DialectLine reads only a `-- @dialect name` line of src.
func DialectLine(src string) (SQLDialect, bool) {
	if m := dialectLineRe.FindStringSubmatch(src); m != nil {
		return DialectByName(m[1])
	}
	return "", false
}

var cteRe = lazyre.New("(?is)(?:\\bWITH(?:\\s+RECURSIVE)?|,)\\s*(\"[^\"]+\"|`[^`]+`|[A-Za-z_][A-Za-z0-9_$]*)\\s*(?:\\([^()]*\\))?\\s*AS\\s*(?:NOT\\s+)?(?:MATERIALIZED\\s*)?\\(")

// CTENames lists the names a WITH clause of stmt defines, in order.
func CTENames(stmt string) []string {
	code := blankLiterals(stmt)
	if !strings.Contains(strings.ToUpper(code), "WITH") {
		return nil
	}
	var out []string
	for _, m := range cteRe.FindAllStringSubmatch(code, -1) {
		out = append(out, strings.Trim(m[1], "\"`"))
	}
	return out
}

// EmbeddedSQL is one string literal of a source file that holds SQL.
type EmbeddedSQL struct {
	Line int // 1-based line of the opening quote
	Col  int // 0-based byte column of the opening quote
	Text string
	Kind Kind
}

// EmbedLang is the language FindEmbeddedSQL reads for path, "" = none.
func EmbedLang(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".py", ".pyi":
		return "python"
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".mts", ".cts", ".tsx":
		return "js"
	}
	return ""
}

var embedStartRe = lazyre.New(`(?is)^\s*(SELECT|INSERT|UPDATE|DELETE|WITH|CREATE|ALTER|DROP|TRUNCATE|MERGE|REPLACE)\b`)

// embedNeeds is a second word each first word needs, so that "Update
// available" or "select a file" are not taken for SQL.
var embedNeeds = map[string]*lazyre.Regexp{
	"SELECT":   lazyre.New(`(?is)^\s*SELECT\s+(?:[\d'*(]|.*\bFROM\b)`),
	"INSERT":   lazyre.New(`(?is)\bINTO\b`),
	"REPLACE":  lazyre.New(`(?is)^\s*REPLACE\s+INTO\b`),
	"UPDATE":   lazyre.New(`(?is)\bSET\b`),
	"DELETE":   lazyre.New(`(?is)^\s*DELETE\s+FROM\b`),
	"WITH":     lazyre.New(`(?is)\bAS\s*\(`),
	"MERGE":    lazyre.New(`(?is)^\s*MERGE\s+INTO\b`),
	"TRUNCATE": lazyre.New(`(?is)^\s*TRUNCATE\s+(?:TABLE\s+)?[A-Za-z_"` + "`" + `]`),
	"CREATE":   embedDDLRe,
	"ALTER":    embedDDLRe,
	"DROP":     embedDDLRe,
}

var embedDDLRe = lazyre.New(`(?is)^\s*\w+\s+(?:OR\s+REPLACE\s+)?(?:UNIQUE\s+|TEMP(?:ORARY)?\s+)?(?:TABLE|VIEW|INDEX|SCHEMA|DATABASE|FUNCTION|PROCEDURE|TRIGGER|SEQUENCE|TYPE|EXTENSION|MATERIALIZED)\b`)

// LooksLikeSQL says whether a string literal's text reads as a statement.
func LooksLikeSQL(s string) bool {
	m := embedStartRe.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	need := embedNeeds[strings.ToUpper(m[1])]
	return need == nil || need.MatchString(s)
}

// FindEmbeddedSQL lists the string literals of src (lang from EmbedLang)
// whose text reads as SQL. Comments are skipped; escapes are left as
// written (the text is for showing and jumping, not for running).
func FindEmbeddedSQL(src, lang string) []EmbeddedSQL {
	if lang == "" {
		return nil
	}
	var out []EmbeddedSQL
	add := func(at int, text string) {
		if !LooksLikeSQL(text) {
			return
		}
		head := src[:at]
		out = append(out, EmbeddedSQL{
			Line: strings.Count(head, "\n") + 1,
			Col:  at - (strings.LastIndexByte(head, '\n') + 1),
			Text: text,
			Kind: classifyStatement(text, "").Kind,
		})
	}
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case (lang == "go" || lang == "js") && strings.HasPrefix(src[i:], "//"),
			lang == "python" && c == '#':
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				return out
			}
			i += j
		case (lang == "go" || lang == "js") && strings.HasPrefix(src[i:], "/*"):
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				return out
			}
			i += 2 + j + 2
		case lang == "python" && (strings.HasPrefix(src[i:], `"""`) || strings.HasPrefix(src[i:], `'''`)):
			q := src[i : i+3]
			j := strings.Index(src[i+3:], q)
			if j < 0 {
				return out
			}
			add(i, src[i+3:i+3+j])
			i += 3 + j + 3
		case c == '`' && (lang == "go" || lang == "js"):
			end := closeQuote(src, i, '`', lang == "js")
			if end < 0 {
				return out
			}
			add(i, src[i+1:end])
			i = end + 1
		case c == '"' || c == '\'':
			end := closeQuote(src, i, c, true)
			if end < 0 { // not a string here (a regex, an apostrophe): go on
				i++
				continue
			}
			if c == '"' || lang != "go" { // a Go 'x' is a rune
				add(i, src[i+1:end])
			}
			i = end + 1
		default:
			i++
		}
	}
	return out
}

// closeQuote is the index of the quote that closes the one at open (a
// backslash escapes the next byte when esc); -1 = not closed. Only a
// backtick string may span lines.
func closeQuote(src string, open int, q byte, esc bool) int {
	for j := open + 1; j < len(src); j++ {
		switch src[j] {
		case '\\':
			if esc {
				j++
			}
		case '\n':
			if q != '`' {
				return -1
			}
		case q:
			return j
		}
	}
	return -1
}
