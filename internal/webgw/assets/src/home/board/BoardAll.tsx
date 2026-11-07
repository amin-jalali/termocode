// The live board of every project (MOB-54, mobile plan 2.5), in the
// phone's Home (home.js; Preact comes from the page through home/jsx.ts).
// The home project's rows come live from its topics; the other projects
// show their roll-up agents (board.projects, asked again every 15 s while
// the board is open: no idle wakeups when it is closed). Tapping a row
// hands its ref to the Home (open a card, a run, a lane); a project
// header opens that project. Read only: nothing here writes.
import type { BoardProjects, RunsSnap, WorkflowsSnap, WorktreesSnap } from "../../../../../webproto/proto";
import { H, h } from "../jsx";
import { type BoardFilter, type BoardRow, type BoardSection, type ProjectData, SHAPE, boardMoney, buildBoard, sectionLine } from "./model";
import { dict } from "../../state/dict";

/** BS are the board's strings: English only (owner 2026-11-07). */
export const BS = {
  title: "Board",
  live: "live",
  today: "{usd} today",
  all: "All",
  needs: "Only needs you",
  running: "Only running",
  empty: "Nothing runs in any project.",
  emptyNeeds: "Nothing needs you.",
  emptyRunning: "Nothing is running.",
  more: "+{n} more projects",
  loading: "Loading the board…",
  retry: "Retry",
  open: "Open {name}",
  summary: "From the project's window. Open it for details.",
} as const;

function fill(s: string, k: string, v: string | number): string {
  return s.replace("{" + k + "}", String(v));
}

const EVERY_MS = 15_000;

/** BoardAllProps: what the Home does on a tap. */
export interface BoardAllProps {
  onRow?: (ws: string, row: BoardRow) => void;
  onProject?: (s: BoardSection) => void;
}

export function BoardAll(props: BoardAllProps) {
  const [list, setList] = H.useState<BoardProjects | null>(null);
  const [err, setErr] = H.useState("");
  const [filter, setFilter] = H.useState<BoardFilter>("all");
  const [tries, setTries] = H.useState(0);

  H.useEffect(() => {
    // kept after the board closes: other Home views read the same topics
    H.link.subscribe("runs", "worktrees", "workflows");
  }, []);

  H.useEffect(() => {
    let alive = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const ask = () => {
      H.link.request("board.projects").then(
        (v) => {
          if (!alive) return;
          setList(v as BoardProjects);
          setErr("");
          timer = setTimeout(ask, EVERY_MS);
        },
        (e: unknown) => {
          if (!alive) return;
          setErr(H.errText(e)); // a calm line with Retry; no retry loop while offline
        },
      );
    };
    ask();
    return () => {
      alive = false;
      if (timer) clearTimeout(timer);
    };
  }, [tries]);

  const runs = H.store.topic("runs").value?.data as RunsSnap | undefined;
  const lanes = H.store.topic("worktrees").value?.data as WorktreesSnap | undefined;
  const flows = H.store.topic("workflows").value?.data as WorkflowsSnap | undefined;
  const data = dict<ProjectData>();
  if (list) data[list.home] = { runs: runs?.runs, worktrees: lanes?.worktrees, workflows: flows?.runs, cards: H.cards.value };
  const view = buildBoard(list, data, filter);

  const chip = (f: BoardFilter, label: string) => (
    <button type="button" class={"chip board-chip" + (filter === f ? " is-on" : "")} aria-pressed={filter === f} onClick={() => setFilter(f)}>
      {label}
    </button>
  );

  return (
    <section class="board" aria-label={BS.title}>
      <header class="board-head">
        <h2 class="board-title">
          {BS.title} <span class="board-live">{H.live.value ? "● " + BS.live : "○"}</span>
        </h2>
        {view.costUSD > 0 && <span class="board-cost">{fill(BS.today, "usd", boardMoney(view.costUSD))}</span>}
      </header>
      {!list && !err && <p class="board-note" role="status">{BS.loading}</p>}
      {err && (
        <p class="board-note board-err" role="alert">
          ✗ {err}{" "}
          <button type="button" class="board-retry" onClick={() => setTries(tries + 1)}>
            {BS.retry}
          </button>
        </p>
      )}
      {list && view.sections.every((s) => s.rows.length === 0) && (
        <p class="board-note">{filter === "needs" ? BS.emptyNeeds : filter === "running" ? BS.emptyRunning : BS.empty}</p>
      )}
      {view.sections.map((s) => (
        <div class={"board-sec tone-" + s.tone} key={s.ws}>
          <button type="button" class="board-proj" aria-label={fill(BS.open, "name", s.name)} onClick={() => props.onProject?.(s)}>
            <span class="board-proj-name">{s.name}</span>
            <span class="board-proj-line">{sectionLine(s)}</span>
          </button>
          {!s.detailed && s.rows.length > 0 && <p class="board-sub">{BS.summary}</p>}
          <ul class="board-rows">
            {s.rows.map((r) => (
              <li key={r.key}>
                <button type="button" class={"board-row tone-" + r.tone} disabled={!r.ref} onClick={() => props.onRow?.(s.ws, r)}>
                  <span class="board-shape" aria-hidden="true">
                    {SHAPE[r.tone]}
                  </span>
                  <span class="board-row-title">{r.title}</span>
                  {r.detail && <span class="board-row-detail">{r.detail}</span>}
                </button>
              </li>
            ))}
          </ul>
        </div>
      ))}
      {view.more > 0 && <p class="board-note">{fill(BS.more, "n", view.more)}</p>}
      <div class="board-chips" role="group" aria-label={BS.title}>
        {chip("all", BS.all)}
        {chip("needs", BS.needs)}
        {chip("running", BS.running)}
      </div>
    </section>
  );
}
