//go:build unix

package extproc_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/ext"
	"github.com/amin-jalali/termocode/internal/extproc"
	"github.com/amin-jalali/termocode/internal/extproc/extproctest"
)

func TestMain(m *testing.M) {
	extproctest.RunIfHelper()
	os.Exit(m.Run())
}

// waitLimit only stops a broken test from hanging; every wait below ends
// on a message or a state, never on the clock.
const waitLimit = 20 * time.Second

// fakeExt writes the fake extension into a temp extensions dir and
// discovers it.
func fakeExt(t *testing.T, extra map[string]any) ext.Extension {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fake", "extension.json"), []byte(extproctest.Manifest(extra)), 0o644); err != nil {
		t.Fatal(err)
	}
	exts, skipped := ext.Discover(dir)
	if len(exts) != 1 || !exts[0].IsProcess() {
		t.Fatalf("discover: %+v %v", exts, skipped)
	}
	return exts[0]
}

func newHost(t *testing.T, root string) *extproc.Host {
	t.Helper()
	if root == "" {
		root = t.TempDir()
	}
	h := extproc.NewHost(extproc.Config{
		Root:        root,
		Version:     "test",
		MaxRestarts: 2,
		Backoff:     func(int) time.Duration { return 0 },
	})
	t.Cleanup(h.Close)
	return h
}

// all waits until every match saw an event, in any order (the started
// event and the first registrations race), and returns them.
func all(t *testing.T, h *extproc.Host, what string, matches ...func(extproc.Event) bool) []extproc.Event {
	t.Helper()
	got := make([]extproc.Event, len(matches))
	ok := make([]bool, len(matches))
	left := len(matches)
	deadline := time.After(waitLimit)
	var seen []string
	for left > 0 {
		select {
		case ev := <-h.Events():
			seen = append(seen, describe(ev))
			for i, m := range matches {
				if !ok[i] && m(ev) {
					ok[i], got[i] = true, ev
					left--
					break
				}
			}
		case <-deadline:
			t.Fatalf("no %s; saw:\n  %s\nlog:\n%s", what, strings.Join(seen, "\n  "), strings.Join(h.Log(), "\n"))
		}
	}
	return got
}

func isStarted(ev extproc.Event) bool { return ev.Kind == extproc.EvStarted }

func isCommand(id string) func(extproc.Event) bool {
	return func(ev extproc.Event) bool {
		var p struct{ ID string }
		_ = json.Unmarshal(ev.Msg.Params, &p)
		return isMethod("registerCommand")(ev) && p.ID == id
	}
}

// next returns the first event that matches, failing on the wait limit.
func next(t *testing.T, h *extproc.Host, what string, match func(extproc.Event) bool) extproc.Event {
	t.Helper()
	deadline := time.After(waitLimit)
	var seen []string
	for {
		select {
		case ev := <-h.Events():
			if match(ev) {
				return ev
			}
			seen = append(seen, describe(ev))
		case <-deadline:
			t.Fatalf("no %s; saw:\n  %s\nlog:\n%s", what, strings.Join(seen, "\n  "), strings.Join(h.Log(), "\n"))
		}
	}
}

func describe(ev extproc.Event) string {
	return fmt.Sprintf("%s %v gen=%d %s %s err=%q", ev.Ext, ev.Kind, ev.Gen, ev.Msg.Method, ev.Msg.Params, ev.Err)
}

func isMethod(method string) func(extproc.Event) bool {
	return func(ev extproc.Event) bool { return ev.Kind == extproc.EvMessage && ev.Msg.Method == method }
}

func isToast(text string) func(extproc.Event) bool {
	return func(ev extproc.Event) bool {
		if ev.Kind != extproc.EvMessage || ev.Msg.Method != "notify" {
			return false
		}
		var p struct{ Message string }
		_ = json.Unmarshal(ev.Msg.Params, &p)
		return strings.HasPrefix(p.Message, text)
	}
}

func toastText(ev extproc.Event) string {
	var p struct{ Message string }
	_ = json.Unmarshal(ev.Msg.Params, &p)
	return p.Message
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), waitLimit)
	t.Cleanup(cancel)
	return c
}

func TestHostStartsRegistersAndRunsCommands(t *testing.T) {
	h := newHost(t, "")
	h.Set([]ext.Extension{fakeExt(t, nil)})
	h.StartAtStartup() // no activation events: starts now
	all(t, h, "start and registrations", isStarted, isCommand("hello"), isMethod("registerStatusItem"))
	if err := h.Execute(ctx(t), "fake", "fake.hello", nil); err != nil {
		t.Fatal(err)
	}
	next(t, h, "hello toast", isToast("Hello from fake"))
	st := h.Status()
	if len(st) != 1 || st[0].State != extproc.StateRunning || st[0].Pid == 0 {
		t.Errorf("status = %+v", st)
	}
}

func TestHostLazyStart(t *testing.T) {
	h := newHost(t, "")
	h.Set([]ext.Extension{fakeExt(t, map[string]any{
		"contributes": map[string]any{"commands": []map[string]string{{"id": "hello", "title": "Say Hello"}}},
	})})
	h.StartAtStartup()
	if st := h.Status(); len(st) != 1 || st[0].State != extproc.StateIdle {
		t.Fatalf("status before first use = %+v", st)
	}
	if err := h.Execute(ctx(t), "fake", "fake.hello", nil); err != nil {
		t.Fatal(err)
	}
	next(t, h, "hello toast", isToast("Hello from fake"))
}

func TestHostEventActivation(t *testing.T) {
	h := newHost(t, "")
	h.Set([]ext.Extension{fakeExt(t, map[string]any{"activationEvents": []string{"onSave"}})})
	h.StartAtStartup()
	h.Event("open", "/w/a.txt") // not an activation event: dropped
	if st := h.Status(); st[0].State != extproc.StateIdle {
		t.Fatalf("an open event started the extension: %+v", st)
	}
	h.Event("save", "/w/b.txt")
	next(t, h, "save event toast", isToast("event save b.txt"))
}

func TestHostRestartsAfterCrashThenGivesUp(t *testing.T) {
	h := newHost(t, "")
	h.Set([]ext.Extension{fakeExt(t, nil)})
	h.StartAtStartup()
	first := all(t, h, "start", isStarted, isCommand("crash"))[0]
	for i := 1; i <= 2; i++ {
		_ = h.Execute(ctx(t), "fake", "fake.crash", nil) // the process dies before it answers
		ex := next(t, h, "exit", func(ev extproc.Event) bool { return ev.Kind == extproc.EvExited })
		if !ex.Restarting || ex.Err == "" {
			t.Fatalf("crash %d: exit event %+v, want restarting with an error", i, ex)
		}
		got := all(t, h, "restart and re-registration", isStarted, isCommand("crash"))
		if st := got[0]; st.Gen <= first.Gen || got[1].Gen != st.Gen {
			t.Errorf("restart %d gen = %d (registration gen %d), want > %d", i, st.Gen, got[1].Gen, first.Gen)
		}
	}
	_ = h.Execute(ctx(t), "fake", "fake.crash", nil)
	next(t, h, "failed", func(ev extproc.Event) bool { return ev.Kind == extproc.EvFailed })
	if st := h.Status(); st[0].State != extproc.StateFailed {
		t.Errorf("status after max restarts = %+v", st)
	}
	if err := h.Execute(ctx(t), "fake", "fake.hello", nil); err == nil {
		t.Error("a failed extension ran a command")
	}
}

func TestHostPromptRoundTrip(t *testing.T) {
	h := newHost(t, "")
	h.Set([]ext.Extension{fakeExt(t, nil)})
	h.StartAtStartup()
	if err := h.Execute(ctx(t), "fake", "fake.ask", nil); err != nil {
		t.Fatal(err)
	}
	p := next(t, h, "prompt request", isMethod("prompt"))
	if !p.Msg.IsRequest() {
		t.Fatalf("prompt is not a request: %+v", p.Msg)
	}
	h.Reply(p.Ext, p.Gen, p.Msg.ID, "Bob", nil)
	next(t, h, "answer toast", isToast("Hello, Bob"))
}

func TestHostReadFileOnlyInWorkspace(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "in.txt"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHost(t, root)
	h.Set([]ext.Extension{fakeExt(t, nil)})
	h.StartAtStartup()
	if err := h.Execute(ctx(t), "fake", "fake.read", []string{"in.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := toastText(next(t, h, "read toast", isToast("read"))); got != "read: inside" {
		t.Errorf("read in.txt = %q", got)
	}
	for _, p := range []string{outside, "../" + filepath.Base(filepath.Dir(outside)) + "/secret.txt"} {
		if err := h.Execute(ctx(t), "fake", "fake.read", []string{p}); err != nil {
			t.Fatal(err)
		}
		got := toastText(next(t, h, "read toast", isToast("read")))
		if !strings.HasPrefix(got, "read error:") || !strings.Contains(got, "outside the workspace") {
			t.Errorf("read %s = %q, want an outside-the-workspace error", p, got)
		}
	}
}

func TestInWorkspace(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if err := os.Symlink(other, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for p, ok := range map[string]bool{
		"a":                     true,
		"a/new.txt":             true, // need not exist
		root:                    true,
		filepath.Join(sub, "x"): true,
		"../x":                  false,
		"a/../../x":             false,
		other:                   false,
		"link/x":                false, // a symlink out of the workspace
	} {
		_, err := extproc.InWorkspace(root, p)
		if (err == nil) != ok {
			t.Errorf("InWorkspace(%q) err = %v, want ok=%v", p, err, ok)
		}
	}
}

// Stopping kills the extension and everything in its process group, and
// an extension that ignores shutdown is killed after the grace time.
func TestHostStopLeavesNoProcess(t *testing.T) {
	for _, ignore := range []bool{false, true} {
		t.Run(fmt.Sprintf("ignore=%v", ignore), func(t *testing.T) {
			if ignore {
				t.Setenv("FAKE_IGNORE_SHUTDOWN", "1")
			}
			h := extproc.NewHost(extproc.Config{Root: t.TempDir(), StopTimeout: 500 * time.Millisecond})
			defer h.Close()
			h.Set([]ext.Extension{fakeExt(t, nil)})
			h.StartAtStartup()
			all(t, h, "started", isStarted)
			if err := h.Execute(ctx(t), "fake", "fake.spawn", nil); err != nil {
				t.Fatal(err)
			}
			child, err := strconv.Atoi(strings.TrimPrefix(toastText(next(t, h, "child toast", isToast("child "))), "child "))
			if err != nil {
				t.Fatal(err)
			}
			pid := h.Status()[0].Pid
			h.StopAll()
			if left := waitGone([]int{pid, child}); len(left) > 0 {
				t.Fatalf("still running after StopAll: %v", left)
			}
			if st := h.Status(); st[0].State != extproc.StateStopped {
				t.Errorf("status after stop = %+v", st)
			}
		})
	}
}

func TestExecuteUnknownExtension(t *testing.T) {
	h := newHost(t, "")
	if err := h.Execute(ctx(t), "nope", "nope.x", nil); err == nil || !errors.Is(err, extproc.ErrUnknown) {
		t.Errorf("err = %v, want ErrUnknown", err)
	}
}

// waitGone polls until no pid runs (a zombie counts as gone).
func waitGone(pids []int) []int {
	deadline := time.Now().Add(waitLimit)
	for {
		var left []int
		for _, p := range pids {
			if alive(p) {
				left = append(left, p)
			}
		}
		if len(left) == 0 || time.Now().After(deadline) {
			return left
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func alive(pid int) bool {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	return err == nil && len(strings.TrimSpace(string(out))) > 0 && !strings.HasPrefix(strings.TrimSpace(string(out)), "Z")
}
