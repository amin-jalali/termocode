// LiveBoard: the phone's live board (MOBUX-9, plan §2.5). Four columns —
// needs you, working, review, done today — side by side in a strip that
// snaps one column per swipe; the tab row above shows each column with
// its shape and count, and a tap on a tab scrolls to it. A tile is
// project · agent/model · step 3/5 · cost · CI; a tap opens its sheet:
// Cancel first, then Logs / Diff / Chat (a card opens its answer sheet
// at once). The board only shows: every answer goes through the card.
//
// The view gets its data and its way out from the page (props), so the
// phone's Home chunk (MOB-4) wires it and the kit can show it alone.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { Route } from "../state/route";
import { t, type StringKey } from "../strings";
import { BoardTile } from "./BoardTile";
import { StatusMark } from "./Mark";
import { PhoneSheet, type SheetAction } from "./Sheet";
import type { Status } from "./status";
import {
  BOARD_COLUMNS,
  BOARD_REFRESH_MS,
  columnAt,
  columnsOf,
  costText,
  countOf,
  drillsOf,
  firstColumn,
  readBoard,
  tileOf,
  type Board,
  type BoardColumn,
  type BoardItem,
  type DrillKind,
} from "./board";

const COLUMN_STATUS: Readonly<Record<BoardColumn, Status>> = {
  needs_you: "needs",
  working: "working",
  review: "review",
  done: "done",
};

const COLUMN_LABEL: Readonly<Record<BoardColumn, StringKey>> = {
  needs_you: "ph.board.needs_you",
  working: "board.col.working",
  review: "ph.race.review",
  done: "board.col.done",
};

const DRILL_LABEL: Readonly<Record<DrillKind, StringKey>> = {
  card: "ph.board.card",
  logs: "ph.board.logs",
  diff: "ph.board.diff",
  chat: "ph.stream",
};

/** useLiveBoard asks for the board when it shows and again when tick
 * changes (a topic the board reads moved), at most once per
 * BOARD_REFRESH_MS; offline it keeps the last board and marks it stale. */
export interface LoadBoard {
  (): Promise<unknown>;
}

export function useLiveBoard(load: LoadBoard, tick: unknown, online: boolean) {
  const [board, setBoard] = useState<Board | null>(null);
  const [failed, setFailed] = useState(false);
  const [n, setN] = useState(0);
  const last = useRef(0);
  const busy = useRef(false);
  useEffect(() => {
    if (!online) return;
    let gone = false;
    const run = () => {
      if (busy.current) return;
      busy.current = true;
      last.current = Date.now();
      load().then(
        (v) => {
          busy.current = false;
          if (gone) return;
          const b = readBoard(v);
          setBoard(b);
          setFailed(b === null);
        },
        () => {
          busy.current = false;
          if (!gone) setFailed(true);
        },
      );
    };
    const wait = BOARD_REFRESH_MS - (Date.now() - last.current);
    const timer = setTimeout(run, wait > 0 ? wait : 0);
    return () => {
      gone = true;
      clearTimeout(timer);
    };
  }, [tick, online, n]);
  return { board, failed, stale: !online && board !== null, retry: () => setN((x) => x + 1) };
}

export function LiveBoard({
  board,
  online,
  failed,
  stale,
  project,
  onRetry,
  onOpen,
}: {
  board: Board | null;
  online: boolean;
  failed?: boolean;
  stale?: boolean;
  /** project is the home project's name (board v1). */
  project?: string;
  onRetry?: () => void;
  /** onOpen goes to a view of the page (logs, diff, chat, a card). */
  onOpen: (r: Route) => void;
}) {
  const strip = useRef<HTMLDivElement | null>(null);
  const cols = useRef<Partial<Record<BoardColumn, HTMLElement | null>>>({});
  const [at, setAt] = useState<BoardColumn | null>(null);
  const [open, setOpen] = useState<BoardItem | null>(null);
  const shown = at ?? firstColumn(board);
  const byCol = columnsOf(board);

  // The board opens on its first column with work (needs you first).
  const opened = useRef(false);
  useEffect(() => {
    if (opened.current || !board) return;
    opened.current = true;
    const c = firstColumn(board);
    setAt(c);
    cols.current[c]?.scrollIntoView({ block: "nearest", inline: "start" });
  }, [board]);

  const goTo = (c: BoardColumn) => {
    setAt(c);
    cols.current[c]?.scrollIntoView({ behavior: "smooth", block: "nearest", inline: "start" });
  };

  const tap = (it: BoardItem) => {
    const d = drillsOf(it);
    if (d.length === 1 && d[0].kind === "card") onOpen(d[0].route);
    else if (d.length) setOpen(it);
  };

  const today = costText(board?.today_usd);
  const actions: SheetAction[] = open
    ? [
        { label: t("cancel"), safe: true, onClick: () => setOpen(null) },
        ...drillsOf(open).map((d, i) => ({
          label: t(DRILL_LABEL[d.kind]),
          primary: i === 0,
          onClick: () => {
            setOpen(null);
            onOpen(d.route);
          },
        })),
      ]
    : [];

  return (
    <section class={`ph-board${stale ? " is-stale" : ""}`} aria-label={t("chips.board")}>
      <header class="ph-board-head">
        <h2 class="ph-board-title">
          <StatusMark s={online ? "working" : "asleep"} live={online} hidden /> {t("chips.board")}
        </h2>
        {today && <span class="ph-board-today">{t("ph.board.today", { cost: today })}</span>}
      </header>
      {stale && <p class="ph-board-note">{t("ph.board.stale")}</p>}
      {failed && (
        <p class="ph-board-note is-error" role="alert">
          {t("ph.board.failed")}{" "}
          {onRetry && (
            <button type="button" class="ph-board-retry" disabled={!online} onClick={onRetry}>
              {t("retry")}
            </button>
          )}
        </p>
      )}
      <div class="ph-board-tabs" role="tablist" aria-label={t("chips.board")}>
        {BOARD_COLUMNS.map((c) => (
          <button
            key={c}
            type="button"
            role="tab"
            id={`ph-board-tab-${c}`}
            aria-controls={`ph-board-col-${c}`}
            aria-selected={shown === c ? "true" : "false"}
            class={`ph-board-tab${shown === c ? " is-on" : ""}`}
            onClick={() => goTo(c)}
          >
            <StatusMark s={COLUMN_STATUS[c]} hidden /> <span>{t(COLUMN_LABEL[c])}</span>{" "}
            <span class="ph-board-count">{countOf(board, c)}</span>
          </button>
        ))}
      </div>
      <div
        ref={strip}
        class="ph-board-cols"
        onScroll={(e) => {
          const el = e.currentTarget as HTMLDivElement;
          const c = columnAt(el.scrollLeft, el.clientWidth);
          if (c !== shown) setAt(c);
        }}
      >
        {BOARD_COLUMNS.map((c) => (
          <div
            key={c}
            ref={(el) => (cols.current[c] = el)}
            role="tabpanel"
            id={`ph-board-col-${c}`}
            aria-labelledby={`ph-board-tab-${c}`}
            class="ph-board-col"
          >
            {byCol[c].length === 0 ? (
              <p class="ph-board-empty">{board ? t("ph.board.empty") : t("loading")}</p>
            ) : (
              <ul class="ph-board-list">
                {byCol[c].map((it) => {
                  const tile = tileOf(it, project);
                  return (
                    <li key={it.id}>
                      <BoardTile {...tile} onClick={() => tap(it)} />
                      {it.why && <span class="ph-board-why">{it.why}</span>}
                    </li>
                  );
                })}
              </ul>
            )}
            {c === "done" && board?.more ? <p class="ph-board-empty">{t("board.more", { n: String(board.more) })}</p> : null}
          </div>
        ))}
      </div>
      {open && (
        <PhoneSheet title={open.title || open.ref} onLater={() => setOpen(null)} actions={actions}>
          {open.why && <p class="ph-board-why">{open.why}</p>}
        </PhoneSheet>
      )}
    </section>
  );
}
