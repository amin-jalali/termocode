// The workbench's git question that goes to the laptop (lib/gitnav.ts
// gitAsk): asked here first, Cancel first and focused, then the laptop's
// card, wherever the command ran.
import { h } from "preact";
import { gitAsk } from "../../lib/gitnav";
import { link } from "../../session";
import { AskLaptop } from "../../ui/AskLaptop";

export function GitAskHost() {
  const a = gitAsk.value;
  if (!a) return null;
  return <AskLaptop key={a.op + a.text} label={a.label} text={a.text} send={() => link.request(a.op, a.args)} onDone={() => (gitAsk.value = null)} />;
}
