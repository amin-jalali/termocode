// The gutter peek (GITX-21 on the web; the laptop's Git: Peek Change
// Under Cursor): a click on a git mark of the desktop editor, or the
// command on the caret's line, reads that change (git.peek) and opens
// its card (views/git/PeekCard.tsx).
import { signal } from "@preact/signals";
import type { GitPeek } from "../../../../webproto/proto";
import { link } from "../session";
import { gitFailed } from "../views/git/errors";

/** peekOpen is the change on the card (null: no card). */
export const peekOpen = signal<GitPeek | null>(null);

/** peekChange reads the change under line (1-based) of path and shows it. */
export function peekChange(path: string, line: number): void {
  link.request("git.peek", { path, line }).then((r) => (peekOpen.value = r as GitPeek), gitFailed);
}

/** peekNext is the first change start below line, or the first one. */
export function peekNext(starts: readonly number[], line: number): number {
  return starts.find((s) => s > line) ?? starts[0] ?? line;
}
