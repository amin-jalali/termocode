package selfwatch

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeFile is the program file as the watcher sees it: what stat says,
// the build id in it and what `--version` prints (or that it fails).
type fakeFile struct {
	st      FileState
	statErr error
	id      string
	version string
	runErr  error
	probes  int
	ids     int
}

func (f *fakeFile) deps() Deps {
	return Deps{
		Stat: func(string) (FileState, error) { return f.st, f.statErr },
		BuildID: func(string) (string, error) {
			f.ids++
			if f.id == "" {
				return "", errors.New("no build id")
			}
			return f.id, nil
		},
		Probe: func(string) (string, error) {
			f.probes++
			return f.version, f.runErr
		},
	}
}

// replace puts a new file in place: a new inode, size and time.
func (f *fakeFile) replace(id, version string) {
	f.st = FileState{Dev: 1, Ino: f.st.Ino + 1, Size: f.st.Size + 10, Mod: f.st.Mod.Add(time.Second), Mode: 0o755}
	f.id, f.version, f.runErr = id, version, nil
}

func newFake() *fakeFile {
	return &fakeFile{st: FileState{Dev: 1, Ino: 10, Size: 100, Mod: time.Unix(1000, 0), Mode: 0o755}, id: "old", version: "termocode v0.0.0-old+dirty"}
}

// pollN polls n times and returns the last answer.
func pollN(w *Watcher, n int) (Build, bool) {
	var b Build
	var ok bool
	for range n {
		b, ok = w.Poll()
	}
	return b, ok
}

// Spec: the file it was started from, replaced by another build that
// runs `--version`, is a new build once it stopped changing for one poll.
func TestPollReportsANewBuild(t *testing.T) {
	f := newFake()
	w := New("/bin/termocode", "old", f.deps())
	if _, ok := pollN(w, 3); ok {
		t.Fatal("the running build itself was reported as new")
	}
	f.replace("new", "termocode v0.0.0-new+dirty")
	if _, ok := w.Poll(); ok {
		t.Fatal("a file that just changed was reported before it settled")
	}
	b, ok := w.Poll()
	if !ok || b.ID != "new" || b.Version != "v0.0.0-new+dirty" {
		t.Fatalf("Poll = %+v, %v; want the new build", b, ok)
	}
	if b2, ok := w.Poll(); !ok || b2 != b {
		t.Fatalf("the ready build was lost on the next poll: %+v %v", b2, ok)
	}
	if f.probes != 1 {
		t.Errorf("--version ran %d times, want once", f.probes)
	}
}

// Spec: a file that is still being written (it changes from one poll to
// the next) is never run or reported.
func TestPollWaitsForAPartialWrite(t *testing.T) {
	f := newFake()
	w := New("/bin/termocode", "old", f.deps())
	pollN(w, 2)
	f.replace("new", "termocode new")
	for i := range 5 {
		f.st.Size += 4096 // the build writes on
		f.st.Mod = f.st.Mod.Add(time.Millisecond)
		if _, ok := w.Poll(); ok {
			t.Fatalf("poll %d: a file still growing was reported", i)
		}
	}
	if f.probes != 0 {
		t.Fatalf("--version ran %d times on a growing file", f.probes)
	}
	if _, ok := pollN(w, 2); !ok {
		t.Fatal("the finished file was not reported")
	}
}

// Spec: a broken binary (its --version fails) is ignored, and is not run
// again on every poll; the next good build is reported.
func TestPollIgnoresABrokenBinary(t *testing.T) {
	f := newFake()
	w := New("/bin/termocode", "old", f.deps())
	pollN(w, 2)
	f.replace("broken", "")
	f.runErr = errors.New("exec format error")
	if _, ok := pollN(w, 5); ok {
		t.Fatal("a binary whose --version fails was reported")
	}
	if f.probes != 1 {
		t.Errorf("the broken binary ran %d times, want once", f.probes)
	}
	f.replace("good", "termocode good")
	if b, ok := pollN(w, 2); !ok || b.ID != "good" {
		t.Fatalf("Poll = %+v, %v; want the good build", b, ok)
	}
}

// Spec: --version must print termocode's own line; anything else is not
// a termocode build.
func TestPollIgnoresAnotherProgram(t *testing.T) {
	f := newFake()
	w := New("/bin/termocode", "old", f.deps())
	pollN(w, 2)
	f.replace("other", "usage: something else")
	if _, ok := pollN(w, 3); ok {
		t.Fatal("a program that is not termocode was reported")
	}
}

// Spec: a file without the execute bit, or with no build id (cut short),
// is not run.
func TestPollIgnoresAFileThatCannotRun(t *testing.T) {
	f := newFake()
	w := New("/bin/termocode", "old", f.deps())
	pollN(w, 2)
	f.replace("new", "termocode new")
	f.st.Mode = 0o644
	if _, ok := pollN(w, 3); ok {
		t.Fatal("a file without the execute bit was reported")
	}
	f.replace("", "termocode new")
	if _, ok := pollN(w, 3); ok {
		t.Fatal("a file without a build id was reported")
	}
	if f.probes != 0 {
		t.Errorf("--version ran %d times", f.probes)
	}
}

// Spec: the build id decides, not the time: the same build copied again
// (new mtime and inode) is no new build, and its --version never runs.
func TestPollIgnoresTheSameBuild(t *testing.T) {
	f := newFake()
	w := New("/bin/termocode", "old", f.deps())
	pollN(w, 2)
	f.replace("old", "termocode old")
	if _, ok := pollN(w, 3); ok {
		t.Fatal("the same build again was reported")
	}
	if f.probes != 0 {
		t.Errorf("--version ran %d times for the same build", f.probes)
	}
}

// Spec: while the file is gone (mid-replace) nothing is reported; a
// build that comes back is.
func TestPollWhileTheFileIsGone(t *testing.T) {
	f := newFake()
	w := New("/bin/termocode", "old", f.deps())
	pollN(w, 2)
	f.statErr = os.ErrNotExist
	if _, ok := pollN(w, 3); ok {
		t.Fatal("a missing file was reported")
	}
	f.statErr = nil
	f.replace("new", "termocode new")
	if _, ok := pollN(w, 2); !ok {
		t.Fatal("the file that came back was not reported")
	}
}

// Spec: started after the file was replaced already (the running image
// is not the file any more), the file on disk is the new build.
func TestPollReplacedBeforeTheStart(t *testing.T) {
	f := newFake()
	f.id, f.version = "new", "termocode new"
	w := New("/bin/termocode", "old", f.deps())
	if _, ok := pollN(w, 2); !ok {
		t.Fatal("a file replaced before the watcher started was not reported")
	}
}

// Spec: Linux names a replaced program file "<path> (deleted)"; the
// watcher watches the path itself.
func TestCleanPath(t *testing.T) {
	for in, want := range map[string]string{
		"/usr/bin/termocode (deleted)": "/usr/bin/termocode",
		"/usr/bin/termocode":           "/usr/bin/termocode",
		"":                             "",
	} {
		if got := CleanPath(in); got != want {
			t.Errorf("CleanPath(%q) = %q, want %q", in, got, want)
		}
	}
}

// Spec: the build id of a real Go program is read from the file and is
// the one `go tool buildid` prints; a file that is no program has none.
func TestReadBuildID(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	id, err := ReadBuildID(exe)
	if err != nil || id == "" {
		t.Fatalf("ReadBuildID(test binary) = %q, %v", id, err)
	}
	if out, err := exec.Command("go", "tool", "buildid", exe).Output(); err == nil {
		if want := strings.TrimSpace(string(out)); want != id {
			t.Errorf("ReadBuildID = %q, go tool buildid = %q", id, want)
		}
	}
	txt := filepath.Join(t.TempDir(), "x")
	if err := os.WriteFile(txt, []byte("#!/bin/sh\necho hi\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if id, err := ReadBuildID(txt); err == nil {
		t.Errorf("ReadBuildID(shell script) = %q, want an error", id)
	}
}

// Spec: Probe runs `<file> --version` and returns what follows
// "termocode "; a program that fails or hangs is no build.
func TestProbe(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good")
	if err := os.WriteFile(good, []byte("#!/bin/sh\n[ \"$1\" = --version ] && echo 'termocode v1.2.3+dirty'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if v, err := Probe(good); err != nil || v != "termocode v1.2.3+dirty" {
		t.Errorf("Probe(good) = %q, %v", v, err)
	}
	bad := filepath.Join(dir, "bad")
	if err := os.WriteFile(bad, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Probe(bad); err == nil {
		t.Error("Probe(a failing program) = nil error")
	}
	old := probeTimeout
	probeTimeout = 200 * time.Millisecond
	defer func() { probeTimeout = old }()
	hang := filepath.Join(dir, "hang")
	if err := os.WriteFile(hang, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := Probe(hang); err == nil {
		t.Error("Probe(a hanging program) = nil error")
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("Probe waited %v for a hanging program", time.Since(start))
	}
}
