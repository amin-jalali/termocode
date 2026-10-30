package webproto

import (
	"strings"
	"testing"
)

// Spec (WEB-25): the tasks and runs data is typed for the page: both
// topics send the whole list again as their delta; tasks.run answers the
// terminal it started; tasks.tail is the capped text tail of a task's
// terminal.
func TestTasksDataGenerated(t *testing.T) {
	b, err := GenerateTS()
	if err != nil {
		t.Fatal(err)
	}
	ts := string(b)
	for _, want := range []string{
		`  "tasks": { snap: TasksSnap; delta: TasksSnap };`,
		`  "runs": { snap: RunsSnap; delta: RunsSnap };`,
		`  "tasks.run": TaskStarted;`,
		`  "tasks.tail": TaskTail;`,
		`  state: "running" | "ok" | "failed";`,
		`  state: "running" | "stalled" | "pausing" | "paused" | "resumable" | "done" | "failed" | "cancelled";`,
	} {
		if !strings.Contains(ts, want) {
			t.Errorf("proto.d.ts misses %q", want)
		}
	}
}
