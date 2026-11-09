// The strings of Pair a Device… and Devices… in the web page (pair.js).
// They load with that chunk only, so neither the phone's page nor the
// workbench carries them; English only, like the laptop's own cards.
// Same voice as strings.ts: calm, short, present tense, "the laptop", no
// other product named. {name} marks a value.
export const PS = {
  "wp.title": "Pair a phone or tablet",
  "wp.scan": "Scan this code with the phone's camera. It works once, for 2 minutes. The phone then shows a 6-digit code; you check it here.",
  "wp.qr": "QR code of the pairing link",
  "wp.link": "Pairing link",
  "wp.copy": "Copy link",
  "wp.copied": "Link copied. It works once, for 2 minutes.",
  "wp.copy_failed": "Could not copy the link. Scan the code instead.",
  "wp.left": "Works for {time} more.",
  "wp.expired": "This link ran out. Make a new one.",
  "wp.new": "New link",
  "wp.opening": "Making a pairing link…",
  "wp.close": "Close",
  "wp.ask_title": "Pair this device?",
  "wp.ask_body": "{name} wants to pair with this termocode.",
  "wp.code_label": "Code on the phone",
  "wp.code_hint": "Pair only if the phone shows the same code.",
  "wp.roles": "A viewer sees your files, chats and terminals. With control it can also type and run any command as you.",
  "wp.not_now": "Not now",
  "wp.viewer": "Pair as viewer",
  "wp.control": "Pair with control",
  "wp.on_laptop": "Answer on the laptop: its card asks the same question.",
  "wp.paired": "{name} may pair now. The phone finishes it with a passkey.",
  "wp.refused": "Not paired. The phone was told Not now.",
  "wp.laptop_paired": "Answered on the laptop: {name} may pair now.",
  "wp.laptop_refused": "Answered on the laptop: not paired.",
  "wp.closed": "The laptop made a new pairing link, so this one is closed.",
  "wp.failed": "Could not do that. Try again.",
  "wp.dev_title": "Paired devices",
  "wp.dev_none": "No paired devices yet.",
  "wp.dev_loading": "Loading the devices…",
  "wp.dev_control": "Control",
  "wp.dev_view": "View only",
  "wp.dev_guest": "guest",
  "wp.dev_self": "this browser",
  "wp.dev_live": "connected now",
  "wp.dev_repair": "must pair again",
  "wp.dev_seen": "last seen {time}",
  "wp.dev_never": "not seen yet",
  "wp.dev_remove": "Remove",
  "wp.dev_remove_label": "Remove {name}?",
  "wp.dev_remove_body": "Remove {name}? It is disconnected at once and must pair again to come back.",
  "wp.dev_pair": "Pair a Device…",
  "wp.ago_now": "just now",
  "wp.ago_min": "{n} min ago",
  "wp.ago_h": "{n} h ago",
  "wp.ago_days": "{n} days ago",
} as const;

export type PairKey = keyof typeof PS;

/** ps is the text of key with its {name} values filled in. */
export function ps(key: PairKey, vars?: Record<string, string | number>): string {
  const text: string = PS[key];
  if (!vars) return text;
  return text.replace(/\{([a-z_]+)\}/g, (m, name: string) => (name in vars ? String(vars[name]) : m));
}

/** ago says how long ago a Unix time in seconds was, in rough words. */
export function ago(sec: number, now = Date.now()): string {
  const d = Math.max(0, now / 1000 - sec);
  if (d < 60) return PS["wp.ago_now"];
  if (d < 3600) return ps("wp.ago_min", { n: Math.floor(d / 60) });
  if (d < 48 * 3600) return ps("wp.ago_h", { n: Math.floor(d / 3600) });
  return ps("wp.ago_days", { n: Math.floor(d / 86400) });
}

/** left writes seconds as m:ss. */
export function left(sec: number): string {
  const s = Math.max(0, Math.ceil(sec));
  return Math.floor(s / 60) + ":" + String(s % 60).padStart(2, "0");
}
