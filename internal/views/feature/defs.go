// Package feature is the feature view registry (mobile plan 3.11.8,
// MOBUX-13): one typed phone view per termocode feature — Git (branches,
// changes, commits), Worktrees, Runs, Workflows, Tasks, DB (one table),
// HTTP (history, one response), Containers, Kubernetes, Tests, CI / PR
// (checks), Logs and AI usage.
//
// Each view is a views.Def: its source on the laptop (a topic, a read op
// or a read-only AI tool), its params, its typed fields, the layouts it
// allows and its default spec. The row builders in rows.go turn what
// that source gives (a topic snapshot or an op result, already redacted
// by the laptop) into typed views.Row values; views.Render applies the
// spec. Nothing here adds a write path: every source is a viewer op or a
// topic the phone may already read.
package feature

import (
	"slices"

	"github.com/amin-jalali/termocode/internal/blocks"
	"github.com/amin-jalali/termocode/internal/views"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// Feature ids, in menu order.
const (
	FeatGit        = "git"
	FeatLanes      = "lanes"
	FeatRuns       = "runs"
	FeatWorkflows  = "workflows"
	FeatTasks      = "tasks"
	FeatDB         = "db"
	FeatHTTP       = "http"
	FeatContainers = "containers"
	FeatKube       = "kube"
	FeatTests      = "tests"
	FeatCI         = "ci"
	FeatLogs       = "logs"
	FeatUsage      = "usage"
)

// View ids.
const (
	ViewGitStatus     = "git.status"
	ViewGitLog        = "git.log"
	ViewGitBranches   = "git.branches"
	ViewLanes         = "lanes"
	ViewRuns          = "runs"
	ViewWorkflows     = "workflows"
	ViewWorkflowRuns  = "workflows.runs"
	ViewTasks         = "tasks"
	ViewDBRows        = "db.rows"
	ViewHTTPHistory   = "http.history"
	ViewHTTPResponse  = "http.response"
	ViewContainers    = "containers"
	ViewKubePods      = "kube.pods"
	ViewTests         = "tests"
	ViewCIChecks      = "ci.checks"
	ViewPRList        = "pr.list"
	ViewLogsTask      = "logs.task"
	ViewLogsContainer = "logs.container"
	ViewLogsPod       = "logs.pod"
	ViewUsage         = "usage"
)

// UsageTool is the read-only AI tool the usage view reads (the laptop's
// USAGE view data; never a vendor endpoint).
const UsageTool = "usage_summary"

const viewer = string(webproto.RoleViewer)

// fld is a sortable, filterable field; on says it is shown by default.
func fld(id, label, kind string, on bool) views.Field {
	return views.Field{ID: id, Label: label, Kind: kind, Sortable: kind != views.FieldMD, Filterable: kind != views.FieldMD, Default: on}
}

func sortBy(field string, desc bool) []blocks.ViewSort {
	return []blocks.ViewSort{{Field: field, Desc: desc}}
}

var rowLayouts = []string{blocks.KindList, blocks.KindTable}
var tableFirst = []string{blocks.KindTable, blocks.KindList}

// textFields is the one field of a log view (its block is text, built
// from the lines; the field only names them for the AI's short text).
var textFields = []views.Field{{ID: "line", Label: "Line", Kind: views.FieldText, Default: true}}

// DBField is the id prefix of a db.rows column field: the table's columns
// become fields c_<name> when the rows arrive (DBTable).
const DBField = "c_"

var defs = []views.Def{
	// ── git ──
	{
		ID: ViewGitStatus, Feature: FeatGit, Title: "Changes", Source: views.Source{Topic: "git"},
		Fields: []views.Field{
			fld("path", "File", views.FieldPath, true),
			fld("state", "State", views.FieldState, true),
			fld("staged", "Staged", views.FieldBool, false),
			fld("x", "Index", views.FieldText, false),
			fld("y", "Work tree", views.FieldText, false),
		},
		Layouts: rowLayouts, Default: blocks.ViewSpec{Group: "state", Limit: 200}, Role: viewer, Live: true,
	},
	{
		ID: ViewGitLog, Feature: FeatGit, Title: "Commits", Source: views.Source{Topic: "git"},
		Fields: []views.Field{
			fld("subject", "Subject", views.FieldText, true),
			fld("hash", "Commit", views.FieldLink, true),
			fld("author", "Author", views.FieldText, true),
			fld("when", "When", views.FieldTime, true),
			fld("unpushed", "Not pushed", views.FieldBool, false),
		},
		Layouts: rowLayouts, Default: blocks.ViewSpec{Limit: 50}, Role: viewer, Live: true,
	},
	{
		ID: ViewGitBranches, Feature: FeatGit, Title: "Branches", Source: views.Source{Op: webproto.OpGitBranches},
		Fields: []views.Field{
			fld("name", "Branch", views.FieldText, true),
			fld("current", "Current", views.FieldBool, true),
			fld("remote", "Remote", views.FieldBool, false),
		},
		Layouts: rowLayouts, Default: blocks.ViewSpec{Sort: []blocks.ViewSort{{Field: "current", Desc: true}, {Field: "name"}}}, Role: viewer,
	},
	// ── worktrees / lanes ──
	{
		ID: ViewLanes, Feature: FeatLanes, Title: "Worktrees", Source: views.Source{Topic: "worktrees"},
		Fields: []views.Field{
			fld("title", "Worktree", views.FieldText, true),
			fld("state", "State", views.FieldState, true),
			fld("branch", "Branch", views.FieldText, true),
			fld("agent", "Agent", views.FieldText, false),
			fld("group", "Group", views.FieldText, false),
			fld("kind", "Kind", views.FieldText, false),
			fld("ahead", "Ahead", views.FieldNum, false),
			fld("behind", "Behind", views.FieldNum, false),
			fld("dirty", "Changed files", views.FieldNum, false),
			fld("where", "Folder", views.FieldPath, false),
			fld("here", "This one", views.FieldBool, false),
			fld("seen", "Last seen", views.FieldTime, false),
		},
		Layouts: rowLayouts, Default: blocks.ViewSpec{Group: "group"}, Role: viewer, Live: true,
	},
	// ── runs ──
	{
		ID: ViewRuns, Feature: FeatRuns, Title: "Runs", Source: views.Source{Topic: "runs"},
		Fields: []views.Field{
			fld("title", "Run", views.FieldText, true),
			fld("state", "State", views.FieldState, true),
			fld("started", "Started", views.FieldTime, true),
			fld("step", "Step", views.FieldNum, false),
			fld("steps", "Steps", views.FieldNum, false),
			fld("step_title", "Step title", views.FieldText, false),
			fld("chat", "Chat", views.FieldText, false),
			fld("profile", "Profile", views.FieldText, false),
			fld("cost", "Cost", views.FieldCost, true),
			fld("needs_you", "Needs you", views.FieldBool, false),
		},
		Layouts: rowLayouts, Default: blocks.ViewSpec{Sort: []blocks.ViewSort{{Field: "needs_you", Desc: true}, {Field: "started", Desc: true}}}, Role: viewer, Live: true,
	},
	// ── workflows ──
	{
		ID: ViewWorkflows, Feature: FeatWorkflows, Title: "Workflows", Source: views.Source{Topic: "workflows"},
		Fields: []views.Field{
			fld("name", "Workflow", views.FieldText, true),
			fld("state", "State", views.FieldState, true),
			fld("when", "Schedule", views.FieldText, true),
			fld("next", "Next run", views.FieldTime, false),
			fld("last", "Last run", views.FieldState, false),
		},
		Layouts: rowLayouts, Default: blocks.ViewSpec{Sort: sortBy("name", false)}, Role: viewer, Live: true,
	},
	{
		ID: ViewWorkflowRuns, Feature: FeatWorkflows, Title: "Workflow runs", Source: views.Source{Topic: "workflows"},
		Fields: []views.Field{
			fld("workflow", "Workflow", views.FieldText, true),
			fld("state", "State", views.FieldState, true),
			fld("started", "Started", views.FieldTime, true),
			fld("ended", "Ended", views.FieldTime, false),
			fld("items", "Items", views.FieldNum, true),
			fld("failed", "Failed", views.FieldNum, true),
			fld("dry", "Dry run", views.FieldBool, false),
			fld("waiting", "Waits at", views.FieldText, false),
		},
		Layouts: tableFirst, Default: blocks.ViewSpec{Sort: sortBy("started", true)}, Role: viewer, Live: true,
	},
	// ── tasks ──
	{
		ID: ViewTasks, Feature: FeatTasks, Title: "Tasks", Source: views.Source{Topic: "tasks"},
		Fields: []views.Field{
			fld("label", "Task", views.FieldText, true),
			fld("state", "Last run", views.FieldState, true),
			fld("when", "Started", views.FieldTime, true),
			fld("group", "Group", views.FieldText, false),
			fld("source", "Source", views.FieldText, false),
			fld("code", "Exit code", views.FieldNum, false),
			fld("default", "Default", views.FieldBool, false),
		},
		Layouts: rowLayouts, Default: blocks.ViewSpec{}, Role: viewer, Live: true,
	},
	// ── db ──
	{
		ID: ViewDBRows, Feature: FeatDB, Title: "Table rows", Source: views.Source{Op: webproto.OpDBRows},
		Params: []views.Param{
			{ID: "conn", Label: "Connection", Kind: views.ParamConn, Required: true},
			{ID: "table", Label: "Table", Kind: views.ParamTable, Required: true},
		},
		// The table's own columns are added when its rows arrive (DBTable);
		// "row" is the row's place on the page.
		Fields:  []views.Field{fld("row", "Row", views.FieldNum, true)},
		Layouts: tableFirst, Default: blocks.ViewSpec{Limit: 200}, Role: viewer, Refresh: 60,
	},
	// ── http ──
	{
		ID: ViewHTTPHistory, Feature: FeatHTTP, Title: "Requests", Source: views.Source{Op: webproto.OpHTTPHistory},
		Fields: []views.Field{
			fld("request", "Request", views.FieldText, true),
			fld("state", "Result", views.FieldState, true),
			fld("status", "Status", views.FieldNum, true),
			fld("at", "When", views.FieldTime, true),
			fld("method", "Method", views.FieldText, false),
			fld("url", "URL", views.FieldText, false),
			fld("ms", "Time (ms)", views.FieldNum, false),
			fld("size", "Size", views.FieldNum, false),
			fld("file", "File", views.FieldPath, false),
			fld("env", "Environment", views.FieldText, false),
			fld("by", "By", views.FieldText, false),
		},
		Layouts: tableFirst, Default: blocks.ViewSpec{Sort: sortBy("at", true)}, Role: viewer, Refresh: 30,
	},
	{
		ID: ViewHTTPResponse, Feature: FeatHTTP, Title: "Response", Source: views.Source{Op: webproto.OpHTTPResponse},
		Params: []views.Param{{ID: "run", Label: "Request run", Kind: views.ParamInt}},
		Fields: []views.Field{
			fld("request", "Request", views.FieldText, true),
			fld("state", "Result", views.FieldState, true),
			fld("status", "Status", views.FieldNum, true),
			fld("status_text", "Status text", views.FieldText, true),
			fld("ms", "Time (ms)", views.FieldNum, true),
			fld("size", "Size", views.FieldNum, true),
			fld("type", "Content type", views.FieldText, true),
			fld("proto", "Protocol", views.FieldText, false),
			fld("env", "Environment", views.FieldText, false),
			fld("by", "By", views.FieldText, false),
			fld("headers", "Response headers", views.FieldMD, true),
			fld("body", "Body", views.FieldMD, true),
			fld("error", "Error", views.FieldText, true),
		},
		Layouts: []string{blocks.KindDetail, blocks.KindCard}, Default: blocks.ViewSpec{}, Role: viewer,
	},
	// ── containers ──
	{
		ID: ViewContainers, Feature: FeatContainers, Title: "Containers", Source: views.Source{Op: webproto.OpContainersList},
		Fields: []views.Field{
			fld("name", "Container", views.FieldText, true),
			fld("state", "State", views.FieldState, true),
			fld("image", "Image", views.FieldText, true),
			fld("status", "Status", views.FieldText, true),
			fld("ports", "Ports", views.FieldText, false),
			fld("project", "Compose project", views.FieldText, false),
		},
		Layouts: tableFirst, Default: blocks.ViewSpec{Sort: sortBy("state", false)}, Role: viewer, Refresh: 15,
	},
	// ── kubernetes ──
	{
		ID: ViewKubePods, Feature: FeatKube, Title: "Pods", Source: views.Source{Op: webproto.OpKubeList},
		Params: []views.Param{
			{ID: "context", Label: "Context", Kind: views.ParamString},
			{ID: "namespace", Label: "Namespace", Kind: views.ParamString},
		},
		Fields: []views.Field{
			fld("name", "Pod", views.FieldText, true),
			fld("status", "Status", views.FieldState, true),
			fld("ready", "Ready", views.FieldText, true),
			fld("restarts", "Restarts", views.FieldNum, true),
			fld("created", "Created", views.FieldTime, false),
			fld("containers", "Containers", views.FieldText, false),
		},
		Layouts: tableFirst, Default: blocks.ViewSpec{Sort: sortBy("restarts", true)}, Role: viewer, Refresh: 15,
	},
	// ── tests ──
	{
		ID: ViewTests, Feature: FeatTests, Title: "Tests", Source: views.Source{Topic: "tests"},
		Fields: []views.Field{
			fld("name", "Test", views.FieldText, true),
			fld("state", "State", views.FieldState, true),
			fld("file", "File", views.FieldPath, true),
			fld("ms", "Time (ms)", views.FieldNum, false),
			fld("message", "Failure", views.FieldText, false),
		},
		Layouts: rowLayouts, Default: blocks.ViewSpec{Group: "state", Limit: 200}, Role: viewer, Live: true,
	},
	// ── ci / pr ──
	{
		ID: ViewCIChecks, Feature: FeatCI, Title: "CI checks", Source: views.Source{Op: webproto.OpCIStatus},
		Fields: []views.Field{
			fld("workflow", "Workflow", views.FieldText, true),
			fld("state", "State", views.FieldState, true),
			fld("title", "Run", views.FieldText, true),
			fld("status", "Status", views.FieldText, false),
			fld("conclusion", "Conclusion", views.FieldText, false),
		},
		Layouts: rowLayouts, Default: blocks.ViewSpec{}, Role: viewer, Refresh: 60,
	},
	{
		ID: ViewPRList, Feature: FeatCI, Title: "Pull requests", Source: views.Source{Op: webproto.OpPRList},
		Fields: []views.Field{
			fld("title", "Pull request", views.FieldText, true),
			fld("number", "Number", views.FieldNum, true),
			fld("checks", "Checks", views.FieldState, true),
			fld("review", "Review", views.FieldText, true),
			fld("author", "Author", views.FieldText, true),
			fld("branch", "Branch", views.FieldText, false),
			fld("base", "Base", views.FieldText, false),
			fld("draft", "Draft", views.FieldBool, false),
		},
		Layouts: tableFirst, Default: blocks.ViewSpec{Sort: sortBy("number", true)}, Role: viewer, Refresh: 60,
	},
	// ── logs ──
	{
		ID: ViewLogsTask, Feature: FeatLogs, Title: "Task output", Source: views.Source{Op: webproto.OpTasksTail},
		Params:  []views.Param{{ID: "label", Label: "Task", Kind: views.ParamString, Required: true}},
		Fields:  textFields,
		Layouts: []string{blocks.KindText}, Default: blocks.ViewSpec{}, Role: viewer, Refresh: 10,
	},
	{
		ID: ViewLogsContainer, Feature: FeatLogs, Title: "Container logs", Source: views.Source{Op: webproto.OpContainersLogs},
		Params:  []views.Param{{ID: "id", Label: "Container", Kind: views.ParamString, Required: true}},
		Fields:  textFields,
		Layouts: []string{blocks.KindText}, Default: blocks.ViewSpec{}, Role: viewer, Refresh: 10,
	},
	{
		ID: ViewLogsPod, Feature: FeatLogs, Title: "Pod logs", Source: views.Source{Op: webproto.OpKubeLogs},
		Params: []views.Param{
			{ID: "pod", Label: "Pod", Kind: views.ParamString, Required: true},
			{ID: "container", Label: "Container", Kind: views.ParamString},
			{ID: "context", Label: "Context", Kind: views.ParamString},
			{ID: "namespace", Label: "Namespace", Kind: views.ParamString},
		},
		Fields:  textFields,
		Layouts: []string{blocks.KindText}, Default: blocks.ViewSpec{}, Role: viewer, Refresh: 10,
	},
	// ── ai usage ──
	{
		ID: ViewUsage, Feature: FeatUsage, Title: "AI usage", Source: views.Source{Tool: UsageTool},
		Params: []views.Param{{ID: "by", Label: "By", Kind: views.ParamEnum, Enum: []string{"model", "agent"}, Default: "model"}},
		Fields: []views.Field{
			fld("name", "Name", views.FieldText, true),
			fld("cost", "Cost", views.FieldCost, true),
			fld("calls", "Calls", views.FieldNum, true),
			fld("input", "Input tokens", views.FieldNum, true),
			fld("output", "Output tokens", views.FieldNum, true),
			fld("billing", "Billing", views.FieldText, false),
		},
		Layouts: tableFirst, Default: blocks.ViewSpec{Sort: sortBy("cost", true)}, Role: viewer, Refresh: 30,
	},
}

// Features lists the feature ids in menu order.
func Features() []string {
	return []string{FeatGit, FeatLanes, FeatRuns, FeatWorkflows, FeatTasks, FeatDB, FeatHTTP, FeatContainers, FeatKube, FeatTests, FeatCI, FeatLogs, FeatUsage}
}

// Defs returns a copy of every feature view, in menu order.
func Defs() []views.Def {
	out := make([]views.Def, len(defs))
	for i, d := range defs {
		out[i] = clone(d)
	}
	return out
}

// Lookup returns the feature view with id.
func Lookup(id string) (views.Def, bool) {
	for _, d := range defs {
		if d.ID == id {
			return clone(d), true
		}
	}
	return views.Def{}, false
}

// IsText reports whether a view's block is text (a log), built by its
// fetch rather than rendered from rows.
func IsText(d *views.Def) bool { return slices.Equal(d.Layouts, []string{blocks.KindText}) }

func clone(d views.Def) views.Def {
	d.Params = slices.Clone(d.Params)
	d.Fields = slices.Clone(d.Fields)
	d.Layouts = slices.Clone(d.Layouts)
	d.Actions = slices.Clone(d.Actions)
	d.Default.Sort = slices.Clone(d.Default.Sort)
	d.Default.Filter = slices.Clone(d.Default.Filter)
	d.Default.Fields = slices.Clone(d.Default.Fields)
	return d
}
