package webproto

// The shell's data (web design 2.1): the status strip, the laptop's
// toasts and the attention events.

func init() {
	registerTopic("status", StatusSnap{}, StatusSnap{})
	registerTopic("toasts", ToastsSnap{}, ToastsDelta{})
	registerEvent(EventAttention, Attention{})
}

// StatusSnap is the status topic. A delta is the whole object again (it
// is small); the hub sends at most 10 a second and a queued one is
// replaced by the next.
type StatusSnap struct {
	Root       string      `json:"root"`            // the workspace folder: the one absolute path the hub sends
	Workspace  string      `json:"workspace_label"` // its name, as the status bar shows it
	Branch     string      `json:"branch"`          // "" outside a git repository
	Ahead      int         `json:"ahead"`
	Behind     int         `json:"behind"`
	DirtyFiles int         `json:"dirty_files"` // files git reports as changed
	AI         StatusAI    `json:"ai"`
	Terminals  StatusTerms `json:"terminals"`
	Runs       StatusRuns  `json:"runs"`
	Web        StatusWeb   `json:"web"`
	Theme      StatusTheme `json:"theme"`
}

// StatusAI is the AI chip.
type StatusAI struct {
	State        string `json:"state" enum:"off|setup|idle|busy"`
	Label        string `json:"label"` // the chip's text: the model, or "profile · model"
	Busy         bool   `json:"busy"`
	CardsWaiting int    `json:"cards_waiting"` // cards the user should answer (laptop or web)
}

// StatusTerms counts the terminal tabs.
type StatusTerms struct {
	Count int `json:"count"`
	Busy  int `json:"busy"` // tabs running a command or a task
}

// StatusRuns counts the AI runs of the open chat.
type StatusRuns struct {
	Live     int `json:"live"`
	NeedsYou int `json:"needs_you"` // waiting for a plan approval or a budget answer
}

// StatusWeb is the web side itself.
type StatusWeb struct {
	Clients int `json:"clients"` // browsers connected now
}

// StatusTheme is the laptop's theme, so the page can match it: Colors maps
// a theme palette field ("BgEditor", "AIAccent", …) to "#rrggbb". The page
// maps the fields to its tokens and derives its light variant.
type StatusTheme struct {
	ID     string            `json:"id"`
	Colors map[string]string `json:"colors"`
}

// ToastsSnap is the toasts topic: the toasts the laptop shows now, oldest
// first.
type ToastsSnap struct {
	Toasts []Toast `json:"toasts"`
}

// Toast is one laptop toast.
type Toast struct {
	ID       int    `json:"id"`
	Severity string `json:"severity" enum:"info|warn|error"`
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
}

// Toast delta ops.
const (
	ToastPush    = "push"    // Toast is new, or replaces the toast with its id in place
	ToastDismiss = "dismiss" // the toast with ID is gone
)

// ToastsDelta is one change of the toasts topic.
type ToastsDelta struct {
	Op    string `json:"op" enum:"push|dismiss"`
	Toast *Toast `json:"toast,omitempty"` // push
	ID    int    `json:"id,omitempty"`    // dismiss
}

// EventAttention is the ev kind that asks for the user's attention.
const EventAttention = "attention"

// Attention kinds.
const (
	AttentionCard      = "card"       // a card waits for an answer (ID: the card)
	AttentionRunDone   = "run_done"   // an AI run finished (ID: the run)
	AttentionRunFailed = "run_failed" // an AI run failed or stopped (ID: the run)
	AttentionTaskDone  = "task_done"  // a task's terminal finished (ID: the task label)
	AttentionBell      = "bell"       // a terminal rang
)

// Attention is the data of an attention ev. It comes from the Model's own
// state changes, never from bytes a program printed; the page badges its
// title and the tab bar and words the hint from Kind (fixed texts).
type Attention struct {
	Kind string `json:"kind" enum:"card|run_done|run_failed|task_done|bell"`
	ID   string `json:"id,omitempty"`
}
