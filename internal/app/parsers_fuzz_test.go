package app

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// FuzzSessionFile reads any session.json. A state that loads writes and
// loads back the same.
func FuzzSessionFile(f *testing.F) {
	for _, s := range []string{
		`{"open_files":["a.go","term://x"],"cursors":{"a.go":[3,4]},"explorer_width":30}`,
		`{"layout":{"sidebar":true}}`, `null`, `[]`, `{"open_files":7}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		dir := t.TempDir()
		p := filepath.Join(dir, "session.json")
		if err := writePrivate(p, b); err != nil {
			t.Fatal(err)
		}
		s, ok := readSessionFile(p)
		if !ok {
			return
		}
		p2 := filepath.Join(dir, "again.json")
		writeSessionFile(p2, s)
		s2, ok := readSessionFile(p2)
		if !ok {
			t.Fatal("written session does not read back")
		}
		a, _ := json.Marshal(s)
		b2, _ := json.Marshal(s2)
		if !bytes.Equal(a, b2) {
			t.Fatalf("session round trip:\n%s\n%s", a, b2)
		}
	})
}

// FuzzSettingsBundle reads any settings export. Only safe file names come
// out, each as valid JSON.
func FuzzSettingsBundle(f *testing.F) {
	for _, s := range []string{
		`{"version":1,"files":{"config.json":{"theme":"dark"},"../evil.json":{},"session.json":{},"keymap.json":{"ctrl+s":"Save"}}}`,
		`{"version":99,"files":{}}`, `{"version":1}`, `{}`, `x`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		files, err := parseSettingsBundle(b)
		if err != nil {
			return
		}
		for name, body := range files {
			if !isExportableConfigFile(name) || strings.ContainsAny(name, `/\`) || name == ".." {
				t.Fatalf("unsafe name %q", name)
			}
			if !json.Valid(body) {
				t.Fatalf("%s is not JSON: %q", name, body)
			}
		}
	})
}

// FuzzSmallAppFiles reads any hot-exit backup, launch.json and
// snippets.json. Nothing panics; a backup names a one-line path, and
// user snippets survive save -> load.
func FuzzSmallAppFiles(f *testing.F) {
	for _, s := range []string{
		"/home/u/a.go\npackage a\n\nfunc x() {}\n",
		`{"version":"0.2.0","configurations":[{"name":"Run","type":"go","request":"launch","program":"${workspaceFolder}","env":{"A":"1"}}]} // c`,
		`{"go":{"iferr":["if err != nil {","\treturn err","}"],"pr":"fmt.Println($1)"},"*":{"bad trigger!":"x"}}`,
		"", "\n", `{"configurations":{}}`,
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		if hb, ok := parseHotBackup(b); ok && (hb.Path == "" || strings.Contains(hb.Path, "\n")) {
			t.Fatalf("bad backup path %q", hb.Path)
		}
		_, _ = parseLaunchJSON(b)
		u, err := parseUserSnippets(b)
		if err != nil {
			return
		}
		u2, err := parseUserSnippets(u.marshal())
		if err != nil {
			t.Fatalf("saved snippets do not load: %v", err)
		}
		if len(u) == 0 && len(u2) == 0 {
			return
		}
		norm := func(x userSnippets) userSnippets {
			out := userSnippets{}
			for sc, items := range x {
				if len(items) > 0 {
					out[sc] = items
				}
			}
			return out
		}
		if !reflect.DeepEqual(norm(u), norm(u2)) {
			t.Fatalf("snippets round trip:\n%v\n%v", u, u2)
		}
	})
}
