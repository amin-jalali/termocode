package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Table views (plan F9): the data view of a table or view is a result page
// whose rows come from the server one page at a time (ORDER BY, WHERE and
// LIMIT/OFFSET are sent to the server; nothing loads a whole table). Every
// identifier goes through the dialect's QuoteIdent; the user's WHERE text
// is checked by dbWhereCheck and by Classify before it is sent.

// dbTable names a table or view and the session that can reach it.
type dbTable struct {
	conn, driver, sessKey, schema, name string
	view                                bool
	path                                string // the tree node
}

// qualified is the quoted name used in SQL.
func (t dbTable) qualified() string {
	d := dbclient.DialectFor(t.driver)
	if d == nil {
		return ""
	}
	if t.schema != "" {
		return d.QuoteIdent(t.schema) + "." + d.QuoteIdent(t.name)
	}
	return d.QuoteIdent(t.name)
}

// label is the name for people: public.users, never quoted.
func (t dbTable) label() string {
	if t.schema != "" {
		return sanitizeDBText(t.schema) + "." + sanitizeDBText(t.name)
	}
	return sanitizeDBText(t.name)
}

// dbTableView is the state of one data view (kept across pages, sorts and
// filters; a new view starts from the tree).
type dbTableView struct {
	dbTable
	where    string
	orderCol string
	desc     bool
	offset   int
	good     int // the offset of the last page that loaded (a cancelled or failed page goes back to it)
	more     bool
	total    int        // -1 until # counts the rows
	keys     []string   // primary key columns (stable default order)
	unique   [][]string // unique indexes (a key for editing when there is no primary key)
	cols     []string   // column names (the only valid ORDER BY targets)
	size     int        // rows on a page, set when the view runs
	rowid    string     // SQLite table without a key: the rowid name used as the hidden row key ("" none)
	rowids   []string   // the rowid of each row of the page that loaded
}

// dbRowidAlias names the hidden rowid column of a data view's query.
const dbRowidAlias = "_termocode_rowid"

// dbSQLiteRowid is the name that reaches the rowid of a SQLite table, or ""
// when there is none: WITHOUT ROWID tables have no rowid, and a column called
// rowid, _rowid_ or oid hides the name of the same spelling.
func dbSQLiteRowid(cols []string, ddl string) string {
	if i := strings.LastIndex(ddl, ")"); i >= 0 && dbWithoutRowid.MatchString(ddl[i:]) {
		return ""
	}
next:
	for _, name := range []string{"rowid", "_rowid_", "oid"} {
		for _, c := range cols {
			if strings.EqualFold(c, name) {
				continue next
			}
		}
		return name
	}
	return ""
}

var dbWithoutRowid = lazyre.New(`(?i)\bWITHOUT\s+ROWID\b`)

func newDBTableView(t dbTable) *dbTableView { return &dbTableView{dbTable: t, total: -1} }

// selectSQL is the page query; limit is the number of rows to ask for.
func (tv *dbTableView) selectSQL(limit int) (string, error) {
	d := dbclient.DialectFor(tv.driver)
	if d == nil {
		return "", errors.New("this connection does not use SQL")
	}
	if err := dbWhereCheck(tv.driver, tv.where); err != nil {
		return "", err
	}
	var b strings.Builder
	if tv.rowid != "" {
		b.WriteString("SELECT *, " + d.QuoteIdent(tv.rowid) + " AS " + d.QuoteIdent(dbRowidAlias) + " FROM " + tv.qualified())
	} else {
		b.WriteString("SELECT * FROM " + tv.qualified())
	}
	if w := strings.TrimSpace(tv.where); w != "" {
		b.WriteString(" WHERE (" + w + ")")
	}
	var order []string
	if tv.orderCol != "" {
		if len(tv.cols) > 0 && !dbContains(tv.cols, tv.orderCol) {
			return "", errors.New("that column is not in the table")
		}
		col := d.QuoteIdent(tv.orderCol)
		if tv.desc {
			col += " DESC"
		}
		order = append(order, col)
	}
	for _, k := range tv.keys {
		if k != tv.orderCol {
			order = append(order, d.QuoteIdent(k))
		}
	}
	if tv.rowid != "" {
		order = append(order, d.QuoteIdent(tv.rowid)) // a stable order for paging
	}
	if len(order) > 0 {
		b.WriteString(" ORDER BY " + strings.Join(order, ", "))
	}
	b.WriteString(d.Limit(limit, max(tv.offset, 0)))
	sql := b.String()
	if err := dbReadOnlyCheck(tv.driver, sql); err != nil {
		return "", err
	}
	return sql, nil
}

// countSQL counts the rows the current WHERE keeps.
func (tv *dbTableView) countSQL() (string, error) {
	if dbclient.DialectFor(tv.driver) == nil {
		return "", errors.New("this connection does not use SQL")
	}
	if err := dbWhereCheck(tv.driver, tv.where); err != nil {
		return "", err
	}
	sql := "SELECT COUNT(*) FROM " + tv.qualified()
	if w := strings.TrimSpace(tv.where); w != "" {
		sql += " WHERE (" + w + ")"
	}
	if err := dbReadOnlyCheck(tv.driver, sql); err != nil {
		return "", err
	}
	return sql, nil
}

func dbContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// dbReadOnlyCheck is the last gate before a generated statement is sent:
// one statement, classified as a plain read for the connection's dialect.
func dbReadOnlyCheck(driver, sql string) error {
	kind, stmts := dbclient.Classify(sql, driver)
	if kind != dbclient.KindRead || len(stmts) != 1 {
		return errors.New("only a plain read is allowed here")
	}
	return nil
}

// dbWriteWords can never appear in a WHERE condition.
var dbWriteWords = map[string]bool{
	"INSERT": true, "UPDATE": true, "DELETE": true, "DROP": true, "ALTER": true, "TRUNCATE": true,
	"CREATE": true, "GRANT": true, "REVOKE": true, "MERGE": true, "CALL": true, "COPY": true, "INTO": true,
}

// dbWhereCheck refuses a WHERE text that is anything but a condition: no
// second statement, no comment (they could hide the closing text), no
// unbalanced bracket, no dollar quote, no write word. Quoted text is free.
func dbWhereCheck(driver, where string) error {
	w := strings.TrimSpace(where)
	if w == "" {
		return nil
	}
	mysql := dbclient.DialectFor(driver) == (dbclient.MySQLDialect{})
	_, pg := dbclient.DialectFor(driver).(dbclient.PostgresDialect)
	escapes := false // inside a string where a backslash escapes the next byte
	depth := 0
	var quote byte
	word := strings.Builder{}
	flush := func() error {
		if word.Len() > 0 {
			if dbWriteWords[strings.ToUpper(word.String())] {
				return fmt.Errorf("%s is not allowed in a filter", strings.ToUpper(word.String()))
			}
			word.Reset()
		}
		return nil
	}
	for i := 0; i < len(w); i++ {
		c := w[i]
		if quote != 0 {
			if c == '\\' && escapes {
				i++
				continue
			}
			if c == quote {
				if i+1 < len(w) && w[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		isWord := c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
		if isWord {
			word.WriteByte(c)
			continue
		}
		if err := flush(); err != nil {
			return err
		}
		switch c {
		case '\'', '"', '`':
			quote = c
			// MySQL strings, and PostgreSQL E'...' strings, honour backslash.
			escapes = c != '`' && (mysql || pg && c == '\'' && i > 0 && (w[i-1] == 'E' || w[i-1] == 'e') && (i < 2 || !(dbIsWordByte(w[i-2]) || w[i-2] == '$')))
		case ';':
			return errors.New("one condition only: remove the ;")
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return errors.New("a ) has no matching (")
			}
		case '-':
			if i+1 < len(w) && w[i+1] == '-' {
				return errors.New("comments are not allowed in a filter")
			}
		case '/':
			if i+1 < len(w) && w[i+1] == '*' {
				return errors.New("comments are not allowed in a filter")
			}
		case '#':
			if mysql {
				return errors.New("comments are not allowed in a filter")
			}
		case '$':
			if i+1 < len(w) && (w[i+1] == '$' || w[i+1] == '_' || w[i+1] >= 'a' && w[i+1] <= 'z' || w[i+1] >= 'A' && w[i+1] <= 'Z') {
				return errors.New("dollar quotes are not allowed in a filter")
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	if quote != 0 {
		return errors.New("a quote is not closed")
	}
	if depth != 0 {
		return errors.New("a ( is not closed")
	}
	return nil
}

// finish trims the extra row that tells whether a next page exists and
// gives an empty page its column names (SQLite prints nothing for zero
// rows).
func (tv *dbTableView) finish(res *dbclient.Result, size int) {
	tv.size = size
	tv.more = false
	if res.Err != nil {
		tv.offset = tv.good
		return
	}
	tv.good = tv.offset
	tv.rowids = nil
	if n := len(res.Columns); tv.rowid != "" && n > 0 && res.Columns[n-1].Name == dbRowidAlias {
		for i, row := range res.Rows {
			id := ""
			if len(row) == n {
				id = row[n-1].Text
				res.Rows[i] = row[:n-1]
			}
			tv.rowids = append(tv.rowids, id)
		}
		res.Columns = res.Columns[:n-1]
	}
	if len(res.Columns) == 0 && len(res.Rows) == 0 {
		for _, c := range tv.cols {
			res.Columns = append(res.Columns, dbclient.Column{Name: c})
		}
	}
	if len(res.Rows) > size {
		res.Rows = res.Rows[:size]
		tv.more = true
	}
	res.Truncated = false
}

func dbIsWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
