package app

import (
	"os"
	"path/filepath"
	"testing"
)

// I + C: the commands in commands.json are listed as a built-in extension
// ("user-commands"). The Go reader stays: an edit shows at once, with no
// reload and before the extensions have loaded.
func TestUserCommandsListAsABuiltInExtension(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	m := procModel(t)
	m.extRebuild()
	for _, x := range m.ext.reg.Extensions {
		if x.Name == "user-commands" {
			t.Fatal("user-commands is listed without a commands.json")
		}
	}
	dir := filepath.Join(cfg, "termocode")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "commands.json"), []byte(`[{"id":"hi","title":"Hi","cmd":"echo hi"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	m.extRebuild() // no Extensions: Reload needed
	var found bool
	for _, x := range m.ext.reg.Extensions {
		if x.Name == "user-commands" {
			found = true
			if x.Version != "built-in" || !x.OK {
				t.Errorf("built-in row = %+v", x)
			}
		}
	}
	if !found {
		t.Fatal("commands.json is not listed as the built-in user-commands extension")
	}
}
