package app

import (
	"testing"

	"github.com/amin-jalali/termocode/internal/containerhost"
)

func TestContainerFilesPickerItems(t *testing.T) {
	ents := []containerhost.BrowseEntry{
		{Name: "nginx", Dir: true},
		{Name: "hosts", Size: 120},
		{Name: "mtab", Link: true},
	}
	items := containerFileItems("/etc", ents)
	ids := make([]string, 0, len(items))
	for _, it := range items {
		ids = append(ids, it.ID)
	}
	want := []string{"up", "upload", "d:nginx", "f:hosts", "f:mtab"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
	if items[2].Title != "nginx/" {
		t.Fatalf("folder title = %q, want a trailing slash", items[2].Title)
	}
	if root := containerFileItems("/", nil); len(root) != 1 || root[0].ID != "upload" {
		t.Fatalf("the root has no up row, got %v", root)
	}
}

func TestContainerFilesCommandIsWired(t *testing.T) {
	found := false
	for _, it := range servicesPaletteItems() {
		if it.ID == "containers-files" {
			found = true
		}
	}
	if !found {
		t.Fatal("containers-files is not in the palette")
	}
	if p, ok := aiActionPolicies["containers-files"]; !ok || p.Gate != aiGateNever {
		t.Fatalf("containers-files policy = %+v, want never", p)
	}
}

func TestContainerFilesUploadPath(t *testing.T) {
	got, err := containerUploadLocalPath("~/a.txt", "/home/me")
	if err != nil || got != "/home/me/a.txt" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := containerUploadLocalPath("rel.txt", "/home/me"); err == nil {
		t.Fatal("a relative path must be refused")
	}
}
