package assets

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Spec (MOBUX-18): the phone's saved views live in the Home's own file
// (src/home, built into home.js), so the phone's first files and the
// desktop do not grow; their strings are English only; a delete asks in
// the row with Cancel first and focused; a pinned view always runs its
// spec again, live, on the laptop (view.run with live), never from
// saved data; and pinned is told by 📌 and words, not colour alone.
func TestSavedViewsPage(t *testing.T) {
	read := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join("src", rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	view, strs, state := read("home/saved.tsx"), read("home/savedstrings.ts"), read("home/savedstate.ts")

	if regexp.MustCompile(`[\x{0600}-\x{06FF}]`).MatchString(strs + view) {
		t.Error("the saved views carry non-English text: the phone app is English only")
	}
	for _, src := range []string{view, state} {
		if strings.Contains(src, `from "preact`) || strings.Contains(src, `from "@preact/signals"`) || strings.Contains(src, `from "../session"`) {
			t.Error("saved views import Preact or the session of their own: they use the page's through the Home's host")
		}
	}

	del := view[strings.Index(view, `step.s === "delete" && (`):]
	cancel, verb := strings.Index(del, `sv("cancel")`), strings.Index(del, `sv("delete.do")`)
	if cancel < 0 || verb < 0 || cancel > verb || !strings.Contains(del[:cancel], "ref={safe}") {
		t.Error("delete does not ask with Cancel first and focused")
	}
	if !strings.Contains(view, `"view.run", { spec: v.spec, live: true }`) {
		t.Error("a pinned view does not run its spec again, live, on the laptop")
	}
	if !strings.Contains(view, `"views_saved"`) {
		t.Error("the page does not hear other phones' changes (ev views_saved)")
	}
	if !strings.Contains(view, "📌") || !strings.Contains(strs, `"row.pinned": "Pinned to Home"`) {
		t.Error("pinned is not told by a mark and words")
	}

	// Nothing outside the Home's folder imports them.
	err := filepath.WalkDir("src", func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(filepath.ToSlash(p), "src/home/") {
			return err
		}
		if b, _ := os.ReadFile(p); strings.Contains(string(b), "home/saved") {
			t.Errorf("%s imports the saved views: only home.js may", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
