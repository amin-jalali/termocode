package assets

import (
	"regexp"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/blocks"
)

// Spec (MOB-3): the page draws every block kind the laptop can send, the
// kit shows one of each, and the renderer stays out of app.js and
// workbench.js (only the phone's lazy views and the kit import it).
func TestBlocksRendererDrawsEveryKind(t *testing.T) {
	files, _ := loadSources(t)
	src, ok := files["src/ui/Blocks.tsx"]
	if !ok {
		t.Fatal("no src/ui/Blocks.tsx")
	}
	kit := files["src/kit/main.tsx"]
	for _, k := range blocks.Kinds() {
		if !strings.Contains(src, `case "`+k+`":`) {
			t.Errorf("Blocks.tsx draws no %s block", k)
		}
		if !regexp.MustCompile(`kind: "` + k + `"`).MatchString(kit) {
			t.Errorf("the kit has no %s block sample", k)
		}
	}
	m := regexp.MustCompile(`export const KINDS = \[([^\]]*)\]`).FindStringSubmatch(src)
	if m == nil {
		t.Fatal("Blocks.tsx has no KINDS list")
	}
	want := `"` + strings.Join(blocks.Kinds(), `", "`) + `"`
	if strings.TrimSpace(m[1]) != want {
		t.Errorf("KINDS = [%s], Go has [%s]", m[1], want)
	}
	if !strings.Contains(src, `t("laptop.open")`) {
		t.Error("an unknown kind does not offer Open on laptop")
	}
	if !strings.Contains(kit, `id="blocks"`) {
		t.Error("the kit has no Blocks section")
	}
}

func TestBlocksRendererIsNotInTheFirstFiles(t *testing.T) {
	files, _ := loadSources(t)
	for _, entry := range []string{"src/main.tsx", "src/workbench.tsx"} {
		got := reachable(files, entry)
		for _, f := range []string{"src/ui/Blocks.tsx", "src/strings.phone.ts"} {
			if got[f] {
				t.Errorf("%s reaches %s; it belongs in a lazy phone chunk", entry, f)
			}
		}
	}
}

// The renderer never builds markup from data and draws a tone with a
// shape, not colour alone.
func TestBlocksRendererRules(t *testing.T) {
	files, _ := loadSources(t)
	src := files["src/ui/Blocks.tsx"]
	for _, bad := range []string{"innerHTML", "dangerouslySetInnerHTML", "eval(", "new Function"} {
		if strings.Contains(src, bad) {
			t.Errorf("Blocks.tsx uses %s", bad)
		}
	}
	for _, shape := range []string{"✓", "●", "✗", "◐", "○"} {
		if !strings.Contains(src, shape) {
			t.Errorf("Blocks.tsx has no %s shape", shape)
		}
	}
}
