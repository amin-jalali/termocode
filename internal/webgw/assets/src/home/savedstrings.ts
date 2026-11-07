// The strings of the phone's saved views (MOBUX-18), English only (owner
// 2026-11-07: the phone app is English only). They live in home.js with
// the Home; same voice as strings.ts: calm, short, present tense. {name}
// marks a value.

export const SV = {
  "save": "Save as view",
  "save.title": "Save as view",
  "save.name": "Name",
  "save.pin": "📌 Pin to Home",
  "save.pin_note": "Pinned views show on Home on every paired phone.",
  "save.replaces": "Replaces the saved view “{name}”.",
  "save.where": "Saved on the laptop. Only the view is saved, never its data: it runs again, live, each time.",
  "save.do": "Save",
  "save.done": "Saved “{name}”.",
  "cancel": "Cancel",
  "list.title": "Saved views",
  "list.empty": "No saved views yet. Open a view and tap Save as view.",
  "list.count": "{n} of {max} saved · {p} of {pmax} on Home",
  "row.open": "Open",
  "row.pin": "📌 Pin to Home",
  "row.unpin": "Unpin from Home",
  "row.rename": "Rename",
  "row.delete": "Delete",
  "row.pinned": "Pinned to Home",
  "row.actions": "Actions for {name}",
  "rename.title": "Rename view",
  "rename.do": "Rename",
  "delete.ask": "Delete “{name}”? Its data stays on the laptop; only the saved view goes.",
  "delete.do": "Delete",
  "pinned.title": "Pinned views",
  "pinned.live": "live",
  "pinned.stale": "Offline — last seen {age}",
  "pinned.expand": "Show {name}",
  "pinned.collapse": "Hide {name}",
  "pinned.refresh": "Refresh",
  "pinned.loading": "Running the view on the laptop…",
  "pinned.cannot": "This view no longer runs on the laptop.",
  "err.offline": "Offline — nothing was saved.",
  "err.full": "32 views are saved already. Delete one first.",
  "err.pins": "8 views are on Home already. Unpin one first.",
  "err.name": "Another view has that name.",
  "err.gone": "That view is not there any more.",
} as const;

export type SVKey = keyof typeof SV;

/** sv is one saved-views string with its {values} filled in. */
export function sv(key: SVKey, vars?: Record<string, string | number>): string {
  let s: string = SV[key];
  if (vars) for (const [k, v] of Object.entries(vars)) s = s.split("{" + k + "}").join(String(v));
  return s;
}
