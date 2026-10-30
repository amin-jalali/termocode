// Every user-facing string of the web UI, in one place (web UX 7.4).
// One entry per line, "key": "text". Calm, short, present tense; "the
// laptop", "the AI"; no exclamation marks. {name} marks a value.
// Plural forms use .one / .other keys.
export const S = {
  "connect.title": "termocode",
  "connect.once": "This link works once and for 60 seconds.",
  "connect.open": "Open workspace",
  "connect.opening": "Opening…",
  "firstrun": "This is the same termocode as on your laptop. What you do here happens there.",
  "firstrun.ok": "Got it",
  "strip.web": "web {n}",
  "strip.ai_idle": "idle",
  "strip.ai_working": "working",
  "strip.ai_waiting": "needs you",
  "strip.theme": "Theme",
  "tab.chat": "Chat",
  "tab.files": "Files",
  "tab.term": "Term",
  "tab.git": "Git",
  "tab.more": "More",
  "tabs.label": "Views",
  "a11y.skip": "Skip to content",
  "pill.cards.one": "{n} approval waiting",
  "pill.cards.other": "{n} approvals waiting",
  "pill.review": "Review",
  "pill.run_done": "A run finished",
  "pill.run_failed": "A run failed",
  "pill.task_done": "A task finished",
  "pill.waiting_laptop": "{n} waiting for the laptop",
  "offline.lost": "Connection lost. Trying again in {s} s…",
  "offline.trying": "Connection lost. Trying again now…",
  "offline.try": "Try now",
  "offline.back": "Reconnected after {s} s.",
  "offline.input": "Offline",
  "rtt.slow": "Slow link ({ms} ms)",
  "rtt.label": "Link {ms} ms",
  "loading": "Loading…",
  "loading.slow": "Still loading…",
  "chat.empty": "No chat yet. Ask the AI something or open a chat on the laptop.",
  "files.no_ws": "No workspace is open on the laptop.",
  "term.none": "No terminal is open on the laptop.",
  "git.clean": "Nothing to commit. The tree is clean.",
  "more.this_phone": "This phone",
  "more.theme": "Theme",
  "more.theme.system": "System",
  "more.theme.dark": "Dark",
  "more.theme.light": "Light",
  "more.dir": "Text direction",
  "more.dir.auto": "Auto",
  "more.dir.ltr": "Left to right",
  "more.dir.rtl": "Right to left",
  "more.about": "About",
  "more.about.body": "Experimental. The page shows what termocode on the laptop shows; it never runs code of its own.",
  "more.licenses": "Licenses",
  "toast.close": "Close",
  "toast.more": "+{n}",
  "e.link_expired": "This link has expired or was already used. In termocode, run Web: Open in Browser again.",
  "e.no_link": "Open this page from termocode on the laptop: run Web: Open in Browser.",
  "e.stopped": "The web session was stopped.",
  "e.idle": "The web session stopped after 15 minutes without typing.",
  "e.max": "The web session stopped after 12 hours.",
  "e.still_running": "Your work is still running on the laptop.",
  "e.ended": "This termocode session has ended.",
  "e.device_removed": "This device was removed. Pair it again from termocode.",
  "e.taken_over": "Another tab took over this session.",
  "e.too_many": "Too many browsers are open on this session.",
  "e.version": "This page is older than termocode. Reload to get the new one.",
  "e.reload": "Reload",
  "e.busy": "termocode is busy; the page will update in a moment.",
  "e.auth_busy": "Too many tries at once. Wait a moment, then open the link again.",
  "err.internal": "Something failed inside termocode. The laptop has the details.",
} as const;

export type StringKey = keyof typeof S;

/** t returns the text of key with each {name} replaced by vars[name]. */
export function t(key: StringKey, vars?: Record<string, string | number>): string {
  const text: string = S[key];
  if (!vars) return text;
  return text.replace(/\{([a-z_]+)\}/g, (m, name: string) => (name in vars ? String(vars[name]) : m));
}

/** tn picks the .one or .other form of a plural key by n. */
export function tn(key: "pill.cards", n: number): string {
  return t(n === 1 ? `${key}.one` : `${key}.other`, { n });
}
