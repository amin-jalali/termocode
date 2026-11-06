package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/amin-jalali/termocode/internal/clock"
)

// State is where one write is.
type State string

const (
	// Sending is recorded before the write goes out: after a crash the
	// write may or may not have reached the other side.
	Sending State = "sending"
	// Done: the other side has it (ExternalID names it).
	Done State = "done"
	// Failed: the send returned an error. It may still have arrived (a
	// timeout), so it is treated like Sending on the next try.
	Failed State = "failed"
	// Undone: the user ran the write's undo recipe.
	Undone State = "undone"
)

// ErrMaybeSent: the write was started before and there is no way to look
// it up by its marker, so it is not sent again. Forget lets the user send
// it again on purpose.
var ErrMaybeSent = errors.New("this write may have been sent already; not sending it twice")

// Marker is the text that ends every comment and PR body a run writes, so
// a resumed run can find a write that was in flight.
func Marker(run, step, item string) string {
	return "termocode-run:" + run + "/" + step + "/" + item
}

// Undo is how to take a write back (shown on the Undo card).
type Undo struct {
	Title string            `json:"title"`
	Tool  string            `json:"tool,omitempty"`
	Args  map[string]string `json:"args,omitempty"`
}

// Write is one outside write of a step for one item. Args are only
// hashed, never stored.
type Write struct {
	Step, Item string
	Target     string
	Args       any
	Undo       *Undo
}

// Result is what the other side answered.
type Result struct {
	ExternalID string
}

// Entry is one write in the ledger.
type Entry struct {
	Step       string `json:"step"`
	Item       string `json:"item"`
	Target     string `json:"target,omitempty"`
	ArgsHash   string `json:"args_hash"`
	Marker     string `json:"marker"`
	State      State  `json:"state"`
	ExternalID string `json:"external_id,omitempty"`
	Undo       *Undo  `json:"undo,omitempty"`
	Error      string `json:"error,omitempty"`
	StartedMs  int64  `json:"started_ms"`
	DoneMs     int64  `json:"done_ms,omitempty"`
}

// SendFunc does the write; marker must be part of the written text.
type SendFunc func(ctx context.Context, marker string) (Result, error)

// FindFunc looks a write up by its marker (search the comments / PRs).
type FindFunc func(ctx context.Context, marker string) (Result, bool, error)

type file struct {
	Workflow string  `json:"workflow"`
	Run      string  `json:"run"`
	Writes   []Entry `json:"writes"`
}

// Ledger is the write ledger of one run. It is safe for the item workers
// of a run to share; one write (step + item) is done by one worker.
type Ledger struct {
	mu   sync.Mutex
	path string
	f    file
}

// Open loads (or starts) the ledger of run of workflow under root.
func Open(root, workflow, run string) (*Ledger, error) {
	if err := checkName(workflow); err != nil {
		return nil, err
	}
	if err := checkName(run); err != nil {
		return nil, err
	}
	l := &Ledger{path: filepath.Join(root, "runs", run, "ledger.json"), f: file{Workflow: workflow, Run: run}}
	b, err := os.ReadFile(l.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return l, nil
	case err != nil:
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("write ledger %s: %w", l.path, err)
	}
	if f.Workflow != workflow || f.Run != run {
		return nil, fmt.Errorf("write ledger %s belongs to %s/%s", l.path, f.Workflow, f.Run)
	}
	l.f = f
	return l, nil
}

// Entries is a copy of every write, oldest first.
func (l *Ledger) Entries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]Entry(nil), l.f.Writes...)
}

// Get is the write of step for item.
func (l *Ledger) Get(step, item string) (Entry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if i := l.find(step, item); i >= 0 {
		return l.f.Writes[i], true
	}
	return Entry{}, false
}

func (l *Ledger) find(step, item string) int {
	for i, e := range l.f.Writes {
		if e.Step == step && e.Item == item {
			return i
		}
	}
	return -1
}

// MarkUndone records that the write of step for item was taken back.
func (l *Ledger) MarkUndone(step, item string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := l.find(step, item)
	if i < 0 {
		return errors.New("write ledger: no write " + step + "/" + item)
	}
	l.f.Writes[i].State = Undone
	return l.saveLocked()
}

// Forget removes the write of step for item (the user chose to send it
// again).
func (l *Ledger) Forget(step, item string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	i := l.find(step, item)
	if i < 0 {
		return nil
	}
	l.f.Writes = append(l.f.Writes[:i], l.f.Writes[i+1:]...)
	return l.saveLocked()
}

// set stores e redacted (replacing the entry of its step and item) and
// saves.
func (l *Ledger) set(e Entry) (Entry, error) {
	e = clean(e)
	l.mu.Lock()
	defer l.mu.Unlock()
	if i := l.find(e.Step, e.Item); i >= 0 {
		l.f.Writes[i] = e
	} else {
		l.f.Writes = append(l.f.Writes, e)
	}
	return e, l.saveLocked()
}

func (l *Ledger) saveLocked() error {
	b, err := json.MarshalIndent(l.f, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(l.path, b)
}

// ArgsHash is the stored stand-in for a write's arguments.
func ArgsHash(args any) string {
	b, err := json.Marshal(args)
	if err != nil {
		b = []byte(fmt.Sprint(args))
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Do makes the write w at most once:
//
//   - done before → the stored entry, nothing sent;
//   - started before (sending / failed) → find looks it up by its marker:
//     found → recorded done, nothing sent; not found → sent; no find →
//     ErrMaybeSent, nothing sent;
//   - else "sending" is saved first (no save, no send), then send runs
//     and the answer is saved as done (or failed).
//
// sent says whether send ran in this call.
func (l *Ledger) Do(ctx context.Context, w Write, send SendFunc, find FindFunc) (Entry, bool, error) {
	marker := Marker(l.f.Run, w.Step, w.Item)
	if old, ok := l.Get(w.Step, w.Item); ok {
		if old.State == Done || old.State == Undone {
			return old, false, nil
		}
		if find == nil {
			return old, false, ErrMaybeSent
		}
		res, found, err := find(ctx, marker)
		if err != nil {
			return old, false, err
		}
		if found {
			old.State, old.ExternalID, old.Error, old.DoneMs = Done, res.ExternalID, "", clock.Now().UnixMilli()
			old, err = l.set(old)
			return old, false, err
		}
	}
	e := Entry{
		Step: w.Step, Item: w.Item, Target: w.Target, ArgsHash: ArgsHash(w.Args),
		Marker: marker, State: Sending, Undo: w.Undo, StartedMs: clock.Now().UnixMilli(),
	}
	e, err := l.set(e)
	if err != nil {
		return e, false, err
	}
	res, err := send(ctx, marker)
	if err != nil {
		e.State, e.Error = Failed, err.Error()
		e, serr := l.set(e)
		return e, true, errors.Join(err, serr)
	}
	e.State, e.ExternalID, e.DoneMs = Done, res.ExternalID, clock.Now().UnixMilli()
	e, err = l.set(e)
	return e, true, err
}
