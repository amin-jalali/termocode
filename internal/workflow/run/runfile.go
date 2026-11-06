package run

import (
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/workflow"
)

// RunFileVersion is the version of the run file format.
const RunFileVersion = 1

// MaxOutputBytes caps one step output (text, JSON) in the run file.
const MaxOutputBytes = 64 << 10

// Run states.
const (
	RunRunning   = "running"
	RunOK        = "done"
	RunFail      = "failed"
	RunCancel    = "cancelled"
	RunPause     = "paused"
	RunInterrupt = "interrupted" // the process ended while it ran (Recover)
)

// Step and item states.
const (
	StatePending     = "pending"
	StateRunning     = "running"
	StateDone        = "done"
	StateFailed      = "failed"
	StateSkipped     = "skipped"   // when was false (or --no-plan)
	StateWouldRun    = "would_run" // dry run: a run step that was not run
	StateRecorded    = "recorded"  // dry run or gate "dry": a write only listed
	StateDenied      = "denied"    // the user said no
	StateCancelled   = "cancelled"
	StatePaused      = "paused"
	StateInterrupted = "interrupted" // the process ended mid-step
)

// WriteSending is a write record that went out but has no answer yet.
const WriteSending = "sending"

// RunFile is the whole state of one run. It is what the run file holds
// (JSON) and what Handle.Snapshot returns.
type RunFile struct {
	Version   int                   `json:"version"`
	ID        string                `json:"id"`
	Workflow  string                `json:"workflow"`
	Hash      string                `json:"hash"`
	Dry       bool                  `json:"dry,omitempty"`
	State     string                `json:"state"`
	StartedAt time.Time             `json:"started_at"`
	EndedAt   time.Time             `json:"ended_at,omitzero"`
	Vars      map[string]any        `json:"vars,omitempty"`
	Steps     map[string]*StepState `json:"steps"`
	Writes    []WriteRecord         `json:"writes,omitempty"`
	Error     string                `json:"error,omitempty"`
	Resumed   int                   `json:"resumed,omitempty"` // how often this run was resumed
	Held      bool                  `json:"held,omitempty"`    // the user paused it: no new step starts
}

// StepState is one step of one list (the run's, or an item's).
type StepState struct {
	State    string     `json:"state"`
	Attempts int        `json:"attempts,omitempty"`
	Error    string     `json:"error,omitempty"`
	Output   *Output    `json:"output,omitempty"`
	Items    []Item     `json:"items,omitempty"` // source, plan, filter: the list it made
	Each     *EachState `json:"each,omitempty"`
}

// EachState holds the items of an each step.
type EachState struct {
	Items []*ItemState `json:"items"`
}

// ItemState is one item of an each step.
type ItemState struct {
	Key   string                `json:"key"`
	Data  any                   `json:"data,omitempty"`
	Plan  *PlanMark             `json:"plan,omitempty"`
	State string                `json:"state"`
	Error string                `json:"error,omitempty"`
	Env   ItemEnv               `json:"env,omitzero"`
	Steps map[string]*StepState `json:"steps"`
}

func (is *ItemState) item() Item { return Item{Key: is.Key, Data: is.Data, Plan: is.Plan} }

// WriteRecord is one write: listed (dry), sent, or refused.
type WriteRecord struct {
	Step   string             `json:"step"`
	Item   string             `json:"item,omitempty"`
	To     string             `json:"to"`
	State  string             `json:"state"` // recorded | sending | done | denied | failed
	Marker string             `json:"marker"`
	Spec   workflow.WriteSpec `json:"spec"`
	URL    string             `json:"url,omitempty"`
	Error  string             `json:"error,omitempty"`
}

// clone deep-copies the run file (through JSON, which also proves it
// can be saved).
func (rf *RunFile) clone() *RunFile {
	b, err := json.Marshal(rf)
	if err != nil {
		return &RunFile{ID: rf.ID, State: rf.State, Error: "run file: " + err.Error()}
	}
	var out RunFile
	if err := json.Unmarshal(b, &out); err != nil {
		return &RunFile{ID: rf.ID, State: rf.State, Error: "run file: " + err.Error()}
	}
	return &out
}

// capOutput cuts an output to what the run file keeps.
func capOutput(o Output) Output {
	if len(o.Text) > MaxOutputBytes {
		cut := MaxOutputBytes
		for cut > 0 && !utf8.RuneStart(o.Text[cut]) {
			cut--
		}
		o.Text = o.Text[:cut]
		o.Trimmed = true
	}
	if o.JSON != nil {
		if b, err := json.Marshal(o.JSON); err != nil || len(b) > MaxOutputBytes {
			o.JSON = nil
			o.Trimmed = true
		}
	}
	return o
}

// RunFileWriter saves the run file. The engine calls it at every step
// and item boundary (never per token) with the whole file; the adapter
// writes it atomically (a temp file, then rename) under
// <state>/termocode/workflows/<ws>/runs/<id>.json.
type RunFileWriter interface {
	WriteRunFile(id string, data []byte) error
}

// Load reads a run file.
func Load(data []byte) (*RunFile, error) {
	var rf RunFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("run file: %w", err)
	}
	if rf.Version > RunFileVersion {
		return nil, fmt.Errorf("run file %s was made by a newer termocode (version %d)", rf.ID, rf.Version)
	}
	if rf.Steps == nil {
		rf.Steps = map[string]*StepState{}
	}
	return &rf, nil
}

// Recover is the state of a run file whose process ended while it ran
// (a crash or a restart): the run and every step or item that was
// running are marked interrupted. Finished ones stay as they are. A
// write that was "sending" stays so; a resumed run checks its marker
// before it sends again. A run file that is not running comes back as
// it is.
func Recover(rf *RunFile) *RunFile {
	out := rf.clone()
	if out.State != RunRunning {
		return out
	}
	out.State = RunInterrupt
	interruptSteps(out.Steps)
	return out
}

func interruptSteps(steps map[string]*StepState) {
	for _, st := range steps {
		if st.State == StateRunning {
			st.State = StateInterrupted
		}
		if st.Each == nil {
			continue
		}
		for _, is := range st.Each.Items {
			if is.State == StateRunning {
				is.State = StateInterrupted
			}
			interruptSteps(is.Steps)
		}
	}
}

// finished is a step state that a resumed run keeps (it is not run
// again).
func finished(state string) bool {
	switch state {
	case StateDone, StateSkipped, StateWouldRun, StateRecorded:
		return true
	}
	return false
}

// itemFinished is an item state that a resumed run keeps.
func itemFinished(state string) bool { return state == StateDone || state == StateFailed }
