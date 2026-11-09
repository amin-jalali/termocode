package assets

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The AI chat's extras (aictx.js: Add to AI Chat, AI: Side Question…, a
// grid's Export Result…) are their own file, loaded by the workbench on
// first use: the phone's files never name it, and it holds no second
// Preact or session (its sources import types and pure helpers only,
// and get the rest from the workbench's host).
func TestAICtxChunkIsLazyAndHoldsNoState(t *testing.T) {
	ai := readBuilt(t, "aictx.js")
	if strings.Contains(ai, "__H") {
		t.Error("aictx.js holds a copy of Preact")
	}
	if !strings.Contains(readBuilt(t, "workbench.js"), `import("/assets/aictx.js")`) {
		t.Error("workbench.js does not load /assets/aictx.js")
	}
	if strings.Contains(readPage(t), "aictx.js") || strings.Contains(readBuilt(t, "index.html"), "aictx.js") {
		t.Error("the page's first files name aictx.js: only the workbench loads it")
	}
	pure := map[string]bool{"state/chat.ts": true, "aictx/export.ts": true}
	importLine := regexp.MustCompile(`(?m)^import\s+(type\s+)?[^;]*?from\s+"([^"]+)"`)
	files, err := filepath.Glob("src/aictx/*")
	if err != nil || len(files) == 0 {
		t.Fatalf("no aictx sources: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range importLine.FindAllStringSubmatch(string(b), -1) {
			if m[1] != "" || strings.HasPrefix(m[2], "./") {
				continue // a type, or the chunk's own file
			}
			rel := filepath.ToSlash(filepath.Clean(filepath.Join("aictx", m[2])))
			if !pure[rel+".ts"] {
				t.Errorf("%s imports %s: aictx.js takes state from the workbench's host only", f, m[2])
			}
		}
	}
}
