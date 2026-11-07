package dbclient

import (
	"strings"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// SQL language helpers (DBX-41): the dialect of a SQL file and its
// statement outline. Pure text work, no connection.

var dialectLineRe = lazyre.New(`(?im)^[ \t]*--[ \t]*@dialect[ \t]+(\S+)`)

// dialectHints are words that only one dialect uses, matched on the code
// with strings and comments blanked out.
var dialectHints = []struct {
	d  SQLDialect
	re *lazyre.Regexp
}{
	{DialectMySQL, lazyre.New(`(?i)\bAUTO_INCREMENT\b`)},
	{DialectMySQL, lazyre.New(`(?i)\bENGINE\s*=`)},
	{DialectMySQL, lazyre.New("`")},
	{DialectMySQL, lazyre.New(`(?i)\bUNSIGNED\b`)},
	{DialectPostgres, lazyre.New(`(?i)\b(BIG|SMALL)?SERIAL\b`)},
	{DialectPostgres, lazyre.New(`::`)},
	{DialectPostgres, lazyre.New(`(?i)\bILIKE\b`)},
	{DialectPostgres, lazyre.New(`(?i)\bRETURNING\b`)},
	{DialectPostgres, lazyre.New(`\$\$`)},
	{DialectPostgres, lazyre.New(`(?i)\bJSONB\b`)},
	{DialectSQLite, lazyre.New(`(?i)\bPRAGMA\b`)},
	{DialectSQLite, lazyre.New(`(?i)\bAUTOINCREMENT\b`)},
	{DialectSQLite, lazyre.New(`(?i)\bWITHOUT\s+ROWID\b`)},
}

// DialectByName maps a dialect or driver name to a SQLDialect.
func DialectByName(name string) (SQLDialect, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "postgres", "postgresql", "pg", "pgsql":
		return DialectPostgres, true
	case "mysql", "mariadb":
		return DialectMySQL, true
	case "sqlite", "sqlite3":
		return DialectSQLite, true
	}
	return "", false
}

// DetectDialect reads a `-- @dialect name` line, or else counts dialect
// hints in the code (strings and comments do not count). ok is false when
// no dialect clearly wins.
func DetectDialect(src string) (SQLDialect, bool) {
	if m := dialectLineRe.FindStringSubmatch(src); m != nil {
		return DialectByName(m[1])
	}
	code := blankLiterals(src)
	score := map[SQLDialect]int{}
	for _, h := range dialectHints {
		if h.re.MatchString(code) {
			score[h.d]++
		}
	}
	best, top, tie := SQLDialect(""), 0, false
	for _, d := range []SQLDialect{DialectPostgres, DialectMySQL, DialectSQLite} {
		switch n := score[d]; {
		case n > top:
			best, top, tie = d, n, false
		case n == top && n > 0:
			tie = true
		}
	}
	if top == 0 || tie {
		return "", false
	}
	return best, true
}

// blankLiterals replaces '…' strings and -- / /* */ comments with spaces.
func blankLiterals(src string) string {
	b := []byte(src)
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '\'':
			j := i + 1
			for j < len(b) {
				if b[j] == '\'' {
					if j+1 < len(b) && b[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			for k := i; k < len(b) && k <= j; k++ {
				b[k] = ' '
			}
			i = j
		case b[i] == '-' && i+1 < len(b) && b[i+1] == '-':
			for i < len(b) && b[i] != '\n' {
				b[i] = ' '
				i++
			}
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			end := strings.Index(string(b[i+2:]), "*/")
			stop := len(b)
			if end >= 0 {
				stop = i + 2 + end + 2
			}
			for k := i; k < stop; k++ {
				if b[k] != '\n' {
					b[k] = ' '
				}
			}
			i = stop - 1
		}
	}
	return string(b)
}

// OutlineItem is one statement of a SQL file.
type OutlineItem struct {
	Line  int // 1-based line of the statement's first word
	Kind  Kind
	Label string
}

const outlineLabelMax = 60

// Outline lists the statements of src. A file that does not split (an
// open quote) has no outline.
func Outline(src, dialect string) []OutlineItem {
	parts, err := Split(src, dialect)
	if err != nil {
		return nil
	}
	d := dialectArg([]string{dialect})
	var out []OutlineItem
	from := 0
	for _, p := range parts {
		at := strings.Index(src[from:], p)
		if at < 0 {
			continue
		}
		at += from
		from = at + len(p)
		body := p[skipLeadingComments(p):]
		start := at + len(p) - len(body)
		st := classifyStatement(p, d)
		label := body
		if st.Kind == KindDDL {
			label, _, _ = strings.Cut(label, "\n")
		}
		out = append(out, OutlineItem{
			Line:  strings.Count(src[:start], "\n") + 1,
			Kind:  st.Kind,
			Label: shortLabel(label),
		})
	}
	return out
}

// skipLeadingComments returns the offset of the first code byte of s.
func skipLeadingComments(s string) int {
	i := 0
	for i < len(s) {
		switch {
		case s[i] == ' ' || s[i] == '\t' || s[i] == '\r' || s[i] == '\n':
			i++
		case strings.HasPrefix(s[i:], "--"):
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				return len(s)
			}
			i += j + 1
		case strings.HasPrefix(s[i:], "/*"):
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				return len(s)
			}
			i += 2 + j + 2
		default:
			return i
		}
	}
	return i
}

func shortLabel(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > outlineLabelMax {
		s = string(r[:outlineLabelMax-1]) + "…"
	}
	return s
}
