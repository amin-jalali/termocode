package multirepo

import (
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fakeResolve(known map[string][2]string) Resolver {
	return func(dir string) (string, string, error) {
		v, ok := known[dir]
		if !ok {
			return "", "", errors.New("not a repository")
		}
		return v[0], v[1], nil
	}
}

func TestReposDedupesByCommonDir(t *testing.T) {
	res := fakeResolve(map[string][2]string{
		"/w/api":               {"/w/api", "/w/api/.git"},
		"/w/web":               {"/w/web", "/w/web/.git"},
		"/w/api.worktrees/fix": {"/w/api", "/w/api/.git"}, // a worktree of api
	})
	got := Repos([]string{"/w/api", "/w/nope", "/w/web", "/w/api.worktrees/fix"}, res)
	want := []Repo{
		{Name: "api", Main: "/w/api", Common: "/w/api/.git"},
		{Name: "web", Main: "/w/web", Common: "/w/web/.git"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Repos = %+v, want %+v", got, want)
	}
}

func TestReposNamesStayUnique(t *testing.T) {
	res := fakeResolve(map[string][2]string{
		"/a/app": {"/a/app", "/a/app/.git"},
		"/b/app": {"/b/app", "/b/app/.git"},
	})
	got := Repos([]string{"/a/app", "/b/app"}, res)
	if len(got) != 2 || got[0].Name != "app" || got[1].Name != "app-2" {
		t.Fatalf("names = %+v, want app and app-2", got)
	}
}

func TestPlanFoldersNeverCollide(t *testing.T) {
	repos := []Repo{{Name: "api", Main: "/w/api"}, {Name: "web", Main: "/w/web"}}
	// next to each repo: <repo>.worktrees/<branch>
	items := Plan(repos, "feat/login", "", "/home/u")
	if items[0].Folder != filepath.Join("/w/api.worktrees", "feat-login") || items[1].Folder != filepath.Join("/w/web.worktrees", "feat-login") {
		t.Fatalf("folders = %q %q", items[0].Folder, items[1].Folder)
	}
	// one shared folder (worktree_dir is absolute): the repo name is added
	items = Plan(repos, "feat/login", "/wt", "/home/u")
	if items[0].Folder != filepath.Join("/wt", "feat-login") || items[1].Folder != filepath.Join("/wt", "feat-login-web") {
		t.Fatalf("shared folders = %q %q", items[0].Folder, items[1].Folder)
	}
}

func TestCheckSaysWhyPerRepo(t *testing.T) {
	items := []Item{{Repo: Repo{Name: "api", Main: "/w/api"}, Folder: "/f/a"}, {Repo: Repo{Name: "web", Main: "/w/web"}, Folder: "/f/b"}}
	ok := Check(items, "feat/x", Probe{
		BranchName:   func(string, string) error { return nil },
		BranchExists: func(dir, _ string) bool { return dir == "/w/web" },
		FolderExists: func(string) bool { return false },
	})
	if ok || items[0].Err != "" || !strings.Contains(items[1].Err, "branch") {
		t.Fatalf("ok=%v errs=%q %q", ok, items[0].Err, items[1].Err)
	}
	items[1].Err = ""
	ok = Check(items, "feat/x", Probe{
		BranchName:   func(string, string) error { return nil },
		BranchExists: func(string, string) bool { return false },
		FolderExists: func(p string) bool { return p == "/f/a" },
	})
	if ok || !strings.Contains(items[0].Err, "folder") {
		t.Fatalf("folder clash not seen: %q", items[0].Err)
	}
}

func TestCreateIsAllOrNothing(t *testing.T) {
	items := []Item{
		{Repo: Repo{Name: "api", Main: "/w/api"}, Folder: "/f/api"},
		{Repo: Repo{Name: "web", Main: "/w/web"}, Folder: "/f/web"},
		{Repo: Repo{Name: "doc", Main: "/w/doc"}, Folder: "/f/doc"},
	}
	var added, undone []string
	err := Create(items, "feat/x", "", func(dir, folder, branch, ref string) error {
		if ref != "HEAD" || branch != "feat/x" {
			t.Fatalf("add got branch %q ref %q", branch, ref)
		}
		if dir == "/w/doc" {
			return errors.New("boom")
		}
		added = append(added, folder)
		return nil
	}, func(dir, folder, branch string) error {
		undone = append(undone, folder)
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "doc") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want the doc repo and git's words", err)
	}
	if !reflect.DeepEqual(added, []string{"/f/api", "/f/web"}) || !reflect.DeepEqual(undone, []string{"/f/web", "/f/api"}) {
		t.Fatalf("added %v undone %v: the made ones must go again, last first", added, undone)
	}
}

func TestCreateAllMade(t *testing.T) {
	items := []Item{{Repo: Repo{Name: "api", Main: "/w/api"}, Folder: "/f/api"}, {Repo: Repo{Name: "web", Main: "/w/web"}, Folder: "/f/web"}}
	n := 0
	err := Create(items, "feat/x", "main", func(_, _, _, ref string) error {
		if ref != "main" {
			t.Fatalf("ref %q", ref)
		}
		n++
		return nil
	}, func(string, string, string) error { t.Fatal("nothing to undo"); return nil })
	if err != nil || n != 2 {
		t.Fatalf("err %v n %d", err, n)
	}
}
