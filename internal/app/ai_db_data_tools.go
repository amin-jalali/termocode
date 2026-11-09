package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/dbclient"
)

// db_data: the AI moves data in and out as the Databases view does (DB
// parity): export a read to a file, import a CSV into a table, dump a
// database and restore one with the server's own tool.
//
// It asks where the GUI asks: an import (the web's db.import card), a
// dump and a restore (the TUI's cards, Cancel first) ask every time with
// no "Always allow". An export writes a workspace file, so it asks as
// write_file does (fileWrite); replacing a file asks every time, as the
// view's "replace?" card does.
//
// Files stay inside the workspace and never a secret file (.env, keys …).
// The export reads in the AI's read-only session; the import writes in a
// session of its own. A dump or restore runs on a connection the user has
// connected: the tool gets the password the view holds in its environment
// (never in the arguments, the card or the result).

var aiDBDataOps = map[string]string{"export": ai.ClassFileWrite, "import_csv": "", "dump": "", "restore": ""}

var aiDBExportFormats = map[string]dbclient.ExportFormat{
	"csv": dbclient.FormatCSV, "tsv": dbclient.FormatTSV, "json": dbclient.FormatJSON, "md": dbclient.FormatMarkdown,
}

type aiDBDataReq struct {
	op      string
	conn    dbclient.Conn
	abs     string // the file
	rel     string // its name for the card
	exists  bool
	sql     string                // export
	format  dbclient.ExportFormat // export
	table   dbTable               // import
	sniff   dbclient.CSVSniff     // import
	rows    int                   // import
	cliLine string                // dump / restore: the command for the card
	err     error
}

func aiDBDataParse(root string, a map[string]any) *aiDBDataReq {
	r := &aiDBDataReq{op: ai.ArgString(a, "op")}
	fail := func(err error) *aiDBDataReq { r.err = err; return r }
	if _, ok := aiDBDataOps[r.op]; !ok {
		return fail(errors.New("give op: export, import_csv, dump or restore"))
	}
	c, err := aiDBAnyConn(root, ai.ArgString(a, "conn"))
	if err != nil {
		return fail(err)
	}
	r.conn = c
	if r.op == "export" || r.op == "import_csv" {
		if _, err := dbAIConn(root, c.ID); err != nil {
			return fail(err)
		}
	}
	if c.ReadOnly && (r.op == "import_csv" || r.op == "restore") {
		return fail(fmt.Errorf("connection %s is read-only", c.ID))
	}
	r.rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(ai.ArgString(a, "path"))))
	if strings.TrimSpace(ai.ArgString(a, "path")) == "" {
		return fail(errors.New("give path: a file inside the workspace"))
	}
	r.abs, err = aiDBInside(root, ai.ArgString(a, "path"))
	if err != nil {
		return fail(err)
	}
	if real, _ := filepath.EvalSymlinks(r.abs); webSecretFile(r.abs) || webSecretFile(real) {
		return fail(errors.New("this file may hold secrets; the AI does not read or write it"))
	}
	st, serr := os.Stat(r.abs)
	r.exists = serr == nil
	if r.exists && st.IsDir() {
		return fail(errors.New("that path is a folder"))
	}
	switch r.op {
	case "export":
		f := ai.ArgString(a, "format")
		if f == "" {
			f = strings.TrimPrefix(strings.ToLower(filepath.Ext(r.abs)), ".")
			if _, ok := aiDBExportFormats[f]; !ok {
				f = "csv"
			}
		}
		format, ok := aiDBExportFormats[f]
		if !ok {
			return fail(errors.New("format is csv, tsv, json or md"))
		}
		r.format = format
		q := dbAIBuild(root, map[string]any{"conn": c.ID, "sql": ai.ArgString(a, "sql")})
		if q.err != nil {
			return fail(q.err)
		}
		if q.kind != dbclient.KindRead {
			return fail(errors.New("an export runs a read (SELECT …); writes go to db_query"))
		}
		r.sql = q.sql
	case "import_csv":
		if !strings.EqualFold(filepath.Ext(r.abs), ".csv") {
			return fail(errors.New("give a .csv file"))
		}
		if !r.exists {
			return fail(fmt.Errorf("%s is not a file", r.rel))
		}
		name := strings.TrimSpace(ai.ArgString(a, "table"))
		if name == "" {
			return fail(errors.New("give table: the table to import into"))
		}
		r.table = dbTable{conn: c.ID, driver: c.Driver, name: name, schema: ai.ArgString(a, "schema")}
		if r.table.schema == "" && c.Driver != "sqlite" {
			r.table.schema = webDBSchema(c)
		}
		sn, err := dbclient.SniffCSV(r.abs)
		if err != nil {
			return fail(errors.New(sanitizeDBText(err.Error())))
		}
		header := true
		if _, ok := a["header"]; ok {
			header = ai.ArgBool(a, "header")
		}
		r.sniff = sn.WithHeader(header)
		r.rows, err = dbclient.CountCSVRows(context.Background(), r.abs, r.sniff)
		if err != nil {
			return fail(errors.New(sanitizeDBText(err.Error())))
		}
	case "dump", "restore":
		if dbclient.DumpTool(c.Driver) == "" {
			return fail(errors.New("dump and restore need a PostgreSQL or MySQL server"))
		}
		if c.SSH != nil || c.Kube != nil {
			return fail(errors.New("dump and restore reach the server directly; not over a tunnel"))
		}
		var cmd dbclient.CLICmd
		o := dbclient.CLIOptions{Root: root} // the card never carries the password
		if r.op == "restore" {
			if !r.exists {
				return fail(fmt.Errorf("%s is not a file", r.rel))
			}
			cmd, err = dbclient.RestoreCommand(c, o, r.abs)
		} else {
			cmd, err = dbclient.DumpCommand(c, o, r.abs)
		}
		if err != nil {
			return fail(errors.New(sanitizeDBText(err.Error())))
		}
		r.cliLine = cmd.Tool + " " + strings.Join(cmd.Args, " ")
		if cmd.Stdin != "" {
			r.cliLine += " < " + cmd.Stdin
		}
	}
	return r
}

func aiDBDataDescribe(r *aiDBDataReq) string {
	if r.err != nil {
		return "db_data (invalid: " + dbOneLine(r.err.Error()) + ")"
	}
	name := sanitizeDBText(dbConnName(r.conn))
	where := webDBWhere(r.conn)
	file := sanitizeDBText(r.rel)
	switch r.op {
	case "export":
		head := "Export to " + file + " (" + string(r.format) + ") from " + name + "?"
		if r.exists {
			head = "Replace " + file + " with an export (" + string(r.format) + ") from " + name + "?"
		}
		return head + "\n\n" + dbSafeMultiline(r.sql)
	case "import_csv":
		body := "Import " + dbPlural(r.rows, "row") + " from " + file + " into " + r.table.label() + " on " + name + " (" + where + ")?\n\n" +
			"File columns: " + sanitizeDBText(strings.Join(r.sniff.Columns, ", ")) + " (matched by name with the table's columns)\n\n" +
			"INSERT INTO " + r.table.qualified() + " (…) VALUES (…)\n\n" +
			"The values are bound, in INSERTs of up to 1000 rows, all in one transaction: the first row that fails rolls everything back."
		return dbSafeMultiline(body)
	case "dump":
		body := ""
		if r.exists {
			body = "The file exists and is replaced.\n\n"
		}
		return "Dump " + name + " to " + file + "?\n\n" + body + dbSafeMultiline(sanitizeDBText(r.cliLine))
	case "restore":
		body := "Every statement of the file runs on the server: tables and rows can be replaced or dropped. It cannot be undone.\n\n"
		if r.conn.Environment == "production" {
			body = "This is a PRODUCTION connection. " + body
		}
		return "Restore " + file + " into " + name + " (" + where + ")?\n\n" + body + dbSafeMultiline(sanitizeDBText(r.cliLine))
	}
	return "db_data " + r.op
}

func aiDBDataTool(root string, call aiIDECaller) ai.Tool {
	str := func(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
	return ai.Tool{
		Name: "db_data",
		Description: "Move data in and out of a connection as the Databases view does. op export (sql: one read, path, format csv|tsv|json|md) writes the rows to a workspace file; " +
			"import_csv (path to a .csv, table, schema, header) inserts its rows in one transaction; dump (path) and restore (path) run the server's own tool (pg_dump / pg_restore / psql, mysqldump / mysql) " +
			"on a connection the user has connected. Each asks the user; files stay inside the workspace.",
		Params: map[string]any{
			"op":     map[string]any{"type": "string", "enum": []string{"dump", "export", "import_csv", "restore"}, "description": "what to do"},
			"conn":   str("connection id (db_connections)"),
			"path":   str("the file, relative to the workspace"),
			"sql":    str("export: one read statement"),
			"format": map[string]any{"type": "string", "enum": []string{"csv", "tsv", "json", "md"}, "description": "export: the file format (default from the file's extension, else csv)"},
			"table":  str("import_csv: the table"), "schema": str("import_csv: its schema (default: public on PostgreSQL, the connection's database on MySQL)"),
			"header": map[string]any{"type": "boolean", "description": "import_csv: the first line names the columns (default true)"},
		},
		Required: []string{"op", "conn", "path"},
		Describe: func(a map[string]any) string { return aiDBDataDescribe(aiDBDataParse(root, a)) },
		Validate: func(a map[string]any) error { return aiDBDataParse(root, a).err },
		Gate: func(a map[string]any) (bool, string) {
			class, ok := aiDBDataOps[ai.ArgString(a, "op")]
			if !ok {
				return true, ""
			}
			if class != "" && ai.ArgString(a, "path") != "" {
				if abs, err := aiDBInside(root, ai.ArgString(a, "path")); err == nil {
					if _, err := os.Stat(abs); err == nil {
						return true, "" // replacing a file always asks
					}
				}
			}
			return true, class
		},
		Run: func(ctx context.Context, a map[string]any) (string, error) {
			r := aiDBDataParse(root, a)
			if r.err != nil {
				return "", r.err
			}
			switch r.op {
			case "export":
				return aiDBExport(ctx, root, call, r)
			case "import_csv":
				return aiDBImport(ctx, root, call, r)
			}
			return aiDBDumpRestore(ctx, root, call, r)
		},
	}
}

func aiDBExport(ctx context.Context, root string, call aiIDECaller, r *aiDBDataReq) (string, error) {
	snap := dbAITake(ctx, call, root, r.conn)
	sess, err := dbAIOpen(ctx, root, r.conn, &snap, true) // a question to the user on the way is not counted in the timeout
	if err != nil {
		return "", dbAIError(err, snap.password)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*loadDBConsoleSettings().timeout())
	defer cancel()
	defer sess.Close()
	if err := os.MkdirAll(filepath.Dir(r.abs), 0o755); err != nil {
		return "", errors.New("could not make the folder")
	}
	mask := loadDBConsoleSettings().exportMask()
	driver, query := r.conn.Driver, r.sql
	n, err := dbclient.ExportToFile(ctx, r.abs, r.format, dbclient.ExportOptions{Driver: driver}, true, func(ctx context.Context, sink dbclient.RowSink) error {
		return dbclient.StreamRows(ctx, sess, driver, query, dbMasked(sink, mask))
	}, nil)
	if err != nil {
		return "", dbAIError(err, snap.password)
	}
	return fmt.Sprintf("exported %s from %s to %s", dbPlural(int(n), "row"), r.conn.ID, r.rel), nil
}

func aiDBImport(ctx context.Context, root string, call aiIDECaller, r *aiDBDataReq) (string, error) {
	snap := dbAITake(ctx, call, root, r.conn)
	ro, err := dbAIOpen(ctx, root, r.conn, &snap, true) // a question to the user on the way is not counted in the timeout
	if err != nil {
		return "", dbAIError(err, snap.password)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*loadDBConsoleSettings().timeout())
	defer cancel()
	ir, err := ro.Introspect(ctx, r.table.request())
	_ = ro.Close()
	if err != nil {
		return "", dbAIError(err, snap.password)
	}
	var targets []string
	for _, col := range ir.Columns {
		targets = append(targets, col.Name)
	}
	if len(targets) == 0 {
		return "", fmt.Errorf("no table %s on %s (db_schema lists the tables)", r.table.label(), r.conn.ID)
	}
	sources := dbclient.AutoMap(targets, r.sniff)
	matched := 0
	for _, s := range sources {
		if s >= 0 {
			matched++
		}
	}
	if matched == 0 {
		return "", fmt.Errorf("no file column matches a column of %s", r.table.label())
	}
	sess, err := dbAIOpen(ctx, root, r.conn, &snap, false)
	if err != nil {
		return "", dbAIError(err, snap.password)
	}
	spec := dbclient.ImportSpec{Path: r.abs, Sniff: r.sniff, Dialect: dbAIDialect(r.conn.Driver), Schema: r.table.schema, Table: r.table.name, Targets: targets, Sources: sources}
	res := dbclient.ImportCSV(ctx, sess, spec, nil)
	_ = sess.Close()
	if res.Err != nil {
		e := dbAIError(res.Err, snap.password)
		if res.FailedLine > 0 {
			return "", fmt.Errorf("nothing imported: line %d: %s", res.FailedLine, e.Error())
		}
		return "", fmt.Errorf("nothing imported: %s", e.Error())
	}
	return fmt.Sprintf("imported %s from %s into %s on %s", dbPlural(int(res.Rows), "row"), r.rel, r.table.label(), r.conn.ID), nil
}

func aiDBDumpRestore(ctx context.Context, root string, call aiIDECaller, r *aiDBDataReq) (string, error) {
	if call == nil {
		return "", errors.New("the Databases view is not available")
	}
	var cmd dbclient.CLICmd
	var path, password string
	if _, err := call(ctx, func(m *Model) (string, tea.Cmd, error) {
		m.ensureDatabasesLoaded()
		if !dbDumpConnected(m, r.conn.ID) {
			return "", nil, fmt.Errorf("%s is not connected: connect it first (db_connection op connect)", r.conn.ID)
		}
		password = m.databases.passwords[r.conn.ID]
		return "", nil, nil
	}); err != nil {
		return "", err
	}
	ai.RegisterSecret(password)
	o := dbclient.CLIOptions{Password: password, Root: root}
	var err error
	if r.op == "restore" {
		cmd, err = dbclient.RestoreCommand(r.conn, o, r.abs)
	} else {
		if err = os.MkdirAll(filepath.Dir(r.abs), 0o755); err == nil {
			cmd, err = dbclient.DumpCommand(r.conn, o, r.abs)
		}
	}
	if err != nil {
		return "", errors.New(dbAIClean(err.Error(), password))
	}
	if path, err = dbToolLookPath(cmd.Tool); err != nil {
		return "", fmt.Errorf("%s was not found: install the %s client tools so it is on PATH", cmd.Tool, r.conn.Driver)
	}
	start := time.Now()
	out, err := dbToolRun(ctx, path, cmd)
	out = dbAIClean(out, password)
	if err != nil {
		if out == "" {
			out = dbAIClean(err.Error(), password)
		}
		return "", fmt.Errorf("%s failed: %s", cmd.Tool, out)
	}
	verb := "Dumped " + r.conn.ID + " to " + r.rel
	if r.op == "restore" {
		verb = "Restored " + r.rel + " into " + r.conn.ID
	}
	text := fmt.Sprintf("%s (%.1f s)", verb, time.Since(start).Seconds())
	if out != "" {
		text += "\n" + out
	}
	return text, nil
}
