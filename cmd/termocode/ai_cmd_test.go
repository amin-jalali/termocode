package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
)

// scriptProvider answers each request with the next scripted reply.
type scriptProvider struct {
	mu      sync.Mutex
	replies []ai.Chunk // Delta is the text, the rest goes on the Done chunk
	reqs    []ai.Request
	block   bool // wait for ctx (timeout test)
}

func (p *scriptProvider) Name() string        { return "script" }
func (p *scriptProvider) Configured() bool    { return true }
func (p *scriptProvider) SupportsTools() bool { return true }
func (p *scriptProvider) Stream(ctx context.Context, req ai.Request) <-chan ai.Chunk {
	ch := make(chan ai.Chunk, 4)
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	var r ai.Chunk
	if len(p.replies) > 0 {
		r, p.replies = p.replies[0], p.replies[1:]
	}
	p.mu.Unlock()
	go func() {
		defer close(ch)
		if p.block {
			<-ctx.Done()
			ch <- ai.Chunk{Done: true, Err: ctx.Err()}
			return
		}
		if r.Delta != "" {
			ch <- ai.Chunk{Delta: r.Delta}
		}
		ch <- ai.Chunk{Done: true, Err: r.Err, ToolCalls: r.ToolCalls, Usage: r.Usage}
	}()
	return ch
}

func withAIExecEnv(t *testing.T, p ai.Provider, setupErr error) {
	t.Helper()
	old := aiExecSetup
	t.Cleanup(func() { aiExecSetup = old })
	aiExecSetup = func(_ func(string) string, model string) (aiExecEnv, error) {
		if setupErr != nil {
			return aiExecEnv{}, setupErr
		}
		if model == "" {
			model = "m-default"
		}
		return aiExecEnv{Provider: p, Model: model}, nil
	}
}

func runAIExec(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	var out, errw bytes.Buffer
	code := runAIIn(context.Background(), args, strings.NewReader(stdin), &out, &errw, func(string) string { return "" })
	return code, out.String(), errw.String()
}

// Spec (CMP-41): `termocode ai -x "…"` runs one AI turn with no UI and
// prints the answer; exit 0.
func TestAIExecText(t *testing.T) {
	p := &scriptProvider{replies: []ai.Chunk{{Delta: "hello from the model"}}}
	withAIExecEnv(t, p, nil)
	code, out, _ := runAIExec(t, "", "-x", "say hi", "--dir", t.TempDir())
	if code != 0 || strings.TrimSpace(out) != "hello from the model" {
		t.Fatalf("code %d out %q", code, out)
	}
	if len(p.reqs) != 1 {
		t.Fatalf("requests %d", len(p.reqs))
	}
	msgs := p.reqs[0].Messages
	if msgs[0].Role != ai.RoleSystem || msgs[len(msgs)-1].Content != "say hi" {
		t.Errorf("messages %+v", msgs)
	}
}

// Spec (CMP-41): --output json prints one result object with ok, text,
// model, usage and the exit code.
func TestAIExecJSON(t *testing.T) {
	p := &scriptProvider{replies: []ai.Chunk{{Delta: "42", Usage: &ai.Usage{Input: 10, Output: 2}}}}
	withAIExecEnv(t, p, nil)
	code, out, _ := runAIExec(t, "", "exec", "--output", "json", "--model", "m1", "--dir", t.TempDir(), "what", "is", "it")
	if code != 0 {
		t.Fatalf("code %d", code)
	}
	var r aiExecResult
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("not one JSON object: %v %q", err, out)
	}
	if r.Type != "result" || !r.OK || r.Text != "42" || r.Model != "m1" || r.ExitCode != 0 || r.Usage == nil || r.Usage.Output != 2 {
		t.Errorf("result %+v", r)
	}
	if got := p.reqs[0].Messages; got[len(got)-1].Content != "what is it" {
		t.Errorf("prompt %q", got[len(got)-1].Content)
	}
}

// Spec (CMP-41): --output stream-json prints one JSON line per event
// (start, text, tool_start, tool_result) and the result line last. A
// read-only tool runs; a mutating one is declined (nobody to ask).
func TestAIExecStreamJSONTools(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha"), 0o644); err != nil {
		t.Fatal(err)
	}
	p := &scriptProvider{replies: []ai.Chunk{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "read_file", Args: map[string]any{"path": "a.txt"}}}},
		{ToolCalls: []ai.ToolCall{{ID: "c2", Name: "write_file", Args: map[string]any{"path": "b.txt", "content": "x"}}}},
		{Delta: "done"},
	}}
	withAIExecEnv(t, p, nil)
	code, out, _ := runAIExec(t, "", "-x", "read a.txt", "--output", "stream-json", "--tools", "all", "--dir", dir)
	if code != 0 {
		t.Fatalf("code %d %s", code, out)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	var types []string
	var sawRead, sawDecline bool
	for _, l := range lines {
		var ev map[string]any
		if err := json.Unmarshal([]byte(l), &ev); err != nil {
			t.Fatalf("line is no JSON: %q", l)
		}
		types = append(types, ev["type"].(string))
		if ev["type"] == "tool_result" && ev["tool"] == "read_file" && strings.Contains(ev["detail"].(string), "alpha") {
			sawRead = true
		}
		if ev["type"] == "tool_result" && ev["tool"] == "write_file" && ev["decision"] == "user denied" {
			sawDecline = true
		}
	}
	if types[0] != "start" || types[len(types)-1] != "result" {
		t.Errorf("types %v", types)
	}
	if !sawRead || !sawDecline {
		t.Errorf("read %v decline %v\n%s", sawRead, sawDecline, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err == nil {
		t.Error("a mutating tool ran without --allow")
	}
}

// Spec (CMP-41): --allow fileWrite lets file changes run; the default
// tool set is read-only (no write_file at all).
func TestAIExecAllowAndDefaultTools(t *testing.T) {
	dir := t.TempDir()
	p := &scriptProvider{replies: []ai.Chunk{
		{ToolCalls: []ai.ToolCall{{ID: "c1", Name: "write_file", Args: map[string]any{"path": "b.txt", "content": "x"}}}},
		{Delta: "ok"},
	}}
	withAIExecEnv(t, p, nil)
	if code, _, errs := runAIExec(t, "", "-x", "write", "--allow", "fileWrite", "--dir", dir); code != 0 {
		t.Fatalf("code %d %s", code, errs)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "b.txt")); err != nil || string(b) != "x" {
		t.Errorf("allowed write did not run: %v %q", err, b)
	}

	p2 := &scriptProvider{replies: []ai.Chunk{{Delta: "ok"}}}
	withAIExecEnv(t, p2, nil)
	if code, _, _ := runAIExec(t, "", "-x", "look", "--dir", dir); code != 0 {
		t.Fatal(code)
	}
	for _, s := range p2.reqs[0].Tools {
		if s.Name == "write_file" || s.Name == "run_command" {
			t.Errorf("default tools hold mutating %s", s.Name)
		}
	}
	p3 := &scriptProvider{replies: []ai.Chunk{{Delta: "ok"}}}
	withAIExecEnv(t, p3, nil)
	if code, _, _ := runAIExec(t, "", "-x", "look", "--tools", "none", "--dir", dir); code != 0 || len(p3.reqs[0].Tools) != 0 {
		t.Errorf("--tools none sent %d tools", len(p3.reqs[0].Tools))
	}
}

// Spec (CMP-41): exit codes for scripts — 2 usage, 3 not configured,
// 1 the model failed, 4 timeout, 5 step limit.
func TestAIExecExitCodes(t *testing.T) {
	withAIExecEnv(t, &scriptProvider{}, nil)
	if code, _, _ := runAIExec(t, ""); code != aiExitUsage {
		t.Errorf("no prompt: %d", code)
	}
	if code, _, _ := runAIExec(t, "", "-x", "a", "--output", "xml"); code != aiExitUsage {
		t.Errorf("bad output: %d", code)
	}
	if code, _, _ := runAIExec(t, "", "-x", "a", "--allow", "everything"); code != aiExitUsage {
		t.Errorf("bad --allow: %d", code)
	}

	withAIExecEnv(t, nil, errAINotConfigured)
	code, out, _ := runAIExec(t, "", "-x", "a", "--output", "json")
	if code != aiExitNotConfigured || !strings.Contains(out, `"ok":false`) {
		t.Errorf("not configured: %d %q", code, out)
	}

	withAIExecEnv(t, &scriptProvider{replies: []ai.Chunk{{Err: errors.New("bad request: sk-ant-secret123456789")}}}, nil)
	code, _, errs := runAIExec(t, "", "-x", "a", "--dir", t.TempDir())
	if code != aiExitFailed || errs == "" {
		t.Errorf("failed: %d %q", code, errs)
	}

	withAIExecEnv(t, &scriptProvider{block: true}, nil)
	if code, _, _ := runAIExec(t, "", "-x", "a", "--timeout", "50ms", "--dir", t.TempDir()); code != aiExitTimeout {
		t.Errorf("timeout: %d", code)
	}

	dir := t.TempDir()
	loop := &scriptProvider{}
	for i := 0; i < 3; i++ {
		loop.replies = append(loop.replies, ai.Chunk{ToolCalls: []ai.ToolCall{{ID: "c", Name: "list_dir", Args: map[string]any{}}}})
	}
	withAIExecEnv(t, loop, nil)
	if code, _, _ := runAIExec(t, "", "-x", "a", "--max-steps", "2", "--dir", dir); code != aiExitStepLimit {
		t.Errorf("step limit: %d", code)
	}
}

// Spec (CMP-41): "-" reads the prompt from stdin; --stdin adds piped
// input under the prompt.
func TestAIExecStdin(t *testing.T) {
	p := &scriptProvider{replies: []ai.Chunk{{Delta: "a"}, {Delta: "b"}}}
	withAIExecEnv(t, p, nil)
	if code, _, _ := runAIExec(t, "from stdin", "-x", "-", "--dir", t.TempDir()); code != 0 {
		t.Fatal(code)
	}
	if m := p.reqs[0].Messages; m[len(m)-1].Content != "from stdin" {
		t.Errorf("prompt %q", m[len(m)-1].Content)
	}
	if code, _, _ := runAIExec(t, "log line", "-x", "explain", "--stdin", "--dir", t.TempDir()); code != 0 {
		t.Fatal(code)
	}
	if m := p.reqs[1].Messages; !strings.HasPrefix(m[len(m)-1].Content, "explain") || !strings.Contains(m[len(m)-1].Content, "log line") {
		t.Errorf("prompt %q", m[len(m)-1].Content)
	}
}
