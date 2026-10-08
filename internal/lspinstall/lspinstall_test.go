package lspinstall

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRegistryIntegrity(t *testing.T) {
	seenName := map[string]bool{}
	seenBin := map[string]bool{}
	for _, tl := range All() {
		if tl.Name == "" || tl.Bin == "" || tl.Spec == "" {
			t.Errorf("%+v: Name/Bin/Spec required", tl.Name)
		}
		if seenName[tl.Name] {
			t.Errorf("duplicate name %s", tl.Name)
		}
		if seenBin[tl.Bin] {
			t.Errorf("duplicate bin %s", tl.Bin)
		}
		seenName[tl.Name], seenBin[tl.Bin] = true, true
		if len(tl.Filetypes) == 0 {
			t.Errorf("%s: no filetypes", tl.Name)
		}
		if tl.Category == CategoryLSP && (len(tl.Cmd) == 0 || len(tl.Root) == 0) {
			t.Errorf("%s: LSP needs Cmd + Root", tl.Name)
		}
		if tl.InstallKind == KindGitHubRelease && (tl.Tag == "" || tl.Asset == "" || len(tl.Exec) == 0) {
			t.Errorf("%s: github-release needs Tag, Asset, Exec", tl.Name)
		}
	}
}

// Every server the old hard-coded lsp_lua.go table knew must still exist,
// with the same client name and executable.
func TestRegistryCoversLegacyServers(t *testing.T) {
	legacy := map[string]string{
		"gopls":         "gopls",
		"pyright":       "pyright-langserver",
		"pylsp":         "pylsp",
		"ts_ls":         "typescript-language-server",
		"rust_analyzer": "rust-analyzer",
		"clangd":        "clangd",
		"lua_ls":        "lua-language-server",
	}
	for name, bin := range legacy {
		tl, ok := Lookup(name)
		if !ok {
			t.Errorf("missing %s", name)
			continue
		}
		if tl.Category != CategoryLSP || tl.Cmd[0] != bin {
			t.Errorf("%s: cmd %v, want %s", name, tl.Cmd, bin)
		}
	}
	for _, name := range []string{"dlv", "debugpy", "js-debug"} {
		if tl, ok := Lookup(name); !ok || tl.Category != CategoryDAP {
			t.Errorf("adapter %s missing", name)
		}
	}
}

func TestForFiletype(t *testing.T) {
	py := ForFiletype(CategoryLSP, "python")
	if len(py) != 2 || py[0].Name != "pyright" || py[1].Name != "pylsp" {
		t.Fatalf("python servers = %v", py)
	}
	if got := ForFiletype(CategoryDAP, "go"); len(got) != 1 || got[0].Name != "dlv" {
		t.Fatalf("go adapters = %v", got)
	}
	if ForFiletype(CategoryLSP, "") != nil || len(ForFiletype(CategoryLSP, "cobol")) != 0 {
		t.Fatal("unknown filetypes must return nothing")
	}
}

func TestReleaseURL(t *testing.T) {
	lua, _ := Lookup("lua_ls")
	got, err := ReleaseURL(lua, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://github.com/LuaLS/lua-language-server/releases/download/3.13.5/lua-language-server-3.13.5-linux-x64.tar.gz"
	if got != want {
		t.Fatalf("got %s", got)
	}
	clangd, _ := Lookup("clangd")
	got, err = ReleaseURL(clangd, "darwin", "arm64")
	if err != nil || !strings.HasSuffix(got, "/19.1.2/clangd-mac-19.1.2.zip") {
		t.Fatalf("clangd mac: %s %v", got, err)
	}
	_, err = ReleaseURL(clangd, "linux", "arm64")
	var unsup *UnsupportedError
	if !errors.As(err, &unsup) || !strings.Contains(err.Error(), "apt install clangd") {
		t.Fatalf("want unsupported with fallback hint, got %v", err)
	}
	js, _ := Lookup("js-debug")
	got, _ = ReleaseURL(js, "linux", "riscv64")
	if !strings.HasSuffix(got, "/v1.96.0/js-debug-dap-v1.96.0.tar.gz") {
		t.Fatalf("js-debug: %s", got)
	}
}

func TestBuildPlanPerKind(t *testing.T) {
	dir := "/tools/x"
	cases := []struct {
		name     string
		requires []string
		argv0    string
		contains string
		exec0    string
	}{
		{"gopls", []string{"go"}, "go", "golang.org/x/tools/gopls@latest", "/tools/x/bin/gopls"},
		{"ts_ls", []string{"npm"}, "npm", "typescript", "/tools/x/node_modules/.bin/typescript-language-server"},
		{"pylsp", []string{"python3"}, "python3", "venv", "/tools/x/venv/bin/pylsp"},
		{"taplo", []string{"cargo"}, "cargo", "--root", "/tools/x/bin/taplo"},
		{"rust_analyzer", []string{"rustup"}, "rustup", "component", ""},
		{"debugpy", []string{"python3"}, "python3", "venv", "/tools/x/venv/bin/python"},
		{"js-debug", []string{"node"}, "", "", "node"},
	}
	for _, c := range cases {
		tl, _ := Lookup(c.name)
		p, err := BuildPlan(tl, dir, "linux", "amd64")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if strings.Join(p.Requires, ",") != strings.Join(c.requires, ",") {
			t.Errorf("%s requires %v", c.name, p.Requires)
		}
		if c.argv0 != "" {
			if p.Steps[0].Argv[0] != c.argv0 || !strings.Contains(strings.Join(p.Steps[0].Argv, " "), c.contains) {
				t.Errorf("%s step0 %v", c.name, p.Steps[0].Argv)
			}
		}
		if c.exec0 == "" {
			if len(p.Exec) != 0 {
				t.Errorf("%s: exec should be resolved at install time", c.name)
			}
		} else if len(p.Exec) == 0 || p.Exec[0] != c.exec0 {
			t.Errorf("%s exec %v", c.name, p.Exec)
		}
	}
	// GOBIN points into the tool dir.
	g, _ := Lookup("gopls")
	p, _ := BuildPlan(g, dir, "linux", "amd64")
	if len(p.Steps[0].Env) != 1 || p.Steps[0].Env[0] != "GOBIN=/tools/x/bin" {
		t.Errorf("gopls env %v", p.Steps[0].Env)
	}
	// github-release: download step + {dir}/{version} expansion.
	c, _ := Lookup("clangd")
	p, _ = BuildPlan(c, dir, "linux", "amd64")
	if p.Steps[0].Kind != StepDownload || p.Steps[0].Archive != "zip" || p.Exec[0] != "/tools/x/clangd_19.1.2/bin/clangd" {
		t.Errorf("clangd plan %+v", p)
	}
	if _, err := BuildPlan(g, dir, "windows", "amd64"); err == nil {
		t.Error("windows must be unsupported")
	}
	if !strings.Contains(p.HumanCommand(), "download clangd-linux-19.1.2.zip") {
		t.Errorf("human: %s", p.HumanCommand())
	}
}

func TestCheckRequiresMissingToolchain(t *testing.T) {
	old := lookPath
	defer func() { lookPath = old }()
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	tl, _ := Lookup("ts_ls")
	p, _ := BuildPlan(tl, "/x", "linux", "amd64")
	err := CheckRequires(p)
	var mt *MissingToolchainError
	if !errors.As(err, &mt) || mt.Bin != "npm" || !strings.Contains(mt.Hint, "nodejs.org") {
		t.Fatalf("got %v", err)
	}
}

func TestClassifyFailure(t *testing.T) {
	base := errors.New("exit status 1")
	var off *OfflineError
	if !errors.As(ClassifyFailure("gopls", "dial tcp: lookup proxy.golang.org: no such host", base), &off) {
		t.Error("go offline output not classified")
	}
	if !errors.As(ClassifyFailure("pyright", "npm ERR! code EAI_AGAIN", base), &off) {
		t.Error("npm offline output not classified")
	}
	if !errors.As(ClassifyFailure("x", "", &net.DNSError{Err: "no such host", Name: "github.com"}), &off) {
		t.Error("net error not classified")
	}
	var mt *MissingToolchainError
	if !errors.As(ClassifyFailure("pylsp", "The virtual environment was not created successfully because ensurepip is not available", base), &mt) {
		t.Error("venv missing not classified")
	}
	if got := ClassifyFailure("x", "compile error", base); got != base {
		t.Errorf("plain error changed: %v", got)
	}
	if ClassifyFailure("x", "", nil) != nil {
		t.Error("nil stays nil")
	}
}

func TestRenderShim(t *testing.T) {
	s := RenderShim("debugpy", []string{"/a b/python", "-m", "it's"})
	if !strings.HasPrefix(s, "#!/bin/sh\n") || !strings.Contains(s, `exec '/a b/python' '-m' 'it'\''s' "$@"`) {
		t.Fatalf("shim:\n%s", s)
	}
}

func TestPrependPath(t *testing.T) {
	env := []string{"HOME=/h", "PATH=/usr/bin:/bin"}
	got := PrependPath(env, "/tools/bin")
	if got[1] != "PATH=/tools/bin:/usr/bin:/bin" || env[1] != "PATH=/usr/bin:/bin" {
		t.Fatalf("got %v (input mutated? %v)", got, env)
	}
	if again := PrependPath(got, "/tools/bin"); again[1] != got[1] {
		t.Fatalf("double prepend: %v", again)
	}
	if none := PrependPath([]string{"A=1"}, "/t"); none[len(none)-1] != "PATH=/t" {
		t.Fatalf("missing PATH: %v", none)
	}
}

func TestStatusAndShimRoundTrip(t *testing.T) {
	root := t.TempDir()
	t.Setenv(ToolsDirEnv, root)
	old := lookPath
	defer func() { lookPath = old }()
	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }

	tl, _ := Lookup("gopls")
	if StatusOf(tl).State != Missing {
		t.Fatal("want missing")
	}
	lookPath = func(string) (string, error) { return "/usr/bin/gopls", nil }
	if st := StatusOf(tl); st.State != System || st.Path != "/usr/bin/gopls" {
		t.Fatalf("want system, got %+v", st)
	}
	if err := WriteShim(filepath.Join(root, "bin"), tl.Bin, tl.Name, []string{"/x/gopls"}); err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Join(root, "gopls"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "gopls", versionFile), []byte("latest\n"), 0o644)
	st := StatusOf(tl)
	if st.State != Managed || st.Version != "latest" {
		t.Fatalf("want managed, got %+v", st)
	}
	if err := Uninstall(tl); err != nil {
		t.Fatal(err)
	}
	if StatusOf(tl).State != System {
		t.Fatal("uninstall should fall back to system")
	}
}

func TestAskedPersistenceAndSuggest(t *testing.T) {
	t.Setenv(ToolsDirEnv, t.TempDir())
	asked := LoadAsked()
	if len(asked) != 0 {
		t.Fatal("fresh state must be empty")
	}
	none := func(Tool) bool { return false }
	tl, ok := SuggestFor("python", asked, none)
	if !ok || tl.Name != "pyright" {
		t.Fatalf("want pyright suggestion, got %v %v", tl.Name, ok)
	}
	asked["python"] = true
	if err := SaveAsked(asked); err != nil {
		t.Fatal(err)
	}
	if !LoadAsked()["python"] {
		t.Fatal("asked not persisted")
	}
	if _, ok := SuggestFor("python", LoadAsked(), none); ok {
		t.Fatal("must not ask twice")
	}
	if _, ok := SuggestFor("go", asked, func(Tool) bool { return true }); ok {
		t.Fatal("available server → no suggestion")
	}
	if _, ok := SuggestFor("markdown", asked, none); ok {
		t.Fatal("unknown filetype → no suggestion")
	}
}

func TestLuaServersTable(t *testing.T) {
	s := LuaServersTable()
	for _, want := range []string{
		`['gopls'] = { cmd = { 'gopls' }, filetypes = { 'go', 'gomod', 'gowork', 'gotmpl' }`,
		`['pyright'] = { cmd = { 'pyright-langserver', '--stdio' }`,
		`order = 1 }`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	if strings.Contains(s, "dlv") {
		t.Error("debug adapters must not be in the LSP table")
	}
	if LuaQuote(`a'b\c`) != `'a\'b\\c'` {
		t.Error("lua quoting")
	}
}
