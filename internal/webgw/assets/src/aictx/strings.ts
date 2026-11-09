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
  "ai.pick": "Search items to add to the AI chat",
  "ai.k_db": "database",
  "ai.k_table": "table",
  "ai.k_host": "host",
  "ai.k_project": "compose project",
  "ai.k_container": "container",
  "ai.model": "AI model",
  "ai.use_model": "Use model {name} for the AI chat?",
  "plus.label": "Add a file or choose the AI model",
  "plus.title": "Add to the chat",
  "plus.file": "Add a file from Files",
  "look.thinking": "Thinking…",
  "look.tokens": "{in} in · {out} out",
  "look.rules": "Rules: {name}",
  "look.step_of": "step {n} of {of}",
  "look.steps": "{n} steps",
  "look.run_view": "Run view",
} as const;
