package svcinstall

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Spec (MOB-8): Installed lists the workspaces of the service files
// termocode wrote (Linux units and macOS agents), escapes undone; a
// file without the marker is not ours and is skipped.
func TestInstalledListsOurServices(t *testing.T) {
	for _, goos := range []string{"linux", "darwin"} {
		t.Run(goos, func(t *testing.T) {
			home := t.TempDir()
			for _, ws := range []string{"/home/u/src/app", "/home/u/50%$ & <x>"} {
				f, err := Render(Spec{GOOS: goos, Home: home, Exe: "/usr/bin/termocode", Workspace: ws})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f.Path, []byte(f.Content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			f, _ := Render(Spec{GOOS: goos, Home: home, Exe: "/usr/bin/termocode", Workspace: "/home/u/foreign"})
			foreign := filepath.Join(filepath.Dir(f.Path), filepath.Base(f.Path)[:len("termocode-")]+"ffffffff"+filepath.Ext(f.Path))
			if goos == "darwin" {
				foreign = filepath.Join(filepath.Dir(f.Path), "dev.termocode.ffffffff.plist")
			}
			if err := os.WriteFile(foreign, []byte("ExecStart=\"/x\" __session-server \"/home/u/foreign\"\n<string>__session-server</string><string>/home/u/foreign</string>"), 0o644); err != nil {
				t.Fatal(err)
			}
			got := Installed(goos, home)
			want := []string{"/home/u/50%$ & <x>", "/home/u/src/app"}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("Installed = %q, want %q", got, want)
			}
		})
	}
	if got := Installed("windows", t.TempDir()); got != nil {
		t.Errorf("windows: %q", got)
	}
	if got := Installed("linux", t.TempDir()); got != nil {
		t.Errorf("no services: %q", got)
	}
}
