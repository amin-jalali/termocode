package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/git"
	"github.com/amin-jalali/termocode/internal/mcp"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// Spec (MS-6): the source lists one resource, termocode://editor/state.
func TestMCPServeResourceList(t *testing.T) {
	src, hub, _ := mcpServeTestSource(t.TempDir(), nil)
	defer hub.stop()
	var rs mcp.ResourceSource = src
	list := rs.Resources()
	if len(list) != 1 || list[0].URI != "termocode://editor/state" || list[0].MimeType != "application/json" {
		t.Fatalf("resources = %+v", list)
	}
}

// Spec (MS-6): reading the state builds a small JSON on the UI goroutine:
// branch, diagnostics counts, no confirm card, one audit line; a
// registered secret in free text is redacted.
func TestMCPServeResourceReadGolden(t *testing.T) {
	m := aiTestModel(t)
	m.gitBranch = git.Branch{Name: "feature/x"}
	m.problems = newProblemsPanel()
	m.problems.setItems([]nvim.Diagnostic{{Path: "/w/a.go", Line: 1, Col: 1, Severity: 1}, {Path: "/w/a.go", Line: 2, Col: 1, Severity: 2}, {Path: "/w/b.go", Line: 3, Col: 1, Severity: 3}})
	src, hub, audit := mcpServeTestSource(t.TempDir(), func(context.Context, string, ai.Tool, string) error {
		t.Error("a resource read asked")
		return nil
	})
	defer hub.stop()
	var got string
	_, err, n := mcpServeDrive(t, &m, hub, func() (mcp.CallResult, error) {
		var err error
		got, err = src.ReadResource(mcpServeCtx("claude-code"), "termocode://editor/state")
		return mcp.CallResult{}, err
	})
	if err != nil || n != 1 {
		t.Fatalf("err = %v, ui calls = %d", err, n)
	}
	want := `{"file":"","line":0,"column":0,"buffers":[],"modified":[],"branch":"feature/x","diagnostics":{"errors":1,"warnings":1,"info":1}}`
	if got != want {
		t.Fatalf("state\n got: %s\nwant: %s", got, want)
	}
	if !json.Valid([]byte(got)) || !strings.Contains(audit.all(), "by=mcp:claude-code resources/read") {
		t.Fatalf("audit = %q", audit.all())
	}
}

// Spec (MS-6): a secret the IDE knows never leaves in the state.
func TestMCPServeResourceRedacts(t *testing.T) {
	m := aiTestModel(t)
	m.gitBranch = git.Branch{Name: "fix-hunter2secretvalue"}
	ai.RegisterSecret("hunter2secretvalue")
	src, hub, _ := mcpServeTestSource(t.TempDir(), nil)
	defer hub.stop()
	var got string
	mcpServeDrive(t, &m, hub, func() (mcp.CallResult, error) {
		got, _ = src.ReadResource(mcpServeCtx("x"), "termocode://editor/state")
		return mcp.CallResult{}, nil
	})
	if strings.Contains(got, "hunter2secretvalue") || !json.Valid([]byte(got)) {
		t.Fatalf("state leaks a secret or is not JSON: %s", got)
	}
}

// Spec (MS-6): an unknown URI is a -32002 error, no UI call.
func TestMCPServeResourceUnknown(t *testing.T) {
	src, hub, _ := mcpServeTestSource(t.TempDir(), nil)
	defer hub.stop()
	_, err := src.ReadResource(context.Background(), "termocode://editor/other")
	var ce *mcp.CallError
	if !errors.As(err, &ce) || ce.Code != mcp.CodeResourceNotFound {
		t.Fatalf("err = %v", err)
	}
}
