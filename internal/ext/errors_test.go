package ext

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestDirWithoutHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := Dir(); err == nil {
		t.Fatal("no XDG and no HOME must fail")
	}
	h := t.TempDir()
	t.Setenv("HOME", h)
	if d, err := Dir(); err != nil || d != filepath.Join(h, ".config", "termocode", "extensions") {
		t.Fatalf("%q %v", d, err)
	}
}

func TestParseRegistryAndPollBadFields(t *testing.T) {
	for _, bad := range []string{`[`, `{"commands": 3}`, `{"panels": "x"}`, `{"status": true}`, `{"extensions": 1}`} {
		if _, err := ParseRegistry(bad); err == nil {
			t.Errorf("ParseRegistry(%s) gave no error", bad)
		}
	}
	for _, bad := range []string{`{`, `{"lines": 3}`, `{"status": "x"}`} {
		if _, err := ParsePoll(bad); err == nil {
			t.Errorf("ParsePoll(%s) gave no error", bad)
		}
	}
	p, err := ParsePoll(`{"panel": 5, "lines": ["a"]}`)
	if err != nil || p.Panel != "" || len(p.Lines) != 1 {
		t.Fatalf("panel of the wrong type is ignored: %+v %v", p, err)
	}
	if p, err := ParsePoll("  "); err != nil || p.Lines != nil {
		t.Fatal("empty poll")
	}
}

func TestMarkupMergesAndKeepsUnknownTags(t *testing.T) {
	got := ParseMarkup("{{bold}}a{{/}}{{bold}}b{{/}}{{nope}}c{{/}}{{/}}d\x1b[31m")
	if len(got) != 2 || got[0].Text != "ab" || got[1].Text != "{{nope}}cd[31m" || got[1].Styles != nil {
		t.Fatalf("spans %+v", got)
	}
	if !IsToken("accent") || IsToken("nope") {
		t.Fatal("IsToken")
	}
	if PlainText("{{muted}}x{{/}}\ty") != "x    y" {
		t.Fatalf("plain %q", PlainText("{{muted}}x{{/}}\ty"))
	}
}

func TestScaffoldErrors(t *testing.T) {
	dir := t.TempDir()
	if _, err := Scaffold(dir, "bad/name"); err == nil {
		t.Fatal("invalid name")
	}
	if _, err := Scaffold(dir, "ok"); err != nil {
		t.Fatal(err)
	}
	if _, err := Scaffold(dir, "ok"); !errors.Is(err, ErrExists) {
		t.Fatalf("second scaffold: %v", err)
	}
	block := filepath.Join(t.TempDir(), "file")
	os.WriteFile(block, nil, 0o644)
	if _, err := Scaffold(block, "x"); err == nil {
		t.Fatal("scaffold under a file")
	}

	helpers := fstest.MapFS{PythonHelper: {Data: []byte("# helper\n")}}
	if _, err := ScaffoldProcess(dir, "", helpers); err == nil {
		t.Fatal("empty name")
	}
	if _, err := ScaffoldProcess(dir, "py", fstest.MapFS{}); err == nil {
		t.Fatal("missing helper")
	}
	if _, err := ScaffoldProcess(dir, "ok", helpers); !errors.Is(err, ErrExists) {
		t.Fatalf("existing folder: %v", err)
	}
	if _, err := ScaffoldProcess(block, "x", helpers); err == nil {
		t.Fatal("process scaffold under a file")
	}
	if os.Geteuid() != 0 {
		ro := t.TempDir()
		os.Chmod(ro, 0o555)
		t.Cleanup(func() { os.Chmod(ro, 0o755) })
		if _, err := ScaffoldProcess(ro, "x", helpers); err == nil {
			t.Fatal("read-only folder")
		}
	}
}

func TestInstallBundledSkipsAndErrors(t *testing.T) {
	src := fstest.MapFS{
		"one/init.lua":   {Data: []byte("-- one")},
		"one/embed.go":   {Data: []byte("package x")},
		"two/init.lua":   {Data: []byte("-- two")},
		"three/init.lua": {Data: []byte("-- three")},
		"README.md":      {Data: []byte("top-level file")},
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "two"), 0o755)            // already there
	os.MkdirAll(filepath.Join(dir, "three.disabled"), 0o755) // turned off by the user
	got, err := InstallBundled(src, dir)
	if err != nil || len(got) != 1 || got[0] != "one" {
		t.Fatalf("installed %v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "one", "embed.go")); !os.IsNotExist(err) {
		t.Fatal(".go file copied")
	}
	if _, err := os.Stat(filepath.Join(dir, "three")); !os.IsNotExist(err) {
		t.Fatal("disabled extension came back")
	}
	block := filepath.Join(t.TempDir(), "file")
	os.WriteFile(block, nil, 0o644)
	if _, err := InstallBundled(src, block); err == nil {
		t.Fatal("install under a file")
	}
	if _, err := InstallBundled(fstest.MapFS{"x": {Data: []byte("not a dir")}}, t.TempDir()); err != nil {
		t.Fatalf("only files: %v", err)
	}
	if os.Geteuid() != 0 {
		ro := t.TempDir()
		os.Chmod(ro, 0o555)
		t.Cleanup(func() { os.Chmod(ro, 0o755) })
		if got, err := InstallBundled(src, ro); err == nil || len(got) != 0 {
			t.Fatalf("read-only target: %v %v", got, err)
		}
	}
}
