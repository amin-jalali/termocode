package app

import (
	"context"
	"encoding/base64"
	"errors"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Cassandra / ScyllaDB on the page (DBX-67). A table is keyspace.table
// (grouped by its keyspace), and CQL has no OFFSET: a read hands back the
// driver's page state as a cursor (base64), and the next call sends it.

// webDBObject is one table or view db.tables lists.
type webDBObject struct{ name, kind, group string }

// webDBCQLPager is a session that reads a CQL page and says where the next
// one starts (dbclient.CassandraSession).
type webDBCQLPager interface {
	RunPage(ctx context.Context, stmt string, opts dbclient.RunOptions, state []byte) (dbclient.Result, []byte)
}

// webDBCQLObjects lists the tables and views of every keyspace: the
// connection's own keyspace first, the system ones last.
func webDBCQLObjects(ctx context.Context, s dbclient.Session, c dbclient.Conn) ([]webDBObject, error) {
	all, err := s.Introspect(ctx, dbclient.IntrospectionRequest{})
	if err != nil {
		return nil, err
	}
	spaces := all.Databases
	sort.SliceStable(spaces, func(i, j int) bool {
		return webDBKeyspaceRank(spaces[i], c.Database) < webDBKeyspaceRank(spaces[j], c.Database)
	})
	var out []webDBObject
	for _, ks := range spaces {
		res, err := s.Introspect(ctx, dbclient.IntrospectionRequest{Schema: ks})
		if err != nil {
			return nil, err
		}
		for _, o := range res.Objects {
			out = append(out, webDBObject{name: ks + "." + o.Name, kind: o.Kind, group: ks})
			if len(out) > webproto.MaxDBTables {
				return out, nil
			}
		}
	}
	return out, nil
}

func webDBKeyspaceRank(ks, own string) int {
	switch {
	case ks == own && own != "":
		return 0
	case strings.HasPrefix(ks, "system"):
		return 2
	}
	return 1
}

// webDBCQLPage reads one page of stmt from cursor (empty: the first) and
// answers it with the cursor of the next page (empty: the last).
func webDBCQLPage(ctx context.Context, s dbclient.Session, stmt string, size int, cursor, password string) (webproto.DBRows, error) {
	p, ok := s.(webDBCQLPager)
	if !ok {
		return webproto.DBRows{}, errors.New("This connection cannot be read a page at a time.")
	}
	var state []byte
	if cursor != "" {
		var err error
		if state, err = base64.StdEncoding.DecodeString(cursor); err != nil {
			return webproto.DBRows{}, errors.New("Bad page cursor.")
		}
	}
	r, next := p.RunPage(ctx, stmt, dbclient.RunOptions{MaxRows: size}, state)
	if r.Err != nil {
		return webproto.DBRows{}, r.Err
	}
	out := webDBResult(r, password)
	if len(next) > 0 {
		out.Cursor = base64.StdEncoding.EncodeToString(next)
	}
	return out, nil
}

// webDBCQLRows reads one page of a listed table (no OFFSET: a.Cursor).
func (m *Model) webDBCQLRows(call *webCall, w webDBConn, a *webproto.DBRowsArgs) tea.Cmd {
	ks, table, ok := strings.Cut(a.Table, ".")
	if !ok || ks == "" || table == "" {
		call.fail(webproto.CodeNotFound, "That table is not in the list. Open the list again.")
		return nil
	}
	q := dbclient.PostgresDialect{} // CQL quotes identifiers like SQL
	stmt := "SELECT * FROM " + q.QuoteIdent(ks) + "." + q.QuoteIdent(table)
	ctx, stop := m.webDBCtx(call, w.c.ID)
	call.later()
	return func() tea.Msg {
		defer stop()
		s, err := w.open(ctx)
		if err != nil {
			w.fail(call, err)
			return nil
		}
		defer s.Close()
		out, err := webDBCQLPage(ctx, s, stmt, webproto.DBPageRows, a.Cursor, w.snap.password)
		if err != nil {
			w.fail(call, err)
			return nil
		}
		call.reply(out)
		return nil
	}
}

// webDBCQLDDL answers a listed table's CREATE TABLE (keyspace.table).
func (m *Model) webDBCQLDDL(call *webCall, w webDBConn, name string) tea.Cmd {
	ks, table, _ := strings.Cut(name, ".")
	return m.webDBReadOnce(call, w, func(ctx context.Context, s dbclient.Session) (any, error) {
		res, err := s.Introspect(ctx, dbclient.IntrospectionRequest{Schema: ks, Object: table})
		if err != nil {
			return nil, err
		}
		return webproto.DBText{Text: dbAIClean(res.DDL, w.snap.password)}, nil
	})
}
