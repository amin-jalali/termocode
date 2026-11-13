// The strings of the session picker's list (picker.js, WEB-55). They ship
// in picker.js, not in app.js: the page hands them to its strings
// (addStrings) when the list is drawn, and strings.fa.ts has their Persian
// texts. Same voice as strings.ts. Pure.
export const PS = {
  "sp.title": "Your sessions",
  "sp.body": "These termocode sessions run on this laptop. Pick the one to open.",
  "sp.loading": "Looking for your sessions…",
  "sp.continue": "Open this session",
  "sp.here": "this one",
  "sp.waiting": "{n} wait",
  "sp.working": "{n} working",
  "sp.ago_now": "just now",
  "sp.ago_min": "{n} min ago",
  "sp.ago_hour": "{n} h ago",
  "sp.ago_day": "{n} d ago",
} as const;
