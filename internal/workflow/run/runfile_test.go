package run

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"
)

// memStore keeps every saved run file; freeze drops later saves (the
// process "died").
type memStore struct {
	mu     sync.Mutex
	saves  [][]byte
	frozen bool
}

func (m *memStore) WriteRunFile(id string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.frozen {
		m.saves = append(m.saves, append([]byte(nil), data...))
	}
	return nil
}

func (m *memStore) freeze() {
	m.mu.Lock()
	m.frozen = true
	m.mu.Unlock()
}

func (m *memStore) last(t *testing.T) *RunFile {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	rf, err := Load(m.saves[len(m.saves)-1])
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return rf
}

func TestRunFileSavedAtEveryBoundary(t *testing.T) {
	fk := newFakes(items("a", "c")...)
	var st memStore
	opts := fk.options()
	opts.Store = &st
	rf := wait(t, Start(context.Background(), mustParse(t, flow), opts))
	if rf.State != RunOK {
		t.Fatalf("state %q %q", rf.State, rf.Error)
	}
	// start, 4 steps × (start, end), 2 items × (start, end, 2 steps ×
	// (start, end), 1 write sending), 1 more write sending, end.
	if n := len(st.saves); n < 1+8+2*7+1+1 {
		t.Fatalf("%d saves", n)
	}
	for i, b := range st.saves {
		if _, err := Load(b); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	if last := st.last(t); last.State != RunOK || last.ID != "r1" || last.Workflow != "w" || len(last.Writes) != 3 {
		t.Fatalf("last save %+v", last)
	}
}

func TestLoadRefusesNewerVersion(t *testing.T) {
	if _, err := Load([]byte(`{"version": 2, "id": "x"}`)); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Fatalf("err %v", err)
	}
	if _, err := Load([]byte(`{`)); err == nil {
		t.Fatal("bad JSON loaded")
	}
}

func TestOutputCappedInRunFile(t *testing.T) {
	o := capOutput(Output{Text: strings.Repeat("é", MaxOutputBytes), JSON: strings.Repeat("x", MaxOutputBytes+1)})
	if len(o.Text) > MaxOutputBytes || !o.Trimmed || o.JSON != nil || !json.Valid([]byte(`"`+o.Text+`"`)) {
		t.Fatalf("len %d trimmed %v json %v", len(o.Text), o.Trimmed, o.JSON != nil)
	}
}

// crashAt runs flow until the fake blocks on key, then "kills" the process:
// later saves are lost. It returns the run file as the disk holds it.
func crashAt(t *testing.T, key string) *RunFile {
	t.Helper()
	fk := newFakes(items("a", "c")...)
	fk.block = key
	var st memStore
	opts := fk.options()
	opts.Store = &st
	h := Start(context.Background(), mustParse(t, flow), opts)
	<-fk.blocked
	st.freeze()
	h.Cancel()
	wait(t, h)
	return st.last(t)
}

func TestCrashMidStepThenResume(t *testing.T) {
	disk := crashAt(t, "code/c")
	if disk.State != RunRunning {
		t.Fatalf("disk state %q", disk.State)
	}
	rf := Recover(disk)
	its := rf.Steps["fix"].Each.Items
	if rf.State != RunInterrupt || rf.Steps["bugs"].State != StateDone || rf.Steps["fix"].State != StateInterrupted ||
		its[0].State != StateDone || its[1].State != StateInterrupted || its[1].Steps["code"].State != StateInterrupted {
		t.Fatalf("recovered run %q fix %q items %q %q code %q", rf.State, rf.Steps["fix"].State, its[0].State, its[1].State, its[1].Steps["code"].State)
	}

	fk := newFakes(items("x")...) // a new source answer must not be used
	var ev events
	opts := fk.options()
	opts.Resume = rf
	opts.OnEvent = ev.on
	end := wait(t, Start(context.Background(), mustParse(t, flow), opts))
	want := []string{
		"enter c base=main",
		`run code/c prompt="fix T c" wt=/wt/c`,
		`write pr/c to=pr title="fix: T c" body="out of code/c" marker=termocode-run:r1/pr/c resumed=false`,
		"leave c ok=true",
		`write sum to=message title="" body="" marker=termocode-run:r1/sum resumed=false`,
	}
	if got := fk.all(); !slices.Equal(got, want) {
		t.Fatalf("calls after resume:\n%s", strings.Join(got, "\n"))
	}
	if end.State != RunOK || end.Resumed != 1 || end.ID != "r1" {
		t.Fatalf("end %q resumed %d id %q (%s)", end.State, end.Resumed, end.ID, end.Error)
	}
	if text := end.Writes[len(end.Writes)-1].Spec.Text; text != "2 done" {
		t.Fatalf("sum %q", text)
	}
}

func TestCrashMidWriteResumesWithMarkerCheck(t *testing.T) {
	disk := crashAt(t, "pr/a")
	if w := disk.Writes; len(w) != 1 || w[0].State != "sending" || w[0].Marker != "termocode-run:r1/pr/a" {
		t.Fatalf("writes on disk %+v", w)
	}
	fk := newFakes()
	opts := fk.options()
	opts.Resume = Recover(disk)
	end := wait(t, Start(context.Background(), mustParse(t, flow), opts))
	calls := fk.all()
	if len(calls) < 2 || calls[0] != "enter a base=main" || calls[1] != `write pr/a to=pr title="fix: T a" body="out of code/a" marker=termocode-run:r1/pr/a resumed=true` {
		t.Fatalf("calls %q", calls)
	}
	if end.State != RunOK || len(end.Writes) != 3 || end.Writes[0].State != StateDone {
		t.Fatalf("end %q writes %+v", end.State, end.Writes)
	}
}

func TestResumeRefusesAChangedFile(t *testing.T) {
	disk := crashAt(t, "code/c")
	fk := newFakes()
	opts := fk.options()
	opts.Resume = Recover(disk)
	changed := mustParse(t, strings.Replace(flow, `"fix {{item.title}}"`, `"fix it"`, 1))
	end := wait(t, Start(context.Background(), changed, opts))
	if end.State != RunFail || !strings.Contains(end.Error, "changed") || len(fk.all()) != 0 {
		t.Fatalf("end %q %q calls %q", end.State, end.Error, fk.all())
	}
}
