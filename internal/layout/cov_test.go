package layout

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsBuiltin(t *testing.T) {
	if !IsBuiltin(Builtins()[0].Name) {
		t.Fatal("a built-in name is not built-in")
	}
	if IsBuiltin("my-own-layout") {
		t.Fatal("an unknown name is built-in")
	}
}

// Sizes below zero are refused, and so is marshalling such a layout.
func TestValidateNegativeWidths(t *testing.T) {
	for name, l := range map[string]Layout{
		"sidebar": {Sidebar: &Sidebar{Width: -1}},
		"chat":    {Chat: &Chat{Width: -1}},
	} {
		err := l.Validate()
		if err == nil || !strings.Contains(err.Error(), "must not be negative") {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := l.Marshal(); err == nil {
			t.Fatalf("%s: Marshal accepted an invalid layout", name)
		}
	}
}

// A panel with the terminal tab but no terminal list says "Terminal".
func TestPreviewTerminalTabWithoutTerminals(t *testing.T) {
	text := strings.Join(Layout{Panel: &Panel{Open: true, Tabs: []string{"terminal"}}}.Preview(), "\n")
	if !strings.Contains(text, "Terminal") {
		t.Fatalf("preview: %q", text)
	}
}

func TestFindMissingLayout(t *testing.T) {
	if _, ok := testStore(t).Find("no-such-layout"); ok {
		t.Fatal("found a layout that does not exist")
	}
}

// Names that differ only in case keep a fixed order.
func TestListOrdersNamesThatDifferOnlyInCase(t *testing.T) {
	if caseInsensitiveFS(t) {
		t.Skip("this file system ignores case (the macOS default): abc and Abc are one file")
	}
	s := testStore(t)
	for _, n := range []string{"abc", "Abc"} {
		if _, err := s.Save(User, Layout{Name: n}); err != nil {
			t.Fatal(err)
		}
	}
	es, _ := s.List()
	got := names(es)
	if len(got) != 2 || got[0] != "Abc:user" || got[1] != "abc:user" {
		t.Fatalf("order %v", got)
	}
}

func TestSaveErrors(t *testing.T) {
	// No folder for the source.
	if _, err := (Store{}).Save(User, Layout{Name: "x"}); err == nil {
		t.Fatal("saved without a folder")
	}
	// An invalid layout is not written.
	s := testStore(t)
	if _, err := s.Save(User, Layout{Name: "bad", Sidebar: &Sidebar{Width: -3}}); err == nil {
		t.Fatal("saved an invalid layout")
	}
	if _, err := os.Stat(s.UserDir); err == nil {
		t.Fatal("made the folder for an invalid layout")
	}
	// The folder cannot be made: a file is in the way.
	root := t.TempDir()
	blocker := filepath.Join(root, "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := (Store{UserDir: filepath.Join(blocker, "layouts")}).Save(User, Layout{Name: "x"}); err == nil {
		t.Fatal("saved under a file")
	}
	// The folder cannot be written to.
	ro := filepath.Join(root, "ro")
	if err := os.Mkdir(ro, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(ro, 0o755) })
	if os.Geteuid() != 0 {
		if _, err := (Store{UserDir: ro}).Save(User, Layout{Name: "x"}); err == nil {
			t.Fatal("saved into a read-only folder")
		}
	}
}

// caseInsensitiveFS reports whether the temp folder's file system treats
// "a" and "A" as one name.
func caseInsensitiveFS(t *testing.T) bool {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := os.Stat(filepath.Join(dir, "A"))
	return err == nil
}
