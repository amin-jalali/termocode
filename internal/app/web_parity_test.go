package app

import (
	"bufio"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/amin-jalali/termocode/internal/activity"
	"github.com/amin-jalali/termocode/internal/webproto"
)

// parityViews names every activity view of the laptop (WEBP-9). A view
// added to internal/activity after ViewKubernetes must be named here and
// classified in testdata/web_parity.txt.
var parityViews = map[activity.View]string{
	activity.ViewFiles:      "view/Files",
	activity.ViewSearch:     "view/Search",
	activity.ViewGit:        "view/Git",
	activity.ViewRun:        "view/Run",
	activity.ViewSettings:   "view/Settings",
	activity.ViewTests:      "view/Tests",
	activity.ViewServices:   "view/Services",
	activity.ViewDatabases:  "view/Databases",
	activity.ViewItermoc:    "view/Itermoc",
	activity.ViewRuns:       "view/Runs",
	activity.ViewKubernetes: "view/Kubernetes",
}

type parityRow struct{ kind, id, detail string }

// parityRows reads testdata/web_parity.txt: kind TAB id TAB detail.
func parityRows(t *testing.T) []parityRow {
	t.Helper()
	f, err := os.Open("testdata/web_parity.txt")
	if err != nil {
		t.Fatalf("the reviewed parity file is missing: %v", err)
	}
	defer f.Close()
	var rows []parityRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		p := strings.SplitN(line, "\t", 3)
		if len(p) != 3 || strings.TrimSpace(p[2]) == "" {
			t.Errorf("bad parity line (kind, id and a detail are needed): %q", line)
			continue
		}
		rows = append(rows, parityRow{p[0], p[1], p[2]})
	}
	return rows
}

func srcText(t *testing.T, names ...string) string {
	t.Helper()
	var b strings.Builder
	for _, n := range names {
		data, err := os.ReadFile("../webgw/assets/src/" + n)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

// Spec (WEBP-9): every laptop palette command and every activity view has
// a web counterpart ("web" row, with a ref that really exists), an
// excluded row (with a one-line reason), or a missing row (the honest
// gap list, printed). An id in none of them, in two, or a row for an id
// the laptop no longer has, fails: new laptop commands must be classified.
func TestWebParityGate(t *testing.T) {
	want := map[string]bool{}
	for _, it := range paletteItems() {
		if !it.File {
			want[it.ID] = true
		}
	}
	for v := activity.View(0); v <= activity.ViewKubernetes; v++ {
		name, ok := parityViews[v]
		if !ok {
			t.Fatalf("activity view %d has no name in parityViews", v)
		}
		want[name] = true
	}
	cmds := srcText(t, "lib/commands.ts", "views/git/gitcommands.ts", "views/desk/dbcommands.ts")
	bar := srcText(t, "ui/ActivityBar.tsx")
	ops := map[string]bool{}
	for _, p := range webproto.Ops() {
		ops[p.Op] = true
	}
	refOK := func(ref string) bool {
		kind, name, _ := strings.Cut(ref, ":")
		switch {
		case ref == "palette":
			return false // checked by id below
		case kind == "op":
			return ops[name]
		case kind == "cmd":
			return regexp.MustCompile(`id: "` + regexp.QuoteMeta(name) + `"`).MatchString(cmds)
		case kind == "view":
			return regexp.MustCompile(`id: "` + regexp.QuoteMeta(name) + `", label`).MatchString(bar)
		}
		return false
	}
	seen := map[string]string{}
	var missing []string
	counts := map[string]int{}
	for _, r := range parityRows(t) {
		if prev, dup := seen[r.id]; dup {
			t.Errorf("%s is listed twice (%s and %s)", r.id, prev, r.kind)
		}
		seen[r.id] = r.kind
		counts[r.kind]++
		if !want[r.id] {
			t.Errorf("%s (%s) is not a laptop palette command or view any more: remove the row", r.id, r.kind)
		}
		switch r.kind {
		case "web":
			if r.detail == "palette" {
				if _, ok := webPaletteAllow[r.id]; !ok {
					t.Errorf("%s: says palette, but it is not on webPaletteAllow", r.id)
				}
			} else if !refOK(r.detail) {
				t.Errorf("%s: the web ref %q does not exist (op, workbench command or activity id)", r.id, r.detail)
			}
		case "excluded":
			if len(strings.TrimSpace(r.detail)) < 12 {
				t.Errorf("%s: the exclusion needs a real reason, got %q", r.id, r.detail)
			}
		case "missing":
			if !strings.Contains(r.detail, "|") {
				t.Errorf("%s: a missing row needs area|size, got %q", r.id, r.detail)
			}
			missing = append(missing, r.id+" ("+r.detail+")")
		default:
			t.Errorf("%s: unknown kind %q", r.id, r.kind)
		}
	}
	var unclassified []string
	for id := range want {
		if _, ok := seen[id]; !ok {
			unclassified = append(unclassified, id)
		}
	}
	sort.Strings(unclassified)
	if len(unclassified) > 0 {
		t.Errorf("%d laptop ids have no web counterpart and no exclusion; add each to testdata/web_parity.txt as web, excluded (with a reason) or missing:\n%s",
			len(unclassified), strings.Join(unclassified, "\n"))
	}
	for id := range webPaletteAllow {
		if seen[id] != "web" {
			t.Errorf("%s is on webPaletteAllow but not a web row", id)
		}
	}
	sort.Strings(missing)
	t.Logf("web parity: %d web, %d excluded, %d missing of %d ids", counts["web"], counts["excluded"], counts["missing"], len(want))
	t.Logf("missing on the web (not excluded):\n%s", strings.Join(missing, "\n"))
}
