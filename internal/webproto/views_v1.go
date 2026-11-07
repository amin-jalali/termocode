package webproto

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// The v1 views (mobile plan 3.11.8, MOB-9): git, lanes, runs, workflows,
// tasks, projects, inbox, cards, chats, ci and the MCP status. Each one's
// rows come from its source's JSON (a topic snapshot or an op result, as
// on the wire) through its rows func, so a fixture is the source JSON, a
// spec and the block it makes (testdata/views/*.json).

// viewRowsFunc makes the rows of a view from its source's JSON. A view
// that is one block by nature (a diff) returns that block instead.
type viewRowsFunc func(args map[string]string, src json.RawMessage) ([]ViewRow, *Block, error)

var viewRows = map[string]viewRowsFunc{}

// ViewRows makes the rows (or the whole block) of view id from src.
func ViewRows(id string, args map[string]string, src json.RawMessage) ([]ViewRow, *Block, error) {
	fn := viewRows[id]
	if fn == nil {
		return nil, nil, fmt.Errorf("view %s has no rows yet", id)
	}
	return fn(args, src)
}

// SourceArgs are the args of a view's source op, from the spec's args
// (only for op-backed views; nil: the op takes none).
func SourceArgs(d ViewDef, args map[string]string) any {
	switch d.Source.Op {
	case OpPRView:
		n, _ := strconv.Atoi(args["number"])
		return PRNumberArgs{Number: n}
	case OpGitDiff:
		return GitDiffArgs{Path: args["path"], Staged: args["side"] == "staged"}
	case OpTasksTail:
		return LabelArgs{Label: args["task"]}
	}
	if v, ok := sourceArgsV2(d.Source.Op, args); ok {
		return v
	}
	return EmptyArgs{}
}

func rowsOf[T any](fn func(T, map[string]string) []ViewRow) viewRowsFunc {
	return func(args map[string]string, src json.RawMessage) ([]ViewRow, *Block, error) {
		var v T
		if err := json.Unmarshal(src, &v); err != nil {
			return nil, nil, err
		}
		return fn(v, args), nil, nil
	}
}

func vtxt(s string) ViewCell  { return ViewCell{S: s} }
func vnum(n int) ViewCell     { return ViewCell{N: float64(n)} }
func vwhen(ms int64) ViewCell { return ViewCell{T: ms} }
func vyes(b bool) ViewCell    { return ViewCell{B: b} }
func vusd(v float64) ViewCell { return ViewCell{N: v} }
func vcells(kv ...any) map[string]ViewCell {
	m := make(map[string]ViewCell, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i].(string)] = kv[i+1].(ViewCell)
	}
	return m
}

// vf makes a field: kind plus flags "s" sortable, "f" filterable, "d" shown
// by default.
func vf(id, label, kind, flags string) ViewField {
	return ViewField{ID: id, Label: label, Kind: kind, Sortable: strings.Contains(flags, "s"),
		Filterable: strings.Contains(flags, "f"), Default: strings.Contains(flags, "d")}
}

// stateTone maps the many state words of the topics to a tone.
func stateTone(s string) string {
	switch s {
	case "fail", "failed", "bad", "conflict", "broken", "error", "CHANGES_REQUESTED":
		return blocks.ToneBad
	case "needs_you", "waiting", "stalled", "pending", "untrusted", "paused", "pausing", "resumable":
		return blocks.ToneWarn
	case "running", "working", "busy", "in_progress", "queued":
		return blocks.ToneBusy
	case "ok", "pass", "done", "ready", "on", "APPROVED", "here":
		return blocks.ToneOK
	case "", "none", "idle", "off", "asleep", "cancelled", "archived", "gone":
		return blocks.ToneIdle
	}
	return blocks.ToneInfo
}

func init() {
	list, table, board := blocks.KindList, blocks.KindTable, blocks.KindBoard
	byNew := []ViewSort{{Field: "when", Desc: true}}

	// git
	registerViews(
		ViewDef{ID: "git.status", Feature: "git", Title: "Changes", Source: ViewSource{Topic: "git"},
			Fields:  []ViewField{vf("path", "File", FieldPath, "sfd"), vf("state", "State", FieldState, "sfd")},
			Layouts: []string{list, table}, Default: ViewSpec{Group: "state"}},
		ViewDef{ID: "git.log", Feature: "git", Title: "Commits", Source: ViewSource{Topic: "git"},
			Fields: []ViewField{vf("subject", "Subject", FieldText, "sfd"), vf("hash", "Commit", FieldText, "fd"),
				vf("author", "Author", FieldText, "sfd"), vf("when", "When", FieldTime, "sfd"), vf("unpushed", "Not pushed", FieldBool, "sf")},
			Layouts: []string{list, table}},
		ViewDef{ID: "git.stash", Feature: "git", Title: "Stashes", Source: ViewSource{Topic: "git"},
			Fields:  []ViewField{vf("message", "Message", FieldText, "sfd"), vf("ref", "Stash", FieldText, "fd")},
			Layouts: []string{list, table}},
		ViewDef{ID: "git.branches", Feature: "git", Title: "Branches", Source: ViewSource{Op: OpGitBranches},
			Fields: []ViewField{vf("name", "Branch", FieldText, "sfd"), vf("current", "Current", FieldBool, "sfd"),
				vf("remote", "Remote", FieldBool, "sf")},
			Layouts: []string{list, table}},
		ViewDef{ID: "git.tags", Feature: "git", Title: "Tags", Source: ViewSource{Op: OpGitTags},
			Fields:  []ViewField{vf("name", "Tag", FieldText, "sfd")},
			Layouts: []string{list, table}},
		ViewDef{ID: "git.diff", Feature: "git", Title: "Diff", Source: ViewSource{Op: OpGitDiff},
			Params: []ViewParam{{ID: "path", Label: "File", Kind: ParamPath, Required: true},
				{ID: "side", Label: "Side", Kind: ParamEnum, Enum: []string{"changes", "staged"}}},
			Fields:  []ViewField{vf("line", "Line", FieldText, "d")},
			Layouts: []string{blocks.KindDiff}},
	)
	viewRows["git.status"] = rowsOf(func(s GitSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Files))
		for _, fl := range s.Files {
			st := "changed"
			switch {
			case fl.Conflict:
				st = "conflict"
			case fl.Untracked:
				st = "new"
			case fl.Staged && !fl.Unstaged:
				st = "staged"
			}
			tone := map[string]string{"conflict": blocks.ToneBad, "new": blocks.ToneInfo, "staged": blocks.ToneOK}[st]
			if tone == "" {
				tone = blocks.ToneWarn
			}
			out = append(out, ViewRow{ID: "f" + strconv.Itoa(len(out)), Cells: vcells("path", vtxt(fl.Path), "state", vtxt(st)), Tone: tone,
				Ref: &BlockRef{Kind: "file", Path: fl.Path}})
		}
		return out
	})
	viewRows["git.log"] = rowsOf(func(s GitSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Log))
		for _, c := range s.Log {
			out = append(out, ViewRow{ID: "c" + c.Hash, Cells: vcells("subject", vtxt(c.Subject), "hash", vtxt(c.Hash), "author", vtxt(c.Author),
				"when", vwhen(c.When), "unpushed", vyes(c.Unpushed)), Ref: &BlockRef{Kind: "commit", ID: c.Hash}})
		}
		return out
	})
	viewRows["git.stash"] = rowsOf(func(s GitSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Stashes))
		for i, st := range s.Stashes {
			out = append(out, ViewRow{ID: "s" + strconv.Itoa(i), Cells: vcells("message", vtxt(st.Message), "ref", vtxt(st.Ref)),
				Ref: &BlockRef{Kind: "stash", ID: st.Ref}})
		}
		return out
	})
	viewRows["git.branches"] = rowsOf(func(s GitBranches, _ map[string]string) []ViewRow {
		var out []ViewRow
		for _, b := range s.Branches {
			tone := ""
			if b == s.Current {
				tone = blocks.ToneOK
			}
			out = append(out, ViewRow{ID: "b" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(b), "current", vyes(b == s.Current), "remote", vyes(false)),
				Tone: tone, Ref: &BlockRef{Kind: "branch", ID: b}})
		}
		for _, b := range s.Remotes {
			out = append(out, ViewRow{ID: "b" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(b), "current", vyes(false), "remote", vyes(true)),
				Ref: &BlockRef{Kind: "branch", ID: b}})
		}
		return out
	})
	viewRows["git.tags"] = rowsOf(func(s GitTags, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Tags))
		for i, t := range s.Tags {
			out = append(out, ViewRow{ID: "t" + strconv.Itoa(i), Cells: vcells("name", vtxt(t)), Ref: &BlockRef{Kind: "tag", ID: t}})
		}
		return out
	})
	viewRows["git.diff"] = func(args map[string]string, src json.RawMessage) ([]ViewRow, *Block, error) {
		var d GitDiff
		if err := json.Unmarshal(src, &d); err != nil {
			return nil, nil, err
		}
		return nil, gitDiffBlock(d), nil
	}

	// lanes
	registerViews(
		ViewDef{ID: "lanes", Feature: "lanes", Title: "Lanes", Source: ViewSource{Topic: "worktrees"},
			Fields: []ViewField{vf("title", "Lane", FieldText, "sfd"), vf("branch", "Branch", FieldText, "sf"),
				vf("state", "State", FieldState, "sfd"), vf("kind", "Kind", FieldState, "sf"), vf("group", "Group", FieldState, "sf"),
				vf("agents", "Agents", FieldNum, "sf"), vf("dirty", "Changed files", FieldNum, "sfd"),
				vf("ahead", "Ahead", FieldNum, "sf"), vf("behind", "Behind", FieldNum, "sf"), vf("where", "Where", FieldPath, "f")},
			Layouts: []string{list, table, board}},
		ViewDef{ID: "lanes.agent", Feature: "lanes", Title: "Agent lanes", Source: ViewSource{Op: OpAIWorktrees},
			Fields: []ViewField{vf("branch", "Branch", FieldText, "sfd"), vf("owner", "Agent", FieldText, "sfd"),
				vf("running", "Running", FieldBool, "sfd"), vf("files", "Files", FieldNum, "sfd")},
			Layouts: []string{list, table}},
	)
	viewRows["lanes"] = rowsOf(func(s WorktreesSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Worktrees))
		for _, w := range s.Worktrees {
			out = append(out, ViewRow{ID: "w" + strconv.Itoa(len(out)), Cells: vcells("title", vtxt(w.Title), "branch", vtxt(w.Branch), "state", vtxt(w.State),
				"kind", vtxt(w.Kind), "group", vtxt(w.Group), "agents", vnum(w.Agents), "dirty", vnum(w.Dirty), "ahead", vnum(w.Ahead),
				"behind", vnum(w.Behind), "where", vtxt(w.Where)), Tone: stateTone(w.State), Ref: &BlockRef{Kind: "lane", ID: w.ID}})
		}
		return out
	})
	// races (MOB-61): the races and how many lanes each has; a lane's
	// own rows are the race screen's business.
	registerViews(ViewDef{ID: "races", Feature: "lanes", Title: "Races", Source: ViewSource{Topic: "races"},
		Fields: []ViewField{vf("name", "Race", FieldText, "sfd"), vf("state", "State", FieldState, "sfd"),
			vf("lanes", "Lanes", FieldNum, "sfd"), vf("winner", "Winner", FieldText, "sf")},
		Layouts: []string{list, table}})
	viewRows["races"] = rowsOf(func(s RacesSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Races))
		for _, r := range s.Races {
			out = append(out, ViewRow{ID: "r" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(r.Name), "state", vtxt(r.State),
				"lanes", vnum(len(r.Lanes)), "winner", vtxt(r.Winner)), Tone: stateTone(r.State)})
		}
		return out
	})
	viewRows["lanes.agent"] = rowsOf(func(s AIWorktrees, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Items))
		for _, w := range s.Items {
			tone := blocks.ToneIdle
			if w.Running {
				tone = blocks.ToneBusy
			}
			out = append(out, ViewRow{ID: "a" + strconv.Itoa(len(out)), Cells: vcells("branch", vtxt(w.Branch), "owner", vtxt(w.Owner),
				"running", vyes(w.Running), "files", vnum(len(w.Files))), Tone: tone, Ref: &BlockRef{Kind: "lane", ID: w.ID}})
		}
		return out
	})

	// runs
	registerViews(
		ViewDef{ID: "runs", Feature: "runs", Title: "AI runs", Source: ViewSource{Topic: "runs"},
			Fields: []ViewField{vf("title", "Request", FieldText, "sfd"), vf("state", "State", FieldState, "sfd"),
				vf("step", "Step", FieldNum, "s"), vf("steps", "Steps", FieldNum, "sf"), vf("when", "Started", FieldTime, "sfd"),
				vf("cost", "Cost", FieldCost, "sfd"), vf("profile", "Profile", FieldText, "sf"), vf("chat", "Chat", FieldText, "sf"),
				vf("needs_you", "Needs you", FieldBool, "sf")},
			Layouts: []string{list, table}, Default: ViewSpec{Sort: byNew}},
	)
	viewRows["runs"] = rowsOf(func(s RunsSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Runs))
		for _, r := range s.Runs {
			tone := stateTone(r.State)
			if r.NeedsYou {
				tone = blocks.ToneWarn
			}
			out = append(out, ViewRow{ID: "r" + strconv.Itoa(len(out)), Cells: vcells("title", vtxt(r.Title), "state", vtxt(r.State), "step", vnum(r.Step),
				"steps", vnum(r.Steps), "when", vwhen(r.Started), "cost", vusd(r.CostUSD), "profile", vtxt(r.Profile), "chat", vtxt(r.Chat),
				"needs_you", vyes(r.NeedsYou)), Tone: tone, Ref: &BlockRef{Kind: "run", ID: r.Key}})
		}
		return out
	})

	// workflows
	registerViews(
		ViewDef{ID: "workflows", Feature: "workflows", Title: "Workflows", Source: ViewSource{Topic: "workflows"},
			Fields: []ViewField{vf("title", "Workflow", FieldText, "sfd"), vf("state", "State", FieldState, "sfd"),
				vf("when", "Schedule", FieldText, "fd"), vf("next", "Next run", FieldTime, "sf"), vf("last", "Last run", FieldState, "sf")},
			Layouts: []string{list, table}},
		ViewDef{ID: "workflows.runs", Feature: "workflows", Title: "Workflow runs", Source: ViewSource{Topic: "workflows"},
			Fields: []ViewField{vf("workflow", "Workflow", FieldText, "sfd"), vf("state", "State", FieldState, "sfd"),
				vf("when", "Started", FieldTime, "sfd"), vf("ended", "Ended", FieldTime, "sf"), vf("items", "Items", FieldNum, "sf"),
				vf("failed", "Failed", FieldNum, "sf"), vf("dry", "Dry run", FieldBool, "sf")},
			Layouts: []string{table, list}, Default: ViewSpec{Sort: byNew}},
		ViewDef{ID: "workflow.tail", Feature: "workflows", Title: "Workflow run output", Source: ViewSource{Topic: "workflows"},
			Params:  []ViewParam{{ID: "run", Label: "Run", Kind: ParamString, Required: true}},
			Fields:  []ViewField{vf("line", "Line", FieldText, "fd")},
			Layouts: []string{blocks.KindText, list}},
	)
	viewRows["workflows"] = rowsOf(func(s WorkflowsSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Workflows))
		for _, w := range s.Workflows {
			title := w.Title
			if title == "" {
				title = w.Name
			}
			out = append(out, ViewRow{ID: "w" + strconv.Itoa(len(out)), Cells: vcells("title", vtxt(title), "state", vtxt(w.State), "when", vtxt(w.When),
				"next", vwhen(w.Next), "last", vtxt(w.Last)), Tone: stateTone(w.State), Ref: &BlockRef{Kind: "workflow", ID: w.Name}})
		}
		return out
	})
	viewRows["workflows.runs"] = rowsOf(func(s WorkflowsSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Runs))
		for _, r := range s.Runs {
			out = append(out, ViewRow{ID: "r" + strconv.Itoa(len(out)), Cells: vcells("workflow", vtxt(r.Workflow), "state", vtxt(r.State),
				"when", vwhen(r.Started), "ended", vwhen(r.Ended), "items", vnum(r.Items), "failed", vnum(r.Failed), "dry", vyes(r.Dry)),
				Tone: stateTone(r.State), Ref: &BlockRef{Kind: "workflow_run", ID: r.ID}})
		}
		return out
	})
	viewRows["workflow.tail"] = rowsOf(func(s WorkflowsSnap, args map[string]string) []ViewRow {
		var out []ViewRow
		for _, r := range s.Runs {
			if r.ID != args["run"] {
				continue
			}
			for i, l := range r.Tail {
				out = append(out, ViewRow{ID: "l" + strconv.Itoa(i), Cells: vcells("line", vtxt(l))})
			}
		}
		return out
	})

	// tasks
	registerViews(
		ViewDef{ID: "tasks", Feature: "tasks", Title: "Build tasks", Source: ViewSource{Topic: "tasks"},
			Fields: []ViewField{vf("label", "Task", FieldText, "sfd"), vf("group", "Group", FieldState, "sf"),
				vf("source", "From", FieldText, "sf"), vf("state", "Last run", FieldState, "sfd"), vf("when", "Ran", FieldTime, "sfd")},
			Layouts: []string{list, table}},
		ViewDef{ID: "task.tail", Feature: "tasks", Title: "Task output", Source: ViewSource{Op: OpTasksTail},
			Params:  []ViewParam{{ID: "task", Label: "Task", Kind: ParamString, Required: true}},
			Fields:  []ViewField{vf("line", "Line", FieldText, "fd")},
			Layouts: []string{blocks.KindText, list}, Refresh: 10},
	)
	viewRows["tasks"] = rowsOf(func(s TasksSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Tasks))
		for _, t := range s.Tasks {
			st, at := "", int64(0)
			if t.Last != nil {
				st, at = t.Last.State, t.Last.When
			}
			out = append(out, ViewRow{ID: "t" + strconv.Itoa(len(out)), Cells: vcells("label", vtxt(t.Label), "group", vtxt(t.Group),
				"source", vtxt(t.Source), "state", vtxt(st), "when", vwhen(at)), Tone: stateTone(st), Ref: &BlockRef{Kind: "task", ID: t.Label}})
		}
		return out
	})
	viewRows["task.tail"] = rowsOf(func(s TaskTail, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Lines))
		for i, l := range s.Lines {
			out = append(out, ViewRow{ID: "l" + strconv.Itoa(i), Cells: vcells("line", vtxt(l))})
		}
		return out
	})

	// projects
	registerViews(
		ViewDef{ID: "projects", Feature: "projects", Title: "Projects", Source: ViewSource{Topic: "projects"},
			Fields: []ViewField{vf("name", "Project", FieldText, "sfd"), vf("state", "State", FieldState, "sfd"),
				vf("working", "Working", FieldNum, "sfd"), vf("waiting", "Waiting", FieldNum, "sfd"),
				vf("where", "Where", FieldPath, "f"), vf("when", "Opened", FieldTime, "sf")},
			Layouts: []string{list, table, board}},
	)
	viewRows["projects"] = rowsOf(func(s ProjectsSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Projects))
		for _, p := range s.Projects {
			tone := stateTone(p.State)
			if p.Waiting > 0 {
				tone = blocks.ToneWarn
			}
			out = append(out, ViewRow{ID: "p" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(p.Name), "state", vtxt(p.State),
				"working", vnum(p.Working), "waiting", vnum(p.Waiting), "where", vtxt(p.Where), "when", vwhen(p.OpenedAt)),
				Tone: tone, Ref: &BlockRef{Kind: "project", ID: p.ID}})
		}
		return out
	})

	// inbox, cards, chats
	registerViews(
		ViewDef{ID: "inbox", Feature: "inbox", Title: "Inbox", Source: ViewSource{Topic: "inbox"},
			Fields: []ViewField{vf("title", "What", FieldText, "sfd"), vf("state", "State", FieldState, "sfd"),
				vf("kind", "Kind", FieldState, "sf"), vf("when", "Since", FieldTime, "sfd"), vf("lane", "Lane", FieldText, "sf"),
				vf("chat", "Chat", FieldText, "sf")},
			Layouts: []string{list, blocks.KindStatus, table}, Default: ViewSpec{Sort: byNew}},
		ViewDef{ID: "cards", Feature: "inbox", Title: "Waiting cards", Source: ViewSource{Topic: "cards"},
			Fields: []ViewField{vf("title", "Card", FieldText, "sfd"), vf("kind", "Kind", FieldState, "sfd"),
				vf("when", "Since", FieldTime, "sfd"), vf("laptop", "Answer on the laptop", FieldBool, "sf")},
			Layouts: []string{list, blocks.KindCard}, Default: ViewSpec{Sort: byNew}},
		ViewDef{ID: "chats", Feature: "chat", Title: "Chats", Source: ViewSource{Topic: "chats"},
			Fields: []ViewField{vf("title", "Chat", FieldText, "sfd"), vf("when", "Updated", FieldTime, "sfd"),
				vf("msgs", "Messages", FieldNum, "sf"), vf("live", "Answering", FieldBool, "sfd"), vf("open", "On the laptop", FieldBool, "sf")},
			Layouts: []string{list, table}, Default: ViewSpec{Sort: byNew}},
	)
	viewRows["inbox"] = rowsOf(func(s InboxSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Items))
		for _, it := range s.Items {
			out = append(out, ViewRow{ID: "i" + strconv.Itoa(len(out)), Cells: vcells("title", vtxt(it.Title), "state", vtxt(it.State),
				"kind", vtxt(it.Kind), "when", vwhen(it.Since), "lane", vtxt(it.Lane), "chat", vtxt(it.Chat)),
				Tone: stateTone(it.State), Ref: &BlockRef{Kind: it.Kind, ID: it.Ref}})
		}
		return out
	})
	viewRows["cards"] = rowsOf(func(s CardsSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Cards))
		for _, c := range s.Cards {
			out = append(out, ViewRow{ID: "k" + strconv.Itoa(len(out)), Cells: vcells("title", vtxt(c.Title), "kind", vtxt(c.Kind),
				"when", vwhen(c.Since), "laptop", vyes(c.ReadOnly)), Tone: blocks.ToneWarn, Ref: &BlockRef{Kind: "card", ID: c.ID}})
		}
		return out
	})
	viewRows["chats"] = rowsOf(func(s ChatsSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Chats))
		for _, c := range s.Chats {
			tone := blocks.ToneIdle
			if c.Live {
				tone = blocks.ToneBusy
			}
			out = append(out, ViewRow{ID: "c" + strconv.Itoa(len(out)), Cells: vcells("title", vtxt(c.Title), "when", vwhen(c.Updated),
				"msgs", vnum(c.Msgs), "live", vyes(c.Live), "open", vyes(c.Open)), Tone: tone, Ref: &BlockRef{Kind: "chat", ID: c.ID}})
		}
		return out
	})

	// ci
	registerViews(
		ViewDef{ID: "pr.list", Feature: "ci", Title: "Pull requests", Source: ViewSource{Op: OpPRList},
			Fields: []ViewField{vf("number", "#", FieldNum, "sfd"), vf("title", "Title", FieldText, "sfd"),
				vf("checks", "Checks", FieldState, "sfd"), vf("review", "Review", FieldState, "sf"), vf("author", "Author", FieldText, "sf"),
				vf("branch", "Branch", FieldText, "sf"), vf("base", "Base", FieldText, "sf"), vf("draft", "Draft", FieldBool, "sf")},
			Layouts: []string{table, list}, Default: ViewSpec{Sort: []ViewSort{{Field: "number", Desc: true}}}, Refresh: GHViewRefresh},
		ViewDef{ID: "pr", Feature: "ci", Title: "Pull request", Source: ViewSource{Op: OpPRView},
			Params: []ViewParam{{ID: "number", Label: "Pull request", Kind: ParamNumberPR, Required: true}},
			Fields: []ViewField{vf("title", "Title", FieldText, "d"), vf("state", "State", FieldState, "d"), vf("checks", "Checks", FieldState, "d"),
				vf("review", "Review", FieldState, "d"), vf("author", "Author", FieldText, "d"), vf("branch", "Branch", FieldText, "d"),
				vf("base", "Base", FieldText, "d"), vf("files", "Files", FieldNum, "d"), vf("mergeable", "Mergeable", FieldText, "d")},
			Layouts: []string{blocks.KindDetail, blocks.KindCard}, Refresh: GHViewRefresh},
		ViewDef{ID: "ci.checks", Feature: "ci", Title: "CI checks", Source: ViewSource{Op: OpCIStatus},
			Fields: []ViewField{vf("workflow", "Workflow", FieldText, "sfd"), vf("state", "State", FieldState, "sfd"),
				vf("title", "Run", FieldText, "sf"), vf("status", "Status", FieldText, "sf"), vf("conclusion", "Conclusion", FieldText, "sf")},
			Layouts: []string{blocks.KindStatus, list, table}, Refresh: GHViewRefresh},
	)
	viewRows["pr.list"] = rowsOf(func(s PRList, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Items))
		for _, p := range s.Items {
			out = append(out, prRow(p))
		}
		return out
	})
	viewRows["pr"] = rowsOf(func(s PRView, _ map[string]string) []ViewRow {
		r := prRow(s.Item)
		r.Cells["state"], r.Cells["files"], r.Cells["mergeable"] = vtxt(s.State), vnum(len(s.Files)), vtxt(s.Mergeable)
		return []ViewRow{r}
	})
	viewRows["ci.checks"] = rowsOf(func(s CIStatus, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Runs))
		for _, r := range s.Runs {
			out = append(out, ViewRow{ID: "r" + strconv.Itoa(len(out)), Cells: vcells("workflow", vtxt(r.Workflow), "state", vtxt(r.State),
				"title", vtxt(r.Title), "status", vtxt(r.Status), "conclusion", vtxt(r.Conclusion)), Tone: stateTone(r.State)})
		}
		return out
	})

	// mcp
	registerViews(
		ViewDef{ID: "mcp.status", Feature: "mcp", Title: "Outside agents (MCP)", Source: ViewSource{Topic: "mcp"},
			Fields: []ViewField{vf("name", "Agent", FieldText, "sfd"), vf("when", "Since", FieldTime, "sfd"),
				vf("calls", "Calls", FieldNum, "sfd"), vf("asked", "Asked you", FieldNum, "sf")},
			Layouts: []string{list, table}},
	)
	viewRows["mcp.status"] = rowsOf(func(s MCPSnap, _ map[string]string) []ViewRow {
		out := make([]ViewRow, 0, len(s.Agents))
		for _, a := range s.Agents {
			name := a.Name
			if name == "" {
				name = "(no name yet)"
			}
			out = append(out, ViewRow{ID: "a" + strconv.Itoa(len(out)), Cells: vcells("name", vtxt(name), "when", vwhen(a.Since),
				"calls", vnum(a.Calls), "asked", vnum(a.Asked)), Tone: blocks.ToneOK})
		}
		return out
	})
}

func prRow(p PRItem) ViewRow {
	return ViewRow{ID: "pr" + strconv.Itoa(p.Number), Cells: vcells("number", vnum(p.Number), "title", vtxt(p.Title), "checks", vtxt(p.Checks),
		"review", vtxt(p.Review), "author", vtxt(p.Author), "branch", vtxt(p.Branch), "base", vtxt(p.Base), "draft", vyes(p.Draft)),
		Tone: stateTone(p.Checks), Ref: &BlockRef{Kind: "pr", ID: strconv.Itoa(p.Number)}}
}

// gitDiffBlock turns git.diff's rows into a diff block (hunk rows start
// hunks; file rows are left out).
func gitDiffBlock(d GitDiff) *Block {
	bd := &BlockDiff{Path: d.Path, Adds: d.Added, Dels: d.Removed, Hunks: []BlockHunk{}}
	lines := 0
	for _, r := range d.Rows {
		if lines == blocks.MaxDiffLines {
			break
		}
		switch r.K {
		case "file":
			continue
		case "hunk":
			if len(bd.Hunks) == blocks.MaxDiffHunks {
				continue
			}
			bd.Hunks = append(bd.Hunks, BlockHunk{Header: viewCut(r.Text, blocks.MaxSub), Lines: []BlockDiffLine{}})
			continue
		}
		if len(bd.Hunks) == 0 {
			bd.Hunks = append(bd.Hunks, BlockHunk{Header: "@@", Lines: []BlockDiffLine{}})
		}
		h := &bd.Hunks[len(bd.Hunks)-1]
		h.Lines = append(h.Lines, BlockDiffLine{K: r.K, A: r.Old, B: r.New, Text: r.Text})
		lines++
	}
	return &Block{ID: "view", Kind: blocks.KindDiff, Title: viewCut(d.Path, blocks.MaxTitle), Diff: bd, Truncated: d.Truncated || d.Next > 0}
}
