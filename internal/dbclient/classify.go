package dbclient

import (
	"errors"
	"regexp"
	"strings"
)

type Kind string

type SQLDialect string

const (
	DialectPostgres SQLDialect = "postgres"
	DialectMySQL    SQLDialect = "mysql"
	DialectSQLite   SQLDialect = "sqlite"
)

const (
	KindRead    Kind = "read"
	KindWrite   Kind = "write"
	KindDDL     Kind = "ddl"
	KindUnknown Kind = "unknown"
)

type Statement struct {
	SQL      string
	Kind     Kind
	HasWhere bool
	Table    string
}

var simpleSelectTable = regexp.MustCompile(`(?i)\bFROM\s+([\w."` + "`" + `]+)`)

// Classify is a conservative hint. The read-only session and a statement
// timeout are the real guard; callers must not use this result as a security
// boundary. Unknown dialects and syntax are treated as writes when uncertain.
func Classify(sql string, dialectName string) (Kind, []Statement) {
	dialect := dialectArg([]string{dialectName})
	sqls, splitErr := Split(sql, string(dialect))
	if splitErr != nil {
		return KindWrite, []Statement{{SQL: strings.TrimSpace(sql), Kind: KindWrite}}
	}
	statements := make([]Statement, len(sqls))
	worst := KindRead
	for i, text := range sqls {
		st := classifyStatement(text, dialect)
		statements[i] = st
		if severity(st.Kind) > severity(worst) {
			worst = st.Kind
		}
	}
	if len(statements) == 0 {
		return KindUnknown, statements
	}
	// MySQL and MariaDB executable comments are part of the SQL program. The
	// tokenizer strips comments, so never let a read-only caller classify them
	// after that stripping has hidden executable tokens.
	if dialect == DialectMySQL && hasExecComment(sql) {
		for i := range statements {
			statements[i].Kind = KindWrite
		}
		return KindWrite, statements
	}
	if dialect == "" && (strings.Contains(sql, "#") || hasExecComment(sql) || strings.Contains(sql, "\\'") || hasAmbiguousDash(sql)) {
		return KindWrite, statements
	}
	if hasExecComment(sql) {
		return KindWrite, statements
	}
	return worst, statements
}

func hasExecComment(sql string) bool {
	return strings.Contains(sql, "/*!") || strings.Contains(sql, "/*M!")
}

func hasAmbiguousDash(sql string) bool {
	for i := 0; i+1 < len(sql); i++ {
		if sql[i] == '-' && sql[i+1] == '-' && i+2 < len(sql) && sql[i+2] > ' ' {
			return true
		}
	}
	return false
}

func dialectArg(dialects []string) SQLDialect {
	if len(dialects) == 0 {
		return ""
	}
	switch strings.ToLower(dialects[0]) {
	case "postgres", "postgresql", "pgx":
		return DialectPostgres
	case "mysql", "mariadb":
		return DialectMySQL
	case "sqlite", "sqlite3":
		return DialectSQLite
	default:
		return ""
	}
}

func classifyStatement(sql string, dialect SQLDialect) Statement {
	words := tokenWordsDialect(sql, dialect)
	st := Statement{SQL: strings.TrimSpace(sql), Kind: KindUnknown}
	if len(words) == 0 {
		return st
	}
	for _, w := range words {
		if w == "WHERE" {
			st.HasWhere = true
		}
	}
	first := words[0]
	switch first {
	case "SELECT", "SHOW", "DESCRIBE", "DESC", "VALUES", "TABLE", "EXPLAIN", "PRAGMA":
		st.Kind = KindRead
	case "ANALYZE":
		if len(words) > 1 && (words[1] == "SELECT" || words[1] == "WITH") {
			st.Kind = KindRead
		}
	case "WITH":
		st.Kind = classifyWith(words)
	case "INSERT", "UPDATE", "DELETE", "MERGE", "REPLACE", "COPY", "CALL", "DO", "LOCK":
		st.Kind = KindWrite
	case "CREATE", "ALTER", "DROP", "TRUNCATE", "GRANT", "REVOKE", "VACUUM", "REINDEX", "ATTACH":
		st.Kind = KindDDL
	default:
		// Unknown statements are treated as writes by the gate.
		st.Kind = KindWrite
	}
	if st.Kind == KindUnknown {
		st.Kind = KindWrite
	}
	if first == "EXPLAIN" {
		for _, w := range words[1:] {
			if w == "ANALYZE" || w == "ANALYSE" {
				st.Kind = KindWrite
				break
			}
		}
	}
	if first == "PRAGMA" && !pragmaReadOnly(words) {
		st.Kind = KindWrite
	}
	if dialect == DialectSQLite && sqliteUnsafeWords(words) {
		st.Kind = KindWrite
	}
	if first == "SELECT" || first == "WITH" || first == "TABLE" || first == "VALUES" {
		for i, w := range words {
			if w == "INTO" {
				st.Kind = KindWrite
			}
			if (w == "FOR" && i+1 < len(words) && (words[i+1] == "UPDATE" || words[i+1] == "SHARE" || words[i+1] == "NO" || words[i+1] == "KEY")) || (w == "LOCK" && i+3 < len(words) && words[i+1] == "IN" && words[i+2] == "SHARE" && words[i+3] == "MODE") {
				st.Kind = KindWrite
			}
		}
		if dialect == DialectMySQL && containsSequence(words, "INTO") {
			st.Kind = KindWrite
		}
		for i, w := range words {
			if i+1 < len(words) && words[i+1] == "(" && (sideEffectFunction(w) || sideEffectPrefix(w)) {
				st.Kind = KindWrite
			}
		}
	}
	// COPY can write to server files or execute a server-side program; refusing
	// all COPY forms in read-only sessions also prevents superuser escapes.
	if first == "COPY" {
		st.Kind = KindWrite
	}
	if first == "PREPARE" || first == "EXECUTE" || first == "DEALLOCATE" || first == "SET" || first == "RESET" || first == "DISCARD" || first == "LOCK" || first == "UNLOCK" {
		st.Kind = KindWrite
	}
	if first == "SELECT" || first == "WITH" {
		for _, w := range words {
			if w == "GET_LOCK" || w == "RELEASE_LOCK" || w == "RELEASE_ALL_LOCKS" || w == "IS_FREE_LOCK" || w == "IS_USED_LOCK" {
				st.Kind = KindWrite
			}
		}
	}
	if first == "SELECT" {
		if m := simpleSelectTable.FindStringSubmatch(stripSQLForTable(sql, dialect)); len(m) > 1 {
			st.Table = strings.Trim(m[1], "\"`")
		}
	}
	return st
}

func sqliteUnsafeWords(words []string) bool {
	for i, word := range words {
		switch word {
		case "ATTACH", "WRITABLE_SCHEMA":
			return true
		case "INTO":
			if containsSequence(words[:i], "VACUUM") {
				return true
			}
		case "TEMP", "TEMPORARY":
			if i > 0 && words[i-1] == "CREATE" {
				return true
			}
		}
		if i+1 < len(words) && words[i+1] == "(" && sqliteUnsafeFunction(word) {
			return true
		}
	}
	return false
}

func sqliteUnsafeFunction(word string) bool {
	if sideEffectFunction(word) {
		return true
	}
	for _, prefix := range []string{"SQLAR_", "SHELL_"} {
		if strings.HasPrefix(word, prefix) {
			return true
		}
	}
	return word == "ZIPFILE" || word == "LSMODE"
}

func severity(k Kind) int {
	switch k {
	case KindRead:
		return 0
	case KindWrite, KindUnknown:
		return 1
	case KindDDL:
		return 2
	default:
		return 1
	}
}

func classifyWith(words []string) Kind {
	depth := 0
	seenCTE := false
	for _, w := range words[1:] {
		switch w {
		case "(":
			depth++
		case ")":
			if depth > 0 {
				depth--
				if depth == 0 {
					seenCTE = true
				}
			}
		case "SELECT", "INSERT", "UPDATE", "DELETE", "MERGE":
			if depth > 0 && w != "SELECT" {
				return KindWrite
			}
			if depth == 0 && seenCTE {
				if w == "SELECT" {
					return KindRead
				}
				return KindWrite
			}
		case "CREATE", "ALTER", "DROP", "TRUNCATE", "GRANT", "REVOKE":
			if depth == 0 && seenCTE {
				return KindDDL
			}
		}
	}
	return KindUnknown
}

func pragmaReadOnly(words []string) bool {
	if len(words) < 2 {
		return false
	}
	// These pragmas expose connection metadata without changing database state.
	switch words[1] {
	case "TABLE_INFO", "TABLE_XINFO", "INDEX_LIST", "INDEX_INFO", "INDEX_XINFO", "FOREIGN_KEY_LIST":
		return len(words) > 3 && words[2] == "(" && words[len(words)-1] == ")"
	case "DATABASE_LIST", "COMPILE_OPTIONS", "PAGE_COUNT", "FREELIST_COUNT":
		return len(words) == 2
	default:
		return false
	}
}

var sideEffectFunctions = map[string]bool{
	"PG_TERMINATE_BACKEND": true, "PG_CANCEL_BACKEND": true, "SETVAL": true,
	"DBLINK_EXEC": true, "DBLINK_SEND_QUERY": true, "LO_IMPORT": true,
	"LO_EXPORT": true, "PG_ADVISORY_LOCK": true, "PG_ADVISORY_XACT_LOCK": true,
	"EDIT": true, "WRITEFILE": true, "READFILE": true, "FSDIR": true, "FTS3_TOKENIZER": true, "LOAD_EXTENSION": true,
	"ZIPFILE": true, "LSMODE": true,
	"TXID_CURRENT": true, "PG_CURRENT_XACT_ID": true, "PG_LOGICAL_EMIT_MESSAGE": true,
	"BENCHMARK": true, "LOAD_FILE": true,
}

func sideEffectFunction(word string) bool { return sideEffectFunctions[word] }

func sideEffectPrefix(word string) bool {
	for _, prefix := range []string{"DBLINK", "PG_ADVISORY_", "PG_TRY_ADVISORY_", "PG_SLEEP", "LO_", "PG_NOTIFY", "PG_STAT_RESET", "PG_STAT_STATEMENTS_RESET"} {
		if strings.HasPrefix(word, prefix) {
			return true
		}
	}
	switch word {
	case "PG_RELOAD_CONF", "PG_ROTATE_LOGFILE", "PG_SWITCH_WAL", "PG_CREATE_RESTORE_POINT", "NEXTVAL", "SETVAL", "SET_CONFIG", "PG_READ_FILE", "PG_READ_BINARY_FILE", "PG_LS_DIR", "LOAD_FILE", "TXID_CURRENT", "PG_CURRENT_XACT_ID", "PG_LOGICAL_EMIT_MESSAGE", "BENCHMARK":
		return true
	}
	return false
}

func containsSequence(words []string, target string) bool {
	for _, word := range words {
		if word == target {
			return true
		}
	}
	return false
}

// tokenWords removes comments and string contents while retaining quoted
// identifiers and punctuation needed to recognize statement structure.
func tokenWords(s string) []string {
	return tokenWordsDialect(s, "")
}

func tokenWordsDialect(s string, dialect SQLDialect) []string {
	clean := stripSQLLiteralsDialect(s, dialect)
	var words []string
	for i := 0; i < len(clean); {
		c := clean[i]
		if isWordByte(c) {
			j := i + 1
			for j < len(clean) && isWordByte(clean[j]) {
				j++
			}
			words = append(words, strings.ToUpper(clean[i:j]))
			i = j
			continue
		}
		if c == '(' || c == ')' {
			words = append(words, string(c))
		}
		i++
	}
	return words
}

func isWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func stripSQLLiterals(sql string) string { return stripSQLLiteralsDialect(sql, "") }

func stripSQLLiteralsDialect(sql string, dialect SQLDialect) string {
	b := []byte(sql)
	for i := 0; i < len(b); {
		if i+1 < len(b) && b[i] == '-' && b[i+1] == '-' && (dialect != DialectMySQL || i+2 == len(b) || b[i+2] <= ' ') {
			for i < len(b) && b[i] != '\n' {
				b[i] = ' '
				i++
			}
			continue
		}
		if b[i] == '#' && dialect == DialectMySQL {
			for i < len(b) && b[i] != '\n' {
				b[i] = ' '
				i++
			}
			continue
		}
		if i+1 < len(b) && b[i] == '/' && b[i+1] == '*' {
			b[i], b[i+1] = ' ', ' '
			i += 2
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				if b[i] != '\n' {
					b[i] = ' '
				}
				i++
			}
			if i+1 < len(b) {
				b[i], b[i+1] = ' ', ' '
				i += 2
			}
			continue
		}
		if b[i] == '$' {
			if tag, end := dollarTag(b, i, dialect); tag != "" {
				close := strings.Index(string(b[end:]), tag)
				if close >= 0 {
					for j := i; j < end+close+len(tag); j++ {
						if b[j] != '\n' {
							b[j] = ' '
						}
					}
					i = end + close + len(tag)
					continue
				}
			}
		}
		if b[i] == '\'' {
			closed := false
			escapes := dialect == DialectMySQL || (dialect == DialectPostgres && isEStringPrefix(b, i))
			for j := i + 1; j < len(b); j++ {
				if b[j] == '\\' && j+1 < len(b) && escapes {
					b[j], b[j+1] = ' ', ' '
					j++
					continue
				}
				if b[j] == '\'' && j+1 < len(b) && b[j+1] == '\'' {
					b[j], b[j+1] = ' ', ' '
					j++
					continue
				}
				if b[j] == '\'' {
					b[i], b[j] = ' ', ' '
					i = j + 1
					closed = true
					break
				}
				if b[j] != '\n' {
					b[j] = ' '
				}
			}
			if !closed {
				i = len(b)
			}
			continue
		}
		if b[i] == '"' || b[i] == '`' {
			q := b[i]
			b[i] = ' '
			closed := false
			for j := i + 1; j < len(b); j++ {
				if b[j] == q && j+1 < len(b) && b[j+1] == q {
					b[j], b[j+1] = ' ', ' '
					j++
					continue
				}
				if b[j] == q {
					b[j] = ' '
					i = j + 1
					closed = true
					break
				}
			}
			if !closed {
				i = len(b)
			}
			continue
		}
		i++
	}
	return string(b)
}

func stripSQLForTable(sql string, dialect SQLDialect) string {
	b := []byte(sql)
	for i := 0; i < len(b); {
		if i+1 < len(b) && b[i] == '-' && b[i+1] == '-' {
			for i < len(b) && b[i] != '\n' {
				b[i] = ' '
				i++
			}
			continue
		}
		if b[i] == '#' {
			for i < len(b) && b[i] != '\n' {
				b[i] = ' '
				i++
			}
			continue
		}
		if i+1 < len(b) && b[i] == '/' && b[i+1] == '*' {
			for i+1 < len(b) && !(b[i] == '*' && b[i+1] == '/') {
				if b[i] != '\n' {
					b[i] = ' '
				}
				i++
			}
			i += 2
			continue
		}
		if b[i] == '$' {
			if tag, end := dollarTag(b, i, dialect); tag != "" {
				close := strings.Index(string(b[end:]), tag)
				if close >= 0 {
					for j := i; j < end+close+len(tag); j++ {
						if b[j] != '\n' {
							b[j] = ' '
						}
					}
					i = end + close + len(tag)
					continue
				}
			}
		}
		if b[i] == '\'' {
			closed := false
			for j := i; j < len(b); j++ {
				if b[j] == '\\' && j+1 < len(b) {
					b[j], b[j+1] = ' ', ' '
					j++
					continue
				}
				if b[j] == '\'' && j+1 < len(b) && b[j+1] == '\'' {
					b[j], b[j+1] = ' ', ' '
					j++
					continue
				}
				if b[j] == '\'' {
					b[i], b[j] = ' ', ' '
					i = j + 1
					closed = true
					break
				}
				if b[j] != '\n' {
					b[j] = ' '
				}
			}
			if !closed {
				i = len(b)
			}
			continue
		}
		if b[i] == '"' || b[i] == '`' {
			q := b[i]
			closed := false
			for j := i + 1; j < len(b); j++ {
				if b[j] == q && j+1 < len(b) && b[j+1] == q {
					j++
					continue
				}
				if b[j] == q {
					i = j + 1
					closed = true
					break
				}
			}
			if !closed {
				i = len(b)
			}
			continue
		}
		i++
	}
	return string(b)
}

// isEStringPrefix reports whether the quote at i starts a Postgres E” string:
// the E must not be the tail of a longer word.
func isEStringPrefix(b []byte, i int) bool {
	if i == 0 || (b[i-1] != 'E' && b[i-1] != 'e') {
		return false
	}
	return i < 2 || !(isWordByte(b[i-2]) || b[i-2] == '$')
}

func dollarTag(b []byte, i int, dialect SQLDialect) (string, int) {
	if dialect != "" && dialect != DialectPostgres {
		return "", i
	}
	if i > 0 && (isWordByte(b[i-1]) || b[i-1] == '$') {
		return "", i
	}
	j := i + 1
	for j < len(b) && (b[j] == '_' || b[j] >= 'a' && b[j] <= 'z' || b[j] >= 'A' && b[j] <= 'Z' || b[j] >= '0' && b[j] <= '9') {
		j++
	}
	if j < len(b) && b[j] == '$' {
		return string(b[i : j+1]), j + 1
	}
	return "", i
}

func Split(sql string, dialectName string) ([]string, error) {
	dialect := dialectArg([]string{dialectName})
	var out []string
	start := 0
	quote := byte(0)
	quoteEscape := false
	dollar := ""
	lineStart := true
	delimiter := ";"
	blockDepth := 0
	for i := 0; i < len(sql); i++ {
		if lineStart {
			j := i
			for j < len(sql) && (sql[j] == ' ' || sql[j] == '\t') {
				j++
			}
			if strings.HasPrefix(strings.ToUpper(sql[j:]), "DELIMITER ") {
				k := strings.IndexByte(sql[j:], '\n')
				if k < 0 {
					k = len(sql) - j
				}
				delimiter = strings.TrimSpace(sql[j+10 : j+k])
				start = j + k
				if start < len(sql) {
					start++
				}
				i = start - 1
				lineStart = true
				continue
			}
		}
		c := sql[i]
		if c == '\n' {
			lineStart = true
		} else if c != ' ' && c != '\t' && c != '\r' {
			lineStart = false
		}
		if quote == 0 && dollar == "" {
			if i+1 < len(sql) && c == '-' && sql[i+1] == '-' && (dialect != DialectMySQL || i+2 == len(sql) || sql[i+2] <= ' ') {
				for i+1 < len(sql) && sql[i+1] != '\n' {
					i++
				}
				continue
			}
			if c == '#' && dialect == DialectMySQL {
				for i+1 < len(sql) && sql[i+1] != '\n' {
					i++
				}
				continue
			}
			if i+1 < len(sql) && c == '/' && sql[i+1] == '*' {
				blockDepth++
				i++
				continue
			}
			if blockDepth > 0 {
				if i+1 < len(sql) && c == '/' && sql[i+1] == '*' {
					blockDepth++
					i++
				} else if i+1 < len(sql) && c == '*' && sql[i+1] == '/' {
					blockDepth--
					i++
				}
				continue
			}
		}
		if quote == 0 && dollar == "" && strings.HasPrefix(sql[i:], delimiter) {
			part := strings.TrimSpace(sql[start:i])
			if dialect == DialectSQLite && incompleteSQLiteTrigger(part) {
				i += len(delimiter) - 1
				continue
			}
			if len(tokenWordsDialect(part, dialect)) > 0 && !strings.HasPrefix(strings.ToUpper(part), "DELIMITER ") {
				out = append(out, part)
			}
			i += len(delimiter) - 1
			start = i + 1
			continue
		}
		if dollar != "" {
			if strings.HasPrefix(sql[i:], dollar) {
				i += len(dollar) - 1
				dollar = ""
			}
			continue
		}
		if quote != 0 {
			closeQuote := c == quote
			if quote == '[' {
				closeQuote = c == ']'
			}
			if closeQuote {
				if i+1 < len(sql) && sql[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			if c == '\\' && quote != '`' && i+1 < len(sql) && quoteEscape {
				i++
			}
			continue
		}
		if c == '\'' || c == '"' || c == '`' || (c == '[' && dialect == DialectSQLite) {
			quote = c
			quoteEscape = dialect == "" || dialect == DialectMySQL || (dialect == DialectPostgres && isEStringPrefix([]byte(sql), i))
			continue
		}
		if c == '$' {
			if tag, end := dollarTag([]byte(sql), i, dialect); tag != "" {
				dollar = tag
				i = end - 1
			}
		}
	}
	if quote != 0 || dollar != "" {
		return nil, errors.New("SQL statement is incomplete: an opening quote or identifier is not closed")
	}
	if blockDepth > 0 {
		return nil, errors.New("SQL statement is incomplete: a block comment is not closed")
	}
	part := strings.TrimSpace(sql[start:])
	if len(tokenWordsDialect(part, dialect)) > 0 && !strings.HasPrefix(strings.ToUpper(part), "DELIMITER ") {
		if dialect == DialectSQLite && incompleteSQLiteTrigger(part) {
			return nil, errors.New("SQL statement is incomplete: trigger body needs END")
		}
		out = append(out, part)
	}
	return out, nil
}

func incompleteSQLiteTrigger(sql string) bool {
	clean := []byte(stripSQLLiteralsDialect(sql, DialectSQLite))
	// stripSQLLiterals does not know [bracket] identifiers.
	for i := 0; i < len(clean); i++ {
		if clean[i] == '[' {
			j := i + 1
			for j < len(clean) && clean[j] != ']' {
				j++
			}
			for k := i; k <= j && k < len(clean); k++ {
				clean[k] = ' '
			}
			i = j
		}
	}
	var toks []string
	for i := 0; i < len(clean); {
		c := clean[i]
		if isWordByte(c) {
			j := i + 1
			for j < len(clean) && isWordByte(clean[j]) {
				j++
			}
			toks = append(toks, strings.ToUpper(string(clean[i:j])))
			i = j
			continue
		}
		if c == ';' {
			toks = append(toks, ";")
		}
		i++
	}
	if len(toks) < 2 || toks[0] != "CREATE" || !containsSequence(toks, "TRIGGER") {
		return false
	}
	begin := -1
	for i, w := range toks {
		if w == "BEGIN" {
			begin = i
			break
		}
	}
	if begin < 0 {
		return false
	}
	depth := 0
	for i := begin + 1; i < len(toks); i++ {
		switch toks[i] {
		case "CASE":
			depth++
		case "END":
			if depth > 0 {
				depth--
			} else if toks[i-1] == ";" || i-1 == begin {
				return false
			}
		}
	}
	return true
}

// twinBlockedIdentifier reports the first helper name that the read-only
// twin must not see anywhere as an identifier, bare or quoted, including as
// a table-valued function (FROM fsdir) or schema-qualified.
func twinBlockedIdentifier(sql string) string {
	b := []byte(sql)
	check := func(name string) string {
		up := strings.ToUpper(name)
		if twinBlockedWords[up] || strings.HasPrefix(up, "SHELL_") {
			return up
		}
		return ""
	}
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == '-' && i+1 < len(b) && b[i+1] == '-':
			for i < len(b) && b[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(b) && b[i+1] == '*':
			i += 2
			for i < len(b) && !(b[i] == '*' && i+1 < len(b) && b[i+1] == '/') {
				i++
			}
			i += 2
		case c == '\'':
			i++
			for i < len(b) {
				if b[i] == '\'' {
					if i+1 < len(b) && b[i+1] == '\'' {
						i += 2
						continue
					}
					break
				}
				i++
			}
			i++
		case c == '"' || c == '`' || c == '[':
			q := c
			if c == '[' {
				q = ']'
			}
			i++
			var name []byte
			for i < len(b) {
				if b[i] == q {
					if q != ']' && i+1 < len(b) && b[i+1] == q {
						name = append(name, q)
						i += 2
						continue
					}
					break
				}
				name = append(name, b[i])
				i++
			}
			i++
			if w := check(strings.TrimSpace(string(name))); w != "" {
				return w
			}
		case isWordByte(c):
			j := i + 1
			for j < len(b) && isWordByte(b[j]) {
				j++
			}
			if w := check(string(b[i:j])); w != "" {
				return w
			}
			i = j
		default:
			i++
		}
	}
	return ""
}

var twinBlockedWords = map[string]bool{
	"FSDIR": true, "ZIPFILE": true, "SQLAR": true, "SQLAR_COMPRESS": true, "SQLAR_UNCOMPRESS": true,
	"LSMODE": true, "READFILE": true, "WRITEFILE": true, "EDIT": true, "LOAD_EXTENSION": true,
	"FTS3_TOKENIZER": true, "TEMP_STORE_DIRECTORY": true,
}
