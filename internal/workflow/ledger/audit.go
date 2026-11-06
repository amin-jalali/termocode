package ledger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditLine is one line of a run's audit.jsonl (plan §6.6). Text fields
// are redacted before they are written.
type AuditLine struct {
	At      time.Time `json:"at"`
	Run     string    `json:"run"`
	Kind    string    `json:"kind"` // a run event kind, or "gate"
	Step    string    `json:"step,omitempty"`
	Item    string    `json:"item,omitempty"`
	Attempt int       `json:"attempt,omitempty"`
	Target  string    `json:"target,omitempty"`
	Answer  string    `json:"answer,omitempty"` // gate: deny, allow once, allow for this workflow, allow-list
	By      string    `json:"by"`               // workflow:<name>[/<step>[/<item>]]
	Err     string    `json:"err,omitempty"`
}

// Audit appends lines to <root>/runs/<run>/audit.jsonl (0600).
type Audit struct {
	mu       sync.Mutex
	path     string
	workflow string
	run      string
}

// OpenAudit returns the audit of run of workflow under root.
func OpenAudit(root, workflow, run string) (*Audit, error) {
	if err := checkName(workflow); err != nil {
		return nil, err
	}
	if err := checkName(run); err != nil {
		return nil, err
	}
	return &Audit{path: filepath.Join(root, "runs", run, "audit.jsonl"), workflow: workflow, run: run}, nil
}

// Path is the audit file.
func (a *Audit) Path() string { return a.path }

// Add writes one line; At, Run and By are filled when empty.
func (a *Audit) Add(l AuditLine) error {
	if a == nil {
		return nil
	}
	if l.At.IsZero() {
		l.At = time.Now().UTC()
	}
	l.Run = a.run
	if l.By == "" {
		l.By = "workflow:" + a.workflow
		if l.Step != "" {
			l.By += "/" + l.Step
			if l.Item != "" {
				l.By += "/" + l.Item
			}
		}
	}
	l.Target, l.Err, l.Item = redact(l.Target), redact(l.Err), redact(l.Item)
	b, err := json.Marshal(l)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(a.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(b, '\n'))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return werr
}
