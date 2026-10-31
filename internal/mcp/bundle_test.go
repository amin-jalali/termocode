package mcp

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testManifest = `{
 "manifest_version": "0.2", "name": "notes-box", "display_name": "Notes Box", "version": "1.4.0",
 "description": "Your notes as tools.", "author": {"name": "Acme"},
 "server": {"type": "node", "entry_point": "server/index.js",
   "mcp_config": {"command": "node", "args": ["${__dirname}/server/index.js", "--dir=${user_config.notes_dir}"],
     "env": {"NOTES_KEY": "${user_config.api_key}", "HOME_DIR": "${HOME}"}}},
 "user_config": {
   "api_key": {"type": "string", "title": "API key", "sensitive": true, "required": true},
   "notes_dir": {"type": "directory", "title": "Notes folder", "default": "${HOME}/notes"}
 },
 "tools": [{"name": "find_note", "description": "Find a note"}]
}`

func makeBundle(t *testing.T, files map[string]string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "b.mcpb")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	return p
}

func TestOpenBundleReadsManifest(t *testing.T) {
	b, err := OpenBundle(makeBundle(t, map[string]string{"manifest.json": testManifest, "server/index.js": "//"}))
	if err != nil {
		t.Fatal(err)
	}
	m := b.Manifest
	if m.Name != "notes-box" || m.Version != "1.4.0" || m.Author.Name != "Acme" || len(m.Tools) != 1 {
		t.Fatalf("manifest %+v", m)
	}
	keys := b.ConfigKeys()
	if strings.Join(keys, ",") != "api_key,notes_dir" {
		t.Fatalf("keys %v", keys)
	}
	if _, err := OpenBundle(makeBundle(t, map[string]string{"x.txt": "no manifest"})); err == nil {
		t.Fatal("a bundle without manifest.json opened")
	}
	if _, err := OpenBundle(makeBundle(t, map[string]string{"manifest.json": `{"name":"x"}`})); err == nil {
		t.Fatal("a manifest without a command opened")
	}
}

func TestBundleRefusesZipSlip(t *testing.T) {
	p := makeBundle(t, map[string]string{"manifest.json": testManifest, "../evil.sh": "x"})
	b, err := OpenBundle(p)
	if err == nil {
		_, err = b.Install(t.TempDir())
	}
	if err == nil {
		t.Fatal("a ../ path was accepted")
	}
}

func TestBundleInstallAndEntry(t *testing.T) {
	b, err := OpenBundle(makeBundle(t, map[string]string{"manifest.json": testManifest, "server/index.js": "//js"}))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dir, err := b.Install(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "server", "index.js")); string(got) != "//js" {
		t.Fatalf("installed file %q", got)
	}
	if _, err := b.Entry(dir, map[string]string{}); err != nil {
		t.Fatalf("defaults should do: %v", err)
	}
	name, entry, err := b.EntryNamed(dir, map[string]string{"notes_dir": "/srv/notes"})
	if err != nil || name != "notes-box" {
		t.Fatalf("entry %q %v", name, err)
	}
	raw, _ := json.Marshal(entry)
	s := string(raw)
	for _, want := range []string{`"command":"node"`, dir + `/server/index.js`, `--dir=/srv/notes`,
		`"NOTES_KEY":"${secret:notes_box_api_key}"`, `"HOME_DIR":"${userHome}"`, `"bundle":{"name":"notes-box","version":"1.4.0"}`} {
		if !strings.Contains(s, want) {
			t.Errorf("entry %s lacks %s", s, want)
		}
	}
	if got := b.Secrets(); len(got) != 1 || got[0] != "notes_box_api_key" {
		t.Errorf("secrets %v", got)
	}
}
