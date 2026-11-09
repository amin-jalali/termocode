package app

import (
	"context"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// contexts_manage: the hosts (servers) of the Containers view as the GUI
// manages them. add (+ Add Server…), make default and remove ask on a
// card every time (no "Always allow"), as the GUI's cards; use (this
// session) asks with the containers class, as the page's confirm; inspect
// and terminal (Open Terminal, which the GUI opens at once) do not ask.

func aiContextsManageTool(t *testing.T) (*Model, ai.Tool, func() []string) {
	t.Helper()
	m, f, call := aiContainersTestModel(t)
	tl := aiContainersTool(t, m.aiContainersTools(call), "contexts_manage")
	acts := func() []string {
		var out []string
		for _, c := range f.Calls() {
			if !strings.HasPrefix(c, "context ls") && !strings.HasPrefix(c, "version") && !strings.HasPrefix(c, "ps ") {
				out = append(out, c)
			}
		}
		return out
	}
	return m, tl, acts
}

func TestAIContextsManageGates(t *testing.T) {
	_, tl, _ := aiContextsManageTool(t)
	if got := aiParamEnum(tl.Params["op"]); strings.Join(got, ",") != "add,use,default,remove,inspect,terminal" {
		t.Errorf("ops = %v", got)
	}
	for op, want := range map[string][2]any{
		"add": {true, ""}, "default": {true, ""}, "remove": {true, ""},
		"use":     {true, ai.ClassContainers},
		"inspect": {false, ""}, "terminal": {false, ""},
	} {
		ask, class := tl.Gate(map[string]any{"op": op})
		if ask != want[0] || class != want[1] {
			t.Errorf("op %s gate = %v %q, want %v %q", op, ask, class, want[0], want[1])
		}
	}
}

// add is + Add Server…: the exact `context create` argv; a taken name or
// a host that is no ssh:// / tcp:// / unix:// address is refused first.
func TestAIContextsManageAdd(t *testing.T) {
	_, tl, acts := aiContextsManageTool(t)
	a := map[string]any{"op": "add", "name": "baaytona", "host": "ssh://deploy@65.109.212.14"}
	if err := tl.Validate(a); err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "add card", tl.Describe(a), "Add server baaytona", "docker context create baaytona --docker host=ssh://deploy@65.109.212.14", "every terminal on this machine")
	out, err := tl.Run(context.Background(), a)
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "add result", out, "added baaytona", "context baaytona")
	if got := strings.Join(acts(), "|"); got != "context create baaytona --docker host=ssh://deploy@65.109.212.14" {
		t.Errorf("ran %q", got)
	}
	for _, bad := range []map[string]any{
		{"op": "add", "name": "prod", "host": "ssh://deploy@x"},
		{"op": "add", "name": "x", "host": "http://x"},
		{"op": "add", "name": "-x", "host": "ssh://a@b"},
		{"op": "add", "name": "x"},
	} {
		if err := tl.Validate(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

// use switches this session only (the default is unchanged; no engine
// write); default runs `context use`; remove refuses the default, the
// built-in and the session's host, else runs `context rm`.
func TestAIContextsManageUseDefaultRemove(t *testing.T) {
	m, tl, acts := aiContextsManageTool(t)
	if _, err := tl.Run(context.Background(), map[string]any{"op": "use", "name": "prod"}); err != nil {
		t.Fatal(err)
	}
	if m.services.sessionContext != "prod" || len(acts()) != 0 {
		t.Errorf("use: session %q, ran %q", m.services.sessionContext, acts())
	}
	if err := tl.Validate(map[string]any{"op": "use", "name": "nope"}); err == nil {
		t.Error("use nope accepted")
	}
	if err := tl.Validate(map[string]any{"op": "remove", "name": "prod"}); err == nil || !strings.Contains(err.Error(), "this session uses") {
		t.Errorf("remove the session's host: %v", err)
	}
	if err := tl.Validate(map[string]any{"op": "remove", "name": "default"}); err == nil {
		t.Error("remove default accepted")
	}
	m.services.sessionContext = ""
	a := map[string]any{"op": "default", "name": "prod"}
	covAIMustContain(t, "default card", tl.Describe(a), "Make prod the default", "docker context use prod", "every terminal and app on this machine")
	if _, err := tl.Run(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	r := map[string]any{"op": "remove", "name": "prod"}
	if err := tl.Validate(r); err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "remove card", tl.Describe(r), "Remove context prod", "docker context rm prod", "remote host and its containers are not touched")
	if _, err := tl.Run(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(acts(), "|"); got != "context use prod|context rm prod" {
		t.Errorf("ran %q", got)
	}
}

// terminal opens Open Terminal's tab: ssh to user@host as argv; inspect
// reads `context inspect`.
func TestAIContextsManageTerminalInspect(t *testing.T) {
	_, tl, acts := aiContextsManageTool(t)
	var jobs []terminalJob
	old := spawnHostTerminal
	spawnHostTerminal = func(_ *Model, j terminalJob) error { jobs = append(jobs, j); return nil }
	t.Cleanup(func() { spawnHostTerminal = old })
	out, err := tl.Run(context.Background(), map[string]any{"op": "terminal", "name": "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || strings.Join(jobs[0].Argv, " ") != "ssh -- deploy@prod.example" {
		t.Fatalf("jobs = %+v", jobs)
	}
	covAIMustContain(t, "terminal", out, "ssh: prod", "deploy@prod.example")
	out, err = tl.Run(context.Background(), map[string]any{"op": "inspect", "name": "prod"})
	if err != nil {
		t.Fatal(err)
	}
	covAIMustContain(t, "inspect", out, "prod")
	if got := strings.Join(acts(), "|"); got != "context inspect prod" {
		t.Errorf("ran %q", got)
	}
}
