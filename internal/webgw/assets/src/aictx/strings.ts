// The strings of the AI chat's extras (aictx.js). They ship in aictx.js,
// not in workbench.js: init gives them to the page's strings
// (addStrings), and strings.fa.ts has their Persian texts. Same voice as
// strings.ts. Pure.
export const AS = {
  "ai.ctx_full": "The message takes up to 8 items. Remove one first.",
  "ai.secret_title": "Some columns may hold secrets",
  "ai.secret_body": "The rows you send to the AI have columns that may hold secrets:",
  "ai.secret_mask": "Mask and send",
  "ai.secret_all": "Send all",
} as const;
