//go:build unix

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/ai"
	"github.com/amin-jalali/termocode/internal/lspinstall"
	"github.com/amin-jalali/termocode/internal/nvim"
)

// fakeAstGrep puts an ast-grep on PATH that prints out (JSON lines) and
// keeps its arguments in <bin>/args.
func fakeAstGrep(t *testing.T, out string) string {
	t.Helper()
	t.Setenv(lspinstall.ToolsDirEnv, t.TempDir())
	bin := t.TempDir()
	data := filepath.Join(bin, "out")
	if err := os.WriteFile(data, []byte(out), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + filepath.Join(bin, "args") + "\ncat " + data + "\n"
	if err := os.WriteFile(filepath.Join(bin, "ast-grep"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin:/bin")
	return bin
}

// direct is an IDE caller that runs fn on m at once (no chat loop).
func direct(m *Model) aiIDECaller {
	return func(_ context.Context, fn aiIDEFunc) (string, error) {
		out, _, err := fn(m)
		return out, err
	}
}

const astMatches = `{"file":"a.go","text":"fmt.Println(x)","lines":"\tfmt.Println(x)","range":{"byteOffset":{"start":23,"end":37},"start":{"line":3,"column":1}},"replacement":"log.Print(x)"}
{"file":"b/c.go","text":"fmt.Println(y)","lines":"fmt.Println(y)","range":{"byteOffset":{"start":0,"end":14},"start":{"line":0,"column":0}},"replacement":"log.Print(y)"}
`

// Spec (OMX-28): ast_grep runs ast-grep and lists the matches as
// file:line; a search runs at once.
func TestAIAstGrepSearch(t *testing.T) {
	bin := fakeAstGrep(t, astMatches)
	aiTestModel(t)
	m := &Model{nvim: &nvim.Client{}, ai: newAIState()}
	dir := t.TempDir()
	tool, ok := ai.FindTool(m.aiChatTools(dir, direct(m)), "ast_grep")
	if !ok {
		t.Fatal("no ast_grep tool")
	}
	a := map[string]any{"pattern": "fmt.Println($A)", "lang": "go"}
	if mut, _ := tool.Gate(a); mut {
		t.Error("a search asks first")
	}
	out, err := tool.Run(context.Background(), a)
	if err != nil || out != "a.go:4: fmt.Println(x)\nb/c.go:1: fmt.Println(y)\n" {
		t.Fatalf("out %q, %v", out, err)
	}
	args, _ := os.ReadFile(filepath.Join(bin, "args"))
	if got := strings.Join(strings.Fields(string(args)), " "); got != "run --pattern fmt.Println($A) --json=stream --lang go ." {
		t.Errorf("args %q", got)
	}
	if mut, class := tool.Gate(map[string]any{"pattern": "x", "rewrite": "y"}); !mut || class != ai.ClassReview {
		t.Errorf("rewrite gate %v %q, want the review card", mut, class)
	}
}

// Spec (OMX-28): without ast-grep the tool says so and opens the install
// card once (Install / Cancel, like a missing language server).
func TestAIAstGrepMissingOffersInstall(t *testing.T) {
	t.Setenv(lspinstall.ToolsDirEnv, t.TempDir())
	bin := t.TempDir() // npm and node exist, ast-grep does not
	for _, n := range []string{"npm", "node"} {
		if err := os.WriteFile(filepath.Join(bin, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	aiTestModel(t)
	m := &Model{nvim: &nvim.Client{}, ai: newAIState(), w: 120, h: 40}
	tool, _ := ai.FindTool(m.aiChatTools(t.TempDir(), direct(m)), "ast_grep")
	_, err := tool.Run(context.Background(), map[string]any{"pattern": "x"})
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("err %v", err)
	}
	if !m.confirmOpen || m.confirmKind != confirmKindToolInstall || m.lspMgr.pending != "ast-grep" {
		t.Fatalf("no install card (open %v kind %v)", m.confirmOpen, m.confirmKind)
	}
	m.confirmOpen = false
	_, _ = tool.Run(context.Background(), map[string]any{"pattern": "x"})
	if m.confirmOpen {
		t.Error("the install card opened a second time")
	}
}

// Spec (OMX-28): a rewrite is shown in the review card; accepting
// writes every file.
func TestAIAstGrepRewriteReviewed(t *testing.T) {
	fakeAstGrep(t, astMatches)
	p := &scriptedAIProvider{replies: []ai.Chunk{toolCall("ast_grep", map[string]any{"pattern": "fmt.Println($A)", "rewrite": "log.Print($A)"})}}
	m, _, dir := reviewModel(t, p, map[string]string{
		"a.go":   "package a\n\nfunc f() {\n\tfmt.Println(x)\n}\n",
		"b/c.go": "fmt.Println(y)\n",
	})
	driveAIReview(t, m, m.sendAIChat("swap"), func(m *Model) {
		if n := len(m.ai.review.items); n != 2 {
			t.Fatalf("%d files in the review, want 2", n)
		}
		reviewKey(t, m, "A")
		if m.aiReviewPending() > 0 {
			reviewKey(t, m, "A")
		}
	})
	if got := readFile(filepath.Join(dir, "a.go")); got != "package a\n\nfunc f() {\n\tlog.Print(x)\n}\n" {
		t.Errorf("a.go %q", got)
	}
	if got := readFile(filepath.Join(dir, "b", "c.go")); got != "log.Print(y)\n" {
		t.Errorf("b/c.go %q", got)
	}
}
