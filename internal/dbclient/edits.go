package dbclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/amin-jalali/termocode/internal/lazyre"
)

// Pending edits of a result grid (plan F6) become one statement per row:
// identifiers go through the dialect's QuoteIdent, every value is a bound
// parameter, and an UPDATE or DELETE finds its row by the full key.
// ApplyEdits runs them as one transaction and checks that each statement
// changed exactly one row.

// EditKind is what a pending row change does.
type EditKind int

const (
	EditUpdate EditKind = iota
	EditInsert
	EditDelete
)

// EditValue is one column of a row change. For an UPDATE, Old is the value
// the page showed; Check adds "AND column = old" (or IS NULL) so a row
// someone else changed in the meantime is not overwritten.
type EditValue struct {
	Column string
	Value  Cell
	Old    Cell
	Check  bool
}

// RowEdit is the pending change of one row. Key holds the key columns and
// the values that find the row (UPDATE, DELETE).
type RowEdit struct {
	Kind   EditKind
	Key    []EditValue
	Values []EditValue
}

// EditStatement is one generated statement: SQL with placeholders only,
// the bound Args, the number of rows it must change, and a Preview with
// the values written as literals (shown to people, never sent).
type EditStatement struct {
	SQL     string
	Args    []any
	Preview string
	Expect  int64
}

// EditStatements builds the statements for edits on schema.table (schema
// may be empty): deletes first, then updates, then inserts, each in the
// order given.
func EditStatements(d Dialect, schema, table string, edits []RowEdit) ([]EditStatement, error) {
	if d == nil {
		return nil, errors.New("this connection does not use SQL")
	}
	if strings.TrimSpace(table) == "" {
		return nil, errors.New("no table to change")
	}
	name := d.QuoteIdent(table)
	if schema != "" {
		name = d.QuoteIdent(schema) + "." + name
	}
	var out []EditStatement
	for _, kind := range []EditKind{EditDelete, EditUpdate, EditInsert} {
		for _, e := range edits {
			if e.Kind != kind {
				continue
			}
			st, err := editStatement(d, name, e)
			if err != nil {
				return nil, err
			}
			out = append(out, st)
		}
	}
	return out, nil
}

// editBuilder writes the SQL and its preview side by side.
type editBuilder struct {
	d            Dialect
	sql, preview strings.Builder
	args         []any
}

func (b *editBuilder) text(s string) {
	b.sql.WriteString(s)
	b.preview.WriteString(s)
}

func (b *editBuilder) ident(name string) error {
	if name == "" {
		return errors.New("a column has no name")
	}
	b.text(b.d.QuoteIdent(name))
	return nil
}

func (b *editBuilder) value(c Cell) {
	b.args = append(b.args, editArg(c))
	b.sql.WriteString(b.d.Placeholder(len(b.args)))
	b.preview.WriteString(EditLiteral(b.d, c))
}

func editArg(c Cell) any {
	if c.Null {
		return nil
	}
	return c.Text
}

// keyWhere writes " WHERE k1 = ? AND k2 = ?" (plus the old-value checks).
func (b *editBuilder) keyWhere(key, checks []EditValue) error {
	if len(key) == 0 {
		return errors.New("the table has no key: the row cannot be found safely")
	}
	b.text(" WHERE ")
	for i, k := range key {
		if k.Value.Null {
			return fmt.Errorf("the key column %s is empty in this row", k.Column)
		}
		if i > 0 {
			b.text(" AND ")
		}
		if err := b.ident(k.Column); err != nil {
			return err
		}
		b.text(" = ")
		b.value(k.Value)
	}
	for _, v := range checks {
		if !v.Check {
			continue
		}
		b.text(" AND ")
		if err := b.ident(v.Column); err != nil {
			return err
		}
		if v.Old.Null {
			b.text(" IS NULL")
			continue
		}
		b.text(" = ")
		b.value(v.Old)
	}
	return nil
}

func editStatement(d Dialect, table string, e RowEdit) (EditStatement, error) {
	b := &editBuilder{d: d}
	switch e.Kind {
	case EditDelete:
		b.text("DELETE FROM " + table)
		if err := b.keyWhere(e.Key, nil); err != nil {
			return EditStatement{}, err
		}
	case EditUpdate:
		if len(e.Values) == 0 {
			return EditStatement{}, errors.New("nothing to change in this row")
		}
		if len(e.Key) == 0 {
			return EditStatement{}, errors.New("the table has no key: the row cannot be found safely")
		}
		b.text("UPDATE " + table + " SET ")
		for i, v := range e.Values {
			if i > 0 {
				b.text(", ")
			}
			if err := b.ident(v.Column); err != nil {
				return EditStatement{}, err
			}
			b.text(" = ")
			b.value(v.Value)
		}
		if err := b.keyWhere(e.Key, e.Values); err != nil {
			return EditStatement{}, err
		}
	case EditInsert:
		b.text("INSERT INTO " + table)
		if len(e.Values) == 0 {
			if _, mysql := d.(MySQLDialect); mysql {
				b.text(" () VALUES ()")
			} else {
				b.text(" DEFAULT VALUES")
			}
			break
		}
		b.text(" (")
		for i, v := range e.Values {
			if i > 0 {
				b.text(", ")
			}
			if err := b.ident(v.Column); err != nil {
				return EditStatement{}, err
			}
		}
		b.text(") VALUES (")
		for i, v := range e.Values {
			if i > 0 {
				b.text(", ")
			}
			b.value(v.Value)
		}
		b.text(")")
	default:
		return EditStatement{}, errors.New("unknown change")
	}
	return EditStatement{SQL: b.sql.String(), Args: b.args, Preview: b.preview.String() + ";", Expect: 1}, nil
}

var editNumber = lazyre.New(`^-?[0-9]+(\.[0-9]+)?([eE][-+]?[0-9]+)?$`)

// EditLiteral writes a value as a SQL literal for the preview and Copy SQL:
// NULL, a plain number as is, anything else quoted for the dialect.
func EditLiteral(d Dialect, c Cell) string {
	if c.Null {
		return "NULL"
	}
	if c.Kind == "number" && editNumber.MatchString(c.Text) {
		return c.Text
	}
	text := strings.ReplaceAll(c.Text, "'", "''")
	if _, mysql := d.(MySQLDialect); mysql {
		text = strings.ReplaceAll(c.Text, `\`, `\\`)
		text = strings.ReplaceAll(text, "'", "''")
	}
	return "'" + text + "'"
}

// EditCountError is a statement that changed another number of rows than
// the one it was built for. The transaction was rolled back.
type EditCountError struct {
	Index     int
	Verb      string
	Got, Want int64
}

func (e *EditCountError) Error() string {
	return fmt.Sprintf("%s changed %d rows, %d expected: the row changed on the server or is gone. Nothing was saved.", e.Verb, e.Got, e.Want)
}

// EditOutcome is the result of ApplyEdits: the rows each statement
// changed, the index of the failed statement (-1: none) and the error.
type EditOutcome struct {
	Affected []int64
	Failed   int
	Err      error
	Elapsed  time.Duration
}

const editSavepoint = "termocode_edit"

// ApplyEdits runs stmts as one unit on sess. Without an open transaction
// it begins one and commits it; inside the user's open transaction
// (manual mode) it uses a savepoint and leaves the commit to the user. A
// statement that fails, or changes another number of rows than it
// expects, rolls everything back.
func ApplyEdits(ctx context.Context, sess Session, stmts []EditStatement, inTx bool) (out EditOutcome) {
	start := time.Now()
	defer func() { out.Elapsed = time.Since(start) }()
	out.Failed = -1
	if len(stmts) == 0 {
		return out
	}
	undo := func() error {
		cctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if inTx {
			r := sess.Run(cctx, "ROLLBACK TO SAVEPOINT "+editSavepoint, RunOptions{})
			if r.Err != nil {
				return r.Err
			}
			return sess.Run(cctx, "RELEASE SAVEPOINT "+editSavepoint, RunOptions{}).Err
		}
		return sess.Rollback(cctx)
	}
	if inTx {
		if r := sess.Run(ctx, "SAVEPOINT "+editSavepoint, RunOptions{}); r.Err != nil {
			out.Err = r.Err
			return out
		}
	} else if err := sess.Begin(ctx); err != nil {
		out.Err = err
		return out
	}
	for i, st := range stmts {
		r := sess.Run(ctx, st.SQL, RunOptions{Args: st.Args})
		if r.Err == nil && r.Affected != st.Expect {
			verb := strings.ToUpper(strings.Fields(st.SQL)[0])
			r.Err = &EditCountError{Index: i, Verb: verb, Got: r.Affected, Want: st.Expect}
		}
		if r.Err != nil {
			out.Failed = i
			out.Err = r.Err
			if uerr := undo(); uerr != nil {
				out.Err = fmt.Errorf("%w (rolling back also failed: %v)", r.Err, uerr)
			}
			return out
		}
		out.Affected = append(out.Affected, r.Affected)
	}
	if inTx {
		out.Err = sess.Run(ctx, "RELEASE SAVEPOINT "+editSavepoint, RunOptions{}).Err
		return out
	}
	if err := sess.Commit(ctx); err != nil {
		out.Err = err
	}
	return out
}
