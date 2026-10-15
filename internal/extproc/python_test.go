//go:build unix

package extproc_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/extensions"
	"github.com/amin-jalali/termocode/internal/ext"
	"github.com/amin-jalali/termocode/internal/extproc"
)

// The shipped Python sample and the "New Process Extension" template run
// for real when python3 is installed.

func needPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}
}

func discoverOne(t *testing.T, dir, name string) ext.Extension {
	t.Helper()
	exts, _ := ext.Discover(dir)
	for _, x := range exts {
		if x.Name == name {
			return x
		}
	}
	t.Fatalf("%s not found in %s", name, dir)
	return ext.Extension{}
}

func TestWordFrequencySample(t *testing.T) {
	needPython(t)
	dir := t.TempDir()
	if _, err := ext.InstallBundled(extensions.FS, dir); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	doc := filepath.Join(root, "doc.txt")
	if err := os.WriteFile(doc, []byte("apple banana apple cherry apple banana {{red}}"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHost(t, root)
	h.Set([]ext.Extension{discoverOne(t, dir, "word-frequency")})
	h.StartAtStartup()
	all(t, h, "start", isStarted, isMethod("registerPanel"), isCommand("refresh"))
	h.Event("open", doc)
	ev := next(t, h, "panel content", func(ev extproc.Event) bool {
		return isMethod("panel/setContent")(ev) && strings.Contains(string(ev.Msg.Params), "apple")
	})
	var p struct{ Lines []string }
	_ = json.Unmarshal(ev.Msg.Params, &p)
	text := strings.Join(p.Lines, "\n")
	if !strings.Contains(text, "doc.txt") || !strings.Contains(text, "{{number}}3{{/}}") {
		t.Errorf("panel lines:\n%s", text)
	}
	if strings.Contains(text, "apple") && strings.Index(text, "apple") > strings.Index(text, "banana") {
		t.Errorf("most used word is not first:\n%s", text)
	}
	st := next(t, h, "status update", isMethod("statusItem/update"))
	if !strings.Contains(string(st.Msg.Params), "7 words") {
		t.Errorf("status = %s", st.Msg.Params)
	}
	// Outside the workspace: readFile is refused, nothing is shown.
	outside := filepath.Join(t.TempDir(), "x.txt")
	_ = os.WriteFile(outside, []byte("secret secret secret"), 0o644)
	h.Event("open", outside)
	if err := h.Execute(ctx(t), "word-frequency", "word-frequency.refresh", nil); err != nil {
		t.Fatal(err)
	}
	// The read answer comes after Execute returns: wait for the log line.
	deadline := time.Now().Add(waitLimit)
	for time.Now().Before(deadline) {
		for _, line := range h.Log() {
			if strings.Contains(line, "cannot read") && strings.Contains(line, "outside the workspace") {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("no refused read in the log:\n%s", strings.Join(h.Log(), "\n"))
}

func TestPythonTemplate(t *testing.T) {
	needPython(t)
	dir := t.TempDir()
	if _, err := ext.ScaffoldProcess(dir, "mine", extensions.FS); err != nil {
		t.Fatal(err)
	}
	h := newHost(t, t.TempDir())
	h.Set([]ext.Extension{discoverOne(t, dir, "mine")})
	h.StartAtStartup()
	all(t, h, "start", isStarted, isCommand("hello"), isMethod("registerStatusItem"), isMethod("registerPanel"))
	if err := h.Execute(ctx(t), "mine", "mine.hello", nil); err != nil {
		t.Fatal(err)
	}
	p := next(t, h, "prompt", isMethod("prompt"))
	h.Reply(p.Ext, p.Gen, p.Msg.ID, "Ann", nil)
	next(t, h, "toast", isToast("Hello, Ann!"))
	h.Event("save", "/w/a.go")
	next(t, h, "status update", func(ev extproc.Event) bool {
		return isMethod("statusItem/update")(ev) && strings.Contains(string(ev.Msg.Params), "1 saves")
	})
}
