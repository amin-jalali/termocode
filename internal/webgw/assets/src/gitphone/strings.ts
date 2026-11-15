// The words of the phone's Git pages that the page's strings do not have.
// They ship in gitphone.js. Same voice as strings.ts. Pure.
const S = {
  all: "All",
  ask: "Ask AI about this",
  add_chat: "Add to chat",
  added: "Added to the chat.",
  already: "It is in the chat already.",
  open_diff: "Open diff",
  discard: "Discard…",
  history: "History",
  sync: "Push, pull and stash",
  more: "More",
  branch: "Branches. Now on {name}",
  ahead: "{n} to push",
  behind: "{n} to pull",
  ai_made: "Made by AI:",
  view_by: "Your view:",
  edit: "Edit",
  reset: "Reset",
  hidden: "{n} more hidden",
  view: "View of {name}",
  group: "Group by",
  g_state: "Status",
  g_folder: "Folder",
  only: "Only files whose path has",
  save: "Save view",
  ask_view: "Ask the AI to change it",
  ask_view_text: "Change the Git page's changes view: ",
  stage_all: "Stage all",
  unstage_all: "Unstage all",
  commit_opts: "More ways to commit",
  stage_commit: "Stage all and commit",
  msg_first: "Write a commit message first.",
  stage_first: "Stage a file first.",
  tick_stage: "Stage {name}",
  tick_unstage: "Unstage {name}",
  commit_n: "Commit {n}",
  menu: "Actions for {name}",
  saved: "Saved the view.",
  view_name: "Git changes",
  folder_root: "Top folder",
  menu_close: "Close the menu",
  commit_title: "Commit changes",
  msg_ph: "Commit message\n\nAdd an optional description",
  staged_one: "1 staged file: {names}",
  staged_files: "{n} staged files: {names}",
  and_more: "and {n} more",
  write: "Write it for me",
  rewrite: "Rewrite",
  writing: "Writing…",
  amend: "Amend last commit…",
  amend_msg: "Put the staged changes into the last commit, with the message “{subject}”?",
  amend_keep: "Put the staged changes into the last commit, with its message?",
} as const;

export type GKey = keyof typeof S;

/** s is one of these strings with its {vars} filled in. */
export function s(key: GKey, vars?: Record<string, string | number>): string {
  let out: string = S[key];
  if (vars) for (const k of Object.keys(vars)) out = out.split(`{${k}}`).join(String(vars[k]));
  return out;
}
