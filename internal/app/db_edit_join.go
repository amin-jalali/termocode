package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/dbclient"
)

// Editing a JOIN result (DBX-55): the cells of one of the joined tables
// can change. That table is the first one in FROM whose key is in the
// result and clearly its own; a column counts as a table's when the
// select list names it with that table (t.col), or only that table has a
// column of that name. Rows are not added or deleted here.

// dbEditJoinKeysCmd looks up the columns and keys of every joined table.
func dbEditJoinKeysCmd(sess dbclient.Session, t *dbTab, run *dbRun, c dbclient.Conn, sessKey string, join *dbclient.JoinTarget) tea.Cmd {
	return func() tea.Msg {
		msg := dbEditKeysMsg{tab: t, run: run, conn: c, sessKey: sessKey, join: join}
		for _, jt := range join.Tables {
			ctx, cancel := context.WithTimeout(context.Background(), dbMetaTimeout)
			ir, err := sess.Introspect(ctx, dbclient.IntrospectionRequest{Schema: jt.Schema, Object: jt.Table, Kind: "table"})
			cancel()
			if err != nil {
				msg.err = err
				return msg
			}
			msg.irs = append(msg.irs, ir)
		}
		return msg
	}
}

// dbJoinColumnOwner is the joined table result column i belongs to, or -1
// (unknown, in more than one table, or in the result twice).
func dbJoinColumnOwner(cols []dbclient.Column, i int, jt dbclient.JoinTarget, irs []dbclient.IntrospectionResult) int {
	name := cols[i].Name
	if dbColumnIndex(cols, name) != i {
		return -1
	}
	var owners []int
	for k, ir := range irs {
		for _, c := range ir.Columns {
			if strings.EqualFold(c.Name, name) {
				owners = append(owners, k)
				break
			}
		}
	}
	qualified, q := 0, -1
	for _, qc := range jt.Quals {
		if strings.EqualFold(qc.Name, name) {
			qualified++
			q = jt.TableOf(qc.Qual)
		}
	}
	bare := false
	for _, b := range jt.Bare {
		bare = bare || strings.EqualFold(b, name)
	}
	switch {
	case qualified == 1 && !bare && q >= 0:
		for _, o := range owners {
			if o == q {
				return q
			}
		}
		return -1
	case qualified == 0 && len(owners) == 1:
		return owners[0]
	}
	return -1
}

// dbJoinEditTarget picks the table of a JOIN result whose cells change.
func dbJoinEditTarget(cols []dbclient.Column, jt dbclient.JoinTarget, irs []dbclient.IntrospectionResult, conn, driver, sessKey string) *dbEditTarget {
	if len(irs) != len(jt.Tables) {
		return &dbEditTarget{why: "read-only", detail: "The joined tables could not be looked up."}
	}
	owner := make([]int, len(cols))
	for i := range cols {
		owner[i] = dbJoinColumnOwner(cols, i, jt, irs)
	}
	for k, ir := range irs {
		if len(ir.Columns) == 0 {
			continue // not a table here (a view, or gone)
		}
		var keys []string
		keys = append(keys, ir.Keys...)
		for _, col := range ir.Columns {
			if col.PK && !dbContains(keys, col.Name) {
				keys = append(keys, col.Name)
			}
		}
		cands := [][]string{}
		if len(keys) > 0 {
			cands = append(cands, keys)
		}
		cands = append(cands, dbUniqueSets(ir)...)
		for _, cand := range cands {
			var key []int
			for _, name := range cand {
				i := dbColumnIndex(cols, name)
				if i < 0 || owner[i] != k {
					key = nil
					break
				}
				key = append(key, i)
			}
			if len(key) == 0 {
				continue
			}
			tbl := jt.Tables[k]
			tg := &dbEditTarget{conn: conn, driver: driver, sessKey: sessKey, schema: tbl.Schema, table: tbl.Table, key: key, editable: map[int]bool{}, join: true}
			for i := range cols {
				if owner[i] == k {
					tg.editable[i] = true
				}
			}
			return tg
		}
	}
	return &dbEditTarget{why: "read-only: no key column", detail: "Add the key column of one of the joined tables to the query to edit its cells. A column that more than one table has needs its table name, such as t.id."}
}
