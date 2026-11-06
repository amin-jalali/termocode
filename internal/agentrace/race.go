// Package agentrace is the data of an agent race (AGX-4…6): one task
// fanned out to 2–4 lanes, each an agent in its own hidden agent worktree
// (internal/git agent worktrees), then compared, one lane merged and the
// rest discarded. It holds the records and the pure rules (lane ids, the
// state words, the verify result, the summary prompt); the app runs the
// lanes and draws the pages (internal/raceview).
package agentrace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Lane states. A lane is Starting until its worktree and tab are ready,
// Working while the agent works, Waiting when the agent asks the user,
// Done when it is back at its input box after the task, Stopped when the
// user stopped it or its tab closed, Failed when it could not start;
// Merged and Discarded close it.
type State string

const (
	Starting  State = "starting"
	Working   State = "working"
	Waiting   State = "waiting"
	Done      State = "done"
	Stopped   State = "stopped"
	Failed    State = "failed"
	Merged    State = "merged"
	Discarded State = "discarded"
)

// Lane limits.
const (
	MinLanes = 2
	MaxLanes = 4
	// MaxKept is how many races the record file keeps (newest).
	MaxKept = 20
	// FileName is the record file in the git dir's termocode folder.
	FileName = "races.json"
)

// Verify states of a lane.
const (
	VerifyNone    = ""
	VerifyRunning = "running"
	VerifyPass    = "pass"
	VerifyFail    = "fail"
)

// FileStat is one file a lane changed.
type FileStat struct {
	Status string `json:"status"` // A, M or D
	Path   string `json:"path"`
	Add    int    `json:"add,omitempty"`
	Del    int    `json:"del,omitempty"`
	Binary bool   `json:"binary,omitempty"`
}

// Verify is the verify command's result in one lane.
type Verify struct {
	State string `json:"state,omitempty"`
	Exit  int    `json:"exit,omitempty"`
}

// VerifyResult is the result of a verify command that ended with code.
func VerifyResult(code int) Verify {
	if code == 0 {
		return Verify{State: VerifyPass}
	}
	return Verify{State: VerifyFail, Exit: code}
}

// Lane is one agent of a race.
type Lane struct {
	ID       string     `json:"id"` // also the agent worktree id
	Agent    string     `json:"agent"`
	Title    string     `json:"title"`
	Worktree string     `json:"worktree,omitempty"` // the agent's folder in its worktree
	Branch   string     `json:"branch,omitempty"`
	State    State      `json:"state"`
	Typed    bool       `json:"typed,omitempty"` // the task was typed into its tab
	Files    []FileStat `json:"files,omitempty"`
	Stats    bool       `json:"stats,omitempty"` // Files were read (an empty list is "no changes")
	Verify   Verify     `json:"verify"`
	// Cost in dollars when known (nil: an outside CLI does not say).
	Cost *float64 `json:"cost,omitempty"`
	Note string   `json:"note,omitempty"` // why it failed, or a short word
}

// Totals are the lane's changed files and lines.
func (l Lane) Totals() (files, add, del int) {
	for _, f := range l.Files {
		add += f.Add
		del += f.Del
	}
	return len(l.Files), add, del
}

// Closed: merged or discarded (its worktree is gone).
func (l Lane) Closed() bool { return l.State == Merged || l.State == Discarded }

// Finished: the agent is no longer working and its worktree is there.
func (l Lane) Finished() bool { return l.State == Done || l.State == Stopped }

// Race is one task raced by several lanes.
type Race struct {
	ID        string `json:"id"` // race-<n>
	Number    int    `json:"number"`
	Name      string `json:"name"` // race/<slug>
	Prompt    string `json:"prompt"`
	Verify    string `json:"verify,omitempty"`
	StartedMs int64  `json:"started_ms"`
	Lanes     []Lane `json:"lanes"`
	// Summary is the AI's difference summary (AGX-6), at most 3 lines,
	// and what it cost ("" unknown).
	Summary     []string `json:"summary,omitempty"`
	SummaryCost string   `json:"summary_cost,omitempty"`
	Winner      string   `json:"winner,omitempty"` // the merged lane
}

// LaneSpec is a lane asked for in the race card.
type LaneSpec struct{ Agent, Title string }

// New makes race number n. The task becomes one line; 2–4 lanes.
func New(n int, prompt, verify string, lanes []LaneSpec, nowMs int64) (Race, error) {
	prompt = strings.Join(strings.Fields(prompt), " ")
	if prompt == "" {
		return Race{}, errors.New("type the task for the agents")
	}
	if len(lanes) < MinLanes || len(lanes) > MaxLanes {
		return Race{}, fmt.Errorf("a race needs %d to %d lanes", MinLanes, MaxLanes)
	}
	r := Race{ID: fmt.Sprintf("race-%d", n), Number: n, Name: "race/" + Slug(prompt), Prompt: prompt,
		Verify: strings.TrimSpace(verify), StartedMs: nowMs}
	for k, s := range lanes {
		if !word(s.Agent) {
			return Race{}, fmt.Errorf("agent %q is not known", s.Agent)
		}
		r.Lanes = append(r.Lanes, Lane{ID: LaneID(n, k+1, s.Agent), Agent: s.Agent, Title: s.Title, State: Starting})
	}
	return r, nil
}

// word: lower case letters, digits and dashes (an agent id).
func word(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// LaneID is lane k (from 1) of race n running agent.
func LaneID(n, k int, agent string) string { return fmt.Sprintf("race-%d-%d-%s", n, k, agent) }

// Slug is the task as a branch-safe name: lower case words joined by
// dashes, at most 32 characters, "task" when nothing is left.
func Slug(text string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(text) {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		switch {
		case ok:
			if b.Len() >= 32 {
				return strings.TrimRight(b.String(), "-")
			}
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			if b.Len() >= 31 {
				return strings.TrimRight(b.String(), "-")
			}
			b.WriteByte('-')
			dash = true
		}
	}
	s := strings.TrimRight(b.String(), "-")
	if s == "" {
		return "task"
	}
	return s
}

// NextNumber is the number of a new race.
func NextNumber(rs []Race) int {
	n := 0
	for _, r := range rs {
		n = max(n, r.Number)
	}
	return n + 1
}

// Lane finds lane id (nil: none).
func (r *Race) Lane(id string) *Lane {
	for i := range r.Lanes {
		if r.Lanes[i].ID == id {
			return &r.Lanes[i]
		}
	}
	return nil
}

func (r *Race) lanes(keep func(Lane) bool) []*Lane {
	var out []*Lane
	for i := range r.Lanes {
		if keep(r.Lanes[i]) {
			out = append(out, &r.Lanes[i])
		}
	}
	return out
}

// Finished are the lanes whose agent stopped and whose worktree is there.
func (r *Race) Finished() []*Lane { return r.lanes(Lane.Finished) }

// Open are the lanes not merged or discarded.
func (r *Race) Open() []*Lane { return r.lanes(func(l Lane) bool { return !l.Closed() }) }

// Load reads the races in dir (the git dir's termocode folder). No file:
// no races. A file that cannot be read is an error, so a save never
// writes over races it did not see.
func Load(dir string) ([]Race, error) {
	b, err := os.ReadFile(filepath.Join(dir, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f struct {
		Races []Race `json:"races"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", FileName, err)
	}
	return f.Races, nil
}

// Save writes rs (the newest MaxKept) to dir atomically, for the user
// only.
func Save(dir string, rs []Race) error {
	if len(rs) > MaxKept {
		rs = rs[len(rs)-MaxKept:]
	}
	b, err := json.MarshalIndent(struct {
		Races []Race `json:"races"`
	}{rs}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, FileName+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(append(b, '\n'))
	cerr := tmp.Close()
	if werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(name, 0o600)
	}
	if werr == nil {
		werr = os.Rename(name, filepath.Join(dir, FileName))
	}
	if werr != nil {
		_ = os.Remove(name)
	}
	return werr
}
