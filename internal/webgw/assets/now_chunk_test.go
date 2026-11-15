package assets

import (
	"strings"
	"testing"
)

// budgetNow is the phone's Now (now.js), gzipped: Home = Now, See all,
// Why this?, Later and the first-run rules card.
const budgetNow = 14 << 10

// Spec (Now on the phone): the Now views live in now.js alone (with
// now.css): it imports nothing (the Home gives it the page's Preact, link
// and toasts), home.js loads it by its fixed path, and the page's first
// files do not carry it.
func TestNowChunkHoldsNow(t *testing.T) {
	js, page := readBuilt(t, "now.js"), readPage(t)
	for _, want := range []string{"Your turn", "All clear.", "Why this?", `"now.act"`, `"now.rules"`, "See all"} {
		if !strings.Contains(js, want) {
			t.Errorf("now.js lacks %q", want)
		}
	}
	if strings.Contains(page, `"now.act"`) {
		t.Error("the page's first files call now.act; it belongs in now.js")
	}
	for _, bad := range []string{"import ", "import(", "app-chunk"} {
		if strings.Contains(js, bad) {
			t.Errorf("now.js imports something (%q): the Home gives it what it needs", bad)
		}
	}
	if !strings.Contains(readBuilt(t, "home.js"), `import("/assets/now.js")`) {
		t.Error("home.js does not load /assets/now.js")
	}
	if css := readBuilt(t, "now.css"); !strings.Contains(css, ".now-rule") {
		t.Error("now.css has no rule tag style")
	}
	if n := gzSize(t, "now.js"); n > budgetNow {
		t.Errorf("now.js is %d B gzipped, budget %d B", n, budgetNow)
	} else {
		t.Logf("gzipped: now.js %d B (on a Home with Now)", n)
	}
}

// Spec (Now on the phone): the sections are built from the inbox's Now
// fields alone: your turn (level, due today first, oldest first), waiting
// on others, everything else; the summary line counts your turn and what
// runs; ages are short (12m, 3h, 1d).
func TestNowStateSections(t *testing.T) {
	out := runJS(t, `
import { sections, summary, age } from "../now/state";
function eq(a: unknown, b: unknown, what: string) {
  const x = JSON.stringify(a), y = JSON.stringify(b);
  if (x !== y) throw new Error(what + ": got " + x + ", want " + y);
}
const now = 1_700_000_000_000;
const it = (id: string, turn: string, level: number, since: number, state = "needs_you") => ({ id, kind: "run", ref: id, title: id, state, since, turn, level });
const items = [
  it("none", "none", 4, now - 1, "working"),
  it("p2", "me", 2, now - 5),
  it("p1-new", "me", 1, now - 1),
  it("p1-old", "me", 1, now - 100),
  it("wait", "others", 4, now - 7),
  it("old-laptop", "", 0, now - 3, "failed"),
];
const s = sections(items as any, now);
eq(s.turn.map((x) => x.id), ["p1-old", "p1-new", "p2"], "your turn");
eq(s.waiting.map((x) => x.id), ["wait"], "waiting");
eq(s.other.map((x) => x.id), ["old-laptop", "none"], "everything else");
eq(s.running.map((x) => x.id), ["none"], "running");
eq(summary(s), { need: 3, running: 1 }, "summary");
eq([age(now - 30_000, now), age(now - 12 * 60_000, now), age(now - 3 * 3600_000, now), age(now - 26 * 3600_000, now)], ["now", "12m", "3h", "1d"], "ages");
console.log("ok");
`, nil)
	if !strings.Contains(out, "ok") {
		t.Fatalf("harness: %s", out)
	}
}
