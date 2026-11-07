// #/more/board: the phone's live board (MOB-18). Its view is its own
// lazy file (board.js, lib/lazy.ts), loaded the first time it opens, so
// the page's first files never carry it; until then a skeleton, after a
// failed load "Try again".
import { h } from "preact";
import { useEffect } from "preact/hooks";
import { board, boardFailed, loadBoard } from "../../lib/lazy";
import { t } from "../../strings";
import { SkeletonRows } from "../../ui/Skeleton";
import { SubView } from "./SubView";

export function BoardRoute() {
  useEffect(() => {
    if (!board.peek()) loadBoard().catch(() => undefined);
  }, []);
  const m = board.value;
  if (m) return <m.BoardView />;
  return (
    <SubView title={t("more.board")} name="board">
      {boardFailed.value ? (
        <div class="board-failed">
          <p class="empty">{t("board.load_failed")}</p>
          <button type="button" class="btn" onClick={() => loadBoard().catch(() => undefined)}>
            {t("retry")}
          </button>
        </div>
      ) : (
        <SkeletonRows rows={4} />
      )}
    </SubView>
  );
}
