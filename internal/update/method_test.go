package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func noOwner(string) (string, bool) { return "", false }

// linkedTempDir is a temp dir reached through a symlink, like the macOS
// temp dir (/var -> /private/var): Detect must still match it.
func linkedTempDir(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	return link
}

// resolved is p with its symlinks resolved: Detect reports and asks the
// package manager about the real file, as pacman -Qqo does too.
func resolved(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDetectBrew(t *testing.T) {
	root := t.TempDir()
	cellar := filepath.Join(root, "Cellar", "termocode", "0.1.0", "bin")
	_ = os.MkdirAll(cellar, 0o755)
	real := filepath.Join(cellar, "termocode")
	_ = os.WriteFile(real, []byte("x"), 0o755)
	bin := filepath.Join(root, "bin")
	_ = os.MkdirAll(bin, 0o755)
	link := filepath.Join(bin, "termocode")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	got := Detect(link, Env{Getenv: envOf(nil), Owner: noOwner})
	if got.Method != MethodBrew || got.Command != "brew upgrade termocode" {
		t.Fatalf("got %+v", got)
	}
	// HOMEBREW_PREFIX without a Cellar in the path (linuxbrew layouts).
	plain := filepath.Join(root, "prefix", "bin")
	_ = os.MkdirAll(plain, 0o755)
	exe := filepath.Join(plain, "termocode")
	_ = os.WriteFile(exe, []byte("x"), 0o755)
	got = Detect(exe, Env{Getenv: envOf(map[string]string{"HOMEBREW_PREFIX": filepath.Join(root, "prefix")}), Owner: noOwner})
	if got.Method != MethodBrew {
		t.Fatalf("HOMEBREW_PREFIX: got %+v", got)
	}
}

func TestDetectGoInstall(t *testing.T) {
	home := t.TempDir()
	for name, env := range map[string]map[string]string{
		"default GOPATH": {"HOME": home},
		"GOPATH":         {"GOPATH": filepath.Join(home, "gp") + string(os.PathListSeparator) + "/nope"},
		"GOBIN":          {"GOBIN": filepath.Join(home, "gobin")},
	} {
		var dir string
		switch {
		case env["GOBIN"] != "":
			dir = env["GOBIN"]
		case env["GOPATH"] != "":
			dir = filepath.Join(home, "gp", "bin")
		default:
			dir = filepath.Join(home, "go", "bin")
		}
		_ = os.MkdirAll(dir, 0o755)
		exe := filepath.Join(dir, "termocode")
		_ = os.WriteFile(exe, []byte("x"), 0o755)
		got := Detect(exe, Env{Getenv: envOf(env), Owner: noOwner})
		if got.Method != MethodGo || !strings.HasPrefix(got.Command, "go install github.com/amin-jalali/termocode/cmd/termocode@") {
			t.Errorf("%s: got %+v", name, got)
		}
	}
}

func TestDetectPacman(t *testing.T) {
	dir := linkedTempDir(t)
	exe := filepath.Join(dir, "termocode")
	_ = os.WriteFile(exe, []byte("x"), 0o755)
	real := resolved(t, exe)
	owner := func(p string) (string, bool) {
		if p == real {
			return "termocode-bin", true
		}
		return "", false
	}
	got := Detect(exe, Env{Getenv: envOf(nil), Owner: owner})
	if got.Method != MethodPackage || got.Package != "termocode-bin" || !strings.Contains(got.Command, "termocode-bin") {
		t.Fatalf("got %+v", got)
	}
}

func TestDetectSelf(t *testing.T) {
	dir := linkedTempDir(t)
	exe := filepath.Join(dir, "termocode")
	_ = os.WriteFile(exe, []byte("x"), 0o755)
	got := Detect(exe, Env{Getenv: envOf(nil), Owner: noOwner})
	if got.Method != MethodSelf || got.Path != resolved(t, exe) {
		t.Fatalf("got %+v", got)
	}
	// Nothing was left behind by the write probe.
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("probe left files: %v", ents)
	}
}

func TestDetectNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := filepath.Join(linkedTempDir(t), "bin")
	_ = os.MkdirAll(dir, 0o755)
	exe := filepath.Join(dir, "termocode")
	_ = os.WriteFile(exe, []byte("x"), 0o755)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	got := Detect(exe, Env{Getenv: envOf(nil), Owner: noOwner})
	if got.Method != MethodReadOnly || !strings.Contains(got.Command, "install.sh") || !strings.Contains(got.Command, "PREFIX="+filepath.Dir(resolved(t, dir))) {
		t.Fatalf("got %+v", got)
	}
}
