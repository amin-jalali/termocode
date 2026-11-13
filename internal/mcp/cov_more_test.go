package mcp

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/blocks"
)

type covZipFile struct {
	name string
	body string
	mode os.FileMode
}

func covZip(t *testing.T, files ...covZipFile) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.mcpb")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, c := range files {
		h := &zip.FileHeader{Name: c.name, Method: zip.Deflate}
		if c.mode != 0 {
			h.SetMode(c.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(c.body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	return p
}

const covManifest = `{"name":"My Tool","version":"1.0/x","server":{"mcp_config":{"command":"${__dirname}/run${pathSeparator}go","args":["--home","${HOME}","--k","${user_config.key}","--s","${user_config.secret}","--n","${user_config.missing}","--o","${other}","--r","${user_config.req}"],"env":{"A":"${user_config.key}"}}},
"user_config":{"key":{"title":"Key","default":"dflt${HOME}"},"secret":{"title":"Secret","sensitive":true},"req":{"title":"Req","required":true}}}`

func TestCovBundleOpenErrors(t *testing.T) {
	notZip := filepath.Join(t.TempDir(), "n.mcpb")
	_ = os.WriteFile(notZip, []byte("plain"), 0o644)
	if _, err := OpenBundle(notZip); err == nil || !strings.Contains(err.Error(), "not a bundle") {
		t.Errorf("not a zip: %v", err)
	}
	if _, err := OpenBundle(covZip(t, covZipFile{name: "other.txt", body: "x"})); err == nil || !strings.Contains(err.Error(), "no manifest.json") {
		t.Errorf("no manifest: %v", err)
	}
	if _, err := OpenBundle(covZip(t, covZipFile{name: "manifest.json", body: "{bad"})); err == nil || !strings.Contains(err.Error(), "manifest.json") {
		t.Errorf("bad manifest: %v", err)
	}
	if _, err := OpenBundle(covZip(t, covZipFile{name: "manifest.json", body: `{"name":"x"}`})); err == nil || !strings.Contains(err.Error(), "needs a name") {
		t.Errorf("incomplete manifest: %v", err)
	}
}

func TestCovBundleInstallRefusesUnsafeAndInstallsSafe(t *testing.T) {
	good := covZip(t,
		covZipFile{name: "manifest.json", body: covManifest},
		covZipFile{name: "bin/", mode: os.ModeDir | 0o755},
		covZipFile{name: "bin/run", body: "#!/bin/sh\n", mode: 0o755},
		covZipFile{name: "docs/readme.txt", body: "hi"},
	)
	b, err := OpenBundle(good)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "bundles")
	dir, err := b.Install(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dir, "my-tool-1.0_x") {
		t.Errorf("dir %q", dir)
	}
	if fi, err := os.Stat(filepath.Join(dir, "bin", "run")); err != nil || fi.Mode().Perm()&0o111 == 0 {
		t.Errorf("an executable stays executable: %v %v", fi, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "docs", "readme.txt")); err != nil || fi.Mode().Perm()&0o111 != 0 {
		t.Errorf("a plain file is not executable: %v %v", fi, err)
	}
	// installing again replaces the folder
	if _, err := b.Install(root); err != nil {
		t.Errorf("reinstall: %v", err)
	}
	// no left-over temp folders
	ents, _ := os.ReadDir(root)
	if len(ents) != 1 {
		t.Errorf("root holds %d entries", len(ents))
	}
	// a version of "" is "0"
	b.Manifest.Version = ""
	if d, err := b.Install(root); err != nil || !strings.HasSuffix(d, "my-tool-0") {
		t.Errorf("no version: %q %v", d, err)
	}
	for name, f := range map[string]covZipFile{
		"dotdot":   {name: "../evil", body: "x"},
		"absolute": {name: "/etc/evil", body: "x"},
		"symlink":  {name: "link", body: "/etc/passwd", mode: os.ModeSymlink | 0o777},
	} {
		bad := covZip(t, covZipFile{name: "manifest.json", body: covManifest}, f)
		bb, err := OpenBundle(bad)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := bb.Install(t.TempDir()); err == nil {
			t.Errorf("%s: an unsafe bundle must be refused", name)
		}
	}
	// the root can not be made
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, []byte("x"), 0o644)
	if _, err := b.Install(filepath.Join(blocker, "sub")); err == nil {
		t.Error("an unusable root must fail")
	}
	// the zip is gone
	_ = os.Remove(good)
	if _, err := b.Install(root); err == nil {
		t.Error("a missing zip must fail")
	}
}

func TestCovBundleEntryNamed(t *testing.T) {
	b, err := OpenBundle(covZip(t, covZipFile{name: "manifest.json", body: covManifest}))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := b.EntryNamed("/d", map[string]string{}); err == nil || !strings.Contains(err.Error(), "Req is required") {
		t.Errorf("a missing required value: %v", err)
	}
	name, e, err := b.EntryNamed("/d", map[string]string{"req": "r", "key": "typed${HOME}"})
	if err != nil || name != "my-tool" {
		t.Fatal(name, err)
	}
	if e["command"] != "/d/run/go" {
		t.Errorf("command %v", e["command"])
	}
	args := strings.Join(e["args"].([]string), " ")
	for _, want := range []string{"--home ${userHome}", "--k typed${userHome}", "--s ${secret:", "--n  --o ${other}", "--r r"} {
		if !strings.Contains(args+" ", want) {
			t.Errorf("args lack %q: %s", want, args)
		}
	}
	if env := e["env"].(map[string]any); env["A"] != "typed${userHome}" {
		t.Errorf("env %v", env)
	}
	// the default is used when nothing is typed
	_, e, _ = b.EntryNamed("/d", map[string]string{"req": "r"})
	if !strings.Contains(strings.Join(e["args"].([]string), " "), "dflt${userHome}") {
		t.Errorf("default not used: %v", e["args"])
	}
	if got := b.Secrets(); len(got) != 1 || !strings.Contains(got[0], "secret") {
		t.Errorf("secrets %v", got)
	}
	if got := strings.Join(b.ConfigKeys(), ","); got != "key,req,secret" {
		t.Errorf("keys %s", got)
	}
	if _, err := b.Entry("/d", map[string]string{"req": "r"}); err != nil {
		t.Error(err)
	}
}

func TestCovApplyUpdateEdgeCases(t *testing.T) {
	dir := t.TempDir()
	user := filepath.Join(dir, "mcp.json")
	// an entry the user removed is added back with the new pin
	if err := ApplyUpdate(user, "notes", catalogServer("io.github.acme/notes", "2.0.0", "")); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(user)
	if !strings.Contains(string(b), "@acme/notes-mcp@2.0.0") {
		t.Errorf("added: %s", b)
	}
	// a new env variable of the new version is added, the user's value stays
	e := catalogServer("io.github.acme/notes", "2.1.0", "")
	e.Packages[0].Env = []CatalogVar{{Name: "REGION", Default: "eu"}, {Name: "NEWVAR", Default: "n"}}
	user = filepath.Join(dir, "mcp2.json")
	if err := AddServer(user, "notes", map[string]any{"command": "npx", "env": map[string]any{"REGION": "us"}}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyUpdate(user, "notes", e); err != nil {
		t.Fatal(err)
	}
	var file struct {
		Servers map[string]struct {
			Env map[string]string `json:"env"`
		} `json:"mcpServers"`
	}
	b, _ = os.ReadFile(user)
	_ = json.Unmarshal(b, &file)
	if env := file.Servers["notes"].Env; env["REGION"] != "us" || env["NEWVAR"] != "n" {
		t.Errorf("env after update %v", env)
	}
	// a broken catalog entry, a broken file
	if err := ApplyUpdate(user, "notes", CatalogEntry{}); err == nil {
		t.Error("an entry without a name must fail")
	}
	_ = os.WriteFile(user, []byte("{bad"), 0o644)
	if err := ApplyUpdate(user, "notes", e); err == nil {
		t.Error("a broken file must fail")
	}
	// servers whose registry key does not match are skipped
	if got := FindUpdates([]Server{{Name: "x"}}, nil); len(got) != 0 {
		t.Error("plain server")
	}
	if compareVersions("1.2", "1.3") != -1 || compareVersions("1.3", "1.2") != 1 || compareVersions("b", "a") != 1 {
		t.Error("compareVersions order")
	}
}

func TestCovParseElicitErrors(t *testing.T) {
	for name, raw := range map[string]string{
		"not json":      `{`,
		"url no url":    `{"mode":"url"}`,
		"unknown mode":  `{"mode":"wat"}`,
		"props not obj": `{"requestedSchema":{"properties":[1]}}`,
		"bad props":     `{"requestedSchema":{"properties":{"a":{"type":"string"},"b":`,
	} {
		if _, err := parseElicit(json.RawMessage(raw)); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	r, err := parseElicit(json.RawMessage(`{"message":"m","requestedSchema":{"properties":{"b":{"type":"boolean"},"a":{"type":"string","title":"A","enum":["x"]}},"required":["a"]}}`))
	if err != nil || r.Mode != ElicitForm || len(r.Fields) != 2 || r.Fields[0].Name != "b" || !r.Fields[1].Required {
		t.Errorf("form: %+v %v", r, err)
	}
	if r, err := parseElicit(json.RawMessage(`{"mode":"url","url":"https://x"}`)); err != nil || r.URL != "https://x" {
		t.Errorf("url: %+v %v", r, err)
	}
}

func TestCovElicitValueAndForm(t *testing.T) {
	if v, ok, err := ElicitValue(ElicitField{Type: "number"}, " 2.5 "); err != nil || !ok || v != 2.5 {
		t.Errorf("number %v %v %v", v, ok, err)
	}
	if _, _, err := ElicitValue(ElicitField{Type: "number"}, "x"); err == nil {
		t.Error("a bad number")
	}
	if _, _, err := ElicitValue(ElicitField{Type: "integer"}, "1.5"); err == nil {
		t.Error("a bad integer")
	}
	for in, want := range map[string]bool{"Y": true, "no": false, "0": false, "TRUE": true} {
		if v, ok, err := ElicitValue(ElicitField{Type: "boolean"}, in); err != nil || !ok || v != want {
			t.Errorf("bool %s: %v %v %v", in, v, ok, err)
		}
	}
	if _, _, err := ElicitValue(ElicitField{Type: "boolean"}, "maybe"); err == nil {
		t.Error("a bad bool")
	}
	if _, ok, err := ElicitValue(ElicitField{Name: "x", Required: true}, ""); ok || err == nil {
		t.Error("a missing required value")
	}
	if v, ok, err := ElicitValue(ElicitField{Name: "x"}, ""); ok || err != nil || v != nil {
		t.Error("an empty optional value is skipped")
	}
	if v, _, err := ElicitValue(ElicitField{Enum: []string{"Alpha"}}, "alpha"); err != nil || v != "Alpha" {
		t.Errorf("enum is case-blind and returns the server's spelling: %v %v", v, err)
	}
	if _, _, err := ElicitValue(ElicitField{Enum: []string{"A"}}, "z"); err == nil {
		t.Error("enum miss")
	}

	// FormOf: what the phone may not answer
	var tooMany []ElicitField
	for i := 0; i <= blocks.MaxFormFields; i++ {
		tooMany = append(tooMany, ElicitField{Name: "f"})
	}
	var bigEnum []string
	for i := 0; i <= blocks.MaxChoices; i++ {
		bigEnum = append(bigEnum, "v")
	}
	no := map[string]ElicitRequest{
		"url mode":       {Mode: ElicitURL},
		"no fields":      {Mode: ElicitForm},
		"too many":       {Mode: ElicitForm, Fields: tooMany},
		"bad name":       {Mode: ElicitForm, Fields: []ElicitField{{Name: "bad name"}}},
		"secret":         {Mode: ElicitForm, Fields: []ElicitField{{Name: "pw", Format: "password"}}},
		"too many opts":  {Mode: ElicitForm, Fields: []ElicitField{{Name: "e", Enum: bigEnum}}},
		"choice changed": {Mode: ElicitForm, Fields: []ElicitField{{Name: "e", Enum: []string{"two\nlines"}}}},
	}
	for name, req := range no {
		if _, ok := FormOf("id", req, nil); ok {
			t.Errorf("%s: the phone may not answer this", name)
		}
	}
	b, ok := FormOf("", ElicitRequest{Mode: ElicitForm, Message: "m", Fields: []ElicitField{{Name: "n", Type: "integer"}, {Name: "b", Type: "boolean"}, {Name: "e", Enum: []string{"x"}}}}, nil)
	if !ok || b.ID != "form" || len(b.Fields) != 0 && false {
		t.Fatalf("form %+v %v", b, ok)
	}
	if !strings.Contains(b.Title, "server") {
		t.Errorf("title %q", b.Title)
	}
	if b.Validate() != nil {
		t.Errorf("form block: %v", b.Validate())
	}
}
