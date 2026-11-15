// Package afteradd is the one flow every "add" event goes through: the
// user adds an MCP server, an ssh host, a database connection, opens a
// project, and so on. The flow has three steps:
//
//   - CHECK: a few read-only checks run at the same time, each with its
//     own timeout (DefaultTimeout when none is set). A check never
//     changes anything: it connects, lists, reads. A check that fails or
//     runs out of time becomes a warning finding, so the user still
//     learns what could not be checked.
//   - LINK: the checks look at what termocode already has (a saved
//     database connection, a repo remote, a kube context) and say so in
//     their findings.
//   - SUGGEST: the rule turns the findings into at most one suggestion:
//     one card with one action. The action is an existing palette op;
//     nothing turns on by itself.
//
// "Not now" on a card is remembered per item (Later, a small JSON file),
// so the same item does not ask again; another item still does.
package afteradd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/amin-jalali/termocode/internal/crash"
)

// Event kinds.
const (
	KindMCP       = "mcp"      // an MCP server was added
	KindSSH       = "ssh"      // an ssh host was added
	KindDB        = "db"       // a database connection was added
	KindProject   = "project"  // a project folder was opened
	KindGitRemote = "git"      // a git remote was added or found
	KindProvider  = "provider" // an AI provider or model was added
	KindKube      = "kube"     // a kube context was added
)

// DefaultTimeout is a check's timeout when it sets none.
const DefaultTimeout = 5 * time.Second

// Event is one thing the user added.
type Event struct {
	Kind  string
	Item  string            // the item's name: the server, host, connection…
	Facts map[string]string // what the caller already knows (url, kind…)
}

// Key is the event's item key: "kind:item".
func (e Event) Key() string { return e.Kind + ":" + e.Item }

// Level is how a finding reads.
type Level int

const (
	Info Level = iota // a plain fact
	OK                // a check passed
	Warn              // a check failed, timed out or found a risk
)

// Finding is one line a check found.
type Finding struct {
	Check string // the check's name (set by Run)
	Level Level
	Text  string            // one short line for the card
	Facts map[string]string // data for the rule's Suggest
}

// Check is one read-only check of an event.
type Check struct {
	Name    string
	Timeout time.Duration // 0: DefaultTimeout
	Run     func(ctx context.Context, ev Event) ([]Finding, error)
}

// Action is a suggestion's one action: an existing palette op.
type Action struct {
	ID    string // stable id of the suggestion (the Not-now key)
	Label string // the button: "Add DB connection"
	Op    string // the palette action it runs
	Arg   string // the op's target, if it takes one
}

// Suggestion is the one card of an event.
type Suggestion struct {
	Event    Event
	Title    string
	Findings []Finding
	Action   Action
}

// Key is the suggestion's Not-now key: "kind:item|action".
func (s Suggestion) Key() string { return s.Event.Key() + "|" + s.Action.ID }

// Rule is the checks and the suggestion of one event kind.
type Rule struct {
	Kind    string
	Checks  []Check
	Suggest func(ev Event, findings []Finding) *Suggestion // nil: findings only
}

// Result is what one Run found.
type Result struct {
	Event      Event
	Findings   []Finding
	Suggestion *Suggestion // nil: nothing to suggest (or Not now)
}

// Engine holds the rules and the Not-now list. It is safe for
// concurrent use.
type Engine struct {
	mu    sync.Mutex
	rules map[string]Rule
	later *Later
}

// New is an engine with rules; later may be nil (Not now is kept in
// memory only).
func New(later *Later, rules ...Rule) *Engine {
	if later == nil {
		later = &Later{}
	}
	e := &Engine{rules: map[string]Rule{}, later: later}
	for _, r := range rules {
		e.rules[r.Kind] = r
	}
	return e
}

// Add sets (or replaces) the rule of r.Kind.
func (e *Engine) Add(r Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules[r.Kind] = r
}

// Later is the engine's Not-now list.
func (e *Engine) Later() *Later { return e.later }

// NotNow remembers that the user said "Not now" to s.
func (e *Engine) NotNow(s Suggestion) error { return e.later.Add(s.Key()) }

// Run runs every check of ev's rule at the same time and builds the
// suggestion. It returns when every check is done or timed out, or when
// ctx ends.
func (e *Engine) Run(ctx context.Context, ev Event) Result {
	e.mu.Lock()
	r, ok := e.rules[ev.Kind]
	e.mu.Unlock()
	res := Result{Event: ev}
	if !ok {
		return res
	}
	per := make([][]Finding, len(r.Checks))
	var wg sync.WaitGroup
	for i, c := range r.Checks {
		wg.Add(1)
		crash.Go("afteradd."+c.Name, func() {
			defer wg.Done()
			per[i] = runCheck(ctx, c, ev)
		})
	}
	wg.Wait()
	for _, fs := range per {
		res.Findings = append(res.Findings, fs...)
	}
	if r.Suggest == nil {
		return res
	}
	s := r.Suggest(ev, res.Findings)
	if s == nil {
		return res
	}
	s.Event = ev
	if s.Findings == nil {
		s.Findings = res.Findings
	}
	if !e.later.Has(s.Key()) {
		res.Suggestion = s
	}
	return res
}

// runCheck runs one check with its timeout; an error, a timeout or a
// panic is one warning finding.
func runCheck(ctx context.Context, c Check, ev Event) (out []Finding) {
	d := c.Timeout
	if d <= 0 {
		d = DefaultTimeout
	}
	cctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	defer func() {
		if recover() != nil {
			out = []Finding{{Check: c.Name, Level: Warn, Text: "could not check: the check failed"}}
		}
	}()
	fs, err := c.Run(cctx, ev)
	if err != nil {
		text := "could not check: " + err.Error()
		if cctx.Err() == context.DeadlineExceeded && ctx.Err() == nil {
			text = "did not answer in " + d.String()
		} else if ctx.Err() != nil {
			text = "stopped before it was done"
		}
		return []Finding{{Check: c.Name, Level: Warn, Text: text}}
	}
	for i := range fs {
		fs[i].Check = c.Name
	}
	return fs
}

// Later is the Not-now list: suggestion keys the user said "Not now"
// to. With a path it is kept in that file; nil or without a path, in
// memory only. A missing or broken file is an empty list.
type Later struct {
	mu   sync.Mutex
	path string
	keys []string
	read bool
}

// OpenLater is the Not-now list kept in path.
func OpenLater(path string) *Later { return &Later{path: path} }

type laterFile struct {
	NotNow []string `json:"not_now"`
}

func (l *Later) load() {
	if l.read {
		return
	}
	l.read = true
	if l.path == "" {
		return
	}
	b, err := os.ReadFile(l.path)
	if err != nil {
		return
	}
	var f laterFile
	if json.Unmarshal(b, &f) == nil {
		l.keys = f.NotNow
	}
}

// Has reports whether key is on the list.
func (l *Later) Has(key string) bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	return slices.Contains(l.keys, key)
}

// Add puts key on the list (and in the file).
func (l *Later) Add(key string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.load()
	if slices.Contains(l.keys, key) {
		return nil
	}
	l.keys = append(l.keys, key)
	if l.path == "" {
		return nil
	}
	b, err := json.MarshalIndent(laterFile{NotNow: l.keys}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, l.path); err != nil {
		return fmt.Errorf("save the not-now list: %w", err)
	}
	return nil
}
