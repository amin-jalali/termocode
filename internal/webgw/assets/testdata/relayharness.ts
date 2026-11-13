// A headless page for relay_node_test.go: the page's relay code (noise,
// tunnel, http, socket) runs in node against the Go gateway side. Lines
// on stdin/stdout: "m <base64>" is a relay message; "out ..." a result.
import { b64url, newStaticKey, unb64url } from "../src/relay/noise";
import { tunnelFetch } from "../src/relay/http";
import { makeSocketClass } from "../src/relay/socket";
import { Tunnel, type Wire } from "../src/relay/tunnel";

declare const process: any;

(globalThis as any).location = new URL("http://laptop/");
const say = (s: string) => process.stdout.write(s + "\n");
const b64 = (b: Uint8Array) => Buffer.from(b).toString("base64");
declare const Buffer: any;

const wire: Wire = { send: (b) => say("m " + b64(b)), close: () => {}, onmessage: null, onclose: null };
let pending = "";
process.stdin.on("data", (d: any) => {
  pending += d.toString();
  let i;
  while ((i = pending.indexOf("\n")) >= 0) {
    const line = pending.slice(0, i);
    pending = pending.slice(i + 1);
    if (line.startsWith("m ")) wire.onmessage?.(new Uint8Array(Buffer.from(line.slice(2), "base64")));
  }
});
process.stdin.on("end", () => wire.onclose?.());

async function main() {
  const key = await newStaticKey(false);
  say("pub " + b64url(key.pub));
  const t = await Tunnel.open(wire, key, unb64url(process.argv[2]), { pair: "p1" });
  const r = await tunnelFetch(t, new URL("http://laptop/hello?x=1"), { method: "POST", headers: { "Content-Type": "application/json" }, body: '{"a":1}' });
  say("out fetch " + r.status + " " + (await r.text()));
  const big = await tunnelFetch(t, new URL("http://laptop/big"));
  say("out big " + (await big.arrayBuffer()).byteLength);
  const WS = makeSocketClass(async () => t);
  const ws = new WS("ws://laptop/echo", ["termocode.v2"]);
  ws.onopen = () => ws.send("ping-from-page");
  ws.onmessage = (e: MessageEvent) => {
    say("out ws " + e.data + " " + ws.protocol);
    ws.close(1000, "bye");
  };
  ws.onclose = (e: any) => {
    say("out closed " + e.code);
    process.exit(0);
  };
}
main().catch((e) => {
  say("out error " + String(e && e.stack ? e.stack : e));
  process.exit(1);
});
