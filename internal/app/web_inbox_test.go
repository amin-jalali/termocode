package app

import (
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/ghci"
	"github.com/amin-jalali/termocode/internal/perfbudget"
	"github.com/amin-jalali/termocode/internal/tasks"
	"github.com/amin-jalali/termocode/internal/webproto"
	"github.com/amin-jalali/termocode/internal/workspace"
)

// webTestInbox is the newest inbox frame (snap or delta) and how many came.
func webTestInbox(t *testing.T, frames []map[string]any) (webproto.InboxSnap, int) {
	t.Helper()
	var s webproto.InboxSnap
	n := 0
	for _, f := range frames {
		if f["topic"] == "inbox" {
			s = webproto.InboxSnap{}
			webTestData(t, f, &s)
			n++
		}
	}
	return s, n
}

// webTestTaskRun adds a task run to the runner's books, as a real start does.
func webTestTaskRun(m *Model, label string, started time.Time, running bool, code int) *taskRun {
	tr := m.ensureTasks()
	buf := 100 + len(tr.order)
	r := &taskRun{Task: tasks.Task{Label: label}, Buf: buf, Started: started, Running: running, Code: code}
	tr.runs[buf] = r
	tr.order = append(tr.order, buf)
	return r
}

func webTestInboxItem(t *testing.T, s webproto.InboxSnap, id string) webproto.InboxItem {
	t.Helper()
	for _, it := range s.Items {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no inbox item %q in %+v", id, s.Items)
	return webproto.InboxItem{}
}

// Spec (MOB-2): the inbox merges the laptop's sources into one list,
// newest first: a waiting card (the cards topic's id, answerable), the
// task runs (one item per task, its newest run), the branch's CI once the
// laptop's watch has an answer. Counts cover every state.
func TestInboxTopicMerge(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	now := webTestClock(t)
	webTestHub(t, &m)
	m.ws = workspace.Workspace{Root: t.TempDir()}
	webTestTaskRun(&m, "build", now.Add(-3*time.Minute), false, 0)
	webTestTaskRun(&m, "test", now.Add(-2*time.Minute), false, 1)
	webTestTaskRun(&m, "build", now.Add(-time.Minute), true, 0)
	m.ci = &ciWatch{branch: "main", loaded: true, snap: ghci.Snapshot{Branch: "main", HeadSHA: "abc1234def",
		Runs: []ghci.Run{{Workflow: "ci", Status: "completed", Conclusion: "failure"}}}}
	webTestToolCard(&m, ai.Tool{Name: "write_file"})

	c := webTestSub(t, &m, "inbox")
	s, n := webTestInbox(t, webTestNow(t, c))
	if n != 1 {
		t.Fatalf("%d inbox frames, want the snap", n)
	}
	var ids []string
	for _, it := range s.Items {
		ids = append(ids, it.ID)
	}
	if got := strings.Join(ids, ","); got != "card:card-1,task:build,task:test,ci:main" {
		t.Fatalf("items %s, want card, newest task, older task, ci", got)
	}
	card := webTestInboxItem(t, s, "card:card-1")
	if card.Kind != webproto.InboxCard || card.Ref != "card-1" || card.State != webproto.InboxNeedsYou ||
		!strings.HasPrefix(card.Title, "AI wants to: ") || strings.Join(card.Can, ",") != "answer" || card.Since != now.UnixMilli() {
		t.Errorf("card %+v", card)
	}
	if b := webTestInboxItem(t, s, "task:build"); b.State != webproto.InboxWorking || strings.Join(b.Can, ",") != "stop" ||
		b.Since != now.Add(-time.Minute).UnixMilli() {
		t.Errorf("build (its newest run) %+v", b)
	}
	if x := webTestInboxItem(t, s, "task:test"); x.State != webproto.InboxFailed || strings.Join(x.Can, ",") != "run" {
		t.Errorf("failed test %+v", x)
	}
	if ci := webTestInboxItem(t, s, "ci:main"); ci.Kind != webproto.InboxCI || ci.State != webproto.InboxFailed || ci.Title != "CI · main" {
		t.Errorf("ci %+v", ci)
	}
	want := webproto.InboxCounts{NeedsYou: 1, Working: 1, Failed: 2}
	if s.Counts != want || s.More != 0 {
		t.Errorf("counts %+v more %d, want %+v", s.Counts, s.More, want)
	}
}

// Spec (MOB-2): a change of a source sends the whole list again (at most
// once per webInboxEvery); an Update that changed nothing sends nothing.
func TestInboxTopicDeltaOnChange(t *testing.T) {
	m := aiTestModel(t)
	m.output = newOutputStore()
	now := webTestClock(t)
	webTestHub(t, &m)
	m.ws = workspace.Workspace{Root: t.TempDir()}
	r := webTestTaskRun(&m, "build", now.Add(-time.Minute), true, 0)
	c := webTestSub(t, &m, "inbox")
	webTestNow(t, c)

	*now = now.Add(time.Second)
	m.webAfterUpdate(nil)
	if _, n := webTestInbox(t, webTestNow(t, c)); n != 0 {
		t.Fatalf("%d frames for no change", n)
	}

	r.Running = false
	*now = now.Add(time.Second)
	m.webAfterUpdate(nil)
	s, n := webTestInbox(t, webTestNow(t, c))
	if n != 1 || len(s.Items) != 1 || s.Items[0].State != webproto.InboxDone || s.Counts.Done != 1 || s.Counts.Working != 0 {
		t.Fatalf("after the task ended: %d frames %+v", n, s)
	}

	// Within webInboxEvery a change waits for the tick.
	webTestTaskRun(&m, "lint", *now, true, 0)
	*now = now.Add(webInboxEvery / 5)
	if cmd := m.webPublishInbox(nil); cmd == nil {
		t.Fatal("no tick for a change inside webInboxEvery")
	}
	if _, n := webTestInbox(t, webTestNow(t, c)); n != 0 {
		t.Fatalf("%d frames before the tick", n)
	}
	*now = now.Add(webInboxEvery)
	m.webPublishInbox(webInboxTickMsg{})
	if s, n := webTestInbox(t, webTestNow(t, c)); n != 1 || len(s.Items) != 2 || s.Items[0].ID != "task:lint" {
		t.Fatalf("after the tick: %d frames %+v", n, s.Items)
	}
}

// Spec (MOB-2): the list keeps the newest MaxInbox items and counts the
// rest; titles are cut at MaxInboxTitle on a rune edge; no title carries
// a path outside the workspace (workspace paths go relative, home is ~,
// other absolute paths keep only their last part).
func TestInboxTopicCapTitleAndPaths(t *testing.T) {
	m := aiTestModel(t)
	now := webTestClock(t)
	webTestHub(t, &m)
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := filepath.Join(home, "src", "app")
	m.ws = workspace.Workspace{Root: root}
	for i := 0; i < webproto.MaxInbox+50; i++ {
		webTestTaskRun(&m, "task "+strconv.Itoa(i), now.Add(time.Duration(i)*time.Second), false, 0)
	}
	long := strings.Repeat("é", 100) // 200 bytes
	webTestTaskRun(&m, long, now.Add(time.Hour), false, 0)
	webTestTaskRun(&m, "run "+root+"/scripts/a.sh", now.Add(2*time.Hour), false, 0)
	webTestTaskRun(&m, "cat /etc/secret/keys.txt", now.Add(3*time.Hour), false, 0)
	webTestTaskRun(&m, "open "+home+"/notes.md", now.Add(4*time.Hour), false, 0)

	s := m.webInboxBuild()
	if len(s.Items) != webproto.MaxInbox || s.More != 54 || s.Counts.Done != webproto.MaxInbox+54 {
		t.Fatalf("%d items, more %d, counts %+v", len(s.Items), s.More, s.Counts)
	}
	for i := 1; i < len(s.Items); i++ {
		if s.Items[i].Since > s.Items[i-1].Since {
			t.Fatalf("not newest first at %d: %+v", i, s.Items[i-1:i+1])
		}
	}
	titles := []string{s.Items[0].Title, s.Items[1].Title, s.Items[2].Title, s.Items[3].Title}
	if titles[0] != "open ~/notes.md" || titles[1] != "cat …/keys.txt" || titles[2] != "run scripts/a.sh" {
		t.Errorf("titles %q", titles[:3])
	}
	if l := titles[3]; len(l) > webproto.MaxInboxTitle || !utf8.ValidString(l) || !strings.HasSuffix(l, "…") {
		t.Errorf("long title %d bytes %q", len(l), l)
	}
	for _, it := range s.Items {
		if strings.Contains(it.Title, home) || strings.Contains(it.Title, "/etc/") {
			t.Errorf("a path outside the workspace: %q", it.Title)
		}
		if it.Ref == "" || it.ID != it.Kind+":"+it.Ref {
			t.Errorf("id %q ref %q", it.ID, it.Ref)
		}
	}
}

// Spec (MOB-2 perf guard): building an inbox of 200 items (cards, task
// runs, CI) stays within web_inbox_build_200, median of 9.
func TestPerfInboxBuild200(t *testing.T) {
	perfbudget.Require(t)
	m := aiTestModel(t)
	now := webTestClock(t)
	webTestHub(t, &m)
	m.ws = workspace.Workspace{Root: t.TempDir()}
	for i := 0; i < webproto.MaxInbox; i++ {
		webTestTaskRun(&m, "task "+strconv.Itoa(i), now.Add(time.Duration(i)*time.Second), i%3 == 0, i%7)
	}
	m.ci = &ciWatch{branch: "main", loaded: true, snap: ghci.Snapshot{Runs: []ghci.Run{{Status: "in_progress"}}}}
	webTestToolCard(&m, ai.Tool{Name: "write_file"})
	var ds []time.Duration
	for i := 0; i < 9; i++ {
		start := time.Now()
		s := m.webInboxBuild()
		ds = append(ds, time.Since(start))
		if len(s.Items) != webproto.MaxInbox {
			t.Fatalf("%d items", len(s.Items))
		}
	}
	perfbudget.Check(t, "web_inbox_build_200", perfMs(perfMedian(ds)))
}
