package migrate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/keymap"
)

func TestReadDispatchesEveryEditor(t *testing.T) {
	dir := t.TempDir()
	for _, id := range []string{"vscode", "vscodium", "cursor", "windsurf", "zed", "helix", "jetbrains", "sublime", "vim"} {
		if _, err := Read(Source{ID: id, Dir: dir}); err != nil {
			t.Errorf("%s on an empty folder: %v", id, err)
		}
	}
	if _, err := Read(Source{ID: "notepad", Dir: dir}); err == nil || !strings.Contains(err.Error(), `unknown editor "notepad"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestReadVSCodeBrokenFiles(t *testing.T) {
	cases := map[string]func(dir string){
		"corrupt settings":    func(d string) { write(t, filepath.Join(d, "settings.json"), `{"a": `) },
		"settings over 1 MB":  func(d string) { write(t, filepath.Join(d, "settings.json"), `{"a":"`+strings.Repeat("x", 1<<20)+`"}`) },
		"settings is folder":  func(d string) { os.MkdirAll(filepath.Join(d, "settings.json"), 0o755) },
		"corrupt keybindings": func(d string) { write(t, filepath.Join(d, "keybindings.json"), `[{"key":`) },
		"keybindings object":  func(d string) { write(t, filepath.Join(d, "keybindings.json"), `{"key":"x"}`) },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			setup(dir)
			if _, err := ReadVSCode(dir); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestReadVSCodeExtensionsFromHomeFolder(t *testing.T) {
	home := t.TempDir()
	user := filepath.Join(home, ".config", "Code", "User")
	write(t, filepath.Join(user, "settings.json"), `{}`)
	ext := filepath.Join(home, ".vscode-oss", "extensions")
	write(t, filepath.Join(ext, "golang.go-0.40.0", "package.json"), `{"publisher":"golang","name":"go"}`)
	write(t, filepath.Join(ext, "half-1.0", "package.json"), `{"name":"nopublisher"}`)
	write(t, filepath.Join(ext, "broken-1.0", "package.json"), `{`)
	write(t, filepath.Join(ext, "extensions.json"), `[]`) // a file, not an extension
	d, err := ReadVSCode(user)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Extensions) != 1 || d.Extensions[0] != "golang.go" {
		t.Fatalf("extensions %v", d.Extensions)
	}
}

func TestReadZedOddFiles(t *testing.T) {
	dir := t.TempDir()
	// A theme object (light/dark) is not a name: no theme, no setting.
	write(t, filepath.Join(dir, "settings.json"), `{"theme": {"mode":"system","dark":"One Dark"}, "tab_size": 2}`)
	write(t, filepath.Join(dir, "keymap.json"), `[{"bindings":{"ctrl-p":"file_finder::Toggle"}},{"context":"Editor"}]`)
	d, err := ReadZed(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Theme != "" || len(d.Settings) != 1 || len(d.Keys) != 1 {
		t.Fatalf("doc %+v", d)
	}
	write(t, filepath.Join(dir, "keymap.json"), `{"not":"a list"}`)
	if _, err := ReadZed(dir); err == nil {
		t.Fatal("keymap object must fail")
	}
	write(t, filepath.Join(dir, "settings.json"), `[1,2]`)
	if _, err := ReadZed(dir); err == nil {
		t.Fatal("settings list must fail")
	}
}

func TestReadHelixAndVimOddFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "config.toml"), "# comment\ntheme = \"onedark\"\n[editor]\nline-number = 'relative'\njunk line\n[keys.normal]\nC-s = \":w\"\n")
	d, err := ReadHelix(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d.Theme != "onedark" || len(d.Settings) != 1 || d.Settings[0].Name != "[editor].line-number" || len(d.Keys) != 1 || d.Keys[0].Key != "C-s" {
		t.Fatalf("doc %+v", d)
	}
	bad := t.TempDir()
	os.MkdirAll(filepath.Join(bad, "config.toml"), 0o755)
	if _, err := ReadHelix(bad); err == nil {
		t.Fatal("config.toml folder must fail")
	}

	vimrc := filepath.Join(t.TempDir(), ".vimrc")
	write(t, vimrc, "set number\nnmap x\nnmap <leader>w :w<CR>\n\" comment\n")
	d, err = ReadVim(vimrc)
	if err != nil || len(d.Keys) != 1 || d.Keys[0].Key != "<leader>w" || len(d.Settings) != 1 {
		t.Fatalf("vim doc %+v %v", d, err)
	}
	if _, err := ReadVim(t.TempDir()); err == nil {
		t.Fatal("vimrc folder must fail")
	}
}

func TestDetectPerPlatform(t *testing.T) {
	if Detect("", "linux", "") != nil {
		t.Fatal("no home, nothing found")
	}
	home := t.TempDir()
	write(t, filepath.Join(home, "Library", "Application Support", "Cursor", "User", "settings.json"), `{}`)
	write(t, filepath.Join(home, ".config", "zed", "keymap.json"), `[]`)
	write(t, filepath.Join(home, "AppData", "Roaming", "Code", "User", "keybindings.json"), `[]`)
	write(t, filepath.Join(home, ".vimrc"), "set nu\n")
	ids := func(srcs []Source) string {
		var out []string
		for _, s := range srcs {
			out = append(out, s.ID)
		}
		return strings.Join(out, ",")
	}
	if got := ids(Detect(home, "darwin", "")); got != "cursor,zed,vim" {
		t.Fatalf("darwin: %s", got)
	}
	if got := ids(Detect(home, "windows", "")); got != "vscode,zed,vim" {
		t.Fatalf("windows: %s", got)
	}
	xdg := filepath.Join(home, "xdg")
	write(t, filepath.Join(xdg, "helix", "languages.toml"), "")
	if got := ids(Detect(home, "linux", xdg)); got != "helix,vim" {
		t.Fatalf("linux with XDG: %s", got)
	}
	if runtime.GOOS == "linux" {
		if got := ids(Detect(home, "", "")); got != "zed,vim" {
			t.Fatalf("host default: %s", got)
		}
	}
}

func TestApplyRefusesBadKeymapAndOddValues(t *testing.T) {
	if _, err := Apply(Plan{Current: Current{KeymapExists: true}}); err == nil || !strings.Contains(err.Error(), "raw keymap.json is required") {
		t.Fatalf("err = %v", err)
	}
	if _, err := Apply(Plan{Current: Current{KeymapExists: true, KeymapRaw: []byte("[1,2]")}}); err == nil || !strings.Contains(err.Error(), "decode keymap") {
		t.Fatalf("err = %v", err)
	}
	row := ReviewRow{Kind: RowSetting, Apply: true, Target: "odd", Value: func() {}}
	if _, err := Apply(Plan{Rows: []ReviewRow{row}}); err == nil || !strings.Contains(err.Error(), "encode config") {
		t.Fatalf("err = %v", err)
	}
	// Overrides already in the file stay as written; new ones are added.
	out, err := Apply(Plan{
		Current: Current{KeymapExists: true, KeymapRaw: []byte(`{"ctrl+s":"Custom"}`),
			Overrides: map[string]keymap.Action{"ctrl+s": keymap.ActionSave, "ctrl+q": keymap.ActionQuit}},
		Rows: []ReviewRow{
			{Kind: RowExtension, Apply: true, Tools: []string{"gopls", "pyright"}},
			{Kind: RowExtension, Apply: true, Tools: []string{"gopls"}},
			{Kind: RowKey, Apply: true},     // no key: ignored
			{Kind: RowSetting, Apply: true}, // no target: ignored
			{Kind: RowTheme, Apply: false, Target: "x"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(out.Tools, ",") != "gopls,pyright" || out.Theme != "" || out.Config != nil {
		t.Fatalf("out %+v", out)
	}
	km := string(out.Keymap)
	if !strings.Contains(km, `"ctrl+s": "Custom"`) || !strings.Contains(km, `"ctrl+q": "`+keymap.ActionName(keymap.ActionQuit)+`"`) {
		t.Fatalf("keymap %s", km)
	}
}

func TestReadInsideRefusesOddFiles(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "big.json"), strings.Repeat("x", maxConfigFile+1))
	write(t, filepath.Join(root, "file"), "x")
	os.MkdirAll(filepath.Join(root, "folder"), 0o755)
	outside := filepath.Join(t.TempDir(), "secret.json")
	write(t, outside, "{}")
	if err := os.Symlink(outside, filepath.Join(root, "out.json")); err != nil {
		t.Skip("no symlinks here")
	}
	os.Symlink(filepath.Join(root, "nothing"), filepath.Join(root, "broken.json"))
	for rel, want := range map[string]string{
		"big.json":    "larger than 1 MB",
		"folder":      "not a regular file",
		"out.json":    "leads outside",
		"broken.json": "broken link",
		"file/below":  "cannot be read",
		"missing":     "",
	} {
		b, found, reason := readInside(root, rel)
		if found || b != nil || !strings.Contains(reason, want) || (want == "" && reason != "") {
			t.Errorf("%s: found=%v reason=%q, want %q", rel, found, reason, want)
		}
	}
	if _, reason := resolveInside(filepath.Join(root, "file"), "x"); !strings.Contains(reason, "cannot be read") {
		t.Errorf("root is a file: %q", reason)
	}
	names, links := listInside(root, ".")
	if strings.Join(links, ",") != "broken.json,out.json" || len(names) != 3 {
		t.Fatalf("names %v links %v", names, links)
	}
	if n, l := listInside(root, "file"); n != nil || l != nil {
		t.Fatal("listing a file gives nothing")
	}
}

func TestReadFileRefusesOddInput(t *testing.T) {
	dir := t.TempDir()
	mk := func(name, body string) string {
		p := filepath.Join(dir, name)
		write(t, p, body)
		return p
	}
	cases := []struct{ path, want string }{
		{"  ", "no file chosen"},
		{filepath.Join(dir, "missing.json"), "cannot be read: no such file"},
		{dir, "is a folder"},
		{mk("huge.json", "["+strings.Repeat(" ", maxConfigFile)+"]"), "larger than 1 MB"},
		{mk("notes.txt", "hi"), "not a supported editor file"},
		{mk("bad.xml", "<keymap"), "not valid XML"},
		{mk("other.xml", "<project/>"), "not a JetBrains keymap"},
		{mk("x.sublime-settings", "[1]"), "not valid JSON"},
		{mk("x.sublime-keymap", "{}"), "not valid JSON (a list"},
		{mk("scalar.json", "42"), "not a settings object"},
		{mk("list.json", "[1, 2]"), "not a settings object"},
		{mk("broken.json", "{"), "not valid JSON"},
	}
	for _, c := range cases {
		if _, _, err := ReadFile(c.path); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("ReadFile(%s): err = %v, want %q", filepath.Base(c.path), err, c.want)
		}
	}
}

func TestReadFileClassifiesByNameAndShape(t *testing.T) {
	dir := t.TempDir()
	for name, want := range map[string]struct{ body, id string }{
		"keymap.json":                      {"[]", "zed"},
		"keybindings.json":                 {"[]", "vscode"},
		"settings.json":                    {"{}", "vscode"},
		"zed-settings.json":                {`{"tab_size":2}`, "zed"},
		"languages.toml":                   {"", "helix"},
		"Package Control.sublime-settings": {`{"installed_packages":["LSP"]}`, "sublime"},
		"Default.sublime-keymap":           {`[]`, "sublime"},
	} {
		p := filepath.Join(dir, name)
		write(t, p, want.body)
		src, _, err := ReadFile(p)
		if err != nil || src.ID != want.id || src.Dir != dir {
			t.Errorf("%s: src %+v err %v, want %s", name, src, err, want.id)
		}
	}
}

func TestMapSettingValueShapes(t *testing.T) {
	cases := []struct {
		source, name string
		value        any
		target       string
		want         any
	}{
		{"vscode", "editor.tabSize", " 8 ", "tab_size", float64(8)},
		{"vscode", "editor.tabSize", "eight", "", nil},
		{"vscode", "editor.tabSize", true, "", nil},
		{"vscode", "editor.tabSize", 4, "tab_size", 4},
		{"vscode", "files.exclude", "**/.*", "", nil},
		{"vscode", "files.exclude", map[string]any{"**/.*": true}, "show_hidden", false},
		{"vscode", "files.exclude", map[string]any{"**/node_modules": true}, "show_hidden", true},
		{"vscode", "editor.wordWrap", float64(80), "", nil},
		{"vscode", "editor.wordWrap", "bounded", "word_wrap", true},
		{"helix", "editor.soft-wrap.enable", "false", "word_wrap", false},
		{"vscode", "files.autoSave", "afterDelay", "auto_save", true},
		{"vscode", "files.autoSave", "off", "auto_save", false},
		{"helix", "editor.auto-save", "true", "auto_save", true},
		{"vscode", "files.autoSave", float64(1), "", nil},
		{"vscode", "editor.fontSize", float64(14), "", nil},
	}
	for _, c := range cases {
		target, got, _ := mapSetting(c.source, Setting{c.name, c.value})
		if target != c.target || (c.target != "" && got != c.want) {
			t.Errorf("%s %s=%v: got %q %v, want %q %v", c.source, c.name, c.value, target, got, c.target, c.want)
		}
	}
}

func TestMapKeyOddKeys(t *testing.T) {
	cases := []struct {
		source string
		key    KeyBinding
		action string
		reason string
	}{
		{"vscode", KeyBinding{"cmd+p", "workbench.action.quickOpen"}, "QuickOpen", "Cmd mapped to Ctrl"},
		{"vscode", KeyBinding{"ctrl+k ctrl+s", "workbench.action.quickOpen"}, "", "two-key chord"},
		{"vscode", KeyBinding{"ctrl+nosuchkey", "workbench.action.quickOpen"}, "", "unsupported key syntax"},
		{"vscode", KeyBinding{"shift+p", "workbench.action.quickOpen"}, "", "needs ctrl or alt"},
		{"vscode", KeyBinding{"p", "workbench.action.quickOpen"}, "", "needs a modifier"},
		{"vscode", KeyBinding{"home", "workbench.action.quickOpen"}, "QuickOpen", ""},
		{"vscode", KeyBinding{"f5", "workbench.action.quickOpen"}, "QuickOpen", ""},
		{"cursor", KeyBinding{"ctrl+p", "workbench.action.quickOpen"}, "QuickOpen", ""},
		{"helix", KeyBinding{"A-C-x", "file_picker"}, "", ""},
		{"jetbrains", KeyBinding{"control shift", "GotoFile"}, "", ""},
		{"jetbrains", KeyBinding{"control A, control B", "GotoFile"}, "", "two-key chord"},
	}
	for _, c := range cases {
		_, action, equiv := mapKey(c.source, c.key)
		if c.action != "" && action != c.action {
			t.Errorf("%s %v: action %q (%s), want %q", c.source, c.key, action, equiv, c.action)
		}
		if c.reason != "" && !strings.Contains(equiv, c.reason) {
			t.Errorf("%s %v: %q lacks %q", c.source, c.key, equiv, c.reason)
		}
	}
}

func TestUnknownThemeGetsASuggestion(t *testing.T) {
	id, equiv := mapTheme("Totally Unknown Colors 9000")
	if id == "" || !strings.Contains(equiv, "suggestion") {
		t.Fatalf("id %q equiv %q", id, equiv)
	}
	p := Build(Source{ID: "vscode"}, Document{Theme: "Totally Unknown Colors 9000"}, Current{})
	for _, r := range p.Rows {
		if r.Kind == RowTheme && r.Apply {
			t.Fatal("a guessed theme must not be on by default")
		}
	}
}
