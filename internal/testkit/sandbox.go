package testkit

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/amin-jalali/termocode/internal/spawn"
)

// DefaultFiles is the workspace content when Options.Files is nil. It has
// nested folders and wide (CJK) and accented names on purpose.
var DefaultFiles = map[string]string{
	"README.md":           "# Demo\n\nA small workspace for tests. 界面 ✓\n",
	"main.go":             "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}\n",
	"pkg/util/util.go":    "package util\n\n// Add adds.\nfunc Add(a, b int) int { return a + b }\n",
	"docs/界面.md":          "# 界面\n\nWide glyphs: 日本語テキスト\n",
	"docs/naïve café.txt": "accents\n",
	"data/config.json":    "{\n  \"name\": \"demo\"\n}\n",
}

// sandbox points every place the app reads or writes user state at temp
// dirs, and cuts the network and the system clipboard off.
func sandbox(t testing.TB, nvim bool) {
	t.Helper()
	root := looseTempDir(t)
	home := filepath.Join(root, "home")
	// Pre-made (empty) plugin checkouts: no background git clone at
	// startup (internal/app/plugins.go).
	for _, d := range []string{"config/termocode", "data", "state", "cache", "tools",
		"plugins/vim-visual-multi/.git", "plugins/nvim-dap/.git",
		"plugins/rainbow-delimiters.nvim/lua/rainbow-delimiters"} {
		_ = os.MkdirAll(filepath.Join(home, d), 0o755)
	}
	// Animations off: frames settle at once. Images as text blocks. The
	// first-run setup card is done (setup tests pass their own config).
	_ = os.WriteFile(filepath.Join(home, "config/termocode/config.json"),
		[]byte(`{"animations": "off", "images": "blocks", "setup_done": true}`), 0o644)
	env := map[string]string{
		"HOME":            home,
		"XDG_CONFIG_HOME": filepath.Join(home, "config"),
		"XDG_DATA_HOME":   filepath.Join(home, "data"),
		"XDG_STATE_HOME":  filepath.Join(home, "state"),
		"XDG_CACHE_HOME":  filepath.Join(home, "cache"),
		// gopls (started by nvim for .go files) writes its cache while it
		// exits, after the test removed its folder: a per-test cache came
		// back as a leftover folder every time. One shared cache instead.
		"GOPLSCACHE": filepath.Join(os.TempDir(), "termocode-testkit-gopls-cache"),
		// Pre-made (empty) plugin checkouts: no git clone at startup.
		"TERMOCODE_PLUGINS_DIR": filepath.Join(home, "plugins"),
		"TERMOCODE_TOOLS_DIR":   filepath.Join(home, "tools"),
		// The Databases view looks at docker and local ports itself;
		// tests that want it switch it on.
		"TERMOCODE_NO_DB_DISCOVERY": "1",
		// gh reads its logins from $XDG_CONFIG_HOME/gh (empty here) and
		// these variables: a real gh is never signed in inside a test.
		"GH_TOKEN":            "",
		"GITHUB_TOKEN":        "",
		"GH_ENTERPRISE_TOKEN": "",
		// No system clipboard, no AI provider, no network.
		"DISPLAY":               "",
		"WAYLAND_DISPLAY":       "",
		"ANTHROPIC_API_KEY":     "",
		"OPENAI_API_KEY":        "",
		"OPENROUTER_API_KEY":    "",
		"TERMOCODE_AI_PROVIDER": "",
		"TERMOCODE_AI_MODEL":    "",
		"TERMOCODE_AI_BASE_URL": "",
		"TERMOCODE_AI_API_KEY":  "",
		"TERMOCODE_GIT_USER":    "",
		"TERMOCODE_GIT_TOKEN":   "",
		"OLLAMA_HOST":           "http://127.0.0.1:9",
		"HTTP_PROXY":            "http://127.0.0.1:9",
		"HTTPS_PROXY":           "http://127.0.0.1:9",
		"http_proxy":            "http://127.0.0.1:9",
		"https_proxy":           "http://127.0.0.1:9",
		"ALL_PROXY":             "http://127.0.0.1:9",
		"NO_PROXY":              "",
		"no_proxy":              "",
		"GIT_TERMINAL_PROMPT":   "0",
		"GIT_CONFIG_NOSYSTEM":   "1",
		"GIT_AUTHOR_NAME":       "Test",
		"GIT_AUTHOR_EMAIL":      "test@example.com",
		"GIT_COMMITTER_NAME":    "Test",
		"GIT_COMMITTER_EMAIL":   "test@example.com",
		"TERMOCODE_ICON_MODE":   "",
		"TERMOCODE_NERD_FONT":   "",
		"TERMOCODE_FONT_DELTA":  "0",
		"TERMOCODE_NO_NVIM":     "1",
		"TERMOCODE_HEADLESS":    "1", // no re-exec, xdg-open, OSC 52 …
	}
	if nvim {
		env["TERMOCODE_NO_NVIM"] = ""
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
}

// makeWorkspace writes files into a new temp dir and, with git, commits
// them over a few commits and leaves one modified and one untracked file.
func makeWorkspace(t testing.TB, files map[string]string, withGit bool) string {
	t.Helper()
	dir := filepath.Join(looseTempDir(t), "workspace")
	if files == nil {
		files = DefaultFiles
	}
	write := func(rel, body string) {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !withGit {
		for rel, body := range files {
			write(rel, body)
		}
		return dir
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	git := func(args ...string) {
		cmd := spawn.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "commit.gpgsign", "false")
	i := 0
	for _, rel := range sortedKeys(files) {
		write(rel, files[rel])
		i++
		if i%2 == 0 {
			git("add", "-A")
			git("commit", "-q", "-m", commitSubject(i))
		}
	}
	git("add", "-A")
	git("commit", "-q", "--allow-empty", "-m", "feat: 界面 support ✓ — a long subject line that is wider than most sidebars")
	git("checkout", "-q", "-b", "feature/x")
	write("CHANGELOG.md", "# Changes\n")
	git("add", "-A")
	git("commit", "-q", "-m", "docs: changelog")
	git("checkout", "-q", "main")
	git("merge", "-q", "--no-ff", "-m", "Merge branch 'feature/x'", "feature/x")
	// Working-tree changes for Source Control.
	if body, ok := files["main.go"]; ok {
		write("main.go", body+"\n// edited\n")
	}
	write("untracked.txt", "new\n")
	return dir
}

func commitSubject(i int) string {
	subjects := []string{"init", "feat: add util", "fix: naïve café", "chore: 日本語 docs", "refactor: data"}
	return subjects[(i/2)%len(subjects)]
}

func sortedKeys(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}

// looseTempDir is t.TempDir without the hard failure on cleanup: language
// servers started by nvim (gopls) may still write their cache while the
// test ends, and that must not fail the test.
func looseTempDir(t testing.TB) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "termocode-testkit-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for range 5 {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	})
	return dir
}
