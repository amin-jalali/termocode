// One changed file in the Git tab (web UX 3.5): badge, the name in bold
// and its folder dim, and the row's action as a button; a conflict row
// has no action.
import { h } from "preact";
import { Button } from "../../ui/Button";
import { GitBadge } from "./GitBadge";
import { splitPath, type GitFile, type Side } from "../../state/git";
import { t } from "../../strings";

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
  return (
    <div class="git-row-wrap" data-path={file.path} data-side={side}>
      <div class="git-row">
        <button type="button" class="git-open" onClick={onOpen}>
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
