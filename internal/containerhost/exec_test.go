package containerhost

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestExecRunsInTheContainerFolder(t *testing.T) {
	h, _, c := newHost(t, func(c *Config) { c.User = "1000:1000" })
	c.Mkdir(t, "/app/sub")
	ctx := context.Background()
	out, _, code, err := h.Exec().Capture(ctx, "", "pwd")
	if err != nil || code != 0 || out != "/app\n" {
		t.Fatalf("root pwd = %q %d %v", out, code, err)
	}
	out, _, _, _ = h.Exec().Capture(ctx, "sub", "pwd")
	if out != "/app/sub\n" {
		t.Fatalf("relative dir pwd = %q", out)
	}
	ex := c.Execs()
	if ex[len(ex)-1].User != "1000:1000" || ex[len(ex)-1].Dir != "/app/sub" {
		t.Fatalf("exec = %+v", ex[len(ex)-1])
	}
}

func TestExecPassesArgvUnchanged(t *testing.T) {
	h, _, c := newHost(t)
	c.AddTool(t, "tool")
	weird := []string{"tool", "$(touch x)", "a b", "-rf", "it's", "x\ny", "`id`"}
	out, _, code, err := h.Exec().Capture(context.Background(), "", weird...)
	if err != nil || code != 0 {
		t.Fatalf("code %d err %v", code, err)
	}
	if want := strings.Join(weird, " ") + "\n"; out != want {
		t.Fatalf("out = %q, want %q", out, want)
	}
	if got := c.Execs()[0].Argv; strings.Join(got, "|") != strings.Join(weird, "|") {
		t.Fatalf("argv = %q", got)
	}
}

func TestExecInputAndStream(t *testing.T) {
	h, _, _ := newHost(t)
	out, _, _, err := h.Exec().CaptureInput(context.Background(), "", []byte("from stdin"), "cat")
	if err != nil || out != "from stdin" {
		t.Fatalf("CaptureInput = %q, %v", out, err)
	}
	r, stop, err := h.Exec().Stream(context.Background(), "", "echo", "streamed")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	stop()
	if string(b) != "streamed\n" {
		t.Fatalf("Stream = %q", b)
	}
}

func TestExecMarksAStoppedContainerOffline(t *testing.T) {
	h, _, c := newHost(t)
	c.Stop()
	if _, _, code, _ := h.Exec().Capture(context.Background(), "", "echo"); code == 0 {
		t.Fatal("exec ran in a stopped container")
	}
	if h.Online() {
		t.Fatal("still online")
	}
}

func TestExecNoCommand(t *testing.T) {
	h, _, _ := newHost(t)
	if _, _, _, err := h.Exec().Capture(context.Background(), ""); err == nil {
		t.Fatal("empty argv accepted")
	}
}

func TestTermArgv(t *testing.T) {
	h, _, _ := newHost(t, func(c *Config) { c.Context = "prod"; c.User = "node" })
	got := strings.Join(h.TermArgv("web", []string{"npm", "test"}), " ")
	if want := "docker --context prod exec -it -w /app/web -u node api-1 npm test"; got != want {
		t.Fatalf("TermArgv = %q, want %q", got, want)
	}
	got = strings.Join(h.TermArgv("", nil), " ")
	if !strings.HasPrefix(got, "docker --context prod exec -it -w /app -u node api-1 sh -c ") {
		t.Fatalf("shell TermArgv = %q", got)
	}
}
