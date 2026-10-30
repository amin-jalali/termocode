// The git topic as the Git tab reads it (web design 2.6). Every field is
// checked here, so a newer or older hub never breaks the view. Pure: no
// DOM, timers or network.

export interface GitFile {
  path: string;
  x: string;
  y: string;
  staged: boolean;
  unstaged: boolean;
  untracked: boolean;
  conflict: boolean;
}

export interface GitCommit {
  hash: string;
  subject: string;
  author: string;
  when: number;
  unpushed: boolean;
}

export interface GitState {
  loading: boolean;
  repo: boolean;
  branch: { name: string; ahead: number; behind: number };
  op: string;
  files: GitFile[];
  more: number;
  log: GitCommit[];
}

export interface DiffLine {
  k: "ctx" | "add" | "del" | "hunk" | "file";
  text: string;
  old: number;
  new: number;
}

export interface DiffPage {
  rows: DiffLine[];
  next: number;
  added: number;
  removed: number;
  binary: boolean;
  truncated: boolean;
  commit: { hash: string; subject: string; body: string; author: string; when: number } | null;
}

type Obj = Record<string, unknown>;

function obj(v: unknown): Obj {
  return typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Obj) : {};
}

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) && v >= 0 ? Math.floor(v) : 0;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function list(v: unknown): unknown[] {
  return Array.isArray(v) ? v : [];
}

/** readGit turns the git topic's data (or nothing yet) into the view's state. */
export function readGit(data: unknown): GitState {
  const d = obj(data), b = obj(d.branch);
  return {
    loading: d.loading === true,
    repo: d.repo === true,
    branch: { name: str(b.name), ahead: num(b.ahead), behind: num(b.behind) },
    op: str(d.op),
    files: list(d.files)
      .map(obj)
      .filter((f) => str(f.path) !== "")
      .map((f) => ({
        path: str(f.path),
        x: str(f.x) || " ",
        y: str(f.y) || " ",
        staged: f.staged === true,
        unstaged: f.unstaged === true,
        untracked: f.untracked === true,
        conflict: f.conflict === true,
      })),
    more: num(d.more),
    log: list(d.log)
      .map(obj)
      .map((c) => ({ hash: str(c.hash), subject: str(c.subject), author: str(c.author), when: num(c.when), unpushed: c.unpushed === true })),
  };
}

const KINDS: ReadonlySet<string> = new Set(["ctx", "add", "del", "hunk", "file"]);

/** readDiff reads one page of git.diff or git.show. */
export function readDiff(data: unknown): DiffPage {
  const d = obj(data), c = obj(d.commit);
  return {
    rows: list(d.rows)
      .map(obj)
      .filter((r) => KINDS.has(str(r.k)))
      .map((r) => ({ k: str(r.k) as DiffLine["k"], text: str(r.text), old: num(r.old), new: num(r.new) })),
    next: num(d.next),
    added: num(d.added),
    removed: num(d.removed),
    binary: d.binary === true,
    truncated: d.truncated === true,
    commit: d.commit ? { hash: str(c.hash), subject: str(c.subject), body: str(c.body), author: str(c.author), when: num(c.when) } : null,
  };
}

export type Side = "conflicts" | "staged" | "changes" | "untracked";

export interface Groups {
  conflicts: GitFile[];
  staged: GitFile[];
  changes: GitFile[];
  untracked: GitFile[];
}

/** groupFiles sorts files into the tab's groups. A file with a staged and
 * an unstaged change is in both Staged and Changes; a conflict only in
 * Conflicts (read-only here). */
export function groupFiles(files: GitFile[]): Groups {
  const g: Groups = { conflicts: [], staged: [], changes: [], untracked: [] };
  for (const f of files) {
    if (f.conflict) g.conflicts.push(f);
    else if (f.untracked) g.untracked.push(f);
    else {
      if (f.staged) g.staged.push(f);
      if (f.unstaged) g.changes.push(f);
    }
  }
  return g;
}

/** badgeOf maps a git status letter to the row's badge. */
export function badgeOf(code: string): "M" | "A" | "?" | "D" | "U" {
  switch (code) {
    case "A":
      return "A";
    case "D":
      return "D";
    case "?":
      return "?";
    case "U":
      return "U";
    default:
      return "M";
  }
}

/** splitPath splits "a/b/c.go" into the name and its folder. */
export function splitPath(p: string): { name: string; dir: string } {
  const trimmed = p.endsWith("/") ? p.slice(0, -1) : p;
  const i = trimmed.lastIndexOf("/");
  const name = (i < 0 ? trimmed : trimmed.slice(i + 1)) + (p.endsWith("/") ? "/" : "");
  return { name, dir: i < 0 ? "" : trimmed.slice(0, i) };
}

/** age writes how long ago a Unix ms time was, short: "now", "5m", "3h", "2d", "4w", "2y". */
export function age(when: number, now: number): string {
  const s = Math.max(0, Math.floor((now - when) / 1000));
  if (s < 60) return "now";
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  if (s < 7 * 86400) return `${Math.floor(s / 86400)}d`;
  if (s < 365 * 86400) return `${Math.floor(s / (7 * 86400))}w`;
  return `${Math.floor(s / (365 * 86400))}y`;
}

/** commitMessage joins the subject and the optional body as git wants it. */
export function commitMessage(subject: string, body: string): string {
  const s = subject.trim(), b = body.trim();
  return b ? `${s}\n\n${b}` : s;
}
