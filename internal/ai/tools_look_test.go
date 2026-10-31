package ai

import (
	"bytes"
	"compress/zlib"
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func lookPDF(text string) string {
	var z bytes.Buffer
	w := zlib.NewWriter(&z)
	w.Write([]byte("BT (" + text + ") Tj ET"))
	w.Close()
	return "%PDF-1.4\n1 0 obj\n<< /Filter /FlateDecode >>\nstream\n" + z.String() + "\nendstream\nendobj\n"
}

func TestLookAtToolImageAndPDF(t *testing.T) {
	dir := t.TempDir()
	w := Workspace{Root: dir}
	writeFile(t, filepath.Join(dir, "shot.png"), "\x89PNG fake")
	writeFile(t, filepath.Join(dir, "spec.pdf"), lookPDF("Max 3 retries"))
	writeFile(t, filepath.Join(dir, "a.go"), "package a\n")
	p := &scripted{replies: []Chunk{{Delta: "A red button that says Save."}}}
	vision := true
	tool := LookAtTool(w, func() (Provider, string, bool) { return p, "m1", vision })
	if tool.Mutating || tool.Name != "look_at" {
		t.Fatalf("tool %+v", tool)
	}
	if got := tool.Describe(map[string]any{"path": "shot.png"}); !strings.Contains(got, "one extra model request") {
		t.Errorf("describe %q does not show the extra request", got)
	}
	out, err := tool.Run(context.Background(), map[string]any{"path": "shot.png", "question": "What does the button say?"})
	if err != nil || !strings.Contains(out, "A red button that says Save.") {
		t.Fatalf("image = %q, %v", out, err)
	}
	req := p.seen[0]
	last := req[len(req)-1]
	if len(last.Images) != 1 || last.Images[0].MediaType != "image/png" || !strings.Contains(last.Content, "What does the button say?") {
		t.Errorf("request %+v", last)
	}
	out, err = tool.Run(context.Background(), map[string]any{"path": "spec.pdf"})
	if err != nil || !strings.Contains(out, "Max 3 retries") || p.calls != 1 {
		t.Fatalf("pdf = %q, %v (model calls %d, want no new one)", out, err, p.calls)
	}
	if _, err := tool.Run(context.Background(), map[string]any{"path": "a.go"}); err == nil || !strings.Contains(err.Error(), "read_file") {
		t.Errorf("text file: err = %v", err)
	}
	vision = false
	if _, err := tool.Run(context.Background(), map[string]any{"path": "shot.png"}); err == nil || !strings.Contains(err.Error(), "can not see images") {
		t.Errorf("no vision: err = %v", err)
	}
	if err := tool.Validate(map[string]any{"path": "../x.png"}); err == nil {
		t.Error("a path outside the workspace passed")
	}
}
