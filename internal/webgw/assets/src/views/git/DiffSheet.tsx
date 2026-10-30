// The diff sheet (web UX 3.5): one side of a file's change as rows, with
// hunk headers that stick while their rows scroll. A long diff comes in
// pages ("Show more rows"); past 512 KiB it says so. At the bottom:
// Stage or Unstage (no confirm: it is undone as easily). A hunk can be
// staged alone.
import { h, Fragment } from "preact";
import { useEffect, useState } from "preact/hooks";
import { link, live } from "../../session";
import { readDiff, splitPath, type DiffLine, type GitFile } from "../../state/git";
import { Button } from "../../ui/Button";
import { DiffRow } from "../../ui/rows";
import { Segmented } from "../../ui/Segmented";
import { Sheet } from "../../ui/Sheet";
import { SkeletonRows } from "../../ui/Skeleton";
import { t } from "../../strings";
import { gitFailed } from "./errors";

type Load = { state: "loading" } | { state: "error"; msg: string } | { state: "ok"; rows: DiffLine[]; next: number; binary: boolean; truncated: boolean; added: number; removed: number };

export function DiffSheet({
  file,
  path,
  staged,
  canAct,
  onSide,
  onClose,
  onStage,
}: {
  /** file is the git entry now, or null when the file has no change any more. */
  file: GitFile | null;
  path: string;
  staged: boolean;
  canAct: boolean;
  onSide: (staged: boolean) => void;
  onClose: () => void;
  onStage: (stage: boolean) => void;
}) {
  const [load, setLoad] = useState<Load>({ state: "loading" });
  const [more, setMore] = useState(false);
  const [round, setRound] = useState(0); // a hunk moved: read again
  const status = file ? file.x + file.y : "";

  const fetchPage = (from: number) =>
    link.request("git.diff", { path, ...(staged ? { staged } : {}), ...(from > 0 ? { from } : {}) }).then(readDiff);

  useEffect(() => {
    if (!file || !live.value) return;
    let stop = false;
    setLoad({ state: "loading" });
    fetchPage(0).then(
      (p) => !stop && setLoad({ state: "ok", ...p }),
      (e: unknown) => !stop && setLoad({ state: "error", msg: e instanceof Error ? e.message : "" }),
    );
    return () => {
      stop = true;
    };
  }, [path, staged, status, live.value, round]);

  const loadMore = () => {
    if (load.state !== "ok" || !load.next || more) return;
    setMore(true);
    fetchPage(load.next)
      .then((p) => setLoad({ ...load, rows: [...load.rows, ...p.rows], next: p.next }), gitFailed)
      .finally(() => setMore(false));
  };

  const hunk = (header: string) =>
    link
      .request(staged ? "git.unstage_hunk" : "git.stage_hunk", { path, hunk: header })
      .catch(gitFailed)
      .finally(() => setRound((r) => r + 1));

  const { name, dir } = splitPath(path);
  const both = file !== null && file.staged && file.unstaged;
  const sides = [
    { value: "staged", label: t("git.staged") },
    { value: "changes", label: t("git.changes") },
  ] as const;
  const hunkAction = canAct && file !== null && !file.untracked;

  let body;
  if (!file) body = <p class="sheet-note">{t("git.gone")}</p>;
  else if (load.state === "loading") body = <SkeletonRows rows={8} />;
  else if (load.state === "error") body = <p class="sheet-note">{load.msg}</p>;
  else if (load.binary) body = <p class="sheet-note">{t("file.binary")}</p>;
  else
    body = (
      <Fragment>
        <div class="diff-rows">
          {load.rows.map((r, i) =>
            r.k === "hunk" ? (
              <div class="hunk-line" key={i}>
                <DiffRow kind="hunk" text={r.text} />
                {hunkAction && (
                  <Button small kind="quiet" needsLink onClick={() => hunk(r.text)}>
                    {staged ? t("git.hunk_unstage") : t("git.hunk_stage")}
                  </Button>
                )}
              </div>
            ) : (
              <DiffRow key={i} kind={r.k === "file" ? "hunk" : r.k} a={r.old || undefined} b={r.new || undefined} text={r.text} />
            ),
          )}
        </div>
        {load.next > 0 && (
          <Button wide onClick={loadMore} disabled={more}>
            {t("git.more_rows")}
          </Button>
        )}
        {load.truncated && load.next === 0 && <p class="sheet-note">{t("git.long")}</p>}
      </Fragment>
    );

  return (
    <Sheet onLater={onClose} label={name}>
      <div class="diff-sheet">
        <header class="diff-head">
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
          {load.state === "ok" && (
            <span class="diff-count" dir="ltr">
              <span class="git-add-n">+{load.added}</span> <span class="git-del-n">−{load.removed}</span>
            </span>
          )}
        </header>
        {both && <Segmented label={t("tab.git")} value={staged ? "staged" : "changes"} choices={sides} onChange={(v) => onSide(v === "staged")} />}
        <div class="diff-body">{body}</div>
        {file && canAct && !file.conflict && (
          <footer class="diff-foot">
            <Button kind="primary" needsLink onClick={() => onStage(!staged)}>
              {staged ? t("git.unstage") : t("git.stage")}
            </Button>
          </footer>
        )}
      </div>
    </Sheet>
  );
}
