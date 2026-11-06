package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is the fake command: the test binary run again with
// WFX_SOURCE_HELPER=1 and a mode after "--".
func TestHelperProcess(t *testing.T) {
	if os.Getenv("WFX_SOURCE_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) < 2 {
		os.Exit(2)
	}
	switch args[1] {
	case "json":
		fmt.Print(`{"findings":[{"id":"GO-1"},{"id":"GO-2"}]}`)
	case "jsonl":
		fmt.Println(`{"config":{}}`)
		fmt.Println(`{"finding":{"osv":"GO-1"}}`)
		fmt.Println()
		fmt.Println(`{"finding":{"osv":"GO-2"}}`)
	case "lines":
		fmt.Print("a.go\r\n\nb.go\n")
	case "pwd":
		wd, _ := os.Getwd()
		b, _ := jsonString(wd)
		fmt.Print(b)
	case "args":
		b, _ := jsonStrings(args[2:])
		fmt.Print(b)
	case "fail":
		fmt.Fprint(os.Stderr, "boom: bad flag")
		os.Exit(3)
	case "sleep":
		time.Sleep(10 * time.Second)
	case "badjson":
		fmt.Print("{nope")
	}
	os.Exit(0)
}

func helperArgv(mode string, extra ...string) []string {
	return append([]string{os.Args[0], "-test.run=TestHelperProcess", "--", mode}, extra...)
}

func newShellSources(t *testing.T) *Sources {
	t.Setenv("WFX_SOURCE_HELPER", "1")
	t.Setenv("GORACE", "atexit_sleep_ms=0") // a -race child sleeps 1 s at exit otherwise
	return &Sources{Workspace: t.TempDir()}
}

func TestShellParseModes(t *testing.T) {
	s := newShellSources(t)
	ctx := context.Background()
	r, err := s.Shell(ctx, ShellInput{Argv: helperArgv("json"), Shape: Shape{Items: "$.findings", Key: "$.id"}})
	if err != nil || len(r.Items) != 2 || r.Items[1].Key != "GO-2" {
		t.Fatalf("json: %+v %v", r, err)
	}
	r, err = s.Shell(ctx, ShellInput{Argv: helperArgv("jsonl"), Parse: ParseJSONL, Shape: Shape{Items: "$.finding", Key: "$.osv"}})
	if err != nil || len(r.Items) != 2 || r.Items[0].Key != "GO-1" || string(r.Items[0].Data) != `{"osv":"GO-1"}` {
		t.Fatalf("jsonl: %+v %v", r, err)
	}
	r, err = s.Shell(ctx, ShellInput{Argv: helperArgv("lines"), Parse: ParseLines})
	if err != nil || len(r.Items) != 2 || string(r.Items[0].Data) != `"a.go"` || string(r.Items[1].Data) != `"b.go"` {
		t.Fatalf("lines: %+v %v", r, err)
	}
	if _, err := s.Shell(ctx, ShellInput{Argv: helperArgv("badjson")}); err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("bad json: %v", err)
	}
}

func TestShellArgvIsNotAShellString(t *testing.T) {
	s := newShellSources(t)
	odd := []string{"$(touch pwned)", "a b; rm -rf x", "`id`", "*"}
	r, err := s.Shell(context.Background(), ShellInput{Argv: helperArgv("args", odd...), Shape: Shape{MaxItems: 10}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Items) != len(odd) {
		t.Fatalf("args: %+v", r)
	}
	for i, a := range odd {
		want, _ := jsonString(a)
		if string(r.Items[i].Data) != want {
			t.Errorf("arg %d: got %s, want %s", i, r.Items[i].Data, want)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Workspace, "pwned")); err == nil {
		t.Error("a shell ran the argv")
	}
}

func TestShellCwdInsideWorkspace(t *testing.T) {
	s := newShellSources(t)
	sub := filepath.Join(s.Workspace, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := s.Shell(context.Background(), ShellInput{Argv: helperArgv("pwd"), Cwd: "sub"})
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(sub)
	if want, _ := jsonString(real); string(r.Items[0].Data) != want {
		t.Errorf("cwd: %s, want %s", r.Items[0].Data, want)
	}
	out := t.TempDir()
	if err := os.Symlink(out, filepath.Join(s.Workspace, "link")); err != nil {
		t.Fatal(err)
	}
	for _, cwd := range []string{"..", "../x", "sub/../..", out, "link", "missing"} {
		_, err := s.Shell(context.Background(), ShellInput{Argv: helperArgv("pwd"), Cwd: cwd})
		if err == nil {
			t.Errorf("cwd %q: want an error", cwd)
		} else if cwd != "missing" && !errors.Is(err, ErrOutside) {
			t.Errorf("cwd %q: want ErrOutside, got %v", cwd, err)
		}
	}
}

func TestShellErrors(t *testing.T) {
	s := newShellSources(t)
	ctx := context.Background()
	_, err := s.Shell(ctx, ShellInput{Argv: helperArgv("fail")})
	if err == nil || !strings.Contains(err.Error(), "exit status 3") || !strings.Contains(err.Error(), "boom: bad flag") {
		t.Errorf("fail: %v", err)
	}
	start := time.Now()
	_, err = s.Shell(ctx, ShellInput{Argv: helperArgv("sleep"), Timeout: 200 * time.Millisecond})
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("sleep: want ErrTimeout, got %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("timeout took %s", d)
	}
	for _, in := range []ShellInput{
		{},
		{Argv: []string{""}},
		{Argv: []string{"echo", "${secret:TOKEN}"}},
		{Argv: []string{"echo", "${env:HOME}"}},
		{Argv: []string{"echo"}, Parse: "yaml"},
		{Argv: []string{"echo"}, Parse: ParseLines, Shape: Shape{Items: "$.a"}},
		{Argv: []string{"echo"}, Timeout: time.Hour},
		{Argv: []string{"echo"}, Shape: Shape{MaxItems: HardMaxItems + 1}},
	} {
		if _, err := s.Shell(ctx, in); err == nil {
			t.Errorf("%+v: want an error", in)
		}
	}
	if _, err := (&Sources{}).Shell(ctx, ShellInput{Argv: []string{"echo"}}); err == nil {
		t.Error("no workspace: want an error")
	}
}

func TestShellOutputCaps(t *testing.T) {
	c := &capped{max: 4}
	c.Write([]byte("abc"))
	c.Write([]byte("def"))
	if !c.over || c.String() != "abcd" {
		t.Errorf("capped: %q over=%v", c.String(), c.over)
	}
	tl := &tail{max: 3}
	tl.Write([]byte("abcdef"))
	if tl.String() != "def" {
		t.Errorf("tail: %q", tl.String())
	}
}
