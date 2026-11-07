package webproto

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// The v2 views (mobile plan 3.11.8, MOB-73): db, http, containers, kube,
// tests, services, search, the logs family, diagnostics, debug state,
// settings (read only), AI usage, lsp / ext / workspace folders /
// layouts, terms and files, plus git blame and conflicts. Same rules as
// v1: rows come from the source's JSON as on the wire, a row action names
// an existing op (its args come from the row's Ref), nothing here adds a
// write path. The laptop has already redacted what it sends: a masked DB
// cell stays masked, a log line keeps its redaction, and an HTTP view
// never shows a body, a header or a URL's query.

// viewSourceArgs makes the args of a v2 view's source op from the spec's
// args, by op. SourceArgs (views_v1.go) asks it for every op it does not
// know itself.
var viewSourceArgs = map[string]func(args map[string]string) any{}

// sourceArgsV2 is SourceArgs' hook for the v2 ops; ok=false: not a v2 op.
func sourceArgsV2(op string, args map[string]string) (any, bool) {
	fn := viewSourceArgs[op]
	if fn == nil {
		return nil, false
	}
	return fn(args), true
}

// logFollow is how often a pinned or open log view re-runs ("follow").
const logFollow = 10

// v2Atoi reads a whole number arg (CheckViewSpec has checked it).
func v2Atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// v2NoQuery cuts a URL's query and fragment: they may carry a token, and
// the phone only needs the method, host and path to know the request.
func v2NoQuery(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	if u, err := url.Parse(raw); err == nil && u.User != nil {
		u.User = nil
		raw = u.String()
	}
	return raw
}

// v2Lines turns a log's lines into text rows (one field, "line").
func v2Lines(lines []string) []ViewRow {
	out := make([]ViewRow, 0, len(lines))
	for i, l := range lines {
		out = append(out, ViewRow{ID: "l" + strconv.Itoa(i), Cells: vcells("line", vtxt(l))})
	}
	return out
}

func v2LineField() []ViewField { return []ViewField{vf("line", "Line", FieldText, "fd")} }

// The http status of a response as a tone: 2xx ok, 3xx info, 4xx warn,
// 5xx and transport errors bad.
func httpTone(status int, err string) string {
	switch {
	case err != "" || status >= 500:
		return blocks.ToneBad
	case status >= 400:
		return blocks.ToneWarn
	case status >= 300:
		return blocks.ToneInfo
	case status >= 200:
		return blocks.ToneOK
	}
	return blocks.ToneIdle
}

// diagSev names an LSP severity (1 error, 2 warning, 3 info, 4 hint).
func diagSev(sev int) (string, string) {
	switch sev {
	case 1:
		return "error", blocks.ToneBad
	case 2:
		return "warning", blocks.ToneWarn
	case 3:
		return "info", blocks.ToneInfo
	}
	return "hint", blocks.ToneIdle
}

// dbRowsBlock turns a db.rows / db.query page into a table block. Its
// columns come from the result (each table has its own), so it is one
// block by nature, like a diff. A masked cell keeps DBMaskedValue; NULL
// shows as NULL.
func dbRowsBlock(title string, r DBRows) *Block {
	b := &Block{ID: "view", Kind: blocks.KindTable, Title: viewCut(title, blocks.MaxTitle), Columns: []BlockColumn{}, Rows: [][]string{}}
	cols := r.Columns
	if len(cols) > blocks.MaxColumns {
		cols = cols[:blocks.MaxColumns]
		b.Truncated = true
	}
	for i, c := range cols {
		b.Columns = append(b.Columns, BlockColumn{ID: "c" + strconv.Itoa(i), Label: viewCut(c, blocks.MaxTitle), Kind: "mono"})
	}
	for _, row := range r.Rows {
		if len(b.Rows) == blocks.MaxRows {
			b.Truncated = true
			break
		}
		cells := make([]string, len(cols))
		for i := range cols {
			if i >= len(row) {
				continue
			}
			c := row[i]
			switch {
			case c.M:
				cells[i] = DBMaskedValue
			case c.N:
				cells[i] = "NULL"
			default:
				cells[i] = viewCut(c.V, blocks.MaxSub)
			}
		}
		b.Rows = append(b.Rows, cells)
	}
	b.Total = len(r.Rows)
	if r.Truncated || r.Next > 0 {
		b.Truncated = true
	}
	return b
}

func init() {
	list, table, text, detail := blocks.KindList, blocks.KindTable, blocks.KindText, blocks.KindDetail
	byNew := []ViewSort{{Field: "when", Desc: true}}
	pathParam := func(id, label string) ViewParam {
		return ViewParam{ID: id, Label: label, Kind: ParamPath, Required: true}
	}

	// git (v2 rows: blame, conflict)
	registerViews(
		ViewDef{ID: "git.blame", Feature: "git", Title: "Blame", Source: ViewSource{Op: OpGitBlame},
			Params: []ViewParam{pathParam("path", "File")},
			Fields: []ViewField{vf("line", "Line", FieldNum, "sfd"), vf("hash", "Commit", FieldText, "fd"),
				vf("author", "Author", FieldText, "sfd"), vf("when", "When", FieldTime, "sfd"), vf("subject", "Subject", FieldText, "sf")},
			Layouts: []string{table, list}},
		ViewDef{ID: "git.conflict", Feature: "git", Title: "Conflict", Source: ViewSource{Op: OpGitConflict},
			Params: []ViewParam{pathParam("path", "File")},
			Fields: []ViewField{vf("line", "Line", FieldNum, "sd"), vf("ours", "Ours", FieldText, "d"), vf("theirs", "Theirs", FieldText, "d"),
				vf("ours_label", "Ours is", FieldText, ""), vf("theirs_label", "Theirs is", FieldText, "")},
			Layouts: []string{list, detail}},
	)
	viewSourceArgs[OpGitBlame] = func(a map[string]string) any { return GitBlameArgs{Path: a["path"]} }
	viewSourceArgs[OpGitConflict] = func(a map[string]string) any { return GitConflictArgs{Path: a["path"]} }
	viewRows["git.blame"] = rowsOf(func(s GitBlame, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Lines))
		for i, ci := range s.Lines {
			if ci < 0 || ci >= len(s.Commits) {
				continue
			}
			c := s.Commits[ci]
			hash := c.Hash
			if c.Uncommitted {
				hash = "not committed"
			}
			out = append(out, ViewRow{ID: "l" + strconv.Itoa(i+1), Cells: vcells("line", vnum(i+1), "hash", vtxt(hash), "author", vtxt(c.Author),
				"when", vwhen(c.When), "subject", vtxt(c.Subject)), Ref: &BlockRef{Kind: "file", Path: s.Path, Line: i + 1}})
		}
		return out
	})
	viewRows["git.conflict"] = rowsOf(func(s GitConflict, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Blocks))
		for i, p := range s.Blocks {
			out = append(out, ViewRow{ID: "k" + strconv.Itoa(i), Cells: vcells("line", vnum(p.Line), "ours", vtxt(p.Ours), "theirs", vtxt(p.Theirs),
				"ours_label", vtxt(p.OursLabel), "theirs_label", vtxt(p.TheirsLabel)), Tone: blocks.ToneBad,
				Ref: &BlockRef{Kind: "file", Path: s.Path, Line: p.Line}})
		}
		return out
	})

	// db
	connParam := ViewParam{ID: "conn", Label: "Connection", Kind: ParamConn, Required: true}
	registerViews(
		ViewDef{ID: "db.conns", Feature: "db", Title: "Databases", Source: ViewSource{Op: OpDBConns},
			Fields: []ViewField{vf("name", "Connection", FieldText, "sfd"), vf("driver", "Kind", FieldState, "sfd"),
				vf("database", "Database", FieldText, "sf"), vf("env", "Env", FieldState, "sfd"), vf("connected", "Connected", FieldBool, "sf"),
				vf("readonly", "Read only", FieldBool, "sf"), vf("problem", "Problem", FieldText, "f")},
			Layouts: []string{list, table}},
		ViewDef{ID: "db.tables", Feature: "db", Title: "Tables", Source: ViewSource{Op: OpDBTables},
			Params:  []ViewParam{connParam},
			Fields:  []ViewField{vf("name", "Table", FieldText, "sfd"), vf("view", "View", FieldBool, "sf")},
			Layouts: []string{list, table}},
		ViewDef{ID: "db.rows", Feature: "db", Title: "Rows", Source: ViewSource{Op: OpDBRows},
			Params:  []ViewParam{connParam, {ID: "table", Label: "Table", Kind: ParamTable, Required: true}, {ID: "from", Label: "From row", Kind: ParamInt}},
			Fields:  []ViewField{vf("row", "Row", FieldText, "d")},
			Layouts: []string{table}},
	)
	viewSourceArgs[OpDBTables] = func(a map[string]string) any { return DBConnArgs{Conn: a["conn"]} }
	viewSourceArgs[OpDBRows] = func(a map[string]string) any {
		return DBRowsArgs{Conn: a["conn"], Table: a["table"], From: v2Atoi(a["from"])}
	}
	viewRows["db.conns"] = rowsOf(func(s DBConns, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Conns))
		for _, c := range s.Conns {
			name := c.Name
			if name == "" {
				name = c.ID
			}
			tone := blocks.ToneIdle
			switch {
			case c.Problem != "":
				tone = blocks.ToneBad
			case c.Connected:
				tone = blocks.ToneOK
			}
			out = append(out, ViewRow{ID: "d" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(name), "driver", vtxt(c.Driver),
				"database", vtxt(c.Database), "env", vtxt(c.Env), "connected", vyes(c.Connected), "readonly", vyes(c.ReadOnly),
				"problem", vtxt(c.Problem)), Tone: tone, Ref: &BlockRef{Kind: "db", ID: c.ID}})
		}
		return out
	})
	viewRows["db.tables"] = rowsOf(func(s DBTables, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Tables))
		for _, tb := range s.Tables {
			out = append(out, ViewRow{ID: "t" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(tb.Name), "view", vyes(tb.View)),
				Ref: &BlockRef{Kind: "db_table", ID: s.Conn + "/" + tb.Name}})
		}
		return out
	})
	viewRows["db.rows"] = func(args map[string]string, src json.RawMessage) ([]ViewRow, *Block, error) {
		var r DBRows
		if err := json.Unmarshal(src, &r); err != nil {
			return nil, nil, err
		}
		return nil, dbRowsBlock(args["table"], r), nil
	}

	// http
	registerViews(
		ViewDef{ID: "http.files", Feature: "http", Title: "HTTP requests", Source: ViewSource{Op: OpHTTPFiles},
			Fields: []ViewField{vf("name", "Request", FieldText, "sfd"), vf("method", "Method", FieldState, "sfd"),
				vf("url", "Where", FieldText, "fd"), vf("file", "File", FieldPath, "sf")},
			Layouts: []string{list, table}, Default: ViewSpec{Group: "file"},
			Actions: []BlockAction{{ID: "send", Label: "Send", Kind: blocks.ActOp, Op: OpHTTPSend}}},
		ViewDef{ID: "http.saved", Feature: "http", Title: "Saved requests", Source: ViewSource{Op: OpHTTPSaved},
			Fields: []ViewField{vf("name", "Request", FieldText, "sfd"), vf("method", "Method", FieldState, "sfd"),
				vf("url", "Where", FieldText, "fd")},
			Layouts: []string{list, table}},
		ViewDef{ID: "http.history", Feature: "http", Title: "HTTP history", Source: ViewSource{Op: OpHTTPHistory},
			Fields: []ViewField{vf("when", "When", FieldTime, "sfd"), vf("method", "Method", FieldState, "sfd"), vf("url", "Where", FieldText, "fd"),
				vf("status", "Status", FieldNum, "sfd"), vf("ms", "Time (ms)", FieldNum, "sf"), vf("size", "Size", FieldNum, "sf"),
				vf("env", "Env", FieldState, "sf"), vf("by", "By", FieldState, "sf"), vf("err", "Error", FieldText, "f")},
			Layouts: []string{table, list}, Default: ViewSpec{Sort: byNew}},
		ViewDef{ID: "http.response", Feature: "http", Title: "HTTP response", Source: ViewSource{Op: OpHTTPResponse},
			Params: []ViewParam{{ID: "run", Label: "Run", Kind: ParamInt}},
			Fields: []ViewField{vf("label", "Request", FieldText, "d"), vf("method", "Method", FieldState, "d"), vf("url", "Where", FieldText, "d"),
				vf("status", "Status", FieldNum, "d"), vf("ms", "Time (ms)", FieldNum, "d"), vf("size", "Size", FieldNum, "d"),
				vf("type", "Type", FieldText, "d"), vf("running", "Running", FieldBool, ""), vf("err", "Error", FieldText, "d")},
			Layouts: []string{detail, blocks.KindCard}},
	)
	viewSourceArgs[OpHTTPResponse] = func(a map[string]string) any { return HTTPRunArgs{Run: v2Atoi(a["run"])} }
	reqRow := func(file string, r HTTPReqItem) ViewRow {
		name := r.Name
		if name == "" {
			name = r.Method + " " + v2NoQuery(r.URL)
		}
		ref := &BlockRef{Kind: "http_request", ID: strconv.Itoa(r.Index)}
		if file != "" {
			ref = &BlockRef{Kind: "http_request", ID: strconv.Itoa(r.Index), Path: file, Line: r.Line}
		}
		return ViewRow{Cells: vcells("name", vtxt(name), "method", vtxt(r.Method), "url", vtxt(v2NoQuery(r.URL)), "file", vtxt(file)), Ref: ref}
	}
	viewRows["http.files"] = rowsOf(func(s HTTPFiles, _ map[string]string) []ViewRow {
		var out []ViewRow
		for _, f := range s.Files {
			for _, r := range f.Requests {
				row := reqRow(f.Path, r)
				row.ID = "r" + strconv.Itoa(len(out))
				out = append(out, row)
			}
		}
		return out
	})
	viewRows["http.saved"] = rowsOf(func(s HTTPSaved, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Requests))
		for _, r := range s.Requests {
			row := reqRow("", r)
			row.ID = "s" + strconv.Itoa(len(out))
			out = append(out, row)
		}
		return out
	})
	viewRows["http.history"] = rowsOf(func(s HTTPHistory, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Entries))
		for _, e := range s.Entries {
			out = append(out, ViewRow{ID: "h" + strconv.Itoa(len(out)), Cells: vcells("when", vwhen(e.At), "method", vtxt(e.Method),
				"url", vtxt(v2NoQuery(e.URL)), "status", vnum(e.Status), "ms", vnum(int(e.ElapsedMS)), "size", vnum(int(e.Size)),
				"env", vtxt(e.Env), "by", vtxt(e.By), "err", vtxt(e.Err)), Tone: httpTone(e.Status, e.Err), Ref: &BlockRef{Kind: "http_history", ID: e.ID}})
		}
		return out
	})
	viewRows["http.response"] = rowsOf(func(s HTTPResponse, _ map[string]string) []ViewRow {
		tone := httpTone(s.Status, s.Err)
		if s.Running {
			tone = blocks.ToneBusy
		}
		return []ViewRow{{ID: "run" + strconv.Itoa(s.Run), Cells: vcells("label", vtxt(s.Label), "method", vtxt(s.Method), "url", vtxt(v2NoQuery(s.URL)),
			"status", vnum(s.Status), "ms", vnum(int(s.ElapsedMS)), "size", vnum(int(s.Size)), "type", vtxt(s.ContentType),
			"running", vyes(s.Running), "err", vtxt(s.Err)), Tone: tone, Ref: &BlockRef{Kind: "http_run", ID: strconv.Itoa(s.Run)}}}
	})

	// containers (stop and remove wait for a laptop card: their ops say so)
	idParam := func(label string) ViewParam {
		return ViewParam{ID: "id", Label: label, Kind: ParamString, Required: true}
	}
	registerViews(
		ViewDef{ID: "containers", Feature: "containers", Title: "Containers", Source: ViewSource{Op: OpContainersList},
			Fields: []ViewField{vf("name", "Container", FieldText, "sfd"), vf("state", "State", FieldState, "sfd"),
				vf("image", "Image", FieldText, "sf"), vf("status", "Status", FieldText, "fd"), vf("ports", "Ports", FieldText, "f"),
				vf("project", "Project", FieldState, "sf")},
			Layouts: []string{table, list, blocks.KindBoard}, Refresh: 30,
			Actions: []BlockAction{{ID: "start", Label: "Start", Kind: blocks.ActOp, Op: OpContainersStart},
				{ID: "restart", Label: "Restart", Kind: blocks.ActOp, Op: OpContainersRestart},
				{ID: "stop", Label: "Stop", Kind: blocks.ActOp, Op: OpContainersStop, Risky: true}}},
	)
	viewRows["containers"] = rowsOf(func(s ContainersList, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Containers))
		for _, c := range s.Containers {
			tone := map[string]string{"running": blocks.ToneOK, "paused": blocks.ToneWarn, "restarting": blocks.ToneBusy, "dead": blocks.ToneBad}[c.State]
			if c.Busy {
				tone = blocks.ToneBusy
			}
			if tone == "" {
				tone = blocks.ToneIdle
			}
			out = append(out, ViewRow{ID: "c" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(c.Name), "state", vtxt(c.State), "image", vtxt(c.Image),
				"status", vtxt(c.Status), "ports", vtxt(c.Ports), "project", vtxt(c.Project)), Tone: tone, Ref: &BlockRef{Kind: "container", ID: c.ID}})
		}
		return out
	})

	// kube (restart waits for a laptop card)
	scope := []ViewParam{{ID: "context", Label: "Context", Kind: ParamString}, {ID: "namespace", Label: "Namespace", Kind: ParamString}}
	scopeArgs := func(a map[string]string) any { return KubeScopeArgs{Context: a["context"], Namespace: a["namespace"]} }
	registerViews(
		ViewDef{ID: "kube.contexts", Feature: "kube", Title: "Kubernetes contexts", Source: ViewSource{Op: OpKubeContexts},
			Fields:  []ViewField{vf("name", "Context", FieldText, "sfd"), vf("current", "Current", FieldBool, "sfd")},
			Layouts: []string{list, table}},
		ViewDef{ID: "kube.namespaces", Feature: "kube", Title: "Namespaces", Source: ViewSource{Op: OpKubeNamespaces},
			Params:  []ViewParam{scope[0]},
			Fields:  []ViewField{vf("name", "Namespace", FieldText, "sfd")},
			Layouts: []string{list, table}},
		ViewDef{ID: "kube.pods", Feature: "kube", Title: "Pods", Source: ViewSource{Op: OpKubeList},
			Params: scope,
			Fields: []ViewField{vf("name", "Pod", FieldText, "sfd"), vf("status", "Status", FieldState, "sfd"), vf("ready", "Ready", FieldText, "fd"),
				vf("restarts", "Restarts", FieldNum, "sfd"), vf("when", "Created", FieldTime, "sf")},
			Layouts: []string{table, list}, Refresh: 30,
			Actions: []BlockAction{{ID: "restart", Label: "Restart", Kind: blocks.ActOp, Op: OpKubeRestart, Risky: true}}},
		ViewDef{ID: "kube.describe", Feature: "kube", Title: "Describe", Source: ViewSource{Op: OpKubeDescribe},
			Params: append(append([]ViewParam(nil), scope...),
				ViewParam{ID: "kind", Label: "Kind", Kind: ParamEnum, Enum: []string{"pod", "deployment", "service", "job"}, Required: true},
				ViewParam{ID: "name", Label: "Name", Kind: ParamString, Required: true}),
			Fields: v2LineField(), Layouts: []string{text, list}},
	)
	viewSourceArgs[OpKubeNamespaces] = func(a map[string]string) any { return KubeScopeArgs{Context: a["context"]} }
	viewSourceArgs[OpKubeList] = scopeArgs
	viewSourceArgs[OpKubeDescribe] = func(a map[string]string) any {
		return KubeObjectArgs{Context: a["context"], Namespace: a["namespace"], Kind: a["kind"], Name: a["name"]}
	}
	viewRows["kube.contexts"] = rowsOf(func(s KubeContexts, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Contexts))
		for _, c := range s.Contexts {
			tone := ""
			if c == s.Current {
				tone = blocks.ToneOK
			}
			out = append(out, ViewRow{ID: "x" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(c), "current", vyes(c == s.Current)), Tone: tone,
				Ref: &BlockRef{Kind: "kube_context", ID: c}})
		}
		return out
	})
	viewRows["kube.namespaces"] = rowsOf(func(s KubeNamespaces, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Namespaces))
		for _, n := range s.Namespaces {
			out = append(out, ViewRow{ID: "n" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(n)), Ref: &BlockRef{Kind: "kube_namespace", ID: n}})
		}
		return out
	})
	viewRows["kube.pods"] = rowsOf(func(s KubeList, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Pods))
		for _, p := range s.Pods {
			tone := map[string]string{"Running": blocks.ToneOK, "Succeeded": blocks.ToneOK, "Pending": blocks.ToneBusy,
				"ContainerCreating": blocks.ToneBusy, "Failed": blocks.ToneBad, "CrashLoopBackOff": blocks.ToneBad, "Error": blocks.ToneBad}[p.Status]
			if tone == "" {
				tone = blocks.ToneWarn
			}
			out = append(out, ViewRow{ID: "p" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(p.Name), "status", vtxt(p.Status), "ready", vtxt(p.Ready),
				"restarts", vnum(p.Restarts), "when", vwhen(p.Created)), Tone: tone, Ref: &BlockRef{Kind: "pod", ID: p.Name}})
		}
		return out
	})
	viewRows["kube.describe"] = rowsOf(func(s KubeText, _ map[string]string) []ViewRow { return v2Lines(s.Lines) })

	// tests
	registerViews(
		ViewDef{ID: "tests", Feature: "tests", Title: "Tests", Source: ViewSource{Topic: "tests"},
			Fields: []ViewField{vf("label", "Test", FieldText, "sfd"), vf("state", "State", FieldState, "sfd"), vf("kind", "Kind", FieldState, "sf"),
				vf("path", "File", FieldPath, "sf"), vf("ms", "Time (ms)", FieldNum, "sf"), vf("message", "Message", FieldText, "f")},
			Layouts: []string{list, table}, Default: ViewSpec{Group: "state", Filter: []ViewCond{{Field: "kind", Op: "eq", Value: "case"}}},
			Actions: []BlockAction{{ID: "run", Label: "Run", Kind: blocks.ActOp, Op: OpTestsRun}}},
		ViewDef{ID: "tests.output", Feature: "tests", Title: "Test output", Source: ViewSource{Op: OpTestsOutput},
			Params: []ViewParam{{ID: "id", Label: "Test", Kind: ParamString}},
			Fields: v2LineField(), Layouts: []string{text, list}, Refresh: logFollow},
	)
	viewSourceArgs[OpTestsOutput] = func(a map[string]string) any { return TestOutputArgs{ID: a["id"]} }
	viewRows["tests"] = rowsOf(func(s TestsSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Items))
		for _, it := range s.Items {
			tone := map[string]string{"passed": blocks.ToneOK, "failed": blocks.ToneBad, "running": blocks.ToneBusy, "skipped": blocks.ToneIdle}[it.State]
			if tone == "" {
				tone = blocks.ToneIdle
			}
			ref := &BlockRef{Kind: "test", ID: it.ID}
			if it.State == "failed" && it.FailPath != "" {
				ref.Path, ref.Line = it.FailPath, it.FailLine
			}
			out = append(out, ViewRow{ID: "t" + strconv.Itoa(len(out)), Cells: vcells("label", vtxt(it.Label), "state", vtxt(it.State), "kind", vtxt(it.Kind),
				"path", vtxt(it.Path), "ms", vnum(int(it.Ms)), "message", vtxt(it.Message)), Tone: tone, Ref: ref})
		}
		return out
	})
	viewRows["tests.output"] = rowsOf(func(s TestOutput, _ map[string]string) []ViewRow { return v2Lines(s.Lines) })

	// services
	registerViews(
		ViewDef{ID: "services", Feature: "services", Title: "Services", Source: ViewSource{Topic: "services"},
			Fields: []ViewField{vf("name", "Service", FieldText, "sfd"), vf("state", "State", FieldState, "sfd"), vf("health", "Health", FieldState, "sf"),
				vf("kind", "Kind", FieldState, "sf"), vf("ports", "Ports", FieldText, "fd"), vf("when", "Up since", FieldTime, "sf")},
			Layouts: []string{list, table},
			Actions: []BlockAction{{ID: "start", Label: "Start", Kind: blocks.ActOp, Op: OpServicesStart},
				{ID: "restart", Label: "Restart", Kind: blocks.ActOp, Op: OpServicesRestart}}},
	)
	viewRows["services"] = rowsOf(func(s ServicesSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Services))
		for _, sv := range s.Services {
			ports := make([]string, 0, len(sv.Ports))
			for _, p := range sv.Ports {
				ports = append(ports, strconv.Itoa(p.Published))
			}
			tone := stateTone(sv.State)
			switch {
			case sv.Busy:
				tone = blocks.ToneBusy
			case sv.Health == "unhealthy" || sv.State == "exited" && sv.Exit != 0:
				tone = blocks.ToneBad
			}
			out = append(out, ViewRow{ID: "s" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(sv.Name), "state", vtxt(sv.State), "health", vtxt(sv.Health),
				"kind", vtxt(sv.Kind), "ports", vtxt(strings.Join(ports, ", ")), "when", vwhen(sv.Since)), Tone: tone, Ref: &BlockRef{Kind: "service", ID: sv.Name}})
		}
		return out
	})

	// search: search.run streams its hits as search.batch events; the hub
	// gathers them (SearchHits) before the rows are made.
	registerViews(
		ViewDef{ID: "search", Feature: "search", Title: "Search", Source: ViewSource{Op: OpSearchRun},
			Params:  []ViewParam{{ID: "query", Label: "Find", Kind: ParamString, Required: true}, {ID: "path", Label: "In files", Kind: ParamPath}},
			Fields:  []ViewField{vf("text", "Line", FieldText, "fd"), vf("path", "File", FieldPath, "sfd"), vf("line", "Line no.", FieldNum, "s")},
			Layouts: []string{list, table}, Default: ViewSpec{Group: "path", Limit: 100}},
	)
	viewSourceArgs[OpSearchRun] = func(a map[string]string) any {
		return SearchRunArgs{Query: a["query"], Include: a["path"], Max: blocks.MaxRows}
	}
	viewRows["search"] = rowsOf(func(s SearchHits, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Hits))
		for _, h := range s.Hits {
			out = append(out, ViewRow{ID: "m" + strconv.Itoa(len(out)), Cells: vcells("text", vtxt(strings.TrimSpace(h.Text)), "path", vtxt(h.Path), "line", vnum(h.Line)),
				Ref: &BlockRef{Kind: "lines", Path: h.Path, Line: h.Line}})
		}
		return out
	})

	// logs: one family, each a text view that follows (re-runs every
	// logFollow seconds while open or pinned).
	registerViews(
		ViewDef{ID: "logs.container", Feature: "logs", Title: "Container logs", Source: ViewSource{Op: OpContainersLogs},
			Params: []ViewParam{idParam("Container")}, Fields: v2LineField(), Layouts: []string{text, list}, Refresh: logFollow},
		ViewDef{ID: "logs.pod", Feature: "logs", Title: "Pod logs", Source: ViewSource{Op: OpKubeLogs},
			Params: append(append([]ViewParam(nil), scope...), ViewParam{ID: "pod", Label: "Pod", Kind: ParamString, Required: true},
				ViewParam{ID: "container", Label: "Container", Kind: ParamString}),
			Fields: v2LineField(), Layouts: []string{text, list}, Refresh: logFollow},
		ViewDef{ID: "logs.service", Feature: "logs", Title: "Service logs", Source: ViewSource{Op: OpServicesLogs},
			Params: []ViewParam{{ID: "name", Label: "Service", Kind: ParamString, Required: true}}, Fields: v2LineField(),
			Layouts: []string{text, list}, Refresh: logFollow},
		ViewDef{ID: "logs.task", Feature: "logs", Title: "Task logs", Source: ViewSource{Op: OpTasksTail},
			Params:  []ViewParam{{ID: "task", Label: "Task", Kind: ParamString, Required: true}},
			Fields:  v2LineField(),
			Layouts: []string{text, list}, Refresh: logFollow},
		ViewDef{ID: "logs.workflow", Feature: "logs", Title: "Workflow logs", Source: ViewSource{Topic: "workflows"},
			Params:  []ViewParam{{ID: "run", Label: "Run", Kind: ParamString, Required: true}},
			Fields:  v2LineField(),
			Layouts: []string{text, list}},
	)
	viewSourceArgs[OpContainersLogs] = func(a map[string]string) any { return ContainerLogsArgs{ID: a["id"]} }
	viewSourceArgs[OpKubeLogs] = func(a map[string]string) any {
		return KubeLogsArgs{Context: a["context"], Namespace: a["namespace"], Pod: a["pod"], Container: a["container"]}
	}
	viewSourceArgs[OpServicesLogs] = func(a map[string]string) any { return ServiceLogsArgs{Name: a["name"]} }
	viewRows["logs.container"] = rowsOf(func(s ContainerLogs, _ map[string]string) []ViewRow { return v2Lines(s.Lines) })
	viewRows["logs.pod"] = rowsOf(func(s KubeText, _ map[string]string) []ViewRow { return v2Lines(s.Lines) })
	viewRows["logs.service"] = rowsOf(func(s ServiceLogs, _ map[string]string) []ViewRow { return v2Lines(s.Lines) })
	viewRows["logs.task"] = viewRows["task.tail"]
	viewRows["logs.workflow"] = viewRows["workflow.tail"]

	// diagnostics
	registerViews(
		ViewDef{ID: "problems", Feature: "diagnostics", Title: "Problems", Source: ViewSource{Topic: "diagnostics"},
			Fields: []ViewField{vf("message", "Problem", FieldText, "fd"), vf("severity", "Severity", FieldState, "sfd"),
				vf("path", "File", FieldPath, "sfd"), vf("line", "Line", FieldNum, "s"), vf("source", "From", FieldState, "sf")},
			Layouts: []string{list, table}, Default: ViewSpec{Group: "path"}},
	)
	viewRows["problems"] = rowsOf(func(s DiagSnap, _ map[string]string) []ViewRow {
		var out []ViewRow
		for _, f := range s.Files {
			for _, d := range f.Items {
				sev, tone := diagSev(d.Sev)
				out = append(out, ViewRow{ID: "d" + strconv.Itoa(len(out)), Cells: vcells("message", vtxt(d.Message), "severity", vtxt(sev),
					"path", vtxt(f.Path), "line", vnum(d.Line), "source", vtxt(d.Source)), Tone: tone,
					Ref: &BlockRef{Kind: "lines", Path: f.Path, Line: d.Line}})
			}
		}
		return out
	})

	// debug: view only, no control from a view.
	registerViews(
		ViewDef{ID: "debug.state", Feature: "debug", Title: "Debugger", Source: ViewSource{Topic: "debug"},
			Fields: []ViewField{vf("name", "Name", FieldText, "fd"), vf("value", "Value", FieldText, "fd"), vf("part", "Part", FieldState, "sf"),
				vf("path", "File", FieldPath, "f"), vf("line", "Line", FieldNum, "")},
			Layouts: []string{list, detail, table}, Default: ViewSpec{Group: "part"}},
	)
	viewRows["debug.state"] = rowsOf(func(s DebugSnap, _ map[string]string) []ViewRow {
		var out []ViewRow
		add := func(part, name, value, path string, line int, tone string) {
			r := ViewRow{ID: "g" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(name), "value", vtxt(value), "part", vtxt(part),
				"path", vtxt(path), "line", vnum(line)), Tone: tone}
			if path != "" && line > 0 {
				r.Ref = &BlockRef{Kind: "lines", Path: path, Line: line}
			}
			out = append(out, r)
		}
		status := s.Status
		if !s.Active {
			status = "not running"
		}
		tone := blocks.ToneIdle
		switch {
		case s.Stopped:
			tone = blocks.ToneWarn
		case s.Active:
			tone = blocks.ToneBusy
		}
		add("session", s.Name, status, "", 0, tone)
		for _, f := range s.Frames {
			add("stack", f.Name, f.File, f.Path, f.Line, "")
		}
		for _, sc := range s.Scopes {
			for _, v := range sc.Vars {
				add("variables", v.Name, v.Value, "", 0, "")
			}
		}
		for _, w := range s.Watches {
			add("watch", w.Expr, w.Value, "", 0, "")
		}
		return out
	})

	// settings: read only; changing one stays on the laptop.
	registerViews(
		ViewDef{ID: "settings", Feature: "settings", Title: "Settings", Source: ViewSource{Topic: "settings"},
			Fields: []ViewField{vf("key", "Setting", FieldText, "sfd"), vf("value", "Value", FieldText, "fd"), vf("group", "Group", FieldState, "sf"),
				vf("changed", "Changed", FieldBool, "sf"), vf("about", "About", FieldText, "f")},
			Layouts: []string{table, detail, list}, Default: ViewSpec{Group: "group"}},
	)
	viewRows["settings"] = rowsOf(func(s SettingsSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Items))
		for _, it := range s.Items {
			changed := it.Value != it.Default
			tone := ""
			if changed {
				tone = blocks.ToneInfo
			}
			out = append(out, ViewRow{ID: "k" + strconv.Itoa(len(out)), Cells: vcells("key", vtxt(it.Key), "value", vtxt(it.Value), "group", vtxt(it.Group),
				"changed", vyes(changed), "about", vtxt(it.Description)), Tone: tone, Ref: &BlockRef{Kind: "setting", ID: it.Key}})
		}
		return out
	})

	// usage: this session's AI usage (ai.usage, numbers and names only).
	registerViews(
		ViewDef{ID: "usage", Feature: "usage", Title: "AI usage", Source: ViewSource{Op: OpAIUsage},
			Params: []ViewParam{{ID: "by", Label: "By", Kind: ParamEnum, Enum: []string{"model", "agent"}}},
			Fields: []ViewField{vf("name", "Model", FieldText, "sfd"), vf("calls", "Calls", FieldNum, "sfd"), vf("input", "Input tokens", FieldNum, "sf"),
				vf("output", "Output tokens", FieldNum, "sf"), vf("cost", "Cost", FieldCost, "sfd"), vf("billing", "Billing", FieldState, "sfd")},
			Layouts: []string{table, list}, Default: ViewSpec{Sort: []ViewSort{{Field: "cost", Desc: true}}, Args: map[string]string{"by": "model"}}, Refresh: 30},
	)
	viewRows["usage"] = rowsOf(func(s AIUsage, args map[string]string) []ViewRow {
		lines := s.ByModel
		if args["by"] == "agent" {
			lines = s.ByAgent
		}
		out := make([]ViewRow, 0, len(lines)+1)
		for _, l := range lines {
			out = append(out, ViewRow{ID: "u" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(l.Name), "calls", vnum(l.Calls), "input", vnum(l.Input),
				"output", vnum(l.Output), "cost", vusd(l.CostUSD), "billing", vtxt(l.Billing))})
		}
		return out
	})

	// lsp / ext / workspace folders / layouts
	registerViews(
		ViewDef{ID: "lsp.servers", Feature: "lsp", Title: "Language servers", Source: ViewSource{Op: OpLSPServers},
			Fields: []ViewField{vf("title", "Server", FieldText, "sfd"), vf("state", "State", FieldState, "sfd"), vf("used", "In use", FieldBool, "sfd"),
				vf("langs", "Languages", FieldText, "f"), vf("version", "Version", FieldText, ""), vf("problem", "Problem", FieldText, "f")},
			Layouts: []string{list, table}},
		ViewDef{ID: "ext.panel", Feature: "ext", Title: "Extension panel", Source: ViewSource{Op: OpExtPanel},
			Params: []ViewParam{{ID: "id", Label: "Extension", Kind: ParamString, Required: true}},
			Fields: v2LineField(), Layouts: []string{text, list}},
		ViewDef{ID: "workspace.folders", Feature: "ext", Title: "Workspace folders", Source: ViewSource{Op: OpWorkspaceList},
			Fields:  []ViewField{vf("name", "Folder", FieldText, "sfd"), vf("recent", "Recent only", FieldBool, "sf")},
			Layouts: []string{list, table}},
		ViewDef{ID: "layouts", Feature: "ext", Title: "Layouts", Source: ViewSource{Op: OpLayoutList},
			Fields: []ViewField{vf("name", "Layout", FieldText, "sfd"), vf("source", "From", FieldState, "sfd"),
				vf("about", "About", FieldText, "fd")},
			Layouts: []string{list, table}},
	)
	viewSourceArgs[OpExtPanel] = func(a map[string]string) any { return ExtIDArgs{ID: a["id"]} }
	viewRows["lsp.servers"] = rowsOf(func(s LSPServerList, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Items))
		for _, sv := range s.Items {
			tone := map[string]string{"managed": blocks.ToneOK, "system": blocks.ToneOK, "installing": blocks.ToneBusy, "missing": blocks.ToneIdle}[sv.State]
			if sv.Problem != "" {
				tone = blocks.ToneBad
			}
			out = append(out, ViewRow{ID: "l" + strconv.Itoa(len(out)), Cells: vcells("title", vtxt(sv.Title), "state", vtxt(sv.State), "used", vyes(sv.Used),
				"langs", vtxt(strings.Join(sv.Langs, ", ")), "version", vtxt(sv.Version), "problem", vtxt(sv.Problem)), Tone: tone,
				Ref: &BlockRef{Kind: "lsp_server", ID: sv.Name}})
		}
		return out
	})
	viewRows["ext.panel"] = rowsOf(func(s ExtPanelView, _ map[string]string) []ViewRow {
		lines := make([]string, 0, len(s.Lines))
		for _, l := range s.Lines {
			var b strings.Builder
			for _, sp := range l {
				b.WriteString(sp.T)
			}
			lines = append(lines, b.String())
		}
		return v2Lines(lines)
	})
	viewRows["workspace.folders"] = rowsOf(func(s WorkspaceList, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Folders)+len(s.Recent))
		for _, f := range s.Folders {
			out = append(out, ViewRow{ID: "w" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(f.Name), "recent", vyes(false)), Tone: blocks.ToneOK,
				Ref: &BlockRef{Kind: "folder", ID: strconv.Itoa(f.ID)}})
		}
		for _, f := range s.Recent {
			out = append(out, ViewRow{ID: "w" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(f.Name), "recent", vyes(true)), Tone: blocks.ToneIdle,
				Ref: &BlockRef{Kind: "folder", ID: strconv.Itoa(f.ID)}})
		}
		return out
	})
	viewRows["layouts"] = rowsOf(func(s LayoutList, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Items))
		for _, l := range s.Items {
			out = append(out, ViewRow{ID: "y" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(l.Name), "source", vtxt(l.Source), "about", vtxt(l.Description)),
				Ref: &BlockRef{Kind: "layout", ID: l.Name}})
		}
		return out
	})

	// terms / files
	registerViews(
		ViewDef{ID: "terms", Feature: "terms", Title: "Terminals", Source: ViewSource{Topic: "terms"},
			Fields: []ViewField{vf("title", "Terminal", FieldText, "sfd"), vf("busy", "Busy", FieldBool, "sfd"), vf("last", "Last command", FieldText, "fd"),
				vf("cwd", "Folder", FieldPath, "sf"), vf("task", "Task", FieldText, "sf"), vf("exited", "Ended", FieldBool, "sf")},
			Layouts: []string{list, table}},
		ViewDef{ID: "files", Feature: "files", Title: "Files", Source: ViewSource{Op: OpFilesList},
			Params:  []ViewParam{{ID: "path", Label: "In folder", Kind: ParamPath}},
			Fields:  []ViewField{vf("path", "File", FieldPath, "sfd"), vf("dir", "Folder", FieldPath, "sf")},
			Layouts: []string{list, table}, Default: ViewSpec{Limit: 200}},
	)
	viewRows["terms"] = rowsOf(func(s TermsSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Terms))
		for _, tm := range s.Terms {
			tone := blocks.ToneIdle
			switch {
			case tm.Exited:
				tone = blocks.ToneIdle
			case tm.Busy:
				tone = blocks.ToneBusy
			case tm.Active:
				tone = blocks.ToneOK
			}
			out = append(out, ViewRow{ID: "t" + strconv.Itoa(tm.Buf), Cells: vcells("title", vtxt(tm.Title), "busy", vyes(tm.Busy), "last", vtxt(tm.LastCmd),
				"cwd", vtxt(tm.Cwd), "task", vtxt(tm.Task), "exited", vyes(tm.Exited)), Tone: tone, Ref: &BlockRef{Kind: "term", ID: strconv.Itoa(tm.Buf)}})
		}
		return out
	})
	viewRows["files"] = rowsOf(func(s FilesListRes, args map[string]string) []ViewRow {
		under := strings.Trim(args["path"], "/")
		out := make([]ViewRow, 0, len(s.Paths))
		for _, p := range s.Paths {
			if under != "" && p != under && !strings.HasPrefix(p, under+"/") {
				continue
			}
			dir := ""
			if i := strings.LastIndex(p, "/"); i >= 0 {
				dir = p[:i]
			}
			out = append(out, ViewRow{ID: "f" + strconv.Itoa(len(out)), Cells: vcells("path", vtxt(p), "dir", vtxt(dir)), Ref: &BlockRef{Kind: "file", Path: p}})
		}
		return out
	})
}

// SearchHits is what the hub gathers for the search view: the hits of
// search.run's search.batch events, then its summary.
type SearchHits struct {
	Hits    []SearchHit   `json:"hits"`
	Summary SearchSummary `json:"summary"`
}
