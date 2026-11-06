package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/webproto"
)

// Spec (WEBP-9): search.replace counts the matches for the laptop card
// (no file changes before its yes) and then replaces them in every file;
// a text with no match is refused at once.
func TestWebSearchReplace(t *testing.T) {
	r, _ := webWorkspaceRig(t)
	r.write("a.go", "oldName oldName\n")
	r.write("pkg/b.go", "x := oldName\n")
	r.write("c.go", "nothing\n")
	if f := r.call(webproto.OpSearchReplace, &webproto.SearchReplaceArgs{Find: "absent", Replace: "y"}); webErrCode(f) != "not_found" {
		t.Errorf("no match: %v", f)
	}
	var ref webproto.AskRef
	webTestData(t, r.call(webproto.OpSearchReplace, &webproto.SearchReplaceArgs{Find: "oldName", Replace: "newName"}), &ref)
	if v := r.m.confirm.View(); !strings.Contains(v, "3 matches") || !strings.Contains(v, "2 files") {
		t.Errorf("card:\n%s", v)
	}
	if b, _ := os.ReadFile(filepath.Join(r.root, "a.go")); string(b) != "oldName oldName\n" {
		t.Fatal("replaced before the laptop said yes")
	}
	webTestLaptopSelect(t, r.m, "confirm")
	a, _ := os.ReadFile(filepath.Join(r.root, "a.go"))
	b, _ := os.ReadFile(filepath.Join(r.root, "pkg", "b.go"))
	if string(a) != "newName newName\n" || string(b) != "x := newName\n" {
		t.Fatalf("after: %q %q", a, b)
	}
}
