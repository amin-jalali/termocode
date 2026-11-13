package assets

import (
	"strings"
	"testing"
)

// Live editing together on the page (G-COL): the page's own unsent
// typing is moved over the laptop's change when the two did not touch
// the same text, like the hub moves a page edit over the laptop's
// (internal/coedit). Only a real overlap is still a conflict.

// liveStubs stand in for the session, the edit helpers and the
// workbench, so lib/live.ts runs in node.
var liveStubs = map[string]string{
	"../session": `
export const sent: any[] = [];
export const replies: any[] = [];
export const link = { request(op: string, args: any) { sent.push({ op, args }); const r = replies.shift(); return r instanceof Error ? Promise.reject(r) : Promise.resolve(r); } };
export const live = { peek: () => true, value: true };
`,
	"./edit": `
export const own = { tick: 0 };
export function save(_buf: number) { return Promise.resolve(true); }
`,
	"./workbench": `
import { signal } from "@preact/signals";
export const focusSlot = signal("g0");
`,
}

const liveHarnessHead = `
import { LiveSync } from "./live";
import { sent, replies } from "../session";
function eq(a: unknown, b: unknown, what: string) {
  const x = JSON.stringify(a), y = JSON.stringify(b);
  if (x !== y) throw new Error(what + ": got " + x + ", want " + y);
}
function fakeEd(text: string) {
  return { t: text, text() { return this.t; }, remote(x: string) { this.t = x; }, replace(x: string) { this.t = x; },
    setHl() {}, undo() { return false; }, redo() { return false; }, canUndo() { return false; }, canRedo() { return false; }, focus() {}, destroy() {} };
}
function snap(tick: number, text: string) { return { buf: 3, path: "f.txt", tick, lines: text.split("\n"), dirty: true, ft: "", readonly: false }; }
`

// Spec (G-COL): merge3 joins the page's change and the laptop's change
// made on one base when they do not overlap, with the same edge rules
// as the hub (internal/coedit): two inserts at one point put the
// laptop's first; an overlap gives null.
func TestPageMerge3(t *testing.T) {
	out := runJS(t, `
import { merge3 } from "./textdiff";
function eq(a: unknown, b: unknown, what: string) {
  if (a !== b) throw new Error(what + ": got " + JSON.stringify(a) + ", want " + JSON.stringify(b));
}
eq(merge3("a\nb\nc", "aX\nb\nc", "a\nb\ncY"), "aX\nb\ncY", "mine before theirs");
eq(merge3("a\nb\nc", "a\nb\ncY", "aX\nb\nc"), "aX\nb\ncY", "theirs before mine");
eq(merge3("abc", "abc", "aLbc"), "aLbc", "only theirs");
eq(merge3("abc", "aPbc", "abc"), "aPbc", "only mine");
eq(merge3("ab", "aPb", "aLb"), "aLPb", "two inserts at one point: the laptop's first");
eq(merge3("hello world", "hello there", "hello earth"), null, "one word changed on both sides");
eq(merge3("one two", "one TWO", "ONE two"), "ONE TWO", "touching edges are no overlap");
eq(merge3("x\ny", "x\ny\nz", "w\nx\ny"), "w\nx\ny\nz", "lines added at both ends");
console.log("ok");
`, nil)
	if !strings.Contains(out, "ok") {
		t.Fatalf("harness: %s", out)
	}
}

// Spec (G-COL): the laptop changes the file while the user's typing is
// still unsent. The editor shows both (no keystroke lost, no conflict)
// and the next send is the user's change alone, on the laptop's tick.
func TestLiveSyncRebasesUnsentTyping(t *testing.T) {
	out := runJS(t, liveHarnessHead+`
const ed = fakeEd("a\nb\nc");
const s = new LiveSync(3, ed as any, snap(1, "a\nb\nc"));
ed.t = "aX\nb\nc";
s.local();
s.store(snap(2, "a\nb\ncY"), false);
eq(ed.t, "aX\nb\ncY", "editor after the laptop's change");
eq(s.status.value.phase === "conflict", false, "no conflict");
replies.push({ tick: 3, dirty: true });
await s.flush();
eq(sent.length, 1, "one send");
eq(sent[0].args, { buf: 3, tick: 2, start: { row: 0, col: 1 }, end: { row: 0, col: 1 }, lines: ["X"] }, "the send");
s.dispose();
console.log("ok");
`, liveStubs)
	if !strings.Contains(out, "ok") {
		t.Fatalf("harness: %s", out)
	}
}

// Spec (G-COL): when both sides changed the same text, the user's text
// stays and the editor shows the conflict as before.
func TestLiveSyncOverlapIsStillAConflict(t *testing.T) {
	out := runJS(t, liveHarnessHead+`
const ed = fakeEd("a\nb\nc");
const s = new LiveSync(3, ed as any, snap(1, "a\nb\nc"));
ed.t = "a\nB1\nc";
s.local();
s.store(snap(2, "a\nB2\nc"), false);
eq(ed.t, "a\nB1\nc", "the user's text stays");
eq(s.status.value.phase, "conflict", "phase");
await s.flush();
eq(sent.length, 0, "nothing sent in a conflict");
s.dispose();
console.log("ok");
`, liveStubs)
	if !strings.Contains(out, "ok") {
		t.Fatalf("harness: %s", out)
	}
}

// Spec (G-COL): the hub moved the page's edit over the laptop's
// (rebased): the laptop's text at the new tick is not the page's text.
// The page sends nothing more until it has that text, then sends only
// what is new, on the right rows.
func TestLiveSyncWaitsForTheTextAfterARebasedSend(t *testing.T) {
	out := runJS(t, liveHarnessHead+`
const ed = fakeEd("a\nb");
const s = new LiveSync(3, ed as any, snap(1, "a\nb"));
ed.t = "aX\nb";
replies.push({ tick: 5, dirty: true, rebased: true });
await s.flush();
ed.t = "aXZ\nb";
await s.flush();
eq(sent.length, 1, "no send before the laptop's text at tick 5");
s.store(snap(5, "L\naX\nb"), false);
eq(ed.t, "L\naXZ\nb", "editor after the laptop's text came");
replies.push({ tick: 6, dirty: true });
await s.flush();
eq(sent.length, 2, "then one more send");
eq(sent[1].args, { buf: 3, tick: 5, start: { row: 1, col: 2 }, end: { row: 1, col: 2 }, lines: ["Z"] }, "the second send");
s.dispose();
console.log("ok");
`, liveStubs)
	if !strings.Contains(out, "ok") {
		t.Fatalf("harness: %s", out)
	}
}

// Spec (G-COL): the editor could not show the laptop's change over
// typing it got meanwhile (the desktop editor's frame): the sync stops
// and shows the conflict; it never sends text that would undo the
// laptop's change.
func TestLiveSyncRemoteLostIsAConflict(t *testing.T) {
	out := runJS(t, liveHarnessHead+`
const ed = fakeEd("a\nb\nc");
const s = new LiveSync(3, ed as any, snap(1, "a\nb\nc"));
ed.t = "aX\nb\nc";
s.store(snap(2, "a\nb\ncY"), false);
ed.t = "aX\nb\nc!"; // the frame refused: its newer typing is on the old text
s.lost();
eq(s.status.value.phase, "conflict", "phase");
await s.flush();
eq(sent.length, 0, "nothing sent");
s.dispose();
console.log("ok");
`, liveStubs)
	if !strings.Contains(out, "ok") {
		t.Fatalf("harness: %s", out)
	}
}

// Spec (G-COL): the desktop editor's frame may have typing the page has
// not seen when the laptop's change comes. It moves the change over that
// typing (merge3 on the text the page had) and, when the two overlap,
// says the change was lost so the page shows the conflict.
func TestFrameRebasesRemoteOverNewerTyping(t *testing.T) {
	frame := raceSrc(t, "monaco/frame.ts")
	desk := raceSrc(t, "lib/deskeditor.ts")
	proto := raceSrc(t, "monaco/protocol.ts")
	live := raceSrc(t, "views/LiveEditor.tsx")
	for _, c := range []struct{ src, want, where string }{
		{frame, "merge3(m.mine, cur.toString(), m.text)", "monaco/frame.ts"},
		{frame, "lost: true", "monaco/frame.ts"},
		{desk, `send({ t: "remote", text: t, seen, mine: text })`, "lib/deskeditor.ts"},
		{desk, "host.onRemoteLost?.()", "lib/deskeditor.ts"},
		{proto, "lost?: boolean", "monaco/protocol.ts"},
		{live, "onRemoteLost: () => sync?.lost()", "views/LiveEditor.tsx"},
	} {
		if !strings.Contains(c.src, c.want) {
			t.Errorf("%s has no %s", c.where, c.want)
		}
	}
}
