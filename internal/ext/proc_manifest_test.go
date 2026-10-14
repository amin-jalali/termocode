package ext

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Tier 2: a folder with no init.lua but an extension.json "run" is a
// process extension; "run" wins over an init.lua next to it.
func TestDiscoverProcessExtensions(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "py", "extension.json"), `{"name":"py","version":"0.1.0",
	  "run":{"command":"python3","args":["main.py"]},"runtime":"python",
	  "activationEvents":["onStartup"],
	  "contributes":{"commands":[{"id":"refresh","title":"Refresh"}]}}`)
	write(t, filepath.Join(dir, "both", "init.lua"), "-- lua")
	write(t, filepath.Join(dir, "both", "extension.json"), `{"run":{"command":"./bin"}}`)
	write(t, filepath.Join(dir, "norun", "extension.json"), `{"name":"x"}`)
	write(t, filepath.Join(dir, "emptycmd", "extension.json"), `{"run":{"command":""}}`)

	exts, skipped := Discover(dir)
	var names []string
	for _, e := range exts {
		names = append(names, e.Name)
	}
	if !reflect.DeepEqual(names, []string{"both", "py"}) {
		t.Fatalf("names = %v (skipped %v)", names, skipped)
	}
	both, py := exts[0], exts[1]
	if !both.IsProcess() || both.Init != "" || both.Problem == "" {
		t.Errorf("both = %+v: want a process extension that notes the ignored init.lua", both)
	}
	if !py.IsProcess() || py.Manifest.Run.Command != "python3" || !reflect.DeepEqual(py.Manifest.Run.Args, []string{"main.py"}) {
		t.Errorf("py = %+v", py)
	}
	if !py.Manifest.StartsAtStartup() || len(py.Manifest.Contributes.Commands) != 1 {
		t.Errorf("py manifest = %+v", py.Manifest)
	}
	if len(skipped) != 1 || !strings.HasPrefix(skipped[0], "emptycmd") {
		t.Errorf("skipped = %v", skipped)
	}
}

func TestManifestStartsAtStartup(t *testing.T) {
	cases := []struct {
		m    Manifest
		want bool
	}{
		{Manifest{}, true}, // nothing declared: it could never be reached otherwise
		{Manifest{ActivationEvents: []string{"onStartup"}}, true},
		{Manifest{ActivationEvents: []string{"*"}}, true},
		{Manifest{ActivationEvents: []string{"onCommand"}}, false},
		{Manifest{Contributes: Contributes{Commands: []Command{{ID: "a"}}}}, false},
		{Manifest{ActivationEvents: []string{"onSave"}}, false},
	}
	for i, c := range cases {
		if got := c.m.StartsAtStartup(); got != c.want {
			t.Errorf("%d: %+v StartsAtStartup = %v, want %v", i, c.m, got, c.want)
		}
	}
}

func TestManifestActivatesOn(t *testing.T) {
	m := Manifest{ActivationEvents: []string{"onSave", "onOpen"}}
	if !m.ActivatesOn("save") || !m.ActivatesOn("open") || m.ActivatesOn("changed") {
		t.Errorf("ActivatesOn wrong for %+v", m)
	}
}
