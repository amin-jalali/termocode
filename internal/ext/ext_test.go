package ext

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"termocode/extensions"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDirHonoursXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/x/cfg")
	d, err := Dir()
	if err != nil || d != "/x/cfg/termocode/extensions" {
		t.Fatalf("Dir() = %q, %v", d, err)
	}
}

func TestDiscover(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "b-good", "init.lua"), "-- ok")
	write(t, filepath.Join(dir, "b-good", "extension.json"), `{"name":"b","version":"1.2.3"}`)
	write(t, filepath.Join(dir, "a-badjson", "init.lua"), "-- ok")
	write(t, filepath.Join(dir, "a-badjson", "extension.json"), `{not json`)
	write(t, filepath.Join(dir, "no-init", "readme.md"), "x")
	write(t, filepath.Join(dir, ".hidden", "init.lua"), "x")
	write(t, filepath.Join(dir, "off.disabled", "init.lua"), "x")
	write(t, filepath.Join(dir, "bad name", "init.lua"), "x")
	write(t, filepath.Join(dir, "file.lua"), "x")

	exts, skipped := Discover(dir)
	var names []string
	for _, e := range exts {
		names = append(names, e.Name)
	}
	if !reflect.DeepEqual(names, []string{"a-badjson", "b-good"}) {
		t.Fatalf("names = %v", names)
	}
	if exts[0].Problem == "" || exts[0].Version != "" {
		t.Errorf("bad manifest should be a problem with empty version: %+v", exts[0])
	}
	if exts[1].Version != "1.2.3" || exts[1].Init != filepath.Join(dir, "b-good", "init.lua") {
		t.Errorf("good ext = %+v", exts[1])
	}
	if len(skipped) != 1 || !strings.HasPrefix(skipped[0], "bad name") {
		t.Errorf("skipped = %v", skipped)
	}
	if e, s := Discover(filepath.Join(dir, "missing")); e != nil || s != nil {
		t.Errorf("missing dir should be empty, got %v %v", e, s)
	}
}

func TestParseRegistryToleratesLuaEmptyTables(t *testing.T) {
	r, err := ParseRegistry(`{"commands":{},"panels":[{"id":"x.p","title":"P","icon":"","ext":"x"}],"status":null,"extensions":[{"name":"x","version":"","ok":true,"error":""}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Commands) != 0 || len(r.Panels) != 1 || r.Panels[0].ID != "x.p" || !r.Extensions[0].OK {
		t.Fatalf("registry = %+v", r)
	}
	if _, err := ParseRegistry("{oops"); err == nil {
		t.Error("want error for bad JSON")
	}
	if r, err := ParseRegistry(""); err != nil || len(r.Commands) != 0 {
		t.Errorf("empty = %+v, %v", r, err)
	}
}

func TestParsePoll(t *testing.T) {
	p, err := ParsePoll(`{"panel":"a.b","lines":["x","{{muted}}y{{/}}"],"status":[{"id":"s","text":"3"}]}`)
	if err != nil || p.Panel != "a.b" || len(p.Lines) != 2 || p.Status[0].Text != "3" {
		t.Fatalf("poll = %+v, %v", p, err)
	}
	p, err = ParsePoll(`{"status":{}}`)
	if err != nil || p.Panel != "" || p.Lines != nil || p.Status != nil {
		t.Fatalf("empty poll = %+v, %v", p, err)
	}
}

func TestParseMarkup(t *testing.T) {
	cases := []struct {
		in   string
		want []Span
	}{
		{"plain", []Span{{Text: "plain"}}},
		{"{{error}}bad{{/}} ok", []Span{{Text: "bad", Styles: []string{"error"}}, {Text: " ok"}}},
		{"{{bold}}a{{muted}}b{{/}}c{{/}}d", []Span{
			{Text: "a", Styles: []string{"bold"}},
			{Text: "b", Styles: []string{"bold", "muted"}},
			{Text: "c", Styles: []string{"bold"}},
			{Text: "d"},
		}},
		// unknown names are literal text
		{"GET {{host}}/x", []Span{{Text: "GET {{host}}/x"}}},
		{"{{error nope}}x", []Span{{Text: "{{error nope}}x"}}},
		// escape
		{"a {{{{bold}} b", []Span{{Text: "a {{bold}} b"}}},
		// unclosed tag, stray close
		{"{{/}}x{{accent}}y", []Span{{Text: "x"}, {Text: "y", Styles: []string{"accent"}}}},
		// control bytes dropped, tab expanded
		{"a\x1b[31mb\tc", []Span{{Text: "a[31mb    c"}}},
		{"{{ green  bold }}ok", []Span{{Text: "ok", Styles: []string{"green", "bold"}}}},
	}
	for _, c := range cases {
		got := ParseMarkup(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("ParseMarkup(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
	if PlainText("{{bold}}x{{/}} {{y}}") != "x {{y}}" {
		t.Errorf("PlainText = %q", PlainText("{{bold}}x{{/}} {{y}}"))
	}
}

func TestRenderHasNoRawEscapesFromInput(t *testing.T) {
	out := Render("{{error}}x\x1b]0;evil\x07{{/}}", 0, nil)
	if strings.Contains(out, "\x1b]0") || strings.Contains(out, "\x07") {
		t.Fatalf("control sequence leaked: %q", out)
	}
	for _, tok := range Tokens {
		_ = Style([]string{tok}, 0, nil) // every token must be handled
		if _, ok := tokenColor(tok); !ok {
			switch tok {
			case "bold", "italic", "underline", "faint":
			default:
				t.Errorf("token %q has no colour", tok)
			}
		}
	}
}

func TestScaffold(t *testing.T) {
	dir := t.TempDir()
	p, err := Scaffold(dir, "hello")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "hello.hello") || strings.Contains(string(b), "NAME") {
		t.Errorf("template not filled:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "hello", ManifestFile)); err != nil {
		t.Error(err)
	}
	if _, err := Scaffold(dir, "hello"); err != ErrExists {
		t.Errorf("second scaffold err = %v, want ErrExists", err)
	}
	if _, err := Scaffold(dir, "../evil"); err == nil {
		t.Error("path traversal name accepted")
	}
	exts, _ := Discover(dir)
	if len(exts) != 1 || exts[0].Version != "0.1.0" {
		t.Errorf("discover after scaffold = %+v", exts)
	}
}

func TestInstallBundled(t *testing.T) {
	dir := t.TempDir()
	// Pre-existing folder must be left alone.
	write(t, filepath.Join(dir, "word-count", "init.lua"), "-- mine")
	names, err := InstallBundled(extensions.FS, dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"open-in-github", "rest-client", "todo-tree"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("installed = %v, want %v", names, want)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "word-count", "init.lua")); string(b) != "-- mine" {
		t.Error("existing extension was overwritten")
	}
	if _, err := os.Stat(filepath.Join(dir, "rest-client", "lua", "rest_client", "parser.lua")); err != nil {
		t.Error("nested module not copied:", err)
	}
	exts, skipped := Discover(dir)
	if len(exts) != 4 || len(skipped) != 0 {
		t.Errorf("discover = %d exts, skipped %v", len(exts), skipped)
	}
	for _, e := range exts {
		if e.Problem != "" {
			t.Errorf("%s: %s", e.Name, e.Problem)
		}
	}
	again, err := InstallBundled(extensions.FS, dir)
	if err != nil || len(again) != 0 {
		t.Errorf("second install = %v, %v", again, err)
	}
}
