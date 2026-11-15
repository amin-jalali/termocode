package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// savedPathRe finds the saved file in a cut note.
var savedPathRe = regexp.MustCompile(`full output: (\S+) \(read_file with offset\)`)

// savedPath is the file a cut output names, or "" when it names none.
func savedPath(out string) string {
	if m := savedPathRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// TestMain points the cache at a temp folder: a test that runs a long
// command never saves its output in the real ~/.cache.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "termocode-ai-cache-")
	if err != nil {
		panic(err)
	}
	if err := os.Setenv("XDG_CACHE_HOME", dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir) // a test cleanup
	os.Exit(code)
}

// cutCache points the termocode cache at a temp folder for one test.
func cutCache(t *testing.T) string {
	t.Helper()
	c := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", c)
	return filepath.Join(c, "termocode", "ai-output")
}

// A cut run_command output is saved in full outside the workspace, the
// cut note names the file, and read_file reads it at the offset where
// the error is (in the middle, the part the cut dropped).
func TestRunCommandCutSavesTheFullOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	store := cutCache(t)
	root := t.TempDir()
	w := Workspace{Root: root}
	const secret = "cut-save-secret-90210"
	RegisterSecret(secret)
	out, err := wsTool(t, w, "run_command").Run(context.Background(), map[string]any{"command": `i=1; while [ $i -le 9000 ]; do
		if [ $i -eq 4500 ]; then echo "ERROR: boom in the middle ` + secret + `"; else echo "ok $i"; fi; i=$((i+1)); done; exit 1`})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "boom in the middle") {
		t.Fatal("the error line was not cut: the test needs a longer output")
	}
	p := savedPath(out)
	if p == "" {
		t.Fatalf("the cut note names no saved file: %q", out[:min(len(out), 4500)])
	}
	if !strings.HasPrefix(p, store+string(filepath.Separator)) || strings.HasPrefix(p, root) {
		t.Fatalf("saved at %q, want inside %q and outside the workspace", p, store)
	}
	got, err := wsTool(t, w, "read_file").Run(context.Background(), map[string]any{"path": p, "offset": 4500, "limit": 1})
	if err != nil {
		t.Fatalf("read_file on the saved output: %v", err)
	}
	if !strings.Contains(got, "4500|ERROR: boom in the middle [REDACTED]") || strings.Contains(got, secret) {
		t.Fatalf("read_file = %q", got)
	}
	if b, _ := os.ReadFile(p); strings.Contains(string(b), secret) {
		t.Fatal("the saved file holds the secret")
	}
	if fi, err := os.Stat(p); err != nil || fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("saved file mode: %v %v", fi.Mode(), err)
	}
}

// A short output is not saved and has no note.
func TestRunCommandShortOutputIsNotSaved(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	store := cutCache(t)
	out, err := wsTool(t, Workspace{Root: t.TempDir()}, "run_command").Run(context.Background(), map[string]any{"command": "echo hi"})
	if err != nil || out != "hi\n" {
		t.Fatalf("out = %q %v", out, err)
	}
	if _, err := os.Stat(store); err == nil {
		t.Fatal("a short output made the output folder")
	}
}

// The folder is read-only for the agent: write, edit and delete still
// refuse it, and read_file reads nothing else outside the workspace (a
// sibling folder, or a link in the folder pointing out of it).
func TestSavedOutputFolderIsReadOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh")
	}
	store := cutCache(t)
	w := Workspace{Root: t.TempDir()}
	out, _ := wsTool(t, w, "run_command").Run(context.Background(), map[string]any{"command": `seq 1 20000`})
	p := savedPath(out)
	if p == "" {
		t.Fatalf("not saved: %q", out[:200])
	}
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"write_file", map[string]any{"path": p, "content": "x"}},
		{"edit_file", map[string]any{"path": p, "old_text": "1\n", "new_text": "x\n"}},
		{"delete", map[string]any{"path": p}},
	} {
		if _, err := runTool(t, w, c.tool, c.args); err == nil {
			t.Errorf("%s changed the saved output", c.tool)
		}
	}
	if b, _ := os.ReadFile(p); !strings.HasPrefix(string(b), "1\n2\n") {
		t.Fatalf("saved file changed: %q", string(b)[:20])
	}
	outside := filepath.Join(filepath.Dir(store), "other.txt")
	if err := os.WriteFile(outside, []byte("private\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(p), "link.log")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{outside, link, filepath.Join(store, "..", "other.txt")} {
		if got, err := wsTool(t, w, "read_file").Run(context.Background(), map[string]any{"path": q}); err == nil {
			t.Errorf("read_file %s = %q, want refused", q, got)
		}
	}
}

// A container host run that is cut is saved on this machine and read
// from there (the host FS never sees the path).
func TestHostRunCutSavesLocally(t *testing.T) {
	store := cutCache(t)
	w, _, x := hostWorkspace(t)
	var big strings.Builder
	for i := 1; i <= 9000; i++ {
		if i == 4500 {
			big.WriteString("ERROR: host boom\n")
			continue
		}
		fmt.Fprintf(&big, "ok %d\n", i)
	}
	x.answer = func([]string) (string, string, int) { return big.String(), "", 2 }
	out, err := runHostTool(t, w, "run_command", map[string]any{"command": "make"})
	if err != nil {
		t.Fatal(err)
	}
	p := savedPath(out)
	if p == "" || !strings.HasPrefix(p, store) || !strings.HasSuffix(out, "[exit status 2]") {
		t.Fatalf("out = %q", out[len(out)-300:])
	}
	got, err := runHostTool(t, w, "read_file", map[string]any{"path": p, "offset": 4500, "limit": 1})
	if err != nil || !strings.Contains(got, "4500|ERROR: host boom") {
		t.Fatalf("read_file = %q %v", got, err)
	}
	if _, err := runHostTool(t, w, "write_file", map[string]any{"path": p, "content": "x"}); err == nil {
		t.Error("write_file reached the saved output")
	}
}
