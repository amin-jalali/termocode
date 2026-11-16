package ghci

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Fetch asks for each run's last change (updatedAt); Updated is the
// newest one of the snapshot's runs, zero when gh gives none.
func TestFetchRunTimes(t *testing.T) {
	f := &fakeGH{answers: map[string]fakeAnswer{
		"run list": {out: `[{"databaseId":30,"workflowName":"CI","status":"completed","conclusion":"failure","headSha":"bbb","url":"u30","updatedAt":"2026-11-16T09:00:00Z"},
 {"databaseId":29,"workflowName":"Lint","status":"completed","conclusion":"success","headSha":"bbb","url":"u29","updatedAt":"2026-11-16T09:05:00Z"}]`},
		"pr view": {out: prJSONText},
	}}
	s, err := Fetch(context.Background(), f.run, "/repo", "feat/x")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.calls[0], " "); !strings.Contains(got, "updatedAt") {
		t.Errorf("run list does not ask for updatedAt: %q", got)
	}
	if want := time.Date(2026, 11, 16, 9, 5, 0, 0, time.UTC); !s.Updated().Equal(want) {
		t.Errorf("updated %v, want %v", s.Updated(), want)
	}
	if !(Snapshot{Runs: []Run{{Workflow: "CI"}, {Workflow: "Lint", Updated: "junk"}}}).Updated().IsZero() {
		t.Error("runs without a time give a time")
	}
}
