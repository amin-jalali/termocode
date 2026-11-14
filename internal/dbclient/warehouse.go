package dbclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Cloud warehouses (DBX-66): Snowflake, Databricks SQL, BigQuery and
// Athena, each over its own REST API with net/http only (no SDK). They
// are HTTP engines; this file holds what they share: the read-only guard,
// the poll loop of an asynchronous statement and the grid of string
// rows.

// warehouseGuard refuses anything but a read on a read-only session,
// before it reaches the account. These APIs have no read-only flag, so
// the classifier decides (in the connection's dialect); anything it
// cannot call a read is refused.
func warehouseGuard(readOnly bool, title, driver, stmt string) error {
	if !readOnly {
		return nil
	}
	if k, _ := Classify(stmt, driver); k != KindRead {
		return fmt.Errorf("%s: this is a read-only session; only reads run here", title)
	}
	return nil
}

// warehousePollEvery is the first wait between two polls of a running
// statement; it doubles up to warehousePollMax.
var warehousePollEvery = 100 * time.Millisecond

const warehousePollMax = 2 * time.Second

// warehousePoll calls step until it reports done, an error, or ctx ends.
func warehousePoll(ctx context.Context, step func() (bool, error)) error {
	wait := warehousePollEvery
	for {
		done, err := step()
		if err != nil || done {
			return err
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
		wait = min(wait*2, warehousePollMax)
	}
}

// warehouseRows appends rows of nullable strings to res, stopping at
// maxRows (> 0) and marking it truncated. It reports whether more rows
// may be read.
func warehouseRows(res *Result, rows [][]*string, maxRows int) bool {
	for _, r := range rows {
		if maxRows > 0 && len(res.Rows) >= maxRows {
			res.Truncated = true
			return false
		}
		row := make([]Cell, len(res.Columns))
		for i := range row {
			if i >= len(r) || r[i] == nil {
				row[i] = Cell{Null: true}
				continue
			}
			row[i] = Cell{Text: *r[i]}
		}
		res.Rows = append(res.Rows, row)
	}
	return true
}

// warehouseColumn finds a column by name (any case), -1 when missing.
func warehouseColumn(res Result, name string) int {
	for i, c := range res.Columns {
		if strings.EqualFold(c.Name, name) {
			return i
		}
	}
	return -1
}

// warehouseList is one column of a result as a list.
func warehouseList(res Result, name string) ([]string, error) {
	if res.Err != nil {
		return nil, res.Err
	}
	i := warehouseColumn(res, name)
	if i < 0 {
		return nil, errors.New("unexpected answer: no " + name + " column")
	}
	var out []string
	for _, r := range res.Rows {
		out = append(out, r[i].Text)
	}
	return out, nil
}

// warehouseParam is c.Params[k] ("" when none).
func warehouseParam(c Conn, k string) string {
	if c.Params == nil {
		return ""
	}
	return strings.TrimSpace(c.Params[k])
}

// warehouseIntrospect is the tree of a warehouse that answers SQL:
// databases, their schemas, a schema's objects, an object's columns and
// DDL. q gives the statement of each level ("" skips it).
type warehouseQueries struct {
	databases, databasesCol string
	schemas                 func(db string) string
	schemasCol              string
	objects                 func(db, schema string) (string, []any)
	columns                 func(db, schema, object string) (string, []any)
	ddl                     func(db, schema, object string) (string, []any)
	// objectCol names the objects' name column ("": the first column,
	// its kind the second).
	objectCol string
}

func warehouseIntrospect(ctx context.Context, s Session, q warehouseQueries, req IntrospectionRequest) (IntrospectionResult, error) {
	var out IntrospectionResult
	var err error
	run := func(stmt string, args []any) Result {
		return s.Run(ctx, stmt, RunOptions{MaxRows: 10000, Args: args})
	}
	switch {
	case req.Database == "":
		out.Databases, err = warehouseList(run(q.databases, nil), q.databasesCol)
	case req.Schema == "":
		out.Schemas, err = warehouseList(run(q.schemas(req.Database), nil), q.schemasCol)
	case req.Object == "":
		res := run(q.objects(req.Database, req.Schema))
		if res.Err != nil {
			return out, res.Err
		}
		if q.objectCol != "" {
			names, err := warehouseList(res, q.objectCol)
			for _, n := range names {
				out.Objects = append(out.Objects, SchemaObject{Name: n, Kind: "BASE TABLE"})
			}
			return out, err
		}
		for _, r := range res.Rows {
			o := SchemaObject{Name: r[0].Text, Kind: "BASE TABLE"}
			if len(r) > 1 && r[1].Text != "" {
				o.Kind = r[1].Text
			}
			out.Objects = append(out.Objects, o)
		}
	default:
		res := run(q.columns(req.Database, req.Schema, req.Object))
		if res.Err != nil {
			return out, res.Err
		}
		for _, r := range res.Rows {
			if len(r) > 1 {
				out.Columns = append(out.Columns, Column{Name: r[0].Text, Type: r[1].Text, Table: req.Object})
			}
		}
		if q.ddl != nil {
			if d := run(q.ddl(req.Database, req.Schema, req.Object)); d.Err == nil && len(d.Rows) > 0 && len(d.Rows[0]) > 0 {
				out.DDL = d.Rows[0][0].Text
			}
		}
	}
	return out, err
}

// warehouseQuote double-quotes one identifier.
func warehouseQuote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
