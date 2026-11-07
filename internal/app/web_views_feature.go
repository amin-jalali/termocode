package app

import (
	"encoding/json"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/views"
	"github.com/amin-jalali/termocode/internal/views/feature"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The feature views on the hub (MOBUX-13, mobile plan 3.11.8): one fetch
// per view of internal/views/feature. A topic-backed view reads the same
// build the topic sends (never the topic's publish state); an op-backed
// view runs the op's own handler in-process (webInnerCall: the same
// policy row, role, arg checks and audit as from the page); the usage
// view reads the laptop's USAGE data. The spec is applied here, on the
// laptop, by views.Render; the phone only draws the block.

// webViewOut is what a view's fetch gave.
type webViewOut struct {
	Def   *views.Def  // a def made for these rows (db.rows: the table's columns); nil: the registry's
	Rows  []views.Row // a row view
	Lines []string    // a text view (logs), redacted
	Text  bool        // Lines is the block
	Cut   bool        // the source itself left rows or lines out
}

// webViewDone hears a fetch on Update.
type webViewDone func(m *Model, out webViewOut, err error) tea.Cmd

// webViewFetch fetches one view's data for call's connection, on Update.
type webViewFetch func(m *Model, call *webCall, d *views.Def, spec blocks.ViewSpec, done webViewDone) tea.Cmd

// webViewError is a source op's refusal, kept with its code so view.run
// answers as the op would have.
type webViewError struct {
	Code webproto.ErrCode
	Msg  string
}

func (e *webViewError) Error() string { return e.Msg }

// webFeatureFetches are the fetches by view id.
var webFeatureFetches = map[string]webViewFetch{}

// webViewRows is a fetch that reads on Update and answers at once.
func webViewRows(rows func(m *Model, spec blocks.ViewSpec) ([]views.Row, bool)) webViewFetch {
	return func(m *Model, _ *webCall, _ *views.Def, spec blocks.ViewSpec, done webViewDone) tea.Cmd {
		rs, cut := rows(m, spec)
		return done(m, webViewOut{Rows: rs, Cut: cut}, nil)
	}
}

// webViewByOp is a fetch through a read op's handler: args builds the
// op's args from the resolved spec (nil: none), conv the rows from its
// result.
func webViewByOp[T any](op string, args func(spec blocks.ViewSpec) any, conv func(d *views.Def, r T) webViewOut) webViewFetch {
	return func(m *Model, call *webCall, d *views.Def, spec blocks.ViewSpec, done webViewDone) tea.Cmd {
		var a any = struct{}{}
		if args != nil {
			a = args(spec)
		}
		return m.webInnerCall(call, op, a, func(m *Model, data json.RawMessage, e *webproto.ErrMsg) tea.Cmd {
			if e != nil {
				return done(m, webViewOut{}, &webViewError{Code: e.Code, Msg: e.Msg})
			}
			var r T
			if err := json.Unmarshal(data, &r); err != nil {
				return done(m, webViewOut{}, err)
			}
			return done(m, conv(d, r), nil)
		})
	}
}

// webViewRedact hides keys and tokens (patterns and the laptop's known
// secrets) in text a view shows.
func webViewRedact(v string) string { return webText(ai.Redact(v)) }

// webViewLines is a text view's output: each line redacted and cleaned.
func webViewLines(lines []string, cut bool) webViewOut {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = webViewRedact(l)
	}
	return webViewOut{Lines: out, Text: true, Cut: cut}
}

func init() {
	git := func(rows func(webproto.GitSnap) []views.Row) webViewFetch {
		return webViewRows(func(m *Model, _ blocks.ViewSpec) ([]views.Row, bool) {
			g := m.webGitBuild()
			return rows(g), g.More > 0
		})
	}
	webFeatureFetches[feature.ViewGitStatus] = git(feature.GitStatusRows)
	webFeatureFetches[feature.ViewGitLog] = git(feature.GitLogRows)
	webFeatureFetches[feature.ViewGitBranches] = webViewByOp(webproto.OpGitBranches, nil, func(_ *views.Def, r webproto.GitBranches) webViewOut {
		return webViewOut{Rows: feature.GitBranchRows(r), Cut: r.More > 0}
	})
	webFeatureFetches[feature.ViewLanes] = webViewRows(func(m *Model, _ blocks.ViewSpec) ([]views.Row, bool) {
		w := m.webWorktreesBuild()
		return feature.LaneRows(w), w.More > 0
	})
	webFeatureFetches[feature.ViewRuns] = webViewRows(func(m *Model, _ blocks.ViewSpec) ([]views.Row, bool) {
		return feature.RunRows(m.webRunsBuild(webNow())), false
	})
	webFeatureFetches[feature.ViewWorkflows] = webViewRows(func(m *Model, _ blocks.ViewSpec) ([]views.Row, bool) {
		return feature.WorkflowRows(m.webWorkflowsBuild()), false
	})
	webFeatureFetches[feature.ViewWorkflowRuns] = webViewRows(func(m *Model, _ blocks.ViewSpec) ([]views.Row, bool) {
		return feature.WorkflowRunRows(m.webWorkflowsBuild()), false
	})
	webFeatureFetches[feature.ViewTasks] = webViewRows(func(m *Model, _ blocks.ViewSpec) ([]views.Row, bool) {
		return feature.TaskRows(m.webTasksBuild()), false
	})
	webFeatureFetches[feature.ViewTests] = webViewRows(func(m *Model, _ blocks.ViewSpec) ([]views.Row, bool) {
		ts := m.webTestsBuild()
		return feature.TestRows(ts), ts.More > 0
	})
	webFeatureFetches[feature.ViewUsage] = webViewRows(func(m *Model, spec blocks.ViewSpec) ([]views.Row, bool) {
		return feature.UsageRows(m.webUsageLines(spec.Args["by"])), false
	})

	webFeatureFetches[feature.ViewDBRows] = webViewByOp(webproto.OpDBRows,
		func(s blocks.ViewSpec) any { return webproto.DBRowsArgs{Conn: s.Args["conn"], Table: s.Args["table"]} },
		func(d *views.Def, r webproto.DBRows) webViewOut {
			def, rows := feature.DBTable(*d, r)
			return webViewOut{Def: &def, Rows: rows, Cut: r.Next != 0 || r.Truncated}
		})
	webFeatureFetches[feature.ViewHTTPHistory] = webViewByOp(webproto.OpHTTPHistory, nil, func(_ *views.Def, r webproto.HTTPHistory) webViewOut {
		return webViewOut{Rows: feature.HTTPHistoryRows(r)}
	})
	webFeatureFetches[feature.ViewHTTPResponse] = webViewByOp(webproto.OpHTTPResponse,
		func(s blocks.ViewSpec) any { return webproto.HTTPRunArgs{Run: webViewInt(s.Args["run"])} },
		func(_ *views.Def, r webproto.HTTPResponse) webViewOut {
			r.Body = webViewRedact(r.Body)
			for i := range r.Headers {
				r.Headers[i].Value = webViewRedact(r.Headers[i].Value)
			}
			return webViewOut{Rows: feature.HTTPResponseRows(r), Cut: r.Truncated}
		})
	webFeatureFetches[feature.ViewContainers] = webViewByOp(webproto.OpContainersList, nil, func(_ *views.Def, r webproto.ContainersList) webViewOut {
		return webViewOut{Rows: feature.ContainerRows(r), Cut: r.More > 0}
	})
	webFeatureFetches[feature.ViewKubePods] = webViewByOp(webproto.OpKubeList,
		func(s blocks.ViewSpec) any {
			return webproto.KubeScopeArgs{Context: s.Args["context"], Namespace: s.Args["namespace"]}
		},
		func(_ *views.Def, r webproto.KubeList) webViewOut {
			return webViewOut{Rows: feature.PodRows(r), Cut: r.More > 0}
		})
	webFeatureFetches[feature.ViewCIChecks] = webViewByOp(webproto.OpCIStatus, nil, func(_ *views.Def, r webproto.CIStatus) webViewOut {
		return webViewOut{Rows: feature.CheckRows(r)}
	})
	webFeatureFetches[feature.ViewPRList] = webViewByOp(webproto.OpPRList, nil, func(_ *views.Def, r webproto.PRList) webViewOut {
		return webViewOut{Rows: feature.PRRows(r)}
	})

	webFeatureFetches[feature.ViewLogsTask] = webViewByOp(webproto.OpTasksTail,
		func(s blocks.ViewSpec) any { return webproto.LabelArgs{Label: s.Args["label"]} },
		func(_ *views.Def, r webproto.TaskTail) webViewOut { return webViewLines(r.Lines, r.Truncated) })
	webFeatureFetches[feature.ViewLogsContainer] = webViewByOp(webproto.OpContainersLogs,
		func(s blocks.ViewSpec) any { return webproto.ContainerLogsArgs{ID: s.Args["id"]} },
		func(_ *views.Def, r webproto.ContainerLogs) webViewOut { return webViewLines(r.Lines, r.Truncated) })
	webFeatureFetches[feature.ViewLogsPod] = webViewByOp(webproto.OpKubeLogs,
		func(s blocks.ViewSpec) any {
			return webproto.KubeLogsArgs{Pod: s.Args["pod"], Container: s.Args["container"], Context: s.Args["context"], Namespace: s.Args["namespace"]}
		},
		func(_ *views.Def, r webproto.KubeText) webViewOut { return webViewLines(r.Lines, r.Truncated) })
}

// webViewInt reads a checked int arg (0 when empty).
func webViewInt(v string) int {
	n := 0
	_, _ = fmt.Sscan(v, &n)
	return n
}

// webUsageLines is the laptop's USAGE table by model or by agent step
// ("agent"), as the usage_summary tool reads it.
func (m *Model) webUsageLines(by string) []feature.UsageLine {
	var log []aiUsageEntry
	if m.ai != nil {
		log = m.ai.usageLog
	}
	group := aiUsageByModel
	if by == "agent" {
		group = aiUsageByAgent
	}
	out := []feature.UsageLine{}
	for _, l := range aiUsageLines(log, group) {
		u := feature.UsageLine{Name: l.Name, Calls: l.Calls, In: l.TokensIn, Out: l.TokensOut, Billing: l.Billing}
		if l.CostUSD != nil {
			u.Cost, u.Priced = *l.CostUSD, true
		}
		out = append(out, u)
	}
	return out
}

// webFeatureView runs one feature view's spec for call's connection: it
// checks the spec, fetches on the laptop, applies the spec and hands done
// the block (on Update). id is the block's id. A source op's refusal
// comes back as a *webViewError with the op's code.
func (m *Model) webFeatureView(call *webCall, id string, spec blocks.ViewSpec, done func(m *Model, b blocks.Block, err error) tea.Cmd) tea.Cmd {
	d, ok := feature.Lookup(spec.View)
	fetch := webFeatureFetches[spec.View]
	if !ok || fetch == nil {
		return done(m, blocks.Block{}, &webViewError{Code: webproto.CodeNotFound, Msg: "There is no view " + webName(spec.View) + "."})
	}
	if d.Role == string(webproto.RoleController) && (call.conn == nil || call.conn.role != webproto.RoleController) {
		return done(m, blocks.Block{}, &webViewError{Code: webproto.CodeForbidden, Msg: "You may not do that from here."})
	}
	// db.rows names the table's own columns, known only once the rows
	// came: its args are checked first, the rest against the full def.
	first := spec
	if d.ID == feature.ViewDBRows {
		first = blocks.ViewSpec{View: spec.View, WS: spec.WS, Args: spec.Args, Limit: spec.Limit, Layout: spec.Layout, Density: spec.Density, Title: spec.Title, Refresh: spec.Refresh}
	}
	rs, err := views.Resolve(&d, first)
	if err != nil {
		return done(m, blocks.Block{}, err)
	}
	return fetch(m, call, &d, rs, func(m *Model, out webViewOut, err error) tea.Cmd {
		if err != nil {
			return done(m, blocks.Block{}, err)
		}
		def := &d
		if out.Def != nil {
			def = out.Def
			if rs, err = views.Resolve(def, spec); err != nil {
				return done(m, blocks.Block{}, err)
			}
		}
		var b blocks.Block
		if out.Text {
			b = feature.LogBlock(id, def, rs, out.Lines, out.Cut)
		} else if b, err = views.Render(id, def, rs, out.Rows, out.Cut, webNow().UnixMilli()); err != nil {
			return done(m, blocks.Block{}, err)
		}
		b.WS = spec.WS
		b.Live = def.Live
		if err := b.Validate(); err != nil {
			return done(m, blocks.Block{}, err)
		}
		return done(m, b, nil)
	})
}

// webInnerCall runs a read op's own handler for parent's connection, in
// process: the op's row must exist and need no laptop card, the
// connection's role must allow it, and its args are checked strictly as
// from the page. done hears the answer on Update (at once, or from the
// handler's later answer). The call is audited as its row says.
func (m *Model) webInnerCall(parent *webCall, op string, args any, done func(m *Model, data json.RawMessage, e *webproto.ErrMsg) tea.Cmd) tea.Cmd {
	fail := func(code webproto.ErrCode, msg string) tea.Cmd {
		return done(m, nil, &webproto.ErrMsg{T: webproto.KindErr, ID: parent.id, Code: code, Msg: msg})
	}
	p, ok := webproto.Lookup(op)
	if !ok || p.LaptopCard || p.Gateway {
		return fail(webproto.CodeNotFound, "This is not available yet.")
	}
	if parent.conn == nil || !webproto.Allowed(parent.conn.role, op) {
		return fail(webproto.CodeForbidden, "You may not do that from here.")
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return fail(webproto.CodeInternal, "Something went wrong.")
	}
	a, err := webproto.DecodeArgs(op, raw)
	if err != nil {
		return fail(webproto.CodeBadRequest, "That request is not valid.")
	}
	ops := webOps
	if parent.conn.hub != nil && parent.conn.hub.ops != nil {
		ops = parent.conn.hub.ops
	}
	fn := ops[op]
	if fn == nil {
		return fail(webproto.CodeNotFound, "This is not available yet.")
	}
	ch := make(chan webproto.Msg, 1)
	inner := &webCall{ctx: parent.ctx, conn: parent.conn, id: parent.id, op: op, args: a, sink: func(msg webproto.Msg) { ch <- msg }}
	finish := func(m *Model, msg webproto.Msg) tea.Cmd {
		switch r := msg.(type) {
		case *webproto.Res:
			return done(m, r.Data, nil)
		case *webproto.ErrMsg:
			return done(m, nil, r)
		}
		return done(m, nil, &webproto.ErrMsg{T: webproto.KindErr, ID: parent.id, Code: webproto.CodeInternal, Msg: "Something went wrong."})
	}
	cmd := m.webRunOp(fn, inner)
	select {
	case msg := <-ch:
		return tea.Batch(cmd, finish(m, msg))
	default:
	}
	if !inner.deferred {
		recordError("[web] " + op + " gave no answer (view)")
		return tea.Batch(cmd, fail(webproto.CodeInternal, "Something went wrong."))
	}
	ctx := inner.ctx
	wait := func() tea.Msg {
		select {
		case msg := <-ch:
			return applyMsg(func(m *Model) tea.Cmd { return finish(m, msg) })
		case <-ctx.Done():
			return nil
		}
	}
	return tea.Batch(cmd, wait)
}
