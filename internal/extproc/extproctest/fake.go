// Package extproctest is a fake Tier 2 extension for tests. A test binary
// re-runs itself (os.Args[0]) as the extension process: its TestMain calls
// RunIfHelper first, which takes over when the helper env var is set.
//
// The fake extension, on "initialize":
//
//   - registers command "hello" (title "Say Hello"), "crash", "ask",
//     "read" and "spawn", status item "pid" (text "fake pid <pid>") and
//     panel "main" (title "Fake", content "line one" / "line two");
//   - "hello" pushes a toast "Hello from fake";
//   - "crash" exits with status 3 at once;
//   - "ask" asks prompt{title "Name"} and toasts "Hello, <answer>";
//   - "read" asks readFile{path: params.args[0]} and toasts the text or
//     the error;
//   - "spawn" starts a child process (this binary in "sleep" mode) in the
//     extension's process group and toasts "child <pid>";
//   - event/notify toasts "event <name> <base name of path>";
//   - panel/select toasts "selected <row>".
//
// With FAKE_IGNORE_SHUTDOWN=1 it ignores shutdown/exit and stdin EOF (to
// test the hard kill).
package extproctest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/amin-jalali/termocode/internal/extproc"
)

// Env is the variable that turns a test binary into the fake extension.
const Env = "TERMOCODE_FAKE_EXT"

// Manifest returns an extension.json body that runs this test binary as
// the fake extension. extra is merged into the top level (for example
// "activationEvents").
func Manifest(extra map[string]any) string {
	m := map[string]any{
		"name":    "fake",
		"version": "1.0.0",
		"run": map[string]any{
			"command": os.Args[0],
			"args":    []string{"-test.run=^$"},
			// A -race binary sleeps 1 s at exit by default.
			"env": map[string]string{Env: "ext", "GORACE": "atexit_sleep_ms=0"},
		},
	}
	for k, v := range extra {
		m[k] = v
	}
	b, _ := json.MarshalIndent(m, "", "  ")
	return string(b)
}

// RunIfHelper runs the fake extension (and exits) when Env is set.
func RunIfHelper() {
	switch os.Getenv(Env) {
	case "ext":
		run()
		os.Exit(0)
	case "sleep":
		time.Sleep(time.Hour) // a child the extension left behind
		os.Exit(0)
	}
}

func run() {
	ignore := os.Getenv("FAKE_IGNORE_SHUTDOWN") == "1"
	quit := make(chan struct{})
	handle := func(c *extproc.Conn, m extproc.Message) {
		toast := func(msg string) {
			_ = c.Notify("notify", map[string]any{"message": msg})
		}
		var p struct {
			ID    string   `json:"id"`
			Args  []string `json:"args"`
			Event string   `json:"event"`
			Path  string   `json:"path"`
			Panel string   `json:"panel"`
			Row   int      `json:"row"`
		}
		_ = json.Unmarshal(m.Params, &p)
		switch m.Method {
		case "initialize":
			_ = c.Reply(m.ID, map[string]any{"name": "fake"}, nil)
			for _, id := range []string{"hello", "crash", "ask", "read", "spawn"} {
				title := id
				if id == "hello" {
					title = "Say Hello"
				}
				_ = c.Notify("registerCommand", map[string]any{"id": id, "title": title})
			}
			_ = c.Notify("registerStatusItem", map[string]any{"id": "pid", "text": fmt.Sprintf("fake pid %d", os.Getpid()), "command": "hello"})
			_ = c.Notify("registerPanel", map[string]any{"id": "main", "title": "Fake", "icon": "F"})
			_ = c.Notify("panel/setContent", map[string]any{"id": "main", "lines": []string{"{{accent}}line one{{/}}", "line two"}})
		case "command/execute":
			switch p.ID {
			case "fake.hello":
				toast("Hello from fake")
			case "fake.crash":
				os.Exit(3)
			case "fake.ask":
				go func() {
					r, err := c.Call(context.Background(), "prompt", map[string]any{"title": "Name", "label": "Your name:"})
					var name *string
					if err == nil {
						_ = json.Unmarshal(r, &name)
					}
					if name == nil {
						toast("asked: cancelled")
						return
					}
					toast("Hello, " + *name)
				}()
			case "fake.read":
				path := ""
				if len(p.Args) > 0 {
					path = p.Args[0]
				}
				go func() {
					r, err := c.Call(context.Background(), "readFile", map[string]any{"path": path})
					if err != nil {
						toast("read error: " + err.Error())
						return
					}
					var out struct{ Text string }
					_ = json.Unmarshal(r, &out)
					toast("read: " + out.Text)
				}()
			case "fake.spawn":
				child := exec.Command(os.Args[0], "-test.run=^$")
				child.Env = append(os.Environ(), Env+"=sleep")
				if err := child.Start(); err != nil {
					toast("spawn error: " + err.Error())
				} else {
					toast(fmt.Sprintf("child %d", child.Process.Pid))
				}
			}
			_ = c.Reply(m.ID, nil, nil)
		case "event/notify":
			toast("event " + p.Event + " " + filepath.Base(p.Path))
		case "panel/select":
			toast(fmt.Sprintf("selected %d", p.Row))
		case "shutdown":
			_ = c.Reply(m.ID, nil, nil)
		case "exit":
			if !ignore {
				close(quit)
			}
		default:
			if m.IsRequest() {
				_ = c.Reply(m.ID, nil, &extproc.Error{Code: extproc.CodeMethodNotFound, Message: m.Method})
			}
		}
	}
	c := extproc.NewConn(bufio.NewReader(os.Stdin), os.Stdout, handle)
	select {
	case <-quit:
	case <-c.Done():
		if ignore {
			select {} // stdin closed, but keep running until killed
		}
	}
}
