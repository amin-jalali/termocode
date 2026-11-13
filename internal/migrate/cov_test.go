package migrate

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/theme"
)

func TestSnippetVariablesSortedAndKnown(t *testing.T) {
	vars := SnippetVariables()
	if len(vars) == 0 || !slices.IsSorted(vars) || !slices.Contains(vars, "TM_FILENAME_BASE") {
		t.Fatalf("variables %v", vars)
	}
}

// A theme named like a built-in (by display name, not by id) maps to it.
func TestMapThemeByDisplayName(t *testing.T) {
	all := theme.AvailableThemes()
	if len(all) == 0 {
		t.Skip("no themes")
	}
	for _, th := range all {
		if ThemeNames[strings.ToLower(th.Name)] != "" {
			continue
		}
		id, equiv := mapTheme(th.Name)
		if id != th.ID || equiv != th.ID {
			t.Fatalf("%q -> %q %q", th.Name, id, equiv)
		}
		return
	}
}

// ReadFile refuses an empty name, a folder, a missing file and a file that
// is not a known editor file.
func TestReadFileRefusalsMore(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := ReadFile("  "); err == nil {
		t.Fatal("empty name")
	}
	if _, _, err := ReadFile(dir); err == nil || !strings.Contains(err.Error(), "folder") {
		t.Fatalf("folder: %v", err)
	}
	if _, _, err := ReadFile(filepath.Join(dir, "gone.json")); err == nil || !strings.Contains(err.Error(), "cannot be read") {
		t.Fatalf("missing: %v", err)
	}
	odd := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(odd, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadFile(odd); err == nil || !strings.Contains(err.Error(), "notes.txt") {
		t.Fatalf("unknown file: %v", err)
	}
	if os.Geteuid() != 0 {
		noRead := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(noRead, []byte("{}"), 0o000); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ReadFile(noRead); err == nil || !strings.Contains(err.Error(), "cannot be read") {
			t.Fatalf("unreadable: %v", err)
		}
	}
}

// readInside names the reason a file cannot be read, never panics.
func TestReadInsideReasons(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, found, reason := readInside(root, "dir"); found || !strings.Contains(reason, "regular") {
		t.Fatalf("folder: %v %q", found, reason)
	}
	if _, found, reason := readInside(root, "missing"); found || reason != "" {
		t.Fatalf("missing: %v %q", found, reason)
	}
	if os.Geteuid() != 0 {
		p := filepath.Join(root, "locked.json")
		if err := os.WriteFile(p, []byte("{}"), 0o000); err != nil {
			t.Fatal(err)
		}
		if _, found, reason := readInside(root, "locked.json"); found || !strings.Contains(reason, "cannot be read") {
			t.Fatalf("locked: %v %q", found, reason)
		}
		sub := filepath.Join(root, "locked-dir")
		if err := os.Mkdir(sub, 0o000); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(sub, 0o755) })
		if _, found, reason := readInside(root, "locked-dir/x.json"); found || !strings.Contains(reason, "cannot be read") {
			t.Fatalf("locked dir: %v %q", found, reason)
		}
	}
	if _, reason := resolveInside(filepath.Join(root, "no-such-root"), "x"); reason != "" {
		t.Fatalf("missing root: %q", reason)
	}
}
