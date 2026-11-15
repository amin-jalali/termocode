// The tool pages' words (pages.js), English only like the phone's own
// views; added to the page's strings when pages.js starts (init).
import { H } from "./host";

export const PG = {
  "pg.ask": "Ask AI about this",
  "pg.add_chat": "Add to chat ↩",
  "pg.chat_added": "Added to the chat's next message.",
  "pg.chat_full": "The chat's next message has 8 items already.",
  "pg.chat_none": "Open Chat once, then try again.",
  "pg.copy": "Copy value",
  "pg.copied": "Copied.",
  "pg.copy_failed": "Could not copy.",
  "pg.customize": "Customize {name}",
  "pg.ask_view": "Ask AI to change this list",
  "pg.ask_view_text": "Change this list on my phone's {page} page: ",
  "pg.search": "Search",
  "pg.cancel": "Cancel",
  "pg.save": "Save",
  "pg.back": "Back",
  "ps.title": "Settings",
  "ps.hint": "The laptop's settings. Changes apply there at once.",
  "ps.none": "No settings to show.",
  "ps.no_match": "No setting matches.",
  "ps.changed": "Changed",
  "ps.search": "Search settings",
  "ps.default": "Default: {value}",
  "ps.more": "More…",
  "ps.less": "Less",
  "ps.ask": "Ask AI about this setting",
  "ps.ask_text": "Explain this setting and whether I should change it: ",
  "ps.risky": "Change {name} to {value}?",
  "ps.risky_why": "This lowers a safety check on the laptop.",
  "ps.change": "Change",
  "ps.value": "Value",
  "ps.on": "On",
  "ps.off": "Off",
  "ps.less_one": "Less",
  "ps.more_one": "More",
} as const;

export type PGKey = keyof typeof PG;

/** tx is the page's t for the pages' own keys. */
export const tx = (k: PGKey, vars?: Record<string, string | number>): string => H.t(k, vars);
