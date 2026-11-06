package ledger

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testWrite() Write {
	return Write{
		Step: "note", Item: "LIN-228", Target: "linear create_comment LIN-228",
		Args: map[string]any{"body": "Fixed in #412"},
		Undo: &Undo{Title: "delete_comment", Tool: "linear delete_comment", Args: map[string]string{"issue": "LIN-228"}},
	}
}

// Spec (plan §4.3): the marker names run, step and item.
func TestMarker(t *testing.T) {
	if got := Marker("r1", "note", "LIN-228"); got != "termocode-run:r1/note/LIN-228" {
		t.Fatal(got)
	}
}

// Spec (WFX-6): a write goes sending → done, the file keeps the marker,
// args hash, external id and undo recipe; a replay (same run reopened)
// never sends again.
func TestLedgerReplayNeverSendsTwice(t *testing.T) {
	root := t.TempDir()
	l, err := Open(root, "weekday-bugs", "r1")
	if err != nil {
		t.Fatal(err)
	}
	sends := 0
	var sawSending bool
	send := func(_ context.Context, marker string) (Result, error) {
		sends++
		if !strings.HasPrefix(marker, "termocode-run:r1/note/LIN-2") {
			t.Errorf("marker %q", marker)
		}
		// Recorded before the write goes out.
		l2, _ := Open(root, "weekday-bugs", "r1")
		e, ok := l2.Get("note", "LIN-228")
		sawSending = ok && e.State == Sending
		return Result{ExternalID: "comment-9"}, nil
	}
	e, sent, err := l.Do(context.Background(), testWrite(), send, nil)
	if err != nil || !sent || e.State != Done || e.ExternalID != "comment-9" {
		t.Fatalf("first: %+v sent=%v err=%v", e, sent, err)
	}
	if !sawSending {
		t.Error("no sending entry on disk before the send")
	}
	if e.ArgsHash == "" || e.Undo == nil || e.Undo.Tool != "linear delete_comment" || e.Marker == "" {
		t.Errorf("entry %+v", e)
	}
	for i := 0; i < 2; i++ {
		re, err := Open(root, "weekday-bugs", "r1")
		if err != nil {
			t.Fatal(err)
		}
		e, sent, err := re.Do(context.Background(), testWrite(), send, nil)
		if err != nil || sent || e.ExternalID != "comment-9" {
			t.Fatalf("replay: %+v sent=%v err=%v", e, sent, err)
		}
	}
	if sends != 1 {
		t.Fatalf("sent %d times", sends)
	}
	// Another item is its own write.
	w := testWrite()
	w.Item = "LIN-230"
	if _, sent, _ := l.Do(context.Background(), w, send, nil); !sent || sends != 2 {
		t.Fatalf("other item sent=%v sends=%d", sent, sends)
	}
}

// Spec (plan §4.3): a write left in "sending" (crash while sending) is
// looked up by its marker first: found → done without a send; not found
// → sent once. Without a way to look it up it is never sent blindly.
func TestLedgerResumeChecksMarker(t *testing.T) {
	root := t.TempDir()
	l, _ := Open(root, "wf", "r2")
	crash := errors.New("crash")
	_, _, err := l.Do(context.Background(), testWrite(), func(context.Context, string) (Result, error) {
		disk, _ := Open(root, "wf", "r2") // still sending on disk
		if e, _ := disk.Get("note", "LIN-228"); e.State != Sending {
			t.Errorf("state %q", e.State)
		}
		return Result{}, crash
	}, nil)
	if !errors.Is(err, crash) {
		t.Fatalf("err %v", err)
	}
	sends := 0
	send := func(context.Context, string) (Result, error) { sends++; return Result{ExternalID: "c2"}, nil }

	re, _ := Open(root, "wf", "r2")
	if _, sent, err := re.Do(context.Background(), testWrite(), send, nil); !errors.Is(err, ErrMaybeSent) || sent || sends != 0 {
		t.Fatalf("no finder: sent=%v err=%v sends=%d", sent, err, sends)
	}
	found := func(_ context.Context, marker string) (Result, bool, error) {
		return Result{ExternalID: "c1"}, marker == Marker("r2", "note", "LIN-228"), nil
	}
	e, sent, err := re.Do(context.Background(), testWrite(), send, found)
	if err != nil || sent || e.State != Done || e.ExternalID != "c1" || sends != 0 {
		t.Fatalf("found: %+v sent=%v err=%v sends=%d", e, sent, err, sends)
	}

	re2, _ := Open(root, "wf", "r3")
	_, _, _ = re2.Do(context.Background(), testWrite(), func(context.Context, string) (Result, error) { return Result{}, crash }, nil)
	missing := func(context.Context, string) (Result, bool, error) { return Result{}, false, nil }
	e, sent, err = re2.Do(context.Background(), testWrite(), send, missing)
	if err != nil || !sent || e.ExternalID != "c2" || sends != 1 {
		t.Fatalf("missing: %+v sent=%v err=%v sends=%d", e, sent, err, sends)
	}
	// Forget lets the user send a failed write again on purpose.
	re3, _ := Open(root, "wf", "r4")
	_, _, _ = re3.Do(context.Background(), testWrite(), func(context.Context, string) (Result, error) { return Result{}, crash }, nil)
	if err := re3.Forget("note", "LIN-228"); err != nil {
		t.Fatal(err)
	}
	if _, sent, err := re3.Do(context.Background(), testWrite(), send, nil); err != nil || !sent {
		t.Fatalf("after Forget: sent=%v err=%v", sent, err)
	}
}

// Spec: the ledger file is per workflow and run, under runs/<run>/; a
// file of another workflow is refused.
func TestLedgerFilePerRun(t *testing.T) {
	root := t.TempDir()
	l, _ := Open(root, "wf", "r1")
	_, _, _ = l.Do(context.Background(), testWrite(), func(context.Context, string) (Result, error) { return Result{}, nil }, nil)
	p := filepath.Join(root, "runs", "r1", "ledger.json")
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), `"workflow": "wf"`) {
		t.Fatalf("%s: %s %v", p, b, err)
	}
	if _, err := Open(root, "other", "r1"); err == nil {
		t.Fatal("a run of another workflow opened")
	}
	if _, err := Open(root, "wf", "../x"); err == nil {
		t.Fatal("bad run id accepted")
	}
}
