package assets

import (
	"strings"
	"testing"
)

// Spec (Now fixes): a row with no time has no age (never days since
// 1970); a finished item that waits on nobody is in no section, not even
// Everything else; count is what Now lists.
func TestNowStateLeavesOutFinished(t *testing.T) {
	out := runJS(t, `
import { sections, count, age } from "../now/state";
function eq(a: unknown, b: unknown, what: string) {
  const x = JSON.stringify(a), y = JSON.stringify(b);
  if (x !== y) throw new Error(what + ": got " + x + ", want " + y);
}
const now = 1_700_000_000_000;
const it = (id: string, turn: string, state: string) => ({ id, kind: "run", ref: id, title: id, state, since: now - 1, turn, level: 4 });
const s = sections([
  it("done-none", "none", "done"),
  it("done-old", "", "done"),
  it("done-review", "me", "done"),
  it("done-wait", "others", "done"),
  it("failed", "none", "failed"),
  it("run", "none", "working"),
] as any, now);
eq(s.other.map((x) => x.id), ["failed", "run"], "everything else");
eq(s.turn.map((x) => x.id), ["done-review"], "your turn");
eq(s.waiting.map((x) => x.id), ["done-wait"], "waiting");
eq(count(s), 4, "count");
eq([age(0, now), age(-5, now)], ["", ""], "no time, no age");
console.log("ok");
`, nil)
	if !strings.Contains(out, "ok") {
		t.Fatalf("harness: %s", out)
	}
}

// Spec (Now fixes): a phone opens on the Home by default. A "tabs" kept
// from the old default (no choice made) reads as the Home; a choice made
// on This phone (phoneHomeSet) is kept.
func TestPhoneHomeDefault(t *testing.T) {
	for _, tc := range []struct{ stored, want string }{
		{``, "chat"},
		{`{"theme":"dark","phoneHome":"tabs"}`, "chat"},
		{`{"phoneHome":"tabs","phoneHomeSet":true}`, "tabs"},
		{`{"phoneHome":"chat","phoneHomeSet":true}`, "chat"},
	} {
		out := runJS(t, `
import "ls";
import { prefs } from "./prefs";
console.log("home=" + prefs.peek().phoneHome);
`, map[string]string{"ls": `const v = ` + "`" + tc.stored + "`" + `;
const m = new Map<string, string>(v ? [["termocode.prefs.v1", v]] : []);
(globalThis as any).localStorage = { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, x: string) => m.set(k, x), removeItem: (k: string) => m.delete(k) };
export {};`})
		if !strings.Contains(out, "home="+tc.want) {
			t.Errorf("stored %s: %s, want home=%s", tc.stored, out, tc.want)
		}
	}
}
