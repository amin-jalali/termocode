// One changed file in the Git tab (web UX 3.5, 6.2): badge, the name in
// bold and its folder dim, and the row's action as a button. The same
// action is a swipe on touch screens: right stages, left unstages; the
// verb shows under the row and a release past half the row does it.
// Every swipe has this button twin; a conflict row has no action.
import { h } from "preact";
import { useRef, useState } from "preact/hooks";
import { Button } from "../../ui/Button";
import { GitBadge } from "./GitBadge";
import { splitPath, type GitFile, type Side } from "../../state/git";
import { t } from "../../strings";

const START_PX = 10; // a move shorter than this is still a tap

export function GitRow({
  file,
  side,
  canAct,
  onOpen,
  onAct,
}: {
  file: GitFile;
  side: Side;
  /** canAct: a controller with a live link may stage here. */
  canAct: boolean;
  onOpen: () => void;
  onAct: () => void;
}) {
  const { name, dir } = splitPath(file.path);
  const stage = side === "changes" || side === "untracked";
  const action = side === "conflicts" ? null : stage ? t("git.stage") : t("git.unstage");
  const code = side === "staged" ? file.x : side === "conflicts" ? "U" : file.y;
  const row = useRef<HTMLDivElement | null>(null);
  const [dx, setDx] = useState(0);
  const drag = useRef<{ x: number; y: number; on: boolean; id: number } | null>(null);
  const moved = useRef(false);
  const swipe = canAct && action !== null;

  const down = (e: PointerEvent) => {
    if (!swipe || e.button > 0) return;
    drag.current = { x: e.clientX, y: e.clientY, on: false, id: e.pointerId };
    moved.current = false;
  };
  const move = (e: PointerEvent) => {
    const d = drag.current;
    if (!d) return;
    const mx = e.clientX - d.x, my = e.clientY - d.y;
    if (!d.on) {
      if (Math.abs(my) > START_PX && Math.abs(my) > Math.abs(mx)) {
        drag.current = null; // a scroll, not a swipe
        return;
      }
      if (Math.abs(mx) < START_PX) return;
      d.on = true;
      moved.current = true;
      (e.currentTarget as HTMLElement).setPointerCapture?.(d.id);
    }
    // Right stages, left unstages: the other way does not move.
    setDx(stage ? Math.max(0, mx) : Math.min(0, mx));
  };
  const up = () => {
    const d = drag.current;
    drag.current = null;
    if (!d || !d.on) return;
    const w = row.current?.offsetWidth ?? 1;
    const done = Math.abs(dx) > w / 2;
    setDx(0);
    if (done) onAct();
  };
  const past = row.current ? Math.abs(dx) > row.current.offsetWidth / 2 : false;

  return (
    <div class={`git-row-wrap${dx !== 0 ? " is-swiping" : ""}${past ? " is-past" : ""}`} data-path={file.path} data-side={side}>
      {dx !== 0 && (
        <div class={`git-under ${stage ? "to-stage" : "to-unstage"}`} aria-hidden="true">
          <span>{action}</span>
        </div>
      )}
      <div
        ref={row}
        class="git-row"
        style={dx !== 0 ? { transform: `translateX(${dx}px)` } : undefined}
        onPointerDown={down}
        onPointerMove={move}
        onPointerUp={up}
        onPointerCancel={up}
      >
        <button
          type="button"
          class="git-open"
          onClick={() => {
            if (moved.current) {
              moved.current = false;
              return;
            }
            onOpen();
          }}
        >
          <GitBadge code={code} />
          <span class="git-name">
            <bdi class="git-file" dir="ltr">
              {name}
            </bdi>
            {dir && (
              <bdi class="git-dir" dir="ltr">
                {dir}
              </bdi>
            )}
          </span>
        </button>
        {side === "conflicts" ? (
          <span class="git-note">{t("git.resolve_laptop")}</span>
        ) : (
          canAct && (
            <Button small kind="quiet" needsLink onClick={onAct}>
              {action}
            </Button>
          )
        )}
      </div>
    </div>
  );
}
