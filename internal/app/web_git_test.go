package app

import (
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// webTestGitState gives the Model a git state as the laptop's refresh
// would: branch main ahead 1, one staged, one changed, one new file, one
// conflict, two commits.
func webTestGitState(m *Model) {
	m.applyGitSnapshot(GitMsg{IsRepo: true, Branch: git.Branch{Name: "main", Ahead: 1},
		Files: []git.FileStatus{{Code: "M ", Path: "a.go"}, {Code: " M", Path: "b.go"}, {Code: "??", Path: "new.txt"}, {Code: "UU", Path: "c.go"}},
		Graph: []git.GraphCommit{
			{Hash: "1a2b3c4", Subject: "second", Author: "Ann", Committed: 1_700_000_100, Unpushed: true},
			{Hash: "0f0f0f0", Subject: "first", Author: "Bob", Committed: 1_700_000_000},
		}})
}

// webTestGitSnap decodes the git snap or delta among frames (the last).
func webTestGitSnap(t *testing.T, frames []map[string]any) (webproto.GitSnap, int) {
	t.Helper()
	var s webproto.GitSnap
	n := 0
	for _, f := range frames {
		if f["topic"] == "git" {
			webTestData(t, f, &s)
			n++
		}
	}
	return s, n
}

// Spec (WEB-23): the git snapshot is the laptop's Source Control state:
// the branch with ahead / behind, every changed file with git's two
// status columns and what they mean, the last commits; a conflict is
// marked and a stopped merge named. Nothing runs git for it.
func TestGitSnapshot(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	webTestGitState(&m)
	c := webTestSub(t, &m, "git")
	s, n := webTestGitSnap(t, webTestNow(t, c))
	if n != 1 || !s.Repo || s.Loading || s.Branch != (webproto.GitBranch{Name: "main", Ahead: 1}) {
		t.Fatalf("snap %+v (frames %d)", s, n)
	}
	want := []webproto.GitFile{
		{Path: "a.go", X: "M", Y: " ", Staged: true},
		{Path: "b.go", X: " ", Y: "M", Unstaged: true},
		{Path: "new.txt", X: "?", Y: "?", Untracked: true},
		{Path: "c.go", X: "U", Y: "U", Staged: true, Unstaged: true, Conflict: true},
	}
	if len(s.Files) != len(want) {
		t.Fatalf("files %+v", s.Files)
	}
	for i := range want {
		if s.Files[i] != want[i] {
			t.Errorf("file %d = %+v, want %+v", i, s.Files[i], want[i])
		}
	}
	if s.Op != "merge" {
		t.Errorf("op %q, want merge (a conflict)", s.Op)
	}
	if len(s.Log) != 2 || s.Log[0] != (webproto.GitCommit{Hash: "1a2b3c4", Subject: "second", Author: "Ann", When: 1_700_000_100_000, Unpushed: true}) {
		t.Errorf("log %+v", s.Log)
	}

	// Not loaded yet: say so; outside a repository: repo false.
	m2 := aiTestModel(t)
	webTestHub(t, &m2)
	s, _ = webTestGitSnap(t, webTestNow(t, webTestSub(t, &m2, "git")))
	if !s.Loading || s.Repo {
		t.Errorf("before the first refresh: %+v", s)
	}
	m2.applyGitSnapshot(GitMsg{IsRepo: false})
	m2.gitRebase = git.RebaseState{}
	s, _ = webTestGitSnap(t, webTestNow(t, webTestSub(t, &m2, "git")))
	if s.Loading || s.Repo || s.Files == nil || s.Log == nil {
		t.Errorf("not a repo: %+v", s)
	}
}

// Spec (WEB-23): the file list is capped at MaxGitFiles; More counts the
// rest; the log at MaxGitLog.
func TestGitSnapshotCaps(t *testing.T) {
	m := aiTestModel(t)
	webTestHub(t, &m)
	files := make([]git.FileStatus, webproto.MaxGitFiles+5)
	for i := range files {
		files[i] = git.FileStatus{Code: " M", Path: "f" + string(rune('a'+i%26))}
	}
	graph := make([]git.GraphCommit, webproto.MaxGitLog+10)
	m.applyGitSnapshot(GitMsg{IsRepo: true, Files: files, Graph: graph})
	s, _ := webTestGitSnap(t, webTestNow(t, webTestSub(t, &m, "git")))
	if len(s.Files) != webproto.MaxGitFiles || s.More != 5 || len(s.Log) != webproto.MaxGitLog {
		t.Fatalf("files %d more %d log %d", len(s.Files), s.More, len(s.Log))
	}
}

// Spec (WEB-23): the topic follows the laptop's own git refresh: an
// Update that brings no new git state sends nothing, however much time
// passes; a refresh that changed the list sends the whole snapshot; a
// refresh with the same content sends nothing. Deltas go at most one a
// second; a change inside that second waits for a one-shot tick.
func TestGitRefreshFollowsTUINotTimer(t *testing.T) {
	m := aiTestModel(t)
	now := webTestClock(t)
	webTestHub(t, &m)
	webTestGitState(&m)
	c := webTestSub(t, &m, "git")
	webTestNow(t, c)
	for i := 0; i < 5; i++ {
		*now = now.Add(10 * time.Second)
		if cmd := m.webAfterUpdate(nil); cmd != nil {
			t.Fatal("a tick was asked for though nothing changed")
		}
	}
	if _, n := webTestGitSnap(t, webTestNow(t, c)); n != 0 {
		t.Fatalf("%d git frames without a refresh", n)
	}
	// The same content again (a poll that found nothing new).
	webTestGitState(&m)
	m.webAfterUpdate(nil)
	if _, n := webTestGitSnap(t, webTestNow(t, c)); n != 0 {
		t.Fatalf("%d git frames for an unchanged refresh", n)
	}
	m.applyGitSnapshot(GitMsg{IsRepo: true, Branch: git.Branch{Name: "main"}, Files: []git.FileStatus{{Code: "A ", Path: "a.go"}}, KeepGraph: true})
	m.webAfterUpdate(nil)
	s, n := webTestGitSnap(t, webTestNow(t, c))
	if n != 1 || len(s.Files) != 1 || s.Files[0].X != "A" || len(s.Log) != 2 {
		t.Fatalf("after a refresh: %d frames, %+v", n, s)
	}
	// Inside the second: held back, then sent by the tick.
	*now = now.Add(300 * time.Millisecond)
	m.applyGitSnapshot(GitMsg{IsRepo: true, Branch: git.Branch{Name: "main"}, KeepGraph: true})
	cmd := m.webAfterUpdate(nil)
	if cmd == nil {
		t.Fatal("no tick for a change inside the second")
	}
	if _, n := webTestGitSnap(t, webTestNow(t, c)); n != 0 {
		t.Fatal("sent inside the second")
	}
	*now = now.Add(time.Second)
	m.webAfterUpdate(webGitTickMsg{})
	if s, n := webTestGitSnap(t, webTestNow(t, c)); n != 1 || len(s.Files) != 0 {
		t.Fatalf("after the tick: %d frames, %+v", n, s)
	}
}

// Spec (WEB-23): the git replay golden: a stage, a commit (HEAD moves)
// and a branch switch replay to the final snapshot.
func TestGitReplayGolden(t *testing.T) {
	m := aiTestModel(t)
	now := webTestClock(t)
	h := webTestHub(t, &m)
	webTestGitState(&m)
	c := webReplayRecord(t, &m, h, "git", func() {
		*now = now.Add(2 * time.Second)
		m.applyGitSnapshot(GitMsg{IsRepo: true, Branch: git.Branch{Name: "main", Ahead: 1},
			Files: []git.FileStatus{{Code: "M ", Path: "a.go"}, {Code: "M ", Path: "b.go"}, {Code: "??", Path: "new.txt"}}, KeepGraph: true})
		m.webAfterUpdate(nil)
		*now = now.Add(2 * time.Second)
		m.applyGitSnapshot(GitMsg{IsRepo: true, Branch: git.Branch{Name: "main", Ahead: 2},
			Files: []git.FileStatus{{Code: "??", Path: "new.txt"}},
			Graph: []git.GraphCommit{{Hash: "2b2b2b2", Subject: "third", Author: "Ann", Committed: 1_700_000_200, Unpushed: true}}})
		m.webAfterUpdate(nil)
		*now = now.Add(2 * time.Second)
		m.applyGitSnapshot(GitMsg{IsRepo: true, Branch: git.Branch{Name: "feat/web"}, KeepGraph: true})
		m.webAfterUpdate(nil)
	})
	if len(c.Deltas) != 1 { // queued whole snapshots: the newest replaces the older
		t.Fatalf("deltas %d, want 1", len(c.Deltas))
	}
	webReplayCheck(t, h.topics["git"], c)
	webReplayGolden(t, webReplayPath("git", "stage_commit_switch"), c)
}

func gitFileConflict(path string) git.FileStatus { return git.FileStatus{Code: "UU", Path: path} }
