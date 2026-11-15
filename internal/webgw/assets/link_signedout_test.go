package assets

import (
	"strings"
	"testing"
)

// linkHarnessHead is a Link over a fake WebSocket class and a fake
// fetch: each test says how the socket ends and what /ws/check answers.
const linkHarnessHead = `
import { Link } from "../net/link";
import { Store } from "../state/store";
function eq(a: unknown, b: unknown, what: string) {
  if (JSON.stringify(a) !== JSON.stringify(b)) throw new Error(what + ": got " + JSON.stringify(a) + ", want " + JSON.stringify(b));
}
let opens = true;
const checks: string[] = [];
let answer: () => Promise<Response> = () => Promise.reject(new TypeError("Failed to fetch"));
(globalThis as any).fetch = (url: string, init: RequestInit) => {
  checks.push(url + " " + init.method + " " + init.body);
  return answer();
};
class FakeWS {
  onopen: (() => void) | null = null;
  onmessage: ((e: unknown) => void) | null = null;
  onclose: ((e: { code: number }) => void) | null = null;
  readyState = 0;
  constructor() {
    setTimeout(() => {
      if (opens) {
        this.readyState = 1;
        this.onopen?.();
      }
      this.readyState = 3;
      this.onclose?.({ code: 1006 });
    }, 0);
  }
  send() {}
  close() {}
}
const tick = () => new Promise((r) => setTimeout(r, 5));
function newLink() {
  return new Link({ store: new Store(), url: "ws://x/ws", token: () => "tok-1", WS: FakeWS as any, random: () => 0 });
}
function reply(status: number, body: unknown) {
  answer = () => Promise.resolve(new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }));
}
`

// Spec (sign-in after the kill switch): a WebSocket refused before it
// opened (the browser says 1006, as for a dropped network) is checked
// on POST /ws/check. A sign-in that is over ends the link for good
// with its close code (signed_out 4001, expired 4002, revoked 4003), so
// the page shows its sign-in instead of "Connection lost" for ever; a
// check that does not get through, or says the sign-in is live, keeps
// the retry. A socket that did open and then dropped is a network drop:
// no check.
func TestLinkRefusedSocketChecksTheSignIn(t *testing.T) {
	out := runJS(t, linkHarnessHead+`
opens = false;
for (const [why, code] of [["signed_out", 4001], ["expired", 4002], ["revoked", 4003]] as const) {
  checks.length = 0;
  reply(401, { error: why });
  const l = newLink();
  l.start();
  await tick();
  eq(checks, ['/ws/check POST {"token":"tok-1"}'], why + ": the check");
  eq(l.state.peek(), "final", why + ": state");
  eq(l.finalCode.peek(), code, why + ": close code");
  l.stop();
}

checks.length = 0;
answer = () => Promise.reject(new TypeError("Failed to fetch"));
let l = newLink();
l.start();
await tick();
eq(checks.length, 1, "network down: one check");
eq(l.state.peek(), "offline", "network down: retrying");
l.stop();

for (const status of [200, 429, 503]) {
  checks.length = 0;
  reply(status, status === 200 ? { ok: true } : { error: "x" });
  l = newLink();
  l.start();
  await tick();
  eq(l.state.peek(), "offline", status + ": retrying");
  l.stop();
}

opens = true;
checks.length = 0;
l = newLink();
l.start();
await tick();
eq(checks.length, 0, "an open socket that dropped: no check");
eq(l.state.peek(), "offline", "dropped: retrying");
l.stop();
console.log("ok");
`, nil)
	if !strings.Contains(out, "ok") {
		t.Fatalf("harness: %s", out)
	}
}
