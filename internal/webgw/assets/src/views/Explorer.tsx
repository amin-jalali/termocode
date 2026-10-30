// The explorer's OUTLINE and TIMELINE sections on the desktop, under the
// tree as on the laptop's editor. OUTLINE lists the symbols of the open
// file (buf.symbols: the language server's, or a guess from the code);
// a click puts the editor's cursor on that line. TIMELINE lists the
// commits that changed the open file (git.file_log); a click opens that
// commit's change of the file in an editor tab, read-only. Both start
// closed and ask the laptop only while open.
import { h, type ComponentChildren } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useState } from "preact/hooks";
import type { BufSymbol, BufSymbols, GitFileLog } from "../../../../webproto/proto";
import { activeDesk } from "../lib/deskeditor";
import { file } from "../lib/file";
import { activeFile, showFile } from "../lib/workbench";
import { OfflineError, ReqError } from "../net/link";
import { link, live, store } from "../session";
import { age, readDiff, readGit, type DiffPage } from "../state/git";
import { t } from "../strings";
import { Button } from "../ui/Button";
import { errText } from "../ui/CardSheet";
import { Cod } from "../ui/icons";
import { DiffRow } from "../ui/rows";
import { SkeletonRows } from "../ui/Skeleton";

/** TIMELINE_TAB starts the editor tab of a commit's change of a file:
 * TIMELINE_TAB + hash + ":" + path. */
const TIMELINE_TAB = "@timeline:";
/** SYMBOLS_MS: the outline asks again this long after the file changed. */
const SYMBOLS_MS = 700;

/** isTimeline: the editor tab is a commit's change of a file. */
export function isTimeline(tab: string): boolean {
  return tab.startsWith(TIMELINE_TAB);
}

/** timelineOf reads a timeline tab (null: not one). */
export function timelineOf(tab: string): { hash: string; path: string } | null {
  if (!isTimeline(tab)) return null;
  const rest = tab.slice(TIMELINE_TAB.length);
  const i = rest.indexOf(":");
  if (i < 4 || !/^[0-9a-f]+$/.test(rest.slice(0, i)) || i === rest.length - 1) return null;
  return { hash: rest.slice(0, i), path: rest.slice(i + 1) };
}

function baseName(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

/** tabName is an editor tab's name: the file's, and the commit's for a timeline tab. */
export function tabName(tab: string): string {
  const tl = timelineOf(tab);
  return tl ? `${baseName(tl.path)} (${tl.hash})` : baseName(tab);
}

/** outlineOpen and timelineOpen: the sections are open (closed at first). */
export const outlineOpen = signal(false);
export const timelineOpen = signal(false);

/** SYMBOL_ICONS: LSP SymbolKind → codicon. */
const SYMBOL_ICONS: Record<number, string> = {
  2: "symbol-namespace",
  3: "symbol-namespace",
  4: "symbol-namespace",
  5: "symbol-class",
  6: "symbol-method",
  7: "symbol-property",
  8: "symbol-field",
  9: "symbol-method",
  10: "symbol-enum",
  11: "symbol-interface",
  12: "symbol-method",
  13: "symbol-variable",
  14: "symbol-constant",
  22: "symbol-enum",
  23: "symbol-struct",
};

function failText(e: unknown): string {
  return e instanceof OfflineError ? t("offline.input") : e instanceof ReqError ? errText(e) : "";
}

/** Section is one of the explorer's folding parts: a header that opens
 * and closes it, and its body while open. */
function Section({ id, title, open, onToggle, children }: { id: string; title: string; open: boolean; onToggle: () => void; children: ComponentChildren }) {
  return (
    <section class={`ex-section${open ? " is-open" : ""}`} data-section={id} aria-labelledby={`ex-${id}`}>
      <h2 class="ex-head">
        <button type="button" class="ex-toggle" id={`ex-${id}`} aria-expanded={open ? "true" : "false"} onClick={onToggle}>
          <Cod name={open ? "chevron-down" : "chevron-right"} />
          <span>{title}</span>
        </button>
      </h2>
      {open && <div class="ex-body">{children}</div>}
    </section>
  );
}

/** realFile is the open file the sections are about (a timeline tab's file too). */
function realFile(): string | null {
  const a = activeFile.value;
  if (!a) return null;
  return timelineOf(a)?.path ?? (isTimeline(a) ? null : a);
}

function Note({ children }: { children: ComponentChildren }) {
  return (
    <p class="ex-note" dir="auto">
      {children}
    </p>
  );
}

type SymLoad = { buf: number; state: "loading" } | { buf: number; state: "ok"; res: BufSymbols } | { buf: number; state: "error"; msg: string };

function Outline() {
  const path = realFile();
  const f = file.value;
  const buf = f && f.kind === "open" && f.path === path ? f.buf : undefined;
  const tick = buf === undefined ? -1 : Number((store.topic("buf:" + buf).value?.data as { tick?: unknown } | undefined)?.tick ?? -1);
  const [load, setLoad] = useState<SymLoad | null>(null);
  const on = live.value;
  useEffect(() => {
    if (buf === undefined || !on) return;
    let stop = false;
    const ask = () =>
      link.request("buf.symbols", { buf }).then(
        (r) => !stop && setLoad({ buf, state: "ok", res: r as BufSymbols }),
        (e: unknown) => !stop && setLoad({ buf, state: "error", msg: failText(e) }),
      );
    // The first ask at once; after an edit, once typing settles.
    const first = load?.buf !== buf;
    if (first) setLoad({ buf, state: "loading" });
    const timer = setTimeout(ask, first ? 0 : SYMBOLS_MS);
    return () => {
      stop = true;
      clearTimeout(timer);
    };
  }, [buf, tick, on]);

  if (!path) return <Note>{t("outline.no_file")}</Note>;
  if (buf === undefined || !load || load.buf !== buf || load.state === "loading") return <SkeletonRows rows={3} />;
  if (load.state === "error") return <Note>{load.msg}</Note>;
  const syms = load.res.symbols ?? [];
  if (syms.length === 0) return <Note>{t("outline.none")}</Note>;
  return (
    <div class="ex-list" role="list" aria-label={t("outline.title")}>
      {load.res.source === "syntax" && <Note>{t("outline.guess")}</Note>}
      {syms.map((s: BufSymbol, i: number) => (
        <div role="listitem" key={i}>
          <button
            type="button"
            class={`ex-row depth-${Math.min(s.depth, 8)}`}
            data-line={s.line}
            title={s.detail ? `${s.name} ${s.detail}` : s.name}
            onClick={() => activeDesk.peek()?.reveal(s.line)}
          >
            <Cod name={SYMBOL_ICONS[s.kind] ?? "symbol-misc"} />
            <span class="ex-name trunc" dir="ltr">
              {s.name}
            </span>
            {s.detail && (
              <span class="ex-detail trunc" dir="ltr">
                {s.detail}
              </span>
            )}
          </button>
        </div>
      ))}
    </div>
  );
}

type LogLoad = { path: string; state: "loading" } | { path: string; state: "ok"; res: GitFileLog } | { path: string; state: "error"; msg: string };

function Timeline() {
  const path = realFile();
  const head = readGit(store.topic("git").value?.data).log[0]?.hash ?? "";
  const [load, setLoad] = useState<LogLoad | null>(null);
  const on = live.value;
  useEffect(() => {
    if (!path || !on) return;
    let stop = false;
    if (load?.path !== path) setLoad({ path, state: "loading" });
    link.request("git.file_log", { path }).then(
      (r) => !stop && setLoad({ path, state: "ok", res: r as GitFileLog }),
      (e: unknown) => !stop && setLoad({ path, state: "error", msg: failText(e) }),
    );
    return () => {
      stop = true;
    };
  }, [path, head, on]);

  if (!path) return <Note>{t("timeline.no_file")}</Note>;
  if (!load || load.path !== path || load.state === "loading") return <SkeletonRows rows={3} />;
  if (load.state === "error") return <Note>{load.msg}</Note>;
  const log = load.res.log ?? [];
  if (log.length === 0) return <Note>{t("timeline.none")}</Note>;
  const now = Date.now();
  return (
    <div class="ex-list" role="list" aria-label={t("timeline.title")}>
      {log.map((c) => (
        <div role="listitem" key={c.hash}>
          <button type="button" class="ex-row ex-commit" data-hash={c.hash} title={`${c.hash} ${c.subject}`} onClick={() => showFile(TIMELINE_TAB + c.hash + ":" + path)}>
            <Cod name="git-commit" />
            <span class="ex-name trunc" dir="auto">
              {c.subject}
            </span>
            <span class="ex-detail trunc" dir="auto">
              {c.author} · {age(c.when, now)}
            </span>
          </button>
        </div>
      ))}
    </div>
  );
}

/** ExplorerSections are OUTLINE and TIMELINE, under the tree. */
export function ExplorerSections() {
  return (
    <div class="ex-sections">
      <Section id="outline" title={t("outline.title")} open={outlineOpen.value} onToggle={() => (outlineOpen.value = !outlineOpen.peek())}>
        <Outline />
      </Section>
      <Section id="timeline" title={t("timeline.title")} open={timelineOpen.value} onToggle={() => (timelineOpen.value = !timelineOpen.peek())}>
        <Timeline />
      </Section>
    </div>
  );
}

/** TimelineDiff is a timeline tab: one commit's change of one file,
 * read-only, in pages. */
export function TimelineDiff({ tab }: { tab: string }) {
  const tl = timelineOf(tab);
  const [page, setPage] = useState<DiffPage | null>(null);
  const [err, setErr] = useState("");
  const [more, setMore] = useState(false);
  const on = live.value;
  useEffect(() => {
    if (!tl || !on) return;
    let stop = false;
    link.request("git.show", { hash: tl.hash, path: tl.path }).then(
      (r) => !stop && setPage(readDiff(r)),
      (e: unknown) => !stop && setErr(failText(e)),
    );
    return () => {
      stop = true;
    };
  }, [tab, on]);
  if (!tl) return null;
  const c = page?.commit;
  const loadMore = () => {
    if (!page || !page.next || more) return;
    setMore(true);
    link
      .request("git.show", { hash: tl.hash, path: tl.path, from: page.next })
      .then(
        (r) => {
          const p = readDiff(r);
          setPage({ ...page, rows: [...page.rows, ...p.rows], next: p.next });
        },
        (e: unknown) => setErr(failText(e)),
      )
      .finally(() => setMore(false));
  };
  return (
    <section class="view tl-diff" data-view="timeline" aria-labelledby="vh-timeline">
      <header class="tl-head">
        <h1 class="tl-title" id="vh-timeline">
          <span dir="ltr">{tl.path}</span> <span class="tl-hash" dir="ltr">{tl.hash}</span>
        </h1>
        {c && (
          <p class="tl-meta" dir="auto">
            {c.subject} · {c.author} · {age(c.when, Date.now())}
          </p>
        )}
        <span class="chip">{t("viewonly")}</span>
      </header>
      <div class="tl-body">
        {err ? (
          <Note>{err}</Note>
        ) : !page ? (
          <SkeletonRows rows={8} />
        ) : page.rows.length === 0 ? (
          <Note>{t(page.binary ? "timeline.binary" : "timeline.empty")}</Note>
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
        {page && page.truncated && page.next === 0 && <Note>{t("git.long")}</Note>}
      </div>
    </section>
  );
}
