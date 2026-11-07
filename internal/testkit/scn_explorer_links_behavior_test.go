package testkit

import (
	"os"
	"path/filepath"
	"testing"
)

// SCN-18: symbolic links in the explorer. (Permission denied on rename
// and delete is SCN-7, scn_explorer_perm_behavior_test.go; rename of a
// git-tracked file is SCN-4, scn_rename_git_test.go.)

// linkFiles starts the app on the text workspace with three links:
// linked → notes (a folder), link.txt → a.txt and broken.txt → a file
// that does not exist.
func linkFiles(t *testing.T) *Driver {
	t.Helper()
	return New(t, Options{Files: textFiles(), Setup: func(t testing.TB, dir string) {
		for name, target := range map[string]string{"linked": "notes", "link.txt": "a.txt", "broken.txt": "missing.txt"} {
			if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
				t.Skipf("no symlinks here: %v", err)
			}
		}
	}})
}

// Spec: Given a link "linked" to the folder notes,
// when the user clicks it in the explorer,
// then it opens like a folder and lists todo.md and done.md.
func TestSCN18LinkedFolderOpensLikeAFolder(t *testing.T) {
	d := linkFiles(t)
	if explorerShows(d, "todo.md") {
		t.Fatalf("todo.md shown before any folder is open:\n%s", screen(d))
	}
	clickExplorer(d, "linked")
	if !waitExplorer(d, "todo.md", true) || !explorerShows(d, "done.md") {
		t.Errorf("the linked folder did not open:\n%s", screen(d))
	}
}

// Spec: Given a link link.txt to a.txt and a dangling link broken.txt,
// when the app starts, then the explorer lists both links.
func TestSCN18LinksAreListed(t *testing.T) {
	d := linkFiles(t)
	for _, name := range []string{"link.txt", "broken.txt", "linked"} {
		if !explorerShows(d, name) {
			t.Errorf("explorer does not list %s:\n%s", name, screen(d))
		}
	}
}

// Spec: Given link.txt, a link to a.txt,
// when the user deletes link.txt from the explorer and confirms,
// then only the link is gone: a.txt keeps its text.
func TestSCN18DeleteLinkKeepsTarget(t *testing.T) {
	d := linkFiles(t)
	clickExplorer(d, "link.txt")
	d.Key("delete")
	d.Key("enter")
	if !eventually(d, func() bool { _, err := os.Lstat(filepath.Join(d.Dir, "link.txt")); return os.IsNotExist(err) }) {
		t.Fatalf("link.txt is still there:\n%s", screen(d))
	}
	if got := diskFile(d, "a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Errorf("a.txt = %q, want its text kept", got)
	}
	if !waitExplorer(d, "link.txt", false) {
		t.Errorf("explorer still lists link.txt:\n%s", screen(d))
	}
}

// Spec: Given linked, a link to the folder notes,
// when the user deletes linked from the explorer and confirms,
// then only the link is gone: notes and its files stay.
func TestSCN18DeleteLinkedFolderKeepsFolder(t *testing.T) {
	d := linkFiles(t)
	clickExplorer(d, "linked")
	d.Key("delete")
	d.Key("enter")
	if !eventually(d, func() bool { _, err := os.Lstat(filepath.Join(d.Dir, "linked")); return os.IsNotExist(err) }) {
		t.Fatalf("linked is still there:\n%s", screen(d))
	}
	if got := diskFile(d, "notes/todo.md"); got != "# Todo\n\n- milk\n" {
		t.Errorf("notes/todo.md = %q, the delete went through the link", got)
	}
}

// Spec: Given link.txt, a link to a.txt,
// when the user renames it to alias.txt in the explorer,
// then alias.txt is still a link to a.txt and a.txt is unchanged.
func TestSCN18RenameLinkKeepsLink(t *testing.T) {
	d := linkFiles(t)
	renameInExplorer(d, "link.txt", "alias.txt")
	p := filepath.Join(d.Dir, "alias.txt")
	if !eventually(d, func() bool { _, err := os.Lstat(p); return err == nil }) {
		t.Fatalf("alias.txt not made:\n%s", screen(d))
	}
	if target, err := os.Readlink(p); err != nil || target != "a.txt" {
		t.Errorf("alias.txt link = %q, %v; want a link to a.txt", target, err)
	}
	if got := diskFile(d, "a.txt"); got != "alpha\nbravo\ncharlie\n" {
		t.Errorf("a.txt = %q", got)
	}
	if !waitExplorer(d, "alias.txt", true) || explorerShows(d, "link.txt") {
		t.Errorf("explorer out of step with the disk:\n%s", screen(d))
	}
}
