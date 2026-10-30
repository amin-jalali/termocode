// One commit of the log: its subject, author and time, and its diff in
// pages, a file row before each file.
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import { link, live } from "../../session";
import { age, readDiff, type DiffPage } from "../../state/git";
import { Button } from "../../ui/Button";
import { DiffRow } from "../../ui/rows";
import { Sheet } from "../../ui/Sheet";
import { SkeletonRows } from "../../ui/Skeleton";
import { t } from "../../strings";
import { gitFailed } from "./errors";

export function ShowSheet({ hash, onClose }: { hash: string; onClose: () => void }) {
  const [page, setPage] = useState<DiffPage | null>(null);
  const [err, setErr] = useState("");
  const [more, setMore] = useState(false);

  useEffect(() => {
    if (!live.value) return;
    let stop = false;
    link.request("git.show", { hash }).then(
      (r) => !stop && setPage(readDiff(r)),
      (e: unknown) => !stop && setErr(e instanceof Error ? e.message : ""),
    );
    return () => {
      stop = true;
    };
  }, [hash, live.value]);

  const loadMore = () => {
    if (!page || !page.next || more) return;
    setMore(true);
    link
      .request("git.show", { hash, from: page.next })
      .then((r) => {
        const p = readDiff(r);
        setPage({ ...page, rows: [...page.rows, ...p.rows], next: p.next });
      }, gitFailed)
      .finally(() => setMore(false));
  };

  const c = page?.commit;
  return (
    <Sheet onLater={onClose} label={hash}>
      <div class="diff-sheet">
        <header class="diff-head show-head">
          <span class="show-hash" dir="ltr">
            {hash}
          </span>
          {c && (
            <span class="show-meta" dir="auto">
              {c.author} · {age(c.when, Date.now())}
            </span>
          )}
        </header>
        {c && (
          <p class="show-subject" dir="auto">
            {c.subject}
          </p>
        )}
        {c && c.body && (
          <p class="show-body" dir="auto">
            {c.body}
          </p>
        )}
        <div class="diff-body">
          {err ? (
            <p class="sheet-note">{err}</p>
          ) : !page ? (
            <SkeletonRows rows={8} />
          ) : (
            <div class="diff-rows">
              {page.rows.map((r, i) => (
                <DiffRow key={i} kind={r.k === "file" ? "hunk" : r.k} a={r.old || undefined} b={r.new || undefined} text={r.text} />
              ))}
            </div>
          )}
          {page && page.next > 0 && (
            <Button wide onClick={loadMore} disabled={more}>
              {t("git.more_rows")}
            </Button>
          )}
          {page && page.truncated && page.next === 0 && <p class="sheet-note">{t("git.long")}</p>}
        </div>
      </div>
    </Sheet>
  );
}
