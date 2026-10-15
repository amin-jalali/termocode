package ext

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/extensions"
)

func TestScaffoldProcess(t *testing.T) {
	dir := t.TempDir()
	mainPath, err := ScaffoldProcess(dir, "pyx", extensions.FS)
	if err != nil {
		t.Fatal(err)
	}
	if mainPath != filepath.Join(dir, "pyx", "main.py") {
		t.Errorf("main = %s", mainPath)
	}
	b, _ := os.ReadFile(mainPath)
	if !strings.Contains(string(b), "import termocode_ext") || strings.Contains(string(b), "NAME") {
		t.Errorf("main.py:\n%s", b)
	}
	helper, _ := os.ReadFile(filepath.Join(dir, "pyx", "termocode_ext.py"))
	sample, _ := extensions.FS.ReadFile(PythonHelper)
	if len(helper) == 0 || string(helper) != string(sample) {
		t.Error("termocode_ext.py is not the sample's helper")
	}
	exts, skipped := Discover(dir)
	if len(exts) != 1 || !exts[0].IsProcess() || exts[0].Problem != "" || len(skipped) != 0 {
		t.Fatalf("discover = %+v, %v", exts, skipped)
	}
	if r := exts[0].Manifest.Run; r.Command != "python3" || !exts[0].Manifest.StartsAtStartup() {
		t.Errorf("manifest = %+v", exts[0].Manifest)
	}
	if _, err := ScaffoldProcess(dir, "pyx", extensions.FS); !errors.Is(err, ErrExists) {
		t.Errorf("second scaffold = %v, want ErrExists", err)
	}
	if _, err := ScaffoldProcess(dir, "bad name", extensions.FS); err == nil {
		t.Error("bad name accepted")
	}
}
