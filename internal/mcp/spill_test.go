package mcp

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func bigCall(text string) CallFunc {
	return func(context.Context, string, string, map[string]any) (CallResult, error) {
		return CallResult{Content: []Content{{Type: "text", Text: text}}}, nil
	}
}

// Spec (AI-4, AI-6): a result over the limit is not cut silently: the
// whole text goes to a session file, the model gets the head, the size
// and how to read the rest; the user gets a warning.
func TestBigResultSpillsToFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spill")
	sp := NewSpill(dir)
	full := strings.Repeat("abcdefghij", 500) + "THE-END"
	var warned []string
	tools := AITools(Server{Name: "fake"}, []Tool{{Name: "big", Annotations: ann(bp(true), nil)}}, bigCall(full),
		AdapterOptions{MaxResult: 1000, Spill: sp, Warn: func(server, tool string, size int) {
			warned = append(warned, server+"/"+tool)
		}})
	out, err := tools[0].Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, full[:1000]) || strings.Contains(out, "THE-END") || !strings.Contains(out, "5007 bytes") {
		t.Fatalf("head: %q", out[len(out)-300:])
	}
	id := regexp.MustCompile(`id "([^"]+)"`).FindStringSubmatch(out)
	if id == nil || !strings.Contains(out, ResultReadTool) {
		t.Fatalf("no id / reader tool named: %q", out)
	}
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Fatalf("files %v", files)
	}
	b, _ := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if string(b) != full {
		t.Errorf("file holds %d bytes", len(b))
	}
	if fi, _ := os.Stat(filepath.Join(dir, files[0].Name())); fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode())
	}
	if len(warned) != 1 || warned[0] != "fake/big" {
		t.Errorf("warn %v", warned)
	}
	// The reader tool returns the rest in parts.
	rd := sp.Tool()
	if rd.Name != ResultReadTool || rd.Mutating {
		t.Fatalf("reader %+v", rd)
	}
	part, err := rd.Run(context.Background(), map[string]any{"id": id[1], "offset": float64(5000), "length": float64(100)})
	if err != nil || !strings.HasPrefix(part, "THE-END") || !strings.Contains(part, "end of the result") {
		t.Errorf("part %q %v", part, err)
	}
	for _, bad := range []string{"../x", "/etc/passwd", "nope"} {
		if _, err := rd.Run(context.Background(), map[string]any{"id": bad}); err == nil {
			t.Errorf("id %q read", bad)
		}
	}
}

// Spec (AI-5): a tool's _meta maxResultSizeChars sets its own limit
// (never above the hard cap).
func TestToolMetaMaxResultSize(t *testing.T) {
	full := strings.Repeat("y", 3000)
	tl := Tool{Name: "t", Annotations: ann(bp(true), nil), Meta: map[string]any{"example.com/maxResultSizeChars": float64(2500)}}
	if tl.MaxResultSize() != 2500 {
		t.Fatalf("meta %d", tl.MaxResultSize())
	}
	out, _ := AITools(Server{Name: "s"}, []Tool{tl}, bigCall(full), AdapterOptions{MaxResult: 1000})[0].Run(context.Background(), nil)
	if !strings.HasPrefix(out, strings.Repeat("y", 2500)+"\n") {
		t.Errorf("limit not used: %q", out[2490:2600])
	}
	huge := Tool{Name: "t", Meta: map[string]any{"maxResultSizeChars": float64(1 << 40)}}
	if huge.MaxResultSize() != HardMaxResult {
		t.Errorf("huge %d", huge.MaxResultSize())
	}
}

// A tool's _meta does not change ToolsHash (no "tools changed" card
// for lists saved before _meta was read).
func TestToolsHashIgnoresMeta(t *testing.T) {
	a := []Tool{{Name: "x"}}
	b := []Tool{{Name: "x", Meta: map[string]any{"maxResultSizeChars": float64(9)}}}
	if ToolsHash(a) != ToolsHash(b) {
		t.Error("meta changes the hash")
	}
}
