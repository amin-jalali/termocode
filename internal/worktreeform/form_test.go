package worktreeform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The branch is drafted from the name the user types (WTX-5): lower
// case, words joined by -, the / of a prefix kept, nothing git refuses.
func TestBranchFromName(t *testing.T) {
	for in, want := range map[string]string{
		"feat/pin-row":         "feat/pin-row",
		"Add a pinned row":     "add-a-pinned-row",
		"  fix: toast race!  ": "fix-toast-race",
		"feat/Pin Row":         "feat/pin-row",
		"a..b~^:?*[\\c":        "a-b-c",
		"/lead//slash/":        "lead/slash",
		"x.lock":               "x-lock",
		".hidden":              "hidden",
		"grüße welt":           "grüße-welt",
		"":                     "",
		"!!!":                  "",
	} {
		if got := BranchFromName(in); got != want {
			t.Errorf("BranchFromName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := BranchFromName(strings.Repeat("abcdefghij", 20)); len(got) > MaxBranch {
		t.Errorf("long name %d chars", len(got))
	}
}

// The folder name is the branch with / as -.
func TestFolderOf(t *testing.T) {
	if got := FolderName("feat/pin-row"); got != "feat-pin-row" {
		t.Errorf("FolderName %q", got)
	}
	main := filepath.FromSlash("/src/app")
	for setting, want := range map[string]string{
		"":              "/src/app.worktrees/feat-x",
		"/data/wt":      "/data/wt/feat-x",
		"~/wt":          "/home/u/wt/feat-x",
		"../elsewhere":  "/src/elsewhere/feat-x",
		"  ":            "/src/app.worktrees/feat-x",
		"/data/wt/../w": "/data/w/feat-x",
	} {
		if got := Folder(BaseDir(main, setting, "/home/u"), "feat/x"); got != filepath.FromSlash(want) {
			t.Errorf("setting %q: %q, want %q", setting, got, want)
		}
	}
}

// .termocode/worktrees.json: include / shared / setup, comments
// allowed; a missing file is empty, a broken one an error; the hash is
// of the file's bytes (a changed file asks for trust again).
func TestLoadConfig(t *testing.T) {
	top := t.TempDir()
	c, hash, err := LoadConfig(top)
	if err != nil || hash != "" || len(c.Include)+len(c.Shared)+len(c.Setup) != 0 {
		t.Fatalf("missing file: %+v %q %v", c, hash, err)
	}
	write(t, top, ConfigFile, `{
		// copied into each new worktree
		"include": [".env", ".env.local"],
		"shared": ["node_modules"],
		"setup": ["pnpm install"]
	}`)
	c, hash, err = LoadConfig(top)
	if err != nil || hash == "" {
		t.Fatal(err)
	}
	if strings.Join(c.Include, ",") != ".env,.env.local" || strings.Join(c.Shared, ",") != "node_modules" || strings.Join(c.Setup, ",") != "pnpm install" {
		t.Errorf("config %+v", c)
	}
	write(t, top, ConfigFile, `{"setup": ["pnpm install", "make"]}`)
	if _, h2, _ := LoadConfig(top); h2 == hash || h2 == "" {
		t.Error("a changed file kept its hash")
	}
	write(t, top, ConfigFile, `{"setup": "not a list"`)
	if _, _, err := LoadConfig(top); err == nil {
		t.Error("a broken file loaded")
	}
}

// PlanFiles says what will be copied and linked, and why a row is
// skipped: missing, a folder to copy, too large, outside the repo.
func TestPlanFiles(t *testing.T) {
	top := t.TempDir()
	outside := t.TempDir()
	write(t, top, ".env", "TOKEN=x\n")
	write(t, top, "conf/local.yml", "a: 1\n")
	write(t, top, "node_modules/x/index.js", "1\n")
	write(t, outside, "secret.txt", "s\n")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(top, "escape")); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(top, "big.bin")
	if err := os.WriteFile(big, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(big, MaxCopy+1); err != nil {
		t.Fatal(err)
	}
	c := Config{
		Include: []string{".env", "conf/local.yml", "missing.env", "node_modules", "big.bin", "escape", "../x", "/etc/passwd", ""},
		Shared:  []string{"node_modules", "gone", "../up"},
	}
	got := map[string]string{}
	var copies, links int
	for _, f := range PlanFiles(top, c) {
		key := f.Rel
		if f.Kind == Link {
			key = "link:" + f.Rel
		}
		got[key] = f.Skip
		if f.Skip == "" && f.Kind == Copy {
			copies++
		}
		if f.Skip == "" && f.Kind == Link {
			links++
		}
	}
	if copies != 2 || links != 1 {
		t.Errorf("copies %d links %d: %v", copies, links, got)
	}
	for rel, want := range map[string]string{
		"missing.env":       "not there",
		"node_modules":      "a folder",
		"big.bin":           "20 MB",
		"escape":            "outside",
		"../x":              "outside",
		"/etc/passwd":       "outside",
		"link:gone":         "not there",
		"link:../up":        "outside",
		".env":              "",
		"link:node_modules": "",
	} {
		if s, ok := got[rel]; !ok || (want == "" && s != "") || (want != "" && !strings.Contains(s, want)) {
			t.Errorf("%s: skip %q (in plan %v), want %q", rel, s, ok, want)
		}
	}
	if _, ok := got[""]; ok {
		t.Error("an empty entry is in the plan")
	}
}

// ApplyFiles copies (same mode) and links into the new worktree, never
// over a file that is there, never through a folder that leads out.
func TestApplyFiles(t *testing.T) {
	top := t.TempDir()
	dest := t.TempDir()
	write(t, top, ".env", "TOKEN=x\n")
	if err := os.Chmod(filepath.Join(top, ".env"), 0o600); err != nil {
		t.Fatal(err)
	}
	write(t, top, "conf/local.yml", "a: 1\n")
	write(t, top, "README.md", "repo\n")
	write(t, dest, "README.md", "tracked\n")
	write(t, top, "node_modules/x/index.js", "1\n")
	out := ApplyFiles(top, dest, PlanFiles(top, Config{Include: []string{".env", "conf/local.yml", "README.md"}, Shared: []string{"node_modules"}}))
	skips := map[string]string{}
	for _, f := range out {
		skips[f.Rel] = f.Skip
	}
	if b, _ := os.ReadFile(filepath.Join(dest, ".env")); string(b) != "TOKEN=x\n" {
		t.Errorf(".env %q", b)
	}
	if fi, err := os.Stat(filepath.Join(dest, ".env")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Errorf(".env mode %v %v", fi, err)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "conf", "local.yml")); string(b) != "a: 1\n" {
		t.Errorf("local.yml %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "README.md")); string(b) != "tracked\n" || !strings.Contains(skips["README.md"], "already") {
		t.Errorf("README overwritten: %q skip %q", b, skips["README.md"])
	}
	target, err := os.Readlink(filepath.Join(dest, "node_modules"))
	if err != nil {
		t.Fatal(err)
	}
	if real, _ := filepath.EvalSymlinks(filepath.Join(top, "node_modules")); target != real {
		t.Errorf("link %q, want %q", target, real)
	}

	// A folder of the worktree that is a link out of it: nothing is
	// written through it.
	dest2 := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dest2, "conf")); err != nil {
		t.Fatal(err)
	}
	out = ApplyFiles(top, dest2, PlanFiles(top, Config{Include: []string{"conf/local.yml"}}))
	if len(out) != 1 || !strings.Contains(out[0].Skip, "outside") {
		t.Errorf("wrote through a link: %+v", out)
	}
	if _, err := os.Stat(filepath.Join(outside, "local.yml")); err == nil {
		t.Error("a file landed outside the worktree")
	}
}

// The setup trust is bound to the config hash.
func TestSetupTrusted(t *testing.T) {
	if SetupTrusted("", "") || SetupTrusted("abc", "") || !SetupTrusted("abc", "abc") || SetupTrusted("abc", "abd") {
		t.Error("trust rule")
	}
}

func write(t *testing.T, dir, rel, body string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// WTX-9: the branch of a worktree made from an issue: the number, then
// the title as a slug, cut on a word border.
func TestBranchFromIssue(t *testing.T) {
	for _, c := range []struct {
		n     int
		title string
		want  string
	}{
		{412, "Pinned row in WORKTREES", "412-pinned-row-in-worktrees"},
		{7, "Fix: crash when `git` is missing!", "7-fix-crash-when-git-is-missing"},
		{9, "../../etc/passwd", "9-etc-passwd"},
		{3, "", "issue-3"},
		{3, "!!!", "issue-3"},
		{12, strings.Repeat("word ", 30), "12-" + strings.TrimSuffix(strings.Repeat("word-", 11), "-")},
	} {
		if got := BranchFromIssue(c.n, c.title); got != c.want {
			t.Errorf("BranchFromIssue(%d, %q) = %q, want %q", c.n, c.title, got, c.want)
		}
		if got := BranchFromIssue(c.n, c.title); len(got) > MaxBranch {
			t.Errorf("%q is over %d", got, MaxBranch)
		}
	}
}

// CMP-5: the run line, the port range and the URL come from the same
// file; the URL fills in $PORT and falls back to localhost when it is
// not a web address.
func TestLoadConfigRun(t *testing.T) {
	top := t.TempDir()
	write(t, top, ConfigFile, `{
		"run": "pnpm dev --port $PORT",
		"ports": "3000-3010",
		"url": "http://localhost:$PORT/app"
	}`)
	c, _, err := LoadConfig(top)
	if err != nil || c.Run != "pnpm dev --port $PORT" || c.Ports != "3000-3010" {
		t.Fatalf("config %+v %v", c, err)
	}
	if got := c.RunURL(3004); got != "http://localhost:3004/app" {
		t.Errorf("url %q", got)
	}
	for _, u := range []string{"", "javascript:alert(1)", "file:///etc/passwd", "localhost:$PORT"} {
		if got := (Config{URL: u}).RunURL(3001); got != "http://localhost:3001" {
			t.Errorf("%q: url %q", u, got)
		}
	}
	if got := (Config{URL: "https://app.test:$PORT"}).RunURL(3002); got != "https://app.test:3002" {
		t.Errorf("https url %q", got)
	}
}
