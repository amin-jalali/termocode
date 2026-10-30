// The git letter of a row, in the git colours of the theme.
import { h } from "preact";
import { badgeOf } from "../../state/git";

const cls = { M: "git-mod", A: "git-add", "?": "git-add", D: "git-del", U: "git-conf" } as const;

export function GitBadge({ code }: { code: string }) {
  const b = badgeOf(code);
  return <span class={`git-badge ${cls[b]}`}>{b}</span>;
}
