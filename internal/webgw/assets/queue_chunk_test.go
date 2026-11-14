package assets

import (
	"io/fs"
	"strings"
	"testing"
)

// budgetQueue is the phone remote's queue (queue.js, CMP-46), gzipped:
// the waiting messages with Send now and Take back. The page loads it
// when the laptop's AI is busy or a message waits.
const budgetQueue = 4 << 10

// Spec (CMP-46): the queue's view and its op are in queue.js alone; the
// page's first files only load it, and queue.js imports nothing (the
// page gives it Preact, the link and the strings).
func TestQueueChunkHoldsTheQueue(t *testing.T) {
	if _, err := fs.Stat(FS(), "queue.js"); err != nil {
		t.Fatalf("no queue.js: %v", err)
	}
	q, page := readBuilt(t, "queue.js"), readPage(t)
	if !strings.Contains(q, `"chat.queue"`) {
		t.Error("queue.js does not call chat.queue")
	}
	if strings.Contains(page, `"chat.queue"`) {
		t.Error("the page's first files call chat.queue; it belongs in queue.js")
	}
	for _, bad := range []string{"import ", "import(", "app-chunk"} {
		if strings.Contains(q, bad) {
			t.Errorf("queue.js imports something (%q): the page gives it what it needs", bad)
		}
	}
	if !strings.Contains(page, `import("/assets/queue.js")`) {
		t.Error("the page does not load /assets/queue.js")
	}
	if strings.Contains(readBuilt(t, "index.html"), "queue.js") {
		t.Error("index.html names queue.js: the page loads it when the AI is busy")
	}
	if n := gzSize(t, "queue.js"); n > budgetQueue {
		t.Errorf("queue.js is %d B gzipped, budget %d B", n, budgetQueue)
	} else {
		t.Logf("gzipped: queue.js %d B (on first busy chat)", n)
	}
}

// Spec (CMP-46): the chat store keeps the queue of the chat topic: the
// snapshot's queue, and a state delta's whole new queue (an empty one
// after Stop clears it).
func TestChatStoreAppliesTheQueue(t *testing.T) {
	out := runJS(t, `
import { reduceChat } from "../state/topics";
import { readChat } from "../state/chat";
function eq(a: unknown, b: unknown, what: string) {
  const x = JSON.stringify(a), y = JSON.stringify(b);
  if (x !== y) throw new Error(what + ": got " + x + ", want " + y);
}
const snap = { id: "c1", title: "T", busy: true, messages: [], queue: [{ id: "q-1", text: "one" }] };
eq(readChat(snap)?.queue, [{ id: "q-1", text: "one" }], "readChat keeps the queue");
eq(readChat({ id: "c1", title: "", busy: false, messages: [] })?.queue, [], "no queue reads empty");
const two = reduceChat(snap, { op: "state", queue: { queue: [{ id: "q-1", text: "one" }, { id: "q-2", text: "two" }] } }) as any;
eq(two.queue.map((q: any) => q.id), ["q-1", "q-2"], "a state delta sets the whole queue");
eq(two.busy, true, "busy stays");
const kept = reduceChat(two, { op: "state", busy: true }) as any;
eq(kept.queue.length, 2, "a state delta without a queue keeps it");
const stopped = reduceChat(two, { op: "state", busy: false, queue: { queue: [] } }) as any;
eq(stopped.queue, [], "Stop: the empty queue clears it");
console.log("ok");
`, nil)
	if !strings.Contains(out, "ok") {
		t.Fatalf("harness: %s", out)
	}
}

// queueHarnessHead loads queue/main.tsx with a fake host: h builds plain
// nodes, request records the ops.
const queueHarnessHead = `
import { init, add, now, remove, full, note, Queue, MAX } from "../queue/main";
const sent: any[] = [];
let fail: any = null;
const strs: Record<string, string> = {};
init({
  h: (type: any, props: any, ...children: any[]) => ({ type, props: props || {}, children: children.flat(9) }),
  request: (op: string, args: any) => (sent.push({ op, args }), fail ? Promise.reject(fail) : Promise.resolve({ queue: [] })),
  t: (k: string, v?: any) => (strs[k] ?? k).replace(/\{(\w+)\}/g, (_: string, n: string) => String(v?.[n] ?? "")),
  addStrings: (d: Record<string, string>) => Object.assign(strs, d),
  isBusy: (e: unknown) => (e as any)?.code === "busy",
});
function eq(a: unknown, b: unknown, what: string) {
  const x = JSON.stringify(a), y = JSON.stringify(b);
  if (x !== y) throw new Error(what + ": got " + x + ", want " + y);
}
function walk(n: any, f: (n: any) => void) { if (!n || typeof n !== "object") return; f(n); for (const c of n.children || []) walk(c, f); }
function text(n: any): string { if (n == null || n === false) return ""; if (typeof n !== "object") return String(n); return (n.children || []).map(text).join(""); }
`

// Spec (CMP-46): add lines a message up, Send now steers with one, Take
// back removes one; at most 5 wait; a full queue says so in plain words.
func TestQueueChunkSteersAndTakesBack(t *testing.T) {
	out := runJS(t, queueHarnessHead+`
eq(MAX, 5, "at most 5 wait");
await add("hello");
await now("q-2");
await remove("q-3");
eq(sent, [{ op: "chat.queue", args: { text: "hello" } }, { op: "chat.queue", args: { now: "q-2" } }, { op: "chat.queue", args: { remove: "q-3" } }], "the ops");
const five = [1, 2, 3, 4, 5].map((i) => ({ id: "q-" + i, text: "m" + i }));
eq(full(five), true, "5 waiting is full");
eq(full(five.slice(1)), false, "4 waiting is not");
const n = note({ code: "busy" });
if (!/5|Five|five/.test(n)) throw new Error("a full queue's note does not say five: " + n);
sent.length = 0;
const tree = Queue({ items: five.slice(0, 2) }) as any;
const all = text(tree);
if (!all.includes("m1") || !all.includes("m2")) throw new Error("the queue does not show its messages: " + all);
if (!all.includes("2")) throw new Error("the queue does not count its messages: " + all);
const buttons: any[] = [];
walk(tree, (x) => x.type === "button" && buttons.push(x));
const nowB = buttons.filter((b) => text(b) === "Send now");
const backB = buttons.filter((b) => text(b) === "Take back");
eq([nowB.length, backB.length], [2, 2], "each message has Send now and Take back");
nowB[1].props.onClick();
backB[0].props.onClick();
eq(sent, [{ op: "chat.queue", args: { now: "q-2" } }, { op: "chat.queue", args: { remove: "q-1" } }], "the buttons' ops");
eq(Queue({ items: [] }), null, "an empty queue draws nothing");
console.log("ok");
`, nil)
	if !strings.Contains(out, "ok") {
		t.Fatalf("harness: %s", out)
	}
}

// Spec (CMP-46): while the laptop's AI answers, the laptop chat's
// composer keeps Stop and lines the typed message up (queue.js) instead
// of refusing it; an old chat the page reads never queues.
func TestComposerQueuesWhileBusy(t *testing.T) {
	comp := toChatSrc(t, "src/views/Composer.tsx")
	for _, want := range []string{"queue?:", "loadQueue(", ".add(body)", "<q.Queue"} {
		if !strings.Contains(comp, want) {
			t.Errorf("Composer.tsx lost %q", want)
		}
	}
	chat := toChatSrc(t, "src/views/Chat.tsx")
	if strings.Count(chat, "queue={") != 1 || !strings.Contains(chat, "queue={laptop?.queue") {
		t.Error("Chat.tsx must give the queue to the laptop chat's composer only")
	}
}
