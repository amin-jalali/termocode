// ProjectTasks (MOB-52): the phone view of a project's tasks — its own
// checklists (TASKS.md, TODO.md), open issues, pull requests and merge
// requests — read with one project.tasks call. The door (MOB-50) sends
// the call to the open project's hub. Chips pick a kind ("All" first);
// "Done too" shows checked checklist items. A section that the laptop
// could not read says why (sign in to gh on the laptop, set a GitLab
// token there) and the others still show. A row's tap hands the task to
// the caller (the action sheet, MOBUX-8; ↩ to chat, MOBUX-19); a link
// opens only after the confirm sheet. Phone layout only: this file comes
// with the phone's lazy chunk (MOB-4 / MOBUX-7 Tasks tab), never app.js.
// The phone app is English only, so its words live here, not in the
// page's translated strings.
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { ProjectTask, ProjectTaskSection, ProjectTasks as Result, ProjectTasksArgs } from "../../../../webproto/proto";
import { askLink } from "../lib/links";
import { link, live } from "../session";
import { Block } from "./Block";
import { PhoneChip, PhoneChips } from "./Chip";
import { Row, Rows } from "./Row";
import type { Status } from "./status";

type Only = NonNullable<ProjectTasksArgs["only"]> | "";

const TXT = {
  title: "Tasks",
  all: "All",
  checklist: "Checklists",
  issues: "Issues",
  prs: "Pull requests",
  mrs: "Merge requests",
  done: "Done too",
  refresh: "Refresh",
  loading: "Reading the project's tasks…",
  none: "No tasks here.",
  noneOpen: "Nothing open. Tap “Done too” to see the checked ones.",
  offline: "Offline — the list shows when the laptop answers.",
  retry: "Retry",
  more: "{n} more on the laptop",
  open: "Open link",
  draft: "draft",
  kinds: "Task kinds",
} as const;

const KINDS: readonly { only: Only; label: string }[] = [
  { only: "", label: TXT.all },
  { only: "checklist", label: TXT.checklist },
  { only: "issues", label: TXT.issues },
  { only: "prs", label: TXT.prs },
  { only: "mrs", label: TXT.mrs },
];

/** taskStatus is a row's status (colour + shape): a checked item is
 * done; a pull or merge request follows its checks; else idle. */
export function taskStatus(it: ProjectTask): Status {
  if (it.state === "done") return "done";
  switch (it.checks) {
    case "fail":
      return "failed";
    case "pending":
      return "working";
    case "pass":
      return "review";
  }
  return "idle";
}

/** taskMeta is the row's short facts line. */
function taskMeta(it: ProjectTask): string {
  const bits: string[] = [];
  if (it.kind === "ptask") {
    if (it.group) bits.push(it.group);
    bits.push(`${it.path ?? ""}:${it.line ?? 0}`);
  } else {
    bits.push(`${it.kind === "mr" ? "!" : "#"}${it.number ?? 0}`);
    if (it.author) bits.push(it.author);
    if (it.branch) bits.push(it.branch);
    if (it.state === "draft") bits.push(TXT.draft);
    for (const l of it.labels ?? []) bits.push(l);
  }
  return bits.join(" · ");
}

function Section({ s, onPick }: { s: ProjectTaskSection; onPick?: (it: ProjectTask) => void }) {
  const hidden = s.total - s.items.length;
  return (
    <Block kind={s.kind === "checklist" ? "list" : s.kind} title={`${s.title} (${s.open})`}>
      {s.note && <p class="ph-note">{s.note}</p>}
      {!s.note && s.items.length === 0 && <p class="ph-note">{s.total > 0 ? TXT.noneOpen : TXT.none}</p>}
      {s.items.length > 0 && (
        <Rows label={s.title}>
          {s.items.map((it) => (
            <div key={it.id} class="ph-task" style={it.depth ? { paddingInlineStart: `${Math.min(it.depth, 4) * 12}px` } : undefined}>
              <Row status={taskStatus(it)} title={it.title} meta={taskMeta(it)} onClick={onPick ? () => onPick(it) : undefined} />
              {it.url && (
                <button type="button" class="ph-row-link" aria-label={TXT.open} title={TXT.open} onClick={() => askLink(it.url ?? "")}>
                  ↗
                </button>
              )}
            </div>
          ))}
        </Rows>
      )}
      {hidden > 0 && s.items.length > 0 && s.kind !== "checklist" && <p class="ph-note">{TXT.more.replace("{n}", String(hidden))}</p>}
    </Block>
  );
}

/** ProjectTasksView lists a project's tasks (ws: its workspace key for
 * the door, "" the open one); onPick receives the tapped task (its id is
 * what an attach of kind ptask / issue / pr names). */
export function ProjectTasksView({ onPick, ws = "" }: { onPick?: (it: ProjectTask) => void; ws?: string }) {
  const [only, setOnly] = useState<Only>("");
  const [done, setDone] = useState(false);
  const [again, setAgain] = useState(0);
  const [data, setData] = useState<Result | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const on = live.value;
  useEffect(() => {
    if (!on) return;
    let stop = false;
    const args: ProjectTasksArgs = {};
    if (only) args.only = only;
    if (done) args.done = true;
    setBusy(true);
    link
      .requestWithID("project.tasks", args, ws || undefined)
      .result
      .then(
        (r) => {
          if (stop) return;
          setData(r as Result);
          setErr("");
        },
        (e: unknown) => !stop && setErr(e instanceof Error ? e.message : String(e)),
      )
      .finally(() => !stop && setBusy(false));
    return () => {
      stop = true;
    };
  }, [only, done, again, on, ws]);

  return (
    <div class="ph-tasks" aria-busy={busy ? "true" : undefined}>
      <PhoneChips label={TXT.kinds}>
        {KINDS.map((k) => (
          <PhoneChip key={k.only || "all"} label={k.label} on={only === k.only} onClick={() => setOnly(k.only)} />
        ))}
        {(only === "" || only === "checklist") && <PhoneChip label={TXT.done} on={done} onClick={() => setDone((d) => !d)} />}
        <PhoneChip label={TXT.refresh} onClick={() => setAgain((x) => x + 1)} />
      </PhoneChips>
      {!on && <p class="ph-note">{TXT.offline}</p>}
      {err && (
        <p class="ph-note ph-err">
          {err}{" "}
          <button type="button" class="ph-retry" onClick={() => setAgain((x) => x + 1)}>
            {TXT.retry}
          </button>
        </p>
      )}
      {!err && !data && on && (
        <p class="ph-note" role="status">
          {TXT.loading}
        </p>
      )}
      {data && data.sections.length === 0 && <p class="ph-note">{TXT.none}</p>}
      {data?.sections.map((s, i) => (
        <Section key={`${s.kind}:${s.title}:${i}`} s={s} onPick={onPick} />
      ))}
    </div>
  );
}
