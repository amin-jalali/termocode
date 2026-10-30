// A diff editor tab (desktop, lib/difftab.ts): the older text on the
// left, read only, and the newer on the right, side by side (or inline),
// with the changed rows and words marked, an overview ruler, unchanged
// regions folded, and Previous / Next Change (F7, Shift+F7, Alt+F5).
// The working tree's side is the laptop's live buffer: typing there edits
// the file through the same tick-guarded sync as the file editor. Every
// other side is git's text (git.text), read only.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useRef, useState } from "preact/hooks";
import type { BufSnap, GitSnap, GitText } from "../../../../webproto/proto";
import { desks, mountDesk, setDesk, type DeskHandle } from "../lib/deskeditor";
import { diffOf, revs, type DiffTab } from "../lib/difftab";
import { closePath, fileOf, holdPath } from "../lib/file";
import { showFile, wordWrap } from "../lib/workbench";
import { modKey } from "../lib/platform";
import { link, live, store } from "../session";
import { isGone } from "../state/files";
import { t } from "../strings";
import { errText } from "../ui/CardSheet";
import { Chip } from "../ui/Chip";
import { Cod } from "../ui/icons";
import { LiveEditor } from "./LiveEditor";
import { View } from "./View";

/** diffInline and diffCollapse: the diff editors' view, for every tab. */
export const diffInline = signal(false);
export const diffCollapse = signal(true);

interface Side {
  text: string;
  binary: boolean;
  cut: boolean;
}

function readSide(r: unknown): Side {
  const g = (r ?? {}) as Partial<GitText>;
  return { text: typeof g.text === "string" ? g.text : "", binary: g.binary === true, cut: g.truncated === true };
}

/** useGitText reads one side from git, again when git's state changes. */
function useGitText(path: string, rev: string, again: unknown): { side: Side | null; err: string } {
  const [side, setSide] = useState<Side | null>(null);
  const [err, setErr] = useState("");
  const on = live.value;
  useEffect(() => {
    if (!rev || !on) return;
    let stop = false;
    link.request("git.text", { path, rev }).then(
      (r) => {
        if (stop) return;
        setErr("");
        setSide(readSide(r));
      },
      (e: unknown) => !stop && setErr(errText(e)),
    );
    return () => {
      stop = true;
    };
  }, [path, rev, on, again]);
  return { side, err };
}

function readBuf(data: unknown): BufSnap | null {
  if (typeof data !== "object" || data === null) return null;
  const d = data as BufSnap;
  return Array.isArray(d.lines) ? d : null;
}

/** StaticDiff shows two texts from git, both read only. */
function StaticDiff({ d, left, right, slot, onCount }: { d: DiffTab; left: Side; right: Side; slot: string; onCount: (n: number, a: number, r: number) => void }) {
  const box = useRef<HTMLDivElement | null>(null);
  const ed = useRef<DeskHandle | null>(null);
  const count = useRef(onCount);
  count.current = onCount;
  useEffect(() => {
    if (!box.current) return;
    const h = mountDesk(box.current, {
      doc: right.text,
      label: d.path,
      path: d.path,
      slot,
      readOnly: true,
      wrap: wordWrap.peek(),
      tabSize: 4,
      insertSpaces: false,
      diff: { original: left.text, inline: diffInline.peek(), collapse: diffCollapse.peek() },
      onDiff: (n, a, r) => count.current(n, a, r),
      onChange: () => undefined,
      onSave: () => undefined,
      onDone: () => undefined,
      onViewport: () => undefined,
    });
    ed.current = h;
    setDesk(slot, h);
    return () => {
      setDesk(slot, null);
      h.destroy();
      ed.current = null;
    };
  }, []);
  useEffect(() => ed.current?.original(left.text), [left.text]);
  useEffect(() => ed.current?.remote(right.text), [right.text]);
  useEffect(() => ed.current?.diffView({ inline: diffInline.value, collapse: diffCollapse.value }), [diffInline.value, diffCollapse.value]);
  useEffect(() => ed.current?.opts({ wrap: wordWrap.value }), [wordWrap.value]);
  return <div ref={box} class="editor-host desk-host" />;
}

/** LiveSide is the working tree's side: the laptop's buffer, live. */
function LiveSide({ d, left, slot, onCount }: { d: DiffTab; left: Side; slot: string; onCount: (n: number, a: number, r: number) => void }) {
  useEffect(() => {
    holdPath(d.path);
    return () => closePath(d.path);
  }, [d.path]);
  const st = fileOf(d.path);
  const buf = st?.kind === "open" && st.buf !== undefined ? readBuf(store.topic("buf:" + st.buf).value?.data) : null;
  if (st && st.kind !== "open" && st.kind !== "loading") return <p class="empty refused">{st.kind === "error" ? errText(st.err) : t("file.missing")}</p>;
  if (!buf || st?.buf === undefined) return <div class="editor-host desk-host" />;
  const controller = link.meta.value?.role === "controller";
  const canEdit = controller && !buf.readonly && !isGone(store.topic("buf:" + st.buf).value?.data);
  return (
    <LiveEditor
      key={buf.buf}
      path={d.path}
      buf={buf.buf}
      slot={slot}
      desk={{ readOnly: !canEdit, wrap: wordWrap.value, tabSize: buf.ts ?? 0, insertSpaces: buf.et === true }}
      diff={{ original: left.text, inline: diffInline.value, collapse: diffCollapse.value, onCount }}
    />
  );
}

export function DiffView({ tab, slot = "g0" }: { tab: string; slot?: string }) {
  const d = diffOf(tab);
  const git = store.topic("git").value;
  const gs = git?.data as GitSnap | undefined;
  const entry = d ? gs?.files?.find((f) => f.path === d.path) : undefined;
  // A file deleted from the working tree has no buffer: its right side is empty.
  const deleted = d?.kind === "wt" && entry?.y === "D";
  const r = d ? revs(d) : { left: "", right: "" };
  const left = useGitText(d?.path ?? "", r.left, git?.seq);
  const right = useGitText(d?.path ?? "", r.right, git?.seq);
  if (!d) return null;
  const name = d.path.slice(d.path.lastIndexOf("/") + 1);
  const go = (dir: 1 | -1) => desks.peek().get(slot)?.diffGo(dir);
  const mod = modKey();
  const leftSide = left.side;
  const rightSide: Side | null = deleted ? { text: "", binary: false, cut: false } : right.side;
  const binary = leftSide?.binary || rightSide?.binary;
  const cut = leftSide?.cut || rightSide?.cut;
  const err = left.err || right.err;
  const onCount = () => undefined;
  return (
    <View
      tab="files"
      name="diff"
      title={name}
      sub={
        <span class="trunc file-path crumbs" dir="ltr" title={d.path}>
          {d.path}
        </span>
      }
      bodyClass="is-editing"
      actions={
        <>
          {d.kind !== "wt" && <Chip>{t("viewonly")}</Chip>}
          <button type="button" class="tb tb-icon" data-act="prev" aria-label={t("dv.prev")} title={`${t("dv.prev")} (Shift+F7)`} onClick={() => go(-1)}>
            <Cod name="arrow-up" />
          </button>
          <button type="button" class="tb tb-icon" data-act="next" aria-label={t("dv.next")} title={`${t("dv.next")} (F7)`} onClick={() => go(1)}>
            <Cod name="arrow-down" />
          </button>
          <button type="button" class={`tb tb-icon${diffInline.value ? " is-on" : ""}`} data-act="inline" aria-pressed={diffInline.value ? "true" : "false"} aria-label={t("dv.inline")} title={t("dv.inline")} onClick={() => (diffInline.value = !diffInline.peek())}>
            <Cod name="diff-single" />
          </button>
          <button type="button" class={`tb tb-icon${diffCollapse.value ? " is-on" : ""}`} data-act="collapse" aria-pressed={diffCollapse.value ? "true" : "false"} aria-label={t("dv.collapse")} title={t("dv.collapse")} onClick={() => (diffCollapse.value = !diffCollapse.peek())}>
            <Cod name="fold" />
          </button>
          {!deleted && (
            <button type="button" class="tb tb-icon" data-act="open" aria-label={t("dv.open_file")} title={`${t("dv.open_file")} (${mod}+Enter)`} onClick={() => showFile(d.path)}>
              <Cod name="go-to-file" />
            </button>
          )}
        </>
      }
    >
      {cut && (
        <p class="file-note" role="status">
          {t("dv.cut")}
        </p>
      )}
      <div class="diff-host">
        {err ? (
          <p class="empty refused">{err}</p>
        ) : binary ? (
          <p class="empty">{t("dv.binary")}</p>
        ) : !leftSide || (d.kind !== "wt" && !rightSide) ? (
          <div class="editor-host desk-host" />
        ) : d.kind === "wt" && !deleted ? (
          <LiveSide d={d} left={leftSide} slot={slot} onCount={onCount} />
        ) : (
          rightSide && <StaticDiff d={d} left={leftSide} right={rightSide} slot={slot} onCount={onCount} />
        )}
      </div>
    </View>
  );
}
