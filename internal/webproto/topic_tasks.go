package webproto

// The tasks and runs data (web design 2.8). Tasks are the ones the
// laptop's task picker lists (tasks.json, user commands, detected ones):
// the web starts only those, by label, never a command line of its own.
// A started task runs in a laptop terminal; until the terminal view can
// follow it, tasks.tail reads the end of its output, capped. Runs are the
// AI runs of the Runs view. Both topics send the whole list as a delta.

func init() {
	registerTopic("tasks", TasksSnap{}, TasksSnap{})
	registerTopic("runs", RunsSnap{}, RunsSnap{})
	registerResult(OpTasksRun, TaskStarted{})
	registerResult(OpTasksTail, TaskTail{})
}

// Tasks and runs limits.
const (
	MaxTasks     = 200      // tasks listed
	MaxRuns      = 100      // runs listed, newest first
	MaxTailLines = 200      // lines of a task tail
	MaxTailBytes = 64 << 10 // bytes of a task tail
)

// TasksSnap is the tasks topic.
type TasksSnap struct {
	Tasks []TaskItem `json:"tasks"`
	Error string     `json:"error,omitempty"` // tasks.json does not parse (the other sources still list)
}

// TaskItem is one task the laptop can run.
type TaskItem struct {
	Label   string    `json:"label"`
	Group   string    `json:"group,omitempty"`   // "build", "test" or ""
	Default bool      `json:"default,omitempty"` // the default task of its group
	Source  string    `json:"source"`            // "tasks.json", "user", "go", "npm", "make" …
	Detail  string    `json:"detail,omitempty"`  // the command line it runs (to read, not to send)
	Last    *TaskLast `json:"last,omitempty"`    // its newest run
}

// TaskLast is a task's newest run.
type TaskLast struct {
	State string `json:"state" enum:"running|ok|failed"`
	Code  int    `json:"code,omitempty"` // exit code once it ended
	When  int64  `json:"when"`           // Unix ms it started
	Buf   int    `json:"buf"`            // its terminal (the term topic's key)
}

// TaskStarted answers tasks.run.
type TaskStarted struct {
	Buf int `json:"buf"`
}

// TaskTail answers tasks.tail: the last lines of the task's newest run.
type TaskTail struct {
	Buf       int      `json:"buf"`
	Lines     []string `json:"lines"`
	Truncated bool     `json:"truncated,omitempty"` // older lines are not here
	Running   bool     `json:"running,omitempty"`
	Code      int      `json:"code,omitempty"`
}

// RunsSnap is the runs topic.
type RunsSnap struct {
	Runs []RunItem `json:"runs"`
}

// RunItem is one AI run.
type RunItem struct {
	Key       string   `json:"key"`               // its id for runs.stop / pause / resume
	Title     string   `json:"title"`             // the user's request, one line
	Chat      string   `json:"chat,omitempty"`    // its chat's title
	Profile   string   `json:"profile,omitempty"` // the agent profile
	State     string   `json:"state" enum:"running|stalled|pausing|paused|resumable|done|failed|cancelled"`
	Why       string   `json:"why,omitempty"` // why it waits or stopped
	Step      int      `json:"step"`          // 1-based step it is on (0: none)
	Steps     int      `json:"steps"`
	StepTitle string   `json:"step_title,omitempty"`
	Started   int64    `json:"started"`            // Unix ms
	ElapsedMs int64    `json:"elapsed_ms"`         // at the time of the snap
	Live      bool     `json:"live,omitempty"`     // elapsed grows: the page adds the time since
	CostUSD   float64  `json:"cost_usd,omitempty"` // known step costs
	NeedsYou  bool     `json:"needs_you,omitempty"`
	Can       []string `json:"can,omitempty"` // what the web may do now: "stop", "pause", "resume"
}
