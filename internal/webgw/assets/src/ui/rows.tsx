// Rows of the dense views: terminal, code, diff and tree. Code-like rows
// are always LTR and isolated, so a right-to-left word inside cannot
// reorder the row (web UX P7); a soft-wrapped row shows ↳ on its
// continuation lines.
import { h } from "preact";

export function TermRow({ text, cont }: { text: string; cont?: boolean }) {
  return (
    <div class={`trow${cont ? " is-cont" : ""}`} dir="ltr">
      <span class="wrap-mark" aria-hidden="true">{cont ? "↳" : ""}</span>
      <span class="trow-text">{text}</span>
    </div>
  );
}

export function CodeRow({ i, n, text, cont, flash, sel }: { i?: number; n?: number; text: string; cont?: boolean; flash?: boolean; sel?: boolean }) {
  return (
    <div class={`crow${flash ? " is-flash" : ""}${sel ? " is-sel" : ""}`} dir="ltr" data-i={i}>
      <span class="crow-n" aria-hidden="true">{cont ? "" : n}</span>
      <span class="wrap-mark" aria-hidden="true">{cont ? "↳" : ""}</span>
      <span class="crow-text">{text}</span>
    </div>
  );
}

export type DiffKind = "add" | "del" | "ctx" | "hunk";

export function DiffRow({ kind, a, b, text }: { kind: DiffKind; a?: number; b?: number; text: string }) {
  const sign = kind === "add" ? "+" : kind === "del" ? "−" : kind === "hunk" ? "" : " ";
  return (
    <div class={`drow drow-${kind}`} dir="ltr">
      <span class="drow-n" aria-hidden="true">{kind === "hunk" ? "" : a ?? ""}</span>
      <span class="drow-n" aria-hidden="true">{kind === "hunk" ? "" : b ?? ""}</span>
      <span class="drow-sign">{sign}</span>
      <span class="drow-text">{text}</span>
    </div>
  );
}

export type GitBadge = "M" | "A" | "?" | "D" | "U" | "R";

const badgeClass: Record<GitBadge, string> = { M: "git-mod", A: "git-add", "?": "git-add", D: "git-del", U: "git-conf", R: "git-mod" };

/** gitBadge is the badge of a git status letter, or undefined when clean or unknown. */
export function gitBadge(letter: unknown): GitBadge | undefined {
  return typeof letter === "string" && letter in badgeClass ? (letter as GitBadge) : undefined;
}

function FolderGlyph({ open }: { open?: boolean }) {
  return (
    <svg class="icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linejoin="round" aria-hidden="true">
      {open ? (
        <path d="M3.5 7a1.5 1.5 0 0 1 1.5-1.5h4l2 2h8a1.5 1.5 0 0 1 1.5 1.5v1H7.5l-3 8.5H5A1.5 1.5 0 0 1 3.5 17z M4.5 18.5l3-8.5H22l-3 8.5z" />
      ) : (
        <path d="M3.5 6.5a1.5 1.5 0 0 1 1.5-1.5h4l2 2h8a1.5 1.5 0 0 1 1.5 1.5v9a1.5 1.5 0 0 1-1.5 1.5H5a1.5 1.5 0 0 1-1.5-1.5z" />
      )}
    </svg>
  );
}

function FileGlyph() {
  return (
    <svg class="icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.75" stroke-linejoin="round" aria-hidden="true">
      <path d="M6 3.5h8l4 4v13H6z M14 3.5v4h4" />
    </svg>
  );
}

export function TreeRow({
  depth = 0,
  dir,
  open,
  name,
  git,
  dirty,
  active,
  dim,
  path,
  onClick,
}: {
  depth?: number;
  dir?: boolean;
  open?: boolean;
  name: string;
  git?: GitBadge;
  dirty?: boolean;
  active?: boolean;
  /** dim: an ignored name, drawn dimmed as on the laptop. */
  dim?: boolean;
  /** path is the workspace path the row stands for. */
  path?: string;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      class={`tree-row depth-${Math.min(depth, 8)}${active ? " is-active" : ""}${dim ? " is-dim" : ""}`}
      aria-expanded={dir ? (open ? "true" : "false") : undefined}
      data-path={path}
      onClick={onClick}
    >
      <span class={`chev${open ? " is-open" : ""}`} aria-hidden="true">{dir ? "›" : ""}</span>
      <span class={`tree-icon${dir ? " is-dir" : ""}`}>{dir ? <FolderGlyph open={open} /> : <FileGlyph />}</span>
      <span class="tree-name" dir="auto">{name}</span>
      {dirty && <span class="dirty-dot" aria-hidden="true">●</span>}
      {git && <span class={`git-badge ${badgeClass[git]}`}>{git}</span>}
    </button>
  );
}
