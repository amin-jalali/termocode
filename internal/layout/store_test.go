package layout

import (
	"os"
	"path/filepath"
	"testing"
)

func testStore(t *testing.T) Store {
	t.Helper()
	root := t.TempDir()
	return Store{UserDir: filepath.Join(root, "cfg", "layouts"), ProjectDir: filepath.Join(root, "proj", ".termocode", "layouts")}
}

// names lists the entries that are not built-in.
func names(es []Entry) (out []string) {
	for _, e := range es {
		if e.Source == Builtin {
			continue
		}
		out = append(out, e.Name+":"+e.Source.String())
	}
	return out
}

func TestSaveThenListAndFind(t *testing.T) {
	s := testStore(t)
	if _, err := s.Save(User, Layout{Name: "zeta", Focus: "editor"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Save(Project, Layout{Name: "Alpha", Panel: &Panel{Open: true, Rows: 8}}); err != nil {
		t.Fatal(err)
	}
	es, broken := s.List()
	if len(broken) != 0 {
		t.Fatalf("broken: %v", broken)
	}
	got := names(es)
	if len(got) != 2 || got[0] != "Alpha:project" || got[1] != "zeta:user" {
		t.Fatalf("list = %v", got)
	}
	e, ok := s.Find("Alpha")
	if !ok || e.Panel == nil || e.Panel.Rows != 8 {
		t.Fatalf("find = %+v %v", e, ok)
	}
}

func TestProjectLayoutWinsOverUserOfTheSameName(t *testing.T) {
	s := testStore(t)
	s.Save(User, Layout{Name: "debug", Description: "mine"})
	s.Save(Project, Layout{Name: "debug", Description: "team"})
	e, ok := s.Find("debug")
	if !ok || e.Source != Project || e.Description != "team" {
		t.Fatalf("find = %+v", e)
	}
}

func TestListReportsBrokenFilesAndKeepsTheRest(t *testing.T) {
	s := testStore(t)
	s.Save(User, Layout{Name: "good"})
	if err := os.WriteFile(filepath.Join(s.UserDir, "bad.jsonc"), []byte(`{"focus":"moon"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(s.UserDir, "notes.txt"), []byte("x"), 0o644)
	es, broken := s.List()
	if g := names(es); len(g) != 1 || g[0] != "good:user" {
		t.Fatalf("entries = %v", names(es))
	}
	if len(broken) != 1 || filepath.Base(broken[0].Path) != "bad.jsonc" {
		t.Fatalf("broken = %+v", broken)
	}
}

func TestMissingFoldersMeanNoLayouts(t *testing.T) {
	es, broken := (Store{}).List()
	if len(names(es)) != 0 || len(broken) != 0 {
		t.Fatalf("%v %v", es, broken)
	}
	if _, err := (Store{}).Save(Project, Layout{Name: "x"}); err == nil {
		t.Fatal("saved without a folder")
	}
}

func TestNamesAreSafeFileNames(t *testing.T) {
	for _, bad := range []string{"", "../x", "a/b", ".hidden", "x\\y", "end.", "end ", "a\x00b",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if ValidName(bad) == nil {
			t.Errorf("%q accepted", bad)
		}
		if _, err := testStore(t).Save(User, Layout{Name: bad}); err == nil {
			t.Errorf("saved as %q", bad)
		}
	}
	for _, ok := range []string{"debug", "My Review 2", "a-b_c.d"} {
		if err := ValidName(ok); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
}

func TestSaveOverwritesAndLeavesNoTempFiles(t *testing.T) {
	s := testStore(t)
	s.Save(User, Layout{Name: "x", Focus: "editor"})
	s.Save(User, Layout{Name: "x", Focus: "sidebar"})
	e, _ := s.Find("x")
	if e.Focus != "sidebar" {
		t.Fatalf("focus = %q", e.Focus)
	}
	files, _ := os.ReadDir(s.UserDir)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
}

func TestDeleteRemovesTheFileOnly(t *testing.T) {
	s := testStore(t)
	s.Save(User, Layout{Name: "a"})
	s.Save(User, Layout{Name: "b"})
	e, _ := s.Find("a")
	if err := s.Delete(e); err != nil {
		t.Fatal(err)
	}
	es, _ := s.List()
	if g := names(es); len(g) != 1 || g[0] != "b:user" {
		t.Fatalf("list = %v", names(es))
	}
	if err := s.Delete(Entry{Layout: Layout{Name: "debug"}, Source: Builtin}); err == nil {
		t.Fatal("built-in deleted")
	}
}

func TestHasLooksOnlyInTheFolderAsked(t *testing.T) {
	s := testStore(t)
	s.Save(User, Layout{Name: "x"})
	if !s.Has(User, "x") || s.Has(Project, "x") || s.Has(User, "y") || s.Has(User, "../x") || s.Has(Builtin, "x") {
		t.Errorf("Has: user %v project %v other %v", s.Has(User, "x"), s.Has(Project, "x"), s.Has(User, "y"))
	}
	if (Store{}).Has(User, "x") {
		t.Error("Has without a folder")
	}
}
