// The file view (WEB-18, web UX 5.5): the file as the laptop's editor has
// it, live. A sticky header with the name, the path and "● Not saved";
// numbered rows, LTR and isolated; long lines wrap (Wrap turns it off).
// When the laptop changes the file, the changed rows flash and a line
// says so for 2 s. Files the web never shows say what they are instead.
import { Fragment, h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { usePinch } from "../lib/rowfont";
import type { BufSnap } from "../../../../webproto/proto";
import { layout } from "../lib/layout";
import { wordWrap } from "../lib/workbench";
import { modKey } from "../lib/platform";
import { FileIcon, IconLaptop, IconPencil, IconRedo, IconRows, IconSave, IconUndo, IconWrap } from "../ui/icons";
import { draft, own, save, saveErr, selection, startEdit, tapRow, undo } from "../lib/edit";
import { closePath, fileOf, holdPath, openPath, type FileState } from "../lib/file";
import { dropHl, hl, rowSegs, wantHl } from "../lib/hl";
import { IMAGE_EXT } from "../lib/image";
import { BlobImg } from "../ui/BlobImg";
import { editing, syncs } from "../lib/live";
import { go } from "../lib/router";
import { toast } from "../lib/toasts";
import { link, live, store } from "../session";
import { isGone } from "../state/files";
import { t } from "../strings";
import { Button } from "../ui/Button";
import { errText } from "../ui/CardSheet";
import { Chip } from "../ui/Chip";
import { Lines, type Flash } from "../ui/Lines";
import { SkeletonRows } from "../ui/Skeleton";
import { EditSheet } from "./EditSheet";
import { doneLive, LiveEditor, saveLive } from "./LiveEditor";
import { Refused } from "./Placeholder";
import { View } from "./View";

const UPDATED_MS = 2000;
/** MAX_FLASH: a bigger change (a reload) does not flash row by row. */
const MAX_FLASH = 200;


function baseName(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? path : path.slice(i + 1);
}

function dirName(path: string): string {
  const i = path.lastIndexOf("/");
  return i < 0 ? "" : path.slice(0, i);
}

/** bytes writes a size the way people read it: 12 KB, 4.2 MB. */
export function bytes(n: number): string {
  if (n < 1024) return n + " B";
  if (n < 1024 * 1024) return Math.round(n / 1024) + " KB";
  return (n / (1024 * 1024)).toFixed(1) + " MB";
}

function readBuf(data: unknown): BufSnap | null {
  if (typeof data !== "object" || data === null) return null;
  const d = data as BufSnap;
  return Array.isArray(d.lines) ? d : null;
}

/** changed is the range of rows that differ between two versions, or null. */
export function changed(prev: readonly string[], next: readonly string[]): { a: number; b: number } | null {
  let a = 0;
  while (a < prev.length && a < next.length && prev[a] === next[a]) a++;
  if (a === prev.length && a === next.length) return null;
  let pe = prev.length - 1, ne = next.length - 1;
  while (pe >= a && ne >= a && prev[pe] === next[ne]) {
    pe--;
    ne--;
  }
  return { a, b: Math.max(a, ne) };
}

/** topRow is the first row on screen, for Edit without a selection. */
function topRow(sc: HTMLElement | null): { a: number; b: number } {
  const top = sc?.getBoundingClientRect().top ?? 0;
  for (const el of sc?.querySelectorAll<HTMLElement>(".crow[data-i]") ?? []) {
    if (el.getBoundingClientRect().bottom > top) {
      const i = Number(el.dataset.i);
      return { a: i, b: i };
    }
  }
  return { a: 0, b: 0 };
}

/** Crumbs is the folder path as a breadcrumb: cmd › covreport. */
function Crumbs({ dir, file }: { dir: string; file: string }) {
  const parts = dir.split("/");
  return (
    <span class="trunc file-path crumbs" title={dir}>
      {parts.map((p, i) => (
        <Fragment key={i}>
          {i > 0 && (
            <span class="crumb-sep" aria-hidden="true">
              ›
            </span>
          )}
          <span class="crumb">{p}</span>
        </Fragment>
      ))}
      <span class="crumb-sep" aria-hidden="true">
        ›
      </span>
      {layout.value === "desktop" && <FileIcon path={file} />}
    </span>
  );
}

export function showOnLaptop(path: string): void {
  link.request("tree.reveal_on_laptop", { path }).then(
    () => toast("ok", t("laptop.shown")),
    (e: unknown) => toast("warn", errText(e)),
  );
}

/** Body draws an open buffer: its rows, live. */
function OpenBody({ st, scroller, wrap, canEdit }: { st: FileState; scroller: { current: HTMLElement | null }; wrap: boolean; canEdit: boolean }) {
  const topic = "buf:" + st.buf;
  const ts = store.topic(topic).value;
  const buf = readBuf(ts?.data);
  const prev = useRef<readonly string[] | null>(null);
  const [flash, setFlash] = useState<Flash | null>(null);
  const [updated, setUpdated] = useState(false);
  const lines = buf?.lines ?? null;

  useEffect(() => {
    const before = prev.current;
    prev.current = lines;
    if (!before || !lines || before === lines) return;
    const c = changed(before, lines);
    if (!c) return;
    if (c.b - c.a < MAX_FLASH) setFlash({ id: Date.now(), a: c.a, b: c.b });
    if (buf && buf.tick <= own.tick) return; // this page's own change
    setUpdated(true);
    const timer = setTimeout(() => setUpdated(false), UPDATED_MS);
    return () => clearTimeout(timer);
  }, [lines]);

  if (!buf) {
    const r = link.refused.value.get(topic);
    if (r) return <Refused topic={topic}>{r.msg}</Refused>;
    return <SkeletonRows rows={12} />;
  }
  const gone = isGone(ts?.data);
  const empty = buf.lines.length === 0 || (buf.lines.length === 1 && buf.lines[0] === "");
  return (
    <div class={`topic${ts?.stale ? " is-stale" : ""}`} style={buf.ts ? { "--ts": String(buf.ts) } : undefined}>
      {gone && (
        <p class="file-note" role="status">
          <span>{t("file.gone")}</span>
          <Button kind="neutral" small needsLink onClick={() => openPath(st.path)}>
            {t("file.open_again")}
          </Button>
        </p>
      )}
      {saveErr.value !== null && (
        <p class="file-note save-err" role="alert">
          <span dir="auto">{t("edit.save_err", { msg: saveErr.value })}</span>
          <Button kind="neutral" small needsLink onClick={() => save(buf.buf).then((ok) => ok && toast("ok", t("file.saved")))}>
            {t("retry")}
          </Button>
        </p>
      )}
      {updated && (
        <p class="file-updated" role="status">
          {t("file.updated")}
        </p>
      )}
      {empty ? (
        <div class="empty">{t("file.empty")}</div>
      ) : (
        <Lines
          lines={buf.lines}
          scroller={scroller}
          wrap={wrap}
          flash={flash}
          sel={canEdit && !gone ? selection.value : null}
          onTap={canEdit && !gone ? tapRow : undefined}
          onLong={canEdit && !gone ? (i) => startEdit(st.path, buf, i, i) : undefined}
          segs={(i, text) => rowSegs(hl.value, buf.buf, i, text)}
          onWin={(a, b) => !gone && wantHl(buf.buf, buf.tick, a, b, () => readBuf(store.topic(topic).peek()?.data)?.lines ?? [], () => readBuf(store.topic(topic).peek()?.data)?.tick ?? -1)}
        />
      )}
      {canEdit && !gone && draft.value && <EditSheet buf={buf} />}
    </div>
  );
}

function Body({ st, scroller, wrap, canEdit }: { st: FileState; scroller: { current: HTMLElement | null }; wrap: boolean; canEdit: boolean }) {
  switch (st.kind) {
    case "loading":
      return <SkeletonRows rows={12} />;
    case "open":
      return <OpenBody st={st} scroller={scroller} wrap={wrap} canEdit={canEdit} />;
    case "head":
      return (
        <div class="topic">
          <p class="file-note" role="status">
            {t("file.big")}
            {st.size !== undefined && <span class="file-meta">{bytes(st.size)}</span>}
          </p>
          <Lines lines={st.head?.lines ?? []} scroller={scroller} wrap={wrap} />
        </div>
      );
    case "binary":
    case "secret":
      if (st.kind === "binary" && IMAGE_EXT.test(st.path) && link.meta.value?.role === "controller")
        return (
          <div class="img-preview">
            <BlobImg op="file.image" args={{ path: st.path }} alt={t("img.alt", { name: baseName(st.path) })} />
            {st.size !== undefined && <span class="file-meta">{bytes(st.size)}</span>}
          </div>
        );
      return (
        <div class="empty">
          <div class="empty-state">
            <p>{t(st.kind === "binary" ? "file.binary" : "file.secret")}</p>
            {st.size !== undefined && <span class="file-meta">{bytes(st.size)}</span>}
          </div>
        </div>
      );
    case "missing":
      return <div class="empty">{t("file.missing")}</div>;
    case "offline":
      return <div class="empty">{t("offline.input")}</div>;
    default:
      return (
        <div class="empty refused" role="status">
          <p dir="auto">{errText(st.err)}</p>
          <Button kind="neutral" small needsLink onClick={() => openPath(st.path)}>
            {t("retry")}
          </Button>
        </div>
      );
  }
}

export function FileView({ path, slot = "g0" }: { path: string; slot?: string }) {
  const body = useRef<HTMLDivElement | null>(null);
  usePinch(body);
  const wrap = wordWrap.value;
  useEffect(() => {
    holdPath(path);
    selection.value = null;
    saveErr.value = null;
    return () => {
      closePath(path);
      draft.value = null;
      editing.value = null;
      dropHl();
    };
  }, [path]);
  const st = fileOf(path) ?? { path, kind: "loading" as const };
  // Offline at the start: open it when the link is back.
  useEffect(() => {
    if (st.kind === "offline" && live.value) openPath(path);
  }, [st.kind, live.value]);
  const buf = st.kind === "open" ? readBuf(store.topic("buf:" + st.buf).value?.data) : null;
  const dir = dirName(path);
  const controller = link.meta.value?.role === "controller";
  const canEdit = controller && !!buf && !buf.readonly && !isGone(store.topic("buf:" + st.buf).value?.data);
  const editLines = () => {
    if (!buf || !live.peek()) return;
    const s = selection.peek() ?? topRow(body.current);
    startEdit(path, buf, s.a, s.b);
  };
  const desk = layout.value === "desktop";
  // The desktop's editor is always live: the laptop's buffer, editable
  // when this page may change it (a viewer or a read-only buffer reads).
  const deskLive = desk && st.kind === "open" && !!buf;
  const isEditing = (canEdit && editing.value === path && st.kind === "open") || deskLive;
  const sync = syncs.value.get(slot) ?? null;
  const phase = sync?.status.value.phase ?? "synced";
  return (
    <View
      tab="files"
      title={baseName(path)}
      sub={dir ? <Crumbs dir={dir} file={path} /> : undefined}
      back={desk ? undefined : { label: t("tab.files"), onClick: () => go({ tab: "files", rest: "", query: {} }) }}
      bodyRef={body}
      bodyClass={isEditing ? "is-editing" : undefined}
      actions={
        deskLive ? (
          <>
            {canEdit && phase !== "synced" && (
              <Chip tone={phase === "sending" ? undefined : "amber"} dot={phase === "sending" ? "busy" : undefined}>
                {phase === "sending" ? t("live.sending") : phase === "waiting" ? t("live.waiting") : t("file.not_saved")}
              </Chip>
            )}
            {canEdit && buf.dirty && phase === "synced" && (
              <Chip tone="amber" dot="busy">
                {t("file.not_saved")}
              </Chip>
            )}
            {!canEdit && <Chip>{t("viewonly")}</Chip>}
            <Button
              kind="quiet"
              small
              icon={<IconWrap />}
              iconOnly
              pressed={wrap}
              title={`${t("term.wrap")} (Alt+Z)`}
              onClick={() => (wordWrap.value = !wrap)}
            >
              {t("term.wrap")}
            </Button>
            {canEdit && (
              <Button kind="quiet" small icon={<IconUndo />} iconOnly title={`${t("undo")} (${modKey()}+Z)`} onClick={() => sync?.undoEd()}>
                {t("undo")}
              </Button>
            )}
            {canEdit && (
              <Button kind="quiet" small icon={<IconRedo />} iconOnly title={`${t("live.redo")} (${modKey()}+Shift+Z)`} onClick={() => sync?.redoEd()}>
                {t("live.redo")}
              </Button>
            )}
            {controller && (
              <Button kind="quiet" small needsLink icon={<IconLaptop />} iconOnly title={t("laptop.show")} onClick={() => showOnLaptop(path)}>
                {t("laptop.show")}
              </Button>
            )}
            {canEdit && (
              <Button kind="quiet" small needsLink icon={<IconSave />} iconOnly title={`${t("file.save")} (${modKey()}+S)`} onClick={saveLive}>
                {t("file.save")}
              </Button>
            )}
          </>
        ) : isEditing ? (
          <>
            <Chip tone={phase === "conflict" || phase === "error" || phase === "gone" ? "amber" : phase === "waiting" ? "amber" : undefined} dot={phase === "sending" ? "busy" : undefined}>
              {phase === "sending" ? t("live.sending") : phase === "waiting" ? t("live.waiting") : phase === "synced" ? t("live.synced") : t("file.not_saved")}
            </Chip>
            <Button kind="quiet" small icon={<IconUndo />} iconOnly={desk} title={`${t("undo")} (${modKey()}+Z)`} onClick={() => sync?.undoEd()}>
              {t("undo")}
            </Button>
            <Button kind="quiet" small icon={<IconRedo />} iconOnly={desk} title={`${t("live.redo")} (${modKey()}+Shift+Z)`} onClick={() => sync?.redoEd()}>
              {t("live.redo")}
            </Button>
            <Button kind="neutral" small needsLink icon={<IconSave />} title={`${t("file.save")} (${modKey()}+S)`} onClick={saveLive}>
              {t("file.save")}
            </Button>
            <Button kind="primary" small onClick={doneLive}>
              {t("done")}
            </Button>
          </>
        ) : (
        <>
          {buf?.dirty && (
            <Chip tone="amber" dot="busy">
              {t("file.not_saved")}
            </Chip>
          )}
          <Button
            kind="quiet"
            small
            icon={<IconWrap />}
            iconOnly={desk}
            pressed={wrap}
            title={t("term.wrap")}
            onClick={() => (wordWrap.value = !wrap)}
          >
            {t("term.wrap")}
          </Button>
          {canEdit && (buf.dirty || own.tick > 0) && (
            <Button kind="quiet" small needsLink icon={<IconUndo />} iconOnly={desk} title={`${t("undo")} (${modKey()}+Z)`} onClick={() => undo(buf.buf).then((why) => why !== null && toast("warn", why))}>
              {t("undo")}
            </Button>
          )}
          {controller && (
            <Button kind="quiet" small needsLink icon={<IconLaptop />} iconOnly={desk} title={t("laptop.show")} onClick={() => showOnLaptop(path)}>
              {t("laptop.show")}
            </Button>
          )}
          {st.kind === "open" && buf && !canEdit && <Chip>{t("viewonly")}</Chip>}
          {canEdit && selection.value && (
            <Button kind="neutral" small needsLink icon={<IconRows />} onClick={editLines}>
              {t("file.edit_lines")}
            </Button>
          )}
          {canEdit && buf.dirty && (
            <Button kind="neutral" small needsLink icon={<IconSave />} title={`${t("file.save")} (${modKey()}+S)`} onClick={() => save(buf.buf).then((ok) => ok && toast("ok", t("file.saved")))}>
              {t("file.save")}
            </Button>
          )}
          {canEdit && (
            <Button kind="primary" small needsLink icon={<IconPencil />} title={t("file.edit")} onClick={() => (editing.value = path)}>
              {t("file.edit")}
            </Button>
          )}
        </>
        )
      }
    >
      {deskLive && buf ? (
        <LiveEditor key={buf.buf} path={path} buf={buf.buf} slot={slot} desk={{ readOnly: !canEdit, wrap, tabSize: buf.ts ?? 0, insertSpaces: buf.et === true }} />
      ) : isEditing && buf ? (
        <LiveEditor path={path} buf={buf.buf} />
      ) : (
        <Body st={st} scroller={body} wrap={wrap} canEdit={canEdit} />
      )}
    </View>
  );
}
