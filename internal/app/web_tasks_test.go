package app

import (
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/nvim"
	"github.com/amin-jalali/termocode/internal/tasks"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// webTestTasksRepo opens a workspace with two tasks in tasks.json.
func webTestTasksRepo(t *testing.T, m *Model) string {
	t.Helper()
	dir := t.TempDir()
	webTestWrite(t, dir, ".termocode/tasks.json", `{"tasks":[
		{"label":"build","command":"go build ./...","group":{"kind":"build","isDefault":true}},
		{"label":"test","command":"go test ./...","group":"test"}]}`)
	m.ws = workspace.Workspace{Root: dir}
	return dir
}

// webTestFakeTasks swaps starting and stopping a task's job for fakes
// that keep the runner's books as the real ones do; started gets the
// labels started, stopped the buffers stopped.
func webTestFakeTasks(t *testing.T) (started *[]string, stopped *[]int) {
	t.Helper()
	var s []string
	var st []int
	oldRun, oldStop := webRunTask, webStopTask
	buf := 40
	webRunTask = func(m *Model, task tasks.Task) {
		s = append(s, task.Label)
		buf++
		tr := m.ensureTasks()
		tr.runs[buf] = &taskRun{Task: task, Buf: buf, Started: webNow(), Running: true}
		tr.order = append(tr.order, buf)
	}
	webStopTask = func(m *Model, r *taskRun) { st = append(st, r.Buf) }
	t.Cleanup(func() { webRunTask, webStopTask = oldRun, oldStop })
	return &s, &st
}

func webTestTasks(t *testing.T, frames []map[string]any) (webproto.TasksSnap, int) {
	t.Helper()
	var s webproto.TasksSnap
	n := 0
	for _, f := range frames {
		if f["topic"] == "tasks" {
			s = webproto.TasksSnap{}
			webTestData(t, f, &s)
			n++
		}
	}
	return s, n
}

// Spec (WEB-25): the tasks topic lists the tasks the laptop's picker
// has, with each one's newest run; tasks.run starts a defined task by
// label and answers its terminal; the start and the exit each send the
// list again; tasks.terminate stops the running one; both are audited.
func TestTasksListAndRun(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	now := webTestClock(t)
	webTestHub(t, &m)
	webTestTasksRepo(t, &m)
	started, stopped := webTestFakeTasks(t)
	c := webTestSub(t, &m, "tasks")
	s, _ := webTestTasks(t, webTestNow(t, c))
	if len(s.Tasks) < 2 || s.Tasks[0].Label != "build" || s.Tasks[0].Group != "build" || !s.Tasks[0].Default ||
		s.Tasks[0].Source != "tasks.json" || s.Tasks[0].Detail != "go build ./..." || s.Tasks[0].Last != nil {
		t.Fatalf("tasks %+v", s.Tasks)
	}

	f, _ := webTestCall(t, &m, c, webproto.OpTasksRun, `{"label":"test"}`)
	var r webproto.TaskStarted
	webTestData(t, f, &r)
	if r.Buf != 41 || strings.Join(*started, ",") != "test" {
		t.Fatalf("run: %+v started %q", r, *started)
	}
	m.webAfterUpdate(nil)
	s, n := webTestTasks(t, webTestNow(t, c))
	if n != 1 || s.Tasks[1].Last == nil || *s.Tasks[1].Last != (webproto.TaskLast{State: "running", When: now.UnixMilli(), Buf: 41}) {
		t.Fatalf("after the start: %d frames, %+v", n, s.Tasks)
	}
	m.webAfterUpdate(nil)
	if _, n := webTestTasks(t, webTestNow(t, c)); n != 0 {
		t.Fatalf("%d frames without a change", n)
	}

	if f, _ := webTestCall(t, &m, c, webproto.OpTasksTerminate, `{"label":"test"}`); f["t"] != "res" || len(*stopped) != 1 || (*stopped)[0] != 41 {
		t.Fatalf("terminate: %v stopped %v", f, *stopped)
	}
	*now = now.Add(time.Second)
	m.onTaskExit(41, 2)
	m.webAfterUpdate(nil)
	s, _ = webTestTasks(t, webTestNow(t, c))
	if l := s.Tasks[1].Last; l == nil || l.State != "failed" || l.Code != 2 {
		t.Fatalf("after the exit: %+v", l)
	}
	if f, _ := webTestCall(t, &m, c, webproto.OpTasksTerminate, `{"label":"test"}`); webTestErr(f) != "conflict" {
		t.Fatalf("terminate a task that is not running: %v", f)
	}
	a := webTestAudit(&m)
	if !strings.Contains(a, "tasks.run  test  by=web:phone  → ok") || !strings.Contains(a, "tasks.terminate  test  by=web:phone  → ok") {
		t.Errorf("audit:\n%s", a)
	}
}

// Spec (WEB-25): the web never runs a command of its own: an unknown
// label is not found (nothing starts), and the args have no field for a
// command.
func TestTasksNoFreeCommand(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	webTestTasksRepo(t, &m)
	started, _ := webTestFakeTasks(t)
	c := webTestSub(t, &m)
	for _, label := range []string{"rm -rf /", "go build ./...", "Build"} {
		if f, _ := webTestCall(t, &m, c, webproto.OpTasksRun, `{"label":"`+label+`"}`); webTestErr(f) != "not_found" {
			t.Errorf("label %q: %v", label, f)
		}
	}
	for _, args := range []string{`{"label":"build","command":"rm -rf /"}`, `{"command":"ls"}`} {
		if _, err := webproto.DecodeArgs(webproto.OpTasksRun, []byte(args)); err == nil {
			t.Errorf("%s decoded", args)
		}
	}
	if len(*started) != 0 {
		t.Fatalf("started %q", *started)
	}
}

// Spec (WEB-25): tasks.tail answers the end of the newest run's terminal:
// trailing blank rows dropped, at most MaxTailLines lines and
// MaxTailBytes bytes, truncated said; a task that never ran is not found.
func TestTasksTail(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	webTestTasksRepo(t, &m)
	webTestFakeTasks(t)
	var lines []string
	for i := 0; i < 300; i++ {
		lines = append(lines, "row "+itoa(i))
	}
	lines = append(lines, "", "")
	old := webTaskLines
	webTaskLines = func(_ *nvim.Client, buf, n int) ([]string, error) {
		if buf != 41 {
			t.Errorf("tail of buffer %d", buf)
		}
		return lines[max(0, len(lines)-n):], nil
	}
	t.Cleanup(func() { webTaskLines = old })
	c := webTestSub(t, &m)
	if f, _ := webTestCall(t, &m, c, webproto.OpTasksTail, `{"label":"test"}`); webTestErr(f) != "not_found" {
		t.Fatalf("tail before a run: %v", f)
	}
	webTestCall(t, &m, c, webproto.OpTasksRun, `{"label":"test"}`)
	id := webTestSearchStart(t, &m, c, webproto.OpTasksTail, `{"label":"test"}`)
	_, f := webTestSearchFrames(t, c, id)
	var tail webproto.TaskTail
	webTestData(t, f, &tail)
	if tail.Buf != 41 || !tail.Running || !tail.Truncated || len(tail.Lines) > webproto.MaxTailLines || tail.Lines[len(tail.Lines)-1] != "row 299" {
		t.Fatalf("tail %d lines, last %q, %+v", len(tail.Lines), tail.Lines[len(tail.Lines)-1], tail.Truncated)
	}
	big := strings.Repeat("x", 1000)
	for i := range lines {
		lines[i] = big
	}
	id = webTestSearchStart(t, &m, c, webproto.OpTasksTail, `{"label":"test"}`)
	_, f = webTestSearchFrames(t, c, id)
	tail = webproto.TaskTail{}
	webTestData(t, f, &tail)
	size := 0
	for _, l := range tail.Lines {
		size += len(l) + 1
	}
	if size > webproto.MaxTailBytes || !tail.Truncated || len(tail.Lines) == 0 {
		t.Fatalf("tail of %d bytes", size)
	}
}

// Spec (WEB-25): the tasks replay golden: a start and an exit replay to
// the final snapshot.
func TestTasksReplayGolden(t *testing.T) {
	m := aiTestModel(t)
	now := webTestClock(t)
	h := webTestHub(t, &m)
	webTestTasksRepo(t, &m)
	webTestFakeTasks(t)
	c := webReplayRecord(t, &m, h, "tasks", func() {
		m.web.ops[webproto.OpTasksRun](&m, &webCall{ctx: h.ctx, conn: &webConn{out: newWebQueue()}, op: webproto.OpTasksRun, args: &webproto.LabelArgs{Label: "build"}})
		m.webAfterUpdate(nil)
		*now = now.Add(time.Second)
		m.onTaskExit(41, 0)
		m.webAfterUpdate(nil)
	})
	webReplayCheck(t, h.topics["tasks"], c)
	webReplayGolden(t, webReplayPath("tasks", "run_exit"), c)
}
