package feature

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/views"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// The row builders: what a source gave (a topic snapshot or an op
// result, built and redacted on the laptop) as typed rows. They keep to
// the source's own text: no path is made absolute, no secret is read.

func s(v string) views.Cell  { return views.Cell{S: v, Null: v == ""} }
func n(v float64) views.Cell { return views.Cell{N: v} }
func ni(v int) views.Cell    { return views.Cell{N: float64(v)} }
func t(ms int64) views.Cell  { return views.Cell{T: ms, Null: ms == 0} }
func b(v bool) views.Cell    { return views.Cell{B: v} }

// GitStatusRows is the git topic's changed files.
func GitStatusRows(g webproto.GitSnap) []views.Row {
	out := make([]views.Row, 0, len(g.Files))
	for _, f := range g.Files {
		out = append(out, views.Row{
			ID: "f" + strconv.Itoa(len(out)),
			Cells: map[string]views.Cell{
				"path": s(f.Path), "state": s(gitFileState(f)), "staged": b(f.Staged), "x": s(f.X), "y": s(f.Y),
			},
			Ref: &blocks.BlockRef{Kind: "file", Path: f.Path},
		})
	}
	return out
}

// gitFileState is one word for a changed file: conflict first, then
// untracked, staged, changed.
func gitFileState(f webproto.GitFile) string {
	switch {
	case f.Conflict:
		return "conflict"
	case f.Untracked:
		return "untracked"
	case f.Staged && f.Unstaged:
		return "partly staged"
	case f.Staged:
		return "staged"
	}
	return "changed"
}

// GitLogRows is the git topic's last commits, newest first.
func GitLogRows(g webproto.GitSnap) []views.Row {
	out := make([]views.Row, 0, len(g.Log))
	for _, c := range g.Log {
		out = append(out, views.Row{
			ID: "c" + c.Hash,
			Cells: map[string]views.Cell{
				"subject": s(c.Subject), "hash": s(c.Hash), "author": s(c.Author), "when": t(c.When), "unpushed": b(c.Unpushed),
			},
			Ref: &blocks.BlockRef{Kind: "commit", ID: c.Hash},
		})
	}
	return out
}

// GitBranchRows is git.branches: the local branches, then the remote ones.
func GitBranchRows(g webproto.GitBranches) []views.Row {
	out := make([]views.Row, 0, len(g.Branches)+len(g.Remotes))
	add := func(name string, remote bool) {
		out = append(out, views.Row{
			ID:    "b" + strconv.Itoa(len(out)),
			Cells: map[string]views.Cell{"name": s(name), "current": b(!remote && name == g.Current), "remote": b(remote)},
			Ref:   &blocks.BlockRef{Kind: "branch", ID: name},
		})
	}
	for _, br := range g.Branches {
		add(br, false)
	}
	for _, br := range g.Remotes {
		add(br, true)
	}
	return out
}

// LaneRows is the worktrees topic.
func LaneRows(w webproto.WorktreesSnap) []views.Row {
	out := make([]views.Row, 0, len(w.Worktrees))
	for _, x := range w.Worktrees {
		out = append(out, views.Row{
			ID: "w" + x.ID,
			Cells: map[string]views.Cell{
				"title": s(x.Title), "state": s(x.State), "branch": s(x.Branch), "agent": s(x.Agent), "group": s(x.Group),
				"kind": s(x.Kind), "ahead": ni(x.Ahead), "behind": ni(x.Behind), "dirty": ni(x.Dirty), "where": s(x.Where),
				"here": b(x.Here), "seen": t(x.LastSeen),
			},
			Ref: &blocks.BlockRef{Kind: "worktree", ID: x.ID},
		})
	}
	return out
}

// RunRows is the runs topic; a run that needs the user is warn whatever
// its state says.
func RunRows(r webproto.RunsSnap) []views.Row {
	out := make([]views.Row, 0, len(r.Runs))
	for _, x := range r.Runs {
		row := views.Row{
			ID: "r" + strconv.Itoa(len(out)),
			Cells: map[string]views.Cell{
				"title": s(x.Title), "state": s(x.State), "started": t(x.Started), "step": ni(x.Step), "steps": ni(x.Steps),
				"step_title": s(x.StepTitle), "chat": s(x.Chat), "profile": s(x.Profile), "cost": n(x.CostUSD), "needs_you": b(x.NeedsYou),
			},
			Ref: &blocks.BlockRef{Kind: "run", ID: x.Key},
		}
		if x.NeedsYou {
			row.Tone = blocks.ToneWarn
		}
		out = append(out, row)
	}
	return out
}

// WorkflowRows is the workflows topic's workflows.
func WorkflowRows(w webproto.WorkflowsSnap) []views.Row {
	out := make([]views.Row, 0, len(w.Workflows))
	for _, x := range w.Workflows {
		name := x.Title
		if name == "" {
			name = x.Name
		}
		when := x.When
		if when == "" {
			when = "by hand"
		}
		out = append(out, views.Row{
			ID:    "wf" + strconv.Itoa(len(out)),
			Cells: map[string]views.Cell{"name": s(name), "state": s(x.State), "when": s(when), "next": t(x.Next), "last": s(x.Last)},
			Ref:   &blocks.BlockRef{Kind: "workflow", ID: x.Name},
		})
	}
	return out
}

// WorkflowRunRows is the workflows topic's runs.
func WorkflowRunRows(w webproto.WorkflowsSnap) []views.Row {
	out := make([]views.Row, 0, len(w.Runs))
	for _, x := range w.Runs {
		row := views.Row{
			ID: "wr" + strconv.Itoa(len(out)),
			Cells: map[string]views.Cell{
				"workflow": s(x.Workflow), "state": s(x.State), "started": t(x.Started), "ended": t(x.Ended),
				"items": ni(x.Items), "failed": ni(x.Failed), "dry": b(x.Dry), "waiting": s(x.Waiting),
			},
			Ref: &blocks.BlockRef{Kind: "workflow", ID: x.Workflow},
		}
		if x.Waiting != "" {
			row.Tone = blocks.ToneWarn
		}
		out = append(out, row)
	}
	return out
}

// TaskRows is the tasks topic. A task never run has no state.
func TaskRows(ts webproto.TasksSnap) []views.Row {
	out := make([]views.Row, 0, len(ts.Tasks))
	for _, x := range ts.Tasks {
		cells := map[string]views.Cell{
			"label": s(x.Label), "group": s(x.Group), "source": s(x.Source), "default": b(x.Default),
			"state": s(""), "when": t(0), "code": {Null: true},
		}
		if x.Last != nil {
			cells["state"], cells["when"] = s(x.Last.State), t(x.Last.When)
			if x.Last.State != "running" {
				cells["code"] = ni(x.Last.Code)
			}
		}
		out = append(out, views.Row{ID: "t" + strconv.Itoa(len(out)), Cells: cells, Ref: &blocks.BlockRef{Kind: "task", ID: x.Label}})
	}
	return out
}

var dbFieldRe = regexp.MustCompile(`[^a-z0-9_]+`)

// dbFieldID makes a field id from a column name: c_<lower snake>, cut to
// the id cap, made unique with a number.
func dbFieldID(col string, seen map[string]bool) string {
	id := strings.Trim(dbFieldRe.ReplaceAllString(strings.ToLower(col), "_"), "_")
	if id == "" {
		id = "col"
	}
	id = DBField + id
	if len(id) > 34 {
		id = id[:34]
	}
	base := id
	for i := 2; seen[id]; i++ {
		id = base + "_" + strconv.Itoa(i)
	}
	seen[id] = true
	return id
}

// DBTable is db.rows: the view with the table's columns as text fields
// (shown by default, at most a table's width) and its rows. A NULL cell
// is empty; a masked cell keeps the laptop's mask (never the value).
func DBTable(base views.Def, r webproto.DBRows) (views.Def, []views.Row) {
	d := clone(base)
	seen := map[string]bool{"row": true}
	ids := make([]string, len(r.Columns))
	for i, c := range r.Columns {
		if len(d.Fields) >= views.MaxDefFields {
			ids[i] = ""
			continue
		}
		ids[i] = dbFieldID(c, seen)
		label := c
		if label == "" {
			label = ids[i]
		}
		d.Fields = append(d.Fields, fld(ids[i], cutLabel(label), views.FieldText, len(d.Fields) < views.MaxTableFields))
	}
	out := make([]views.Row, 0, len(r.Rows))
	for i, row := range r.Rows {
		cells := map[string]views.Cell{"row": ni(r.From + i + 1)}
		for j, c := range row {
			if j >= len(ids) || ids[j] == "" {
				continue
			}
			if c.N {
				cells[ids[j]] = views.Cell{Null: true}
				continue
			}
			cells[ids[j]] = s(c.V)
		}
		out = append(out, views.Row{ID: "d" + strconv.Itoa(r.From+i), Cells: cells})
	}
	return d, out
}

func cutLabel(v string) string {
	r := []rune(v)
	if len(r) > blocks.MaxTitle {
		return string(r[:blocks.MaxTitle-1]) + "…"
	}
	return v
}

// httpState is one word for a request's outcome.
func httpState(status int, err string, running bool) string {
	switch {
	case running:
		return "running"
	case err != "":
		return "error"
	case status >= 500:
		return "failed"
	case status >= 400:
		return "warning"
	case status > 0:
		return "ok"
	}
	return ""
}

// HTTPHistoryRows is http.history, newest first as the laptop keeps it.
func HTTPHistoryRows(h webproto.HTTPHistory) []views.Row {
	out := make([]views.Row, 0, len(h.Entries))
	for _, e := range h.Entries {
		req := e.Name
		if req == "" {
			req = e.Method + " " + e.URL
		}
		out = append(out, views.Row{
			ID: "h" + strconv.Itoa(len(out)),
			Cells: map[string]views.Cell{
				"request": s(req), "state": s(httpState(e.Status, e.Err, false)), "status": ni(e.Status), "at": t(e.At),
				"method": s(e.Method), "url": s(e.URL), "ms": n(float64(e.ElapsedMS)), "size": n(float64(e.Size)),
				"file": s(e.File), "env": s(e.Env), "by": s(e.By),
			},
			Ref: &blocks.BlockRef{Kind: "http", ID: e.ID, Path: e.File},
		})
	}
	return out
}

// HTTPResponseRows is http.response as one row (a detail block). The
// headers and the body are the laptop's redacted copies.
func HTTPResponseRows(r webproto.HTTPResponse) []views.Row {
	req := r.Label
	if req == "" {
		req = r.Method + " " + r.URL
	}
	hs := make([]string, 0, len(r.Headers))
	for _, h := range r.Headers {
		hs = append(hs, h.Name+": "+h.Value)
	}
	body := r.Body
	if r.Binary {
		body = "(binary, " + strconv.FormatInt(r.Size, 10) + " bytes: open on the laptop)"
	} else if r.Truncated && body != "" {
		body += " …"
	}
	return []views.Row{{
		ID: "resp" + strconv.Itoa(r.Run),
		Cells: map[string]views.Cell{
			"request": s(req), "state": s(httpState(r.Status, r.Err, r.Running)), "status": ni(r.Status), "status_text": s(r.StatusText),
			"ms": n(float64(r.ElapsedMS)), "size": n(float64(r.Size)), "type": s(r.ContentType), "proto": s(r.Proto),
			"env": s(r.Env), "by": s(r.By), "headers": s(strings.Join(hs, "; ")), "body": s(body), "error": s(r.Err),
		},
		Ref: &blocks.BlockRef{Kind: "http", ID: strconv.Itoa(r.Run), Path: r.File},
	}}
}

// ContainerRows is containers.list.
func ContainerRows(l webproto.ContainersList) []views.Row {
	out := make([]views.Row, 0, len(l.Containers))
	for _, c := range l.Containers {
		state := c.State
		if c.Busy {
			state = "busy"
		}
		row := views.Row{
			ID: "k" + strconv.Itoa(len(out)),
			Cells: map[string]views.Cell{
				"name": s(c.Name), "state": s(state), "image": s(c.Image), "status": s(c.Status), "ports": s(c.Ports), "project": s(c.Project),
			},
			Ref: &blocks.BlockRef{Kind: "container", ID: c.ID},
		}
		if state == "running" { // up is ok here, not busy
			row.Tone = blocks.ToneOK
		}
		out = append(out, row)
	}
	return out
}

// PodRows is kube.list's pods.
func PodRows(l webproto.KubeList) []views.Row {
	out := make([]views.Row, 0, len(l.Pods))
	for _, p := range l.Pods {
		row := views.Row{
			ID: "p" + strconv.Itoa(len(out)),
			Cells: map[string]views.Cell{
				"name": s(p.Name), "status": s(p.Status), "ready": s(p.Ready), "restarts": ni(p.Restarts),
				"created": t(p.Created), "containers": s(strings.Join(p.Containers, ", ")),
			},
			Ref: &blocks.BlockRef{Kind: "pod", ID: p.Name},
		}
		if p.Status == "Running" { // up: ok, or warn when it restarted
			row.Tone = blocks.ToneOK
			if p.Restarts > 0 {
				row.Tone = blocks.ToneWarn
			}
		}
		out = append(out, row)
	}
	return out
}

// TestRows is the tests topic's test cases (packages and files sum up
// their tests; the phone lists the tests themselves).
func TestRows(ts webproto.TestsSnap) []views.Row {
	out := []views.Row{}
	for _, x := range ts.Items {
		if x.Kind != "case" {
			continue
		}
		file := x.Path
		line := x.Line
		if x.FailPath != "" {
			file, line = x.FailPath, x.FailLine
		}
		out = append(out, views.Row{
			ID: "x" + strconv.Itoa(len(out)),
			Cells: map[string]views.Cell{
				"name": s(x.Label), "state": s(x.State), "file": s(file), "ms": n(float64(x.Ms)), "message": s(x.Message),
			},
			Ref: &blocks.BlockRef{Kind: "test", ID: x.ID, Path: file, Line: line},
		})
	}
	return out
}

// CheckRows is ci.status: the branch's workflow runs.
func CheckRows(c webproto.CIStatus) []views.Row {
	out := make([]views.Row, 0, len(c.Runs))
	for _, r := range c.Runs {
		out = append(out, views.Row{
			ID: "ci" + strconv.Itoa(len(out)),
			Cells: map[string]views.Cell{
				"workflow": s(r.Workflow), "state": s(r.State), "title": s(r.Title), "status": s(r.Status), "conclusion": s(r.Conclusion),
			},
			Ref: &blocks.BlockRef{Kind: "ci", ID: r.Workflow},
		})
	}
	return out
}

// prReview is a review decision in plain words.
var prReview = map[string]string{"APPROVED": "approved", "CHANGES_REQUESTED": "changes requested", "REVIEW_REQUIRED": "review required"}

// PRRows is pr.list.
func PRRows(l webproto.PRList) []views.Row {
	out := make([]views.Row, 0, len(l.Items))
	for _, p := range l.Items {
		out = append(out, views.Row{
			ID: "pr" + strconv.Itoa(p.Number),
			Cells: map[string]views.Cell{
				"title": s(p.Title), "number": ni(p.Number), "checks": s(p.Checks), "review": s(prReview[p.Review]),
				"author": s(p.Author), "branch": s(p.Branch), "base": s(p.Base), "draft": b(p.Draft),
			},
			Ref: &blocks.BlockRef{Kind: "pr", ID: strconv.Itoa(p.Number)},
		})
	}
	return out
}

// UsageLine is one row of the laptop's USAGE table (by model or by
// agent step). Cost is the known prices only; Priced says there was one.
type UsageLine struct {
	Name           string
	Calls, In, Out int
	Cost           float64
	Priced         bool
	Billing        string // per token | subscription | mixed | price unknown
}

// UsageRows is the usage view's rows. A line with no known price has no
// cost (never $0.00): a subscription is not billed per token.
func UsageRows(ls []UsageLine) []views.Row {
	out := make([]views.Row, 0, len(ls))
	for _, l := range ls {
		cost := views.Cell{Null: true}
		if l.Priced {
			cost = n(l.Cost)
		}
		out = append(out, views.Row{
			ID: "u" + strconv.Itoa(len(out)),
			Cells: map[string]views.Cell{
				"name": s(l.Name), "cost": cost, "calls": ni(l.Calls), "input": ni(l.In), "output": ni(l.Out), "billing": s(l.Billing),
			},
		})
	}
	return out
}

// MaxLogBytes caps the text of a log block (the block's own cap is
// blocks.MaxBlockBytes; the rest is its title and spec).
const MaxLogBytes = 48 << 10

// LogBlock is a log view's block: the newest lines as one code block of
// text, cut from the top to MaxLogBytes. lines are the laptop's redacted
// lines; cut says the source already left older lines out.
func LogBlock(id string, d *views.Def, spec blocks.ViewSpec, lines []string, cut bool) blocks.Block {
	keep, size := len(lines), 0
	for keep > 0 && size+len(lines[keep-1])+1 <= MaxLogBytes {
		size += len(lines[keep-1]) + 1
		keep--
	}
	if keep > 0 {
		cut = true
	}
	text := strings.Join(lines[keep:], "\n")
	title := spec.Title
	if title == "" {
		title = d.Title
	}
	sp := spec
	blk := blocks.Block{
		ID: id, Kind: blocks.KindText, Title: cutLabel(title), Source: d.ID, Spec: &sp,
		MD:    &blocks.MdNode{K: "doc", C: []blocks.MdNode{{K: "codeblock", Text: text}}},
		Total: len(lines), Truncated: cut,
	}
	if len(lines) == 0 {
		blk.MD = &blocks.MdNode{K: "doc", C: []blocks.MdNode{{K: "p", C: []blocks.MdNode{{K: "text", Text: "No output yet."}}}}}
	}
	return blk
}
