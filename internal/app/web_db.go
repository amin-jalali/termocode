package app

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/toast"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Databases from the page (WEB-43, web design 2.9, T43): the
// connections, the tables of one and their rows, and a typed SQL
// statement. Everything reads through a read-only session of its own
// (the same door as the AI's db tools: BEGIN READ ONLY, or a sqlite3
// -readonly sidecar), never the user's session or transaction. A
// statement the classifier does not call a plain read goes to db.exec,
// which waits for a laptop card showing the connection and the exact
// SQL; a read-only connection refuses it. Passwords stay on the laptop:
// the connection list carries no host, user, URL or password, and every
// cell passes the database redaction, a cell equal to or holding the
// connection's password is masked, and so is every cell of a column
// whose name says it holds a secret.

func init() {
	webOps[webproto.OpDBConns] = (*Model).webDBConns
	webOps[webproto.OpDBTables] = (*Model).webDBTables
	webOps[webproto.OpDBRows] = (*Model).webDBRows
	webOps[webproto.OpDBQuery] = (*Model).webDBQuery
	webAskOps[webproto.OpDBExec] = (*Model).webDBExec
}

// webDBSQL reports a driver the web can browse.
func webDBSQL(driver string) bool {
	switch driver {
	case "postgres", "mysql", "sqlite":
		return true
	}
	return false
}

// webDBDatabase is the database name of c, never its host or user.
func webDBDatabase(c dbclient.Conn) string {
	if c.Driver == "sqlite" {
		return filepath.Base(c.Path)
	}
	if c.URL != "" {
		if p, err := dbclient.ParseURL(c.URL); err == nil {
			return p.Database
		}
	}
	return c.Database
}

func (m *Model) webDBConns(call *webCall) tea.Cmd {
	root, err := m.rootDir()
	if err != nil {
		call.fail(webproto.CodeNotFound, "No workspace is open.")
		return nil
	}
	call.later()
	return func() tea.Msg {
		_, _, list := dbLoadConfigs(root)
		out := webproto.DBConns{Conns: []webproto.DBConn{}}
		for i, c := range list {
			if i == webproto.MaxDBConns {
				break
			}
			d := c.Driver
			switch d {
			case "postgres", "mysql", "sqlite", "redis":
			default:
				d = "other"
			}
			it := webproto.DBConn{ID: c.ID, Driver: d, Database: webDBDatabase(c), Env: c.Environment, ReadOnly: c.ReadOnly,
				SQL: webDBSQL(c.Driver) && c.Problem == ""}
			if n := dbConnName(c); n != c.ID {
				it.Name = n
			}
			if c.Problem != "" {
				it.Problem = dbFriendlyProblem(c.Problem)
			}
			out.Conns = append(out.Conns, it)
		}
		call.reply(out)
		return nil
	}
}

// webDBConn is a SQL connection by id, with what opening a read-only
// session needs from the Model (read on Update).
type webDBConn struct {
	root string
	c    dbclient.Conn
	snap dbAISnapshot
}

func (m *Model) webDBFind(call *webCall, id string) (webDBConn, bool) {
	root, err := m.rootDir()
	if err != nil {
		call.fail(webproto.CodeNotFound, "No workspace is open.")
		return webDBConn{}, false
	}
	c, err := dbAIConn(root, id)
	switch {
	case err != nil && c.ID == "":
		call.fail(webproto.CodeNotFound, "That connection does not exist.")
		return webDBConn{}, false
	case err != nil:
		call.fail(webproto.CodeConflict, "This connection cannot be read here. Open it on the laptop.")
		return webDBConn{}, false
	}
	out := webDBConn{root: root, c: c}
	if d := m.databases; d != nil {
		out.snap.password, out.snap.hasPW = d.passwords[id]
		out.snap.user = d.sessions[id]
		if c.SSH != nil {
			out.snap.dial, _ = d.dbDialFor(c)
		}
	}
	return out, true
}

// open is a read-only session of the connection (the caller closes it).
func (w *webDBConn) open(ctx context.Context) (dbclient.Session, error) {
	s, err := dbAIOpen(ctx, w.root, w.c, &w.snap, true)
	if err != nil {
		return nil, errors.New(dbAIClean(dbOneLine(err.Error()), w.snap.password))
	}
	return s, nil
}

// webDBSchema is the schema whose tables the page lists.
func webDBSchema(c dbclient.Conn) string {
	switch c.Driver {
	case "mysql":
		return webDBDatabase(c)
	case "sqlite":
		return "main"
	}
	return "public"
}

func (m *Model) webDBTables(call *webCall) tea.Cmd {
	w, ok := m.webDBFind(call, call.args.(*webproto.DBConnArgs).Conn)
	if !ok {
		return nil
	}
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, loadDBConsoleSettings().timeout())
		defer cancel()
		s, err := w.open(ctx)
		if err != nil {
			call.fail(webproto.CodeInternal, err.Error())
			return nil
		}
		defer s.Close()
		schema := webDBSchema(w.c)
		res, err := s.Introspect(ctx, dbclient.IntrospectionRequest{Schema: schema})
		if err != nil {
			call.fail(webproto.CodeInternal, dbAIError(err, w.snap.password).Error())
			return nil
		}
		out := webproto.DBTables{Conn: w.c.ID, Schema: schema, Tables: []webproto.DBTable{}}
		names := map[string]bool{}
		for i, o := range res.Objects {
			if i == webproto.MaxDBTables {
				out.More = len(res.Objects) - i
				break
			}
			names[o.Name] = true
			out.Tables = append(out.Tables, webproto.DBTable{Name: o.Name, View: strings.Contains(strings.ToLower(o.Kind), "view")})
		}
		return applyMsg(func(m *Model) tea.Cmd {
			if m.web != nil {
				if m.web.more.dbTables == nil {
					m.web.more.dbTables = map[string]map[string]bool{}
				}
				m.web.more.dbTables[w.c.ID] = names
			}
			call.reply(out)
			return nil
		})
	}
}

func (m *Model) webDBRows(call *webCall) tea.Cmd {
	a := call.args.(*webproto.DBRowsArgs)
	w, ok := m.webDBFind(call, a.Conn)
	if !ok {
		return nil
	}
	if !m.web.more.dbTables[a.Conn][a.Table] {
		call.fail(webproto.CodeNotFound, "That table is not in the list. Open the list again.")
		return nil
	}
	d := dbAIDialect(w.c.Driver)
	from := d.QuoteIdent(a.Table)
	if w.c.Driver == "postgres" {
		from = d.QuoteIdent(webDBSchema(w.c)) + "." + from
	}
	sql := "SELECT * FROM " + from + d.Limit(webproto.DBPageRows+1, a.From)
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, loadDBConsoleSettings().timeout())
		defer cancel()
		s, err := w.open(ctx)
		if err != nil {
			call.fail(webproto.CodeInternal, err.Error())
			return nil
		}
		defer s.Close()
		r := s.Run(ctx, sql, dbclient.RunOptions{MaxRows: webproto.DBPageRows + 1})
		if r.Err != nil {
			call.fail(webproto.CodeInternal, dbAIError(r.Err, w.snap.password).Error())
			return nil
		}
		more := len(r.Rows) > webproto.DBPageRows
		if more {
			r.Rows = r.Rows[:webproto.DBPageRows]
		}
		out := webDBResult(r, w.snap.password)
		out.From = a.From
		if more && !out.Truncated {
			out.Next = a.From + webproto.DBPageRows
		}
		call.reply(out)
		return nil
	}
}

// webDBStatement is the one statement of sql and its kind.
func webDBStatement(c dbclient.Conn, sql string) (string, dbclient.Kind, error) {
	stmts, err := dbclient.Split(sql, c.Driver)
	if err != nil {
		return "", dbclient.KindUnknown, errors.New("Could not read the SQL.")
	}
	if len(stmts) != 1 {
		return "", dbclient.KindUnknown, errors.New("Send one SQL statement at a time.")
	}
	one := strings.TrimSpace(stmts[0])
	if dbAITxWords[strings.ToUpper(strings.Trim(firstSQLWord(dbStripComments(one)), ";"))] {
		return "", dbclient.KindUnknown, errors.New("Transaction statements are not sent: each statement commits by itself.")
	}
	kind, _ := dbclient.Classify(one, c.Driver)
	return one, kind, nil
}

func (m *Model) webDBQuery(call *webCall) tea.Cmd {
	a := call.args.(*webproto.DBSQLArgs)
	w, ok := m.webDBFind(call, a.Conn)
	if !ok {
		return nil
	}
	sql, kind, err := webDBStatement(w.c, a.SQL)
	if err != nil {
		call.fail(webproto.CodeBadRequest, err.Error())
		return nil
	}
	if kind != dbclient.KindRead {
		call.fail(webproto.CodeForbidden, "This statement may change data. It needs the laptop.")
		return nil
	}
	m.webAuditOp(call, w.c.ID+" "+strconv.Itoa(len(sql))+" bytes", "read")
	call.later()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(call.ctx, loadDBConsoleSettings().timeout())
		defer cancel()
		s, err := w.open(ctx)
		if err != nil {
			call.fail(webproto.CodeInternal, err.Error())
			return nil
		}
		defer s.Close()
		start := time.Now()
		r := s.Run(ctx, sql, dbclient.RunOptions{MaxRows: webproto.MaxDBRows})
		if r.Err != nil {
			call.fail(webproto.CodeInternal, dbAIError(r.Err, w.snap.password).Error())
			return nil
		}
		out := webDBResult(r, w.snap.password)
		out.Truncated = out.Truncated || r.Truncated
		out.Ms = time.Since(start).Milliseconds()
		call.reply(out)
		return nil
	}
}

// webDBExec asks the laptop to run a statement that may change data.
func (m *Model) webDBExec(call *webCall) *webAsk {
	a := call.args.(*webproto.DBSQLArgs)
	w, ok := m.webDBFind(call, a.Conn)
	if !ok {
		return nil
	}
	if w.c.ReadOnly {
		call.fail(webproto.CodeForbidden, "This connection is read-only.")
		return nil
	}
	sql, kind, err := webDBStatement(w.c, a.SQL)
	if err != nil {
		call.fail(webproto.CodeBadRequest, err.Error())
		return nil
	}
	if kind == dbclient.KindRead {
		call.fail(webproto.CodeBadRequest, "This is a read. Run it as a query.")
		return nil
	}
	where := w.c.Driver
	if db := webDBDatabase(w.c); db != "" {
		where += ", " + db
	}
	if w.c.Environment != "" {
		where += ", " + w.c.Environment
	}
	name := sanitizeDBText(dbConnName(w.c))
	return &webAsk{
		summary: w.c.ID + " " + strconv.Itoa(len(sql)) + " bytes",
		title:   "Run SQL?",
		body:    "Run on " + name + " (" + sanitizeDBText(where) + "):\n\n" + dbSafeMultiline(sql) + "\n\nIt may change data.",
		action:  "Run",
		run: func(m *Model) tea.Cmd {
			// A plain auto-commit session of its own, never the user's.
			fresh, ok := m.webDBFindQuiet(w.c.ID)
			if !ok {
				return m.pushToastDetail(toast.Warn, "Nothing ran", "The connection changed.")
			}
			return func() tea.Msg {
				ctx, cancel := context.WithTimeout(context.Background(), loadDBConsoleSettings().timeout())
				defer cancel()
				s, err := dbAIOpen(ctx, fresh.root, fresh.c, &fresh.snap, false)
				var r dbclient.Result
				if err == nil {
					r = s.Run(ctx, sql, dbclient.RunOptions{MaxRows: 1})
					_ = s.Close()
					err = r.Err
				}
				return applyMsg(func(m *Model) tea.Cmd {
					if err != nil {
						return m.pushToastDetail(toast.Errr, "SQL failed on "+name, dbAIError(err, fresh.snap.password).Error())
					}
					return m.pushToastDetail(toast.Info, "SQL ran on "+name, dbPlural(int(r.Affected), "row")+" changed")
				})
			}
		},
	}
}

// webDBFindQuiet is webDBFind without a call (the laptop confirmed).
func (m *Model) webDBFindQuiet(id string) (webDBConn, bool) {
	root, err := m.rootDir()
	if err != nil {
		return webDBConn{}, false
	}
	c, err := dbAIConn(root, id)
	if err != nil || c.ReadOnly {
		return webDBConn{}, false
	}
	out := webDBConn{root: root, c: c}
	if d := m.databases; d != nil {
		out.snap.password, out.snap.hasPW = d.passwords[id]
		if c.SSH != nil {
			out.snap.dial, _ = d.dbDialFor(c)
		}
	}
	return out, true
}

// webDBSecretWords mark a column whose values are secrets.
var webDBSecretWords = []string{"password", "passwd", "pwd", "secret", "token", "apikey", "credential", "credentials",
	"privatekey", "salt", "otp", "totp", "hash", "pin", "ssn", "cvv"}

// webDBSecretColumn reports a column name that says it holds a secret
// ("password", "api_key", "AccessToken", "secret_hash" …).
func webDBSecretColumn(name string) bool {
	n := strings.ToLower(name)
	flat := strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(n)
	for _, w := range []string{"apikey", "accesskey", "privatekey", "secretkey", "authkey", "clientsecret"} {
		if strings.Contains(flat, w) {
			return true
		}
	}
	words := strings.FieldsFunc(n, func(r rune) bool { return r == '_' || r == '-' || r == ' ' || r == '.' })
	if len(words) == 1 { // camelCase: split before upper-case letters of the original
		words = nil
		cur := []rune{}
		for _, r := range name {
			if r >= 'A' && r <= 'Z' && len(cur) > 0 {
				words = append(words, strings.ToLower(string(cur)))
				cur = cur[:0]
			}
			cur = append(cur, r)
		}
		words = append(words, strings.ToLower(string(cur)))
	}
	for _, w := range words {
		for _, s := range webDBSecretWords {
			if w == s {
				return true
			}
		}
		for _, s := range aiSecretWords { // the settings' secret words: key, token, auth …
			if w == s {
				return true
			}
		}
	}
	return false
}

// webDBResult turns a result into the page's rows: columns cut at
// MaxDBColumns, cells cut at MaxDBCell, the answer at MaxDBBytes; NULL
// kept apart; secrets masked.
func webDBResult(r dbclient.Result, password string) webproto.DBRows {
	out := webproto.DBRows{Columns: []string{}, Rows: [][]webproto.DBCell{}, Ms: r.Elapsed.Milliseconds()}
	cols := r.Columns
	if len(cols) > webproto.MaxDBColumns {
		cols = cols[:webproto.MaxDBColumns]
		out.Truncated = true
	}
	secret := make([]bool, len(cols))
	for i, c := range cols {
		out.Columns = append(out.Columns, webName(c.Name))
		secret[i] = webDBSecretColumn(c.Name)
	}
	size := 0
	for _, row := range r.Rows {
		cells := make([]webproto.DBCell, len(cols))
		for i := range cols {
			if i >= len(row) || row[i].Null {
				cells[i] = webproto.DBCell{N: true}
				continue
			}
			t := row[i].Text
			switch {
			case secret[i], password != "" && strings.Contains(t, password):
				cells[i] = webproto.DBCell{V: webproto.DBMaskedValue, M: true}
				out.Masked++
				continue
			}
			t = dbRedact(t)
			c := webproto.DBCell{V: t}
			if len(t) > webproto.MaxDBCell {
				cut := webproto.MaxDBCell
				for cut > 0 && !utf8.RuneStart(t[cut]) {
					cut--
				}
				c.V, c.C = t[:cut], true
			}
			cells[i] = c
			size += len(c.V)
		}
		if size > webproto.MaxDBBytes {
			out.Truncated = true
			break
		}
		out.Rows = append(out.Rows, cells)
	}
	return out
}
