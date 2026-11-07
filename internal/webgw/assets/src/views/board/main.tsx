// The live board of the phone (MOB-18, mobile plan §2.5, MOBUX-9), its
// own lazy file (board.js): what goes on in this project in four
// columns — needs you, working, review, done today. Swipe sideways
// between the columns, or tap a column above them. A tile shows its
// source, its state as a shape and a color, the agent or profile, the
// step, the cost and a lane's changes; a tap opens the card or the view
// it comes from. The laptop builds the board (board.get); the page asks
// again when one of its topics changes, at most twice a second, so the
// board stays live. Offline, the last board stays, marked as old.
// #/more/board
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { Board, BoardItem } from "../../../../../webproto/proto";
import { go } from "../../lib/router";
import { link, store } from "../../session";
import { t } from "../../strings";
import { SkeletonRows } from "../../ui/Skeleton";
import { SubView } from "../more/SubView";
import { COLUMNS, COLUMN_KEY, KIND_KEY, SHAPE, STATE_KEY, byColumn, openRoute, usd, type Column } from "./model";

/** SOURCES are the topics the board is built from. */
const SOURCES = ["cards", "runs", "worktrees", "workflows"];
const MIN_GAP_MS = 500;

/** useBoard keeps the board fresh: it asks when a source topic moves or
 * the link comes back, never more than once per MIN_GAP_MS, one request
 * at a time. */
function useBoard(): { board: Board | null; failed: boolean; again: () => void } {
  const [board, setBoard] = useState<Board | null>(null);
  const [failed, setFailed] = useState(false);
  const r = useRef({ last: 0, timer: 0, busy: false, more: false, alive: true });
  const ask = () => {
    const s = r.current;
    if (s.busy) {
      s.more = true;
      return;
    }
    s.busy = true;
    s.last = Date.now();
    link
      .request("board.get")
      .then(
        (d) => {
          if (!s.alive) return;
          setBoard(d as Board);
          setFailed(false);
        },
        () => s.alive && setFailed(true),
      )
      .then(() => {
        s.busy = false;
        if (s.more) {
          s.more = false;
          later();
        }
      });
  };
  const later = () => {
    const s = r.current;
    if (s.timer || !s.alive) return;
    s.timer = window.setTimeout(
      () => {
        s.timer = 0;
        ask();
      },
      Math.max(0, s.last + MIN_GAP_MS - Date.now()),
    );
  };
  useEffect(() => {
    link.subscribe(...SOURCES);
    const s = r.current;
    return () => {
      s.alive = false;
      if (s.timer) clearTimeout(s.timer);
    };
  }, []);
  const seqs = SOURCES.map((n) => store.topic(n).value?.seq ?? -1).join(",");
  const online = link.live.value;
  useEffect(() => {
    if (online) later();
  }, [seqs, online]);
  return { board, failed, again: later };
}

function Tile({ it }: { it: BoardItem }) {
  const meta: string[] = [t(KIND_KEY[it.kind])];
  if (it.sub) meta.push(it.sub);
  if (it.steps) meta.push(t("runs.step", { n: it.step ?? 0, m: it.steps }));
  if (it.cost_usd) meta.push(usd(it.cost_usd));
  if (it.ahead) meta.push(t("board.ahead", { n: it.ahead }));
  if (it.dirty) meta.push(t("board.dirty", { n: it.dirty }));
  return (
    <li class={`board-tile st-${it.state}`} data-id={it.id}>
      <button type="button" class="board-open" onClick={() => go(openRoute(it))}>
        <span class="board-shape" aria-hidden="true">
          {SHAPE[it.state]}
        </span>
        <span class="board-text">
          <span class="sr-only">{t(STATE_KEY[it.state])}</span>
          <span class="board-title" dir="auto">
            {it.title}
          </span>
          <span class="board-meta" dir="auto">
            {meta.join(" · ")}
          </span>
          {it.why && (
            <span class="board-why" dir="auto">
              {it.why}
            </span>
          )}
        </span>
      </button>
    </li>
  );
}

export function BoardView() {
  const { board, failed, again } = useBoard();
  const [col, setCol] = useState<Column>("needs_you");
  const cols = useRef<HTMLDivElement | null>(null);
  const offline = !link.live.value;
  // A tap on a column title scrolls to it; a swipe sets the title.
  const show = (c: Column) => {
    setCol(c);
    const el = cols.current?.querySelector<HTMLElement>(`[data-col="${c}"]`);
    el?.scrollIntoView({ behavior: "smooth", block: "nearest", inline: "start" });
  };
  const onScroll = () => {
    const el = cols.current;
    if (!el || !el.clientWidth) return;
    const i = Math.round(Math.abs(el.scrollLeft) / el.clientWidth);
    const c = COLUMNS[Math.min(COLUMNS.length - 1, i)];
    if (c !== col) setCol(c);
  };
  const today = board?.today_usd ? t("board.today", { s: usd(board.today_usd) }) : "";
  const groups = board ? byColumn(board) : null;
  return (
    <SubView title={t("more.board")} name="board" actions={today ? <span class="board-cost">{today}</span> : undefined}>
      {!board ? (
        failed ? (
          <div class="board-failed">
            <p class="empty">{t("board.load_failed")}</p>
            <button type="button" class="btn" onClick={again}>
              {t("retry")}
            </button>
          </div>
        ) : (
          <SkeletonRows rows={4} />
        )
      ) : (
        <div class={`board${offline ? " is-stale" : ""}`}>
          {offline && <p class="board-stale">{t("board.stale")}</p>}
          <div class="board-tabs" role="tablist" aria-label={t("board.columns")}>
            {COLUMNS.map((c) => (
              <button
                key={c}
                type="button"
                role="tab"
                class={`board-tab col-${c}${c === col ? " is-on" : ""}`}
                aria-selected={c === col ? "true" : "false"}
                onClick={() => show(c)}
              >
                {t(COLUMN_KEY[c])} <span class="board-n">{board.counts[c]}</span>
              </button>
            ))}
          </div>
          {board.items.length === 0 ? (
            <p class="empty">{t("board.empty_body")}</p>
          ) : (
            <div class="board-cols" ref={cols} onScroll={onScroll}>
              {COLUMNS.map((c) => (
                <section key={c} class={`board-col col-${c}`} data-col={c} aria-label={t(COLUMN_KEY[c])}>
                  {groups && groups[c].length > 0 ? (
                    <ul class="board-list">
                      {groups[c].map((it) => (
                        <Tile key={it.id} it={it} />
                      ))}
                    </ul>
                  ) : (
                    <p class="empty">{t("board.empty_col")}</p>
                  )}
                </section>
              ))}
            </div>
          )}
          {board.more ? <p class="board-more">{t("board.more", { n: board.more })}</p> : null}
        </div>
      )}
    </SubView>
  );
}
