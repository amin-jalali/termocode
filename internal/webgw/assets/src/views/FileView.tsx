// The file view (WEB-18, web UX 5.5): the file as the laptop's editor has
// it, live. A sticky header with the name, the path and "● Not saved";
// numbered rows, LTR and isolated; long lines wrap (Wrap turns it off).
// When the laptop changes the file, the changed rows flash and a line
// says so for 2 s. Files the web never shows say what they are instead.
import { Fragment, h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { BufSnap } from "../../../../webproto/proto";
import { closePath, file, openPath, type FileState } from "../lib/file";
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
import { Refused } from "./Placeholder";
import { View } from "./View";

const UPDATED_MS = 2000;
/** MAX_FLASH: a bigger change (a reload) does not flash row by row. */
const MAX_FLASH = 200;

/** wrap is on by default; turning it off scrolls long rows sideways. */
const wrapOn = { value: true };

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

function showOnLaptop(path: string): void {
  link.request("tree.reveal_on_laptop", { path }).then(
    () => toast("ok", t("laptop.shown")),
    (e: unknown) => toast("warn", errText(e)),
  );
}

/** Body draws an open buffer: its rows, live. */
function OpenBody({ st, scroller, wrap }: { st: FileState; scroller: { current: HTMLElement | null }; wrap: boolean }) {
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
    <div class={`topic${ts?.stale ? " is-stale" : ""}`}>
      {gone && (
        <p class="file-note" role="status">
          <span>{t("file.gone")}</span>
          <Button kind="neutral" small needsLink onClick={() => openPath(st.path)}>
            {t("file.open_again")}
          </Button>
        </p>
      )}
      {updated && (
        <p class="file-updated" role="status">
          {t("file.updated")}
        </p>
      )}
      {empty ? <div class="empty">{t("file.empty")}</div> : <Lines lines={buf.lines} scroller={scroller} wrap={wrap} flash={flash} />}
    </div>
  );
}

function Body({ st, scroller, wrap }: { st: FileState; scroller: { current: HTMLElement | null }; wrap: boolean }) {
  switch (st.kind) {
    case "loading":
      return <SkeletonRows rows={12} />;
    case "open":
      return <OpenBody st={st} scroller={scroller} wrap={wrap} />;
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

export function FileView({ path }: { path: string }) {
  const body = useRef<HTMLDivElement | null>(null);
  const [wrap, setWrap] = useState(wrapOn.value);
  useEffect(() => {
    openPath(path);
    return () => closePath(path);
  }, [path]);
  const st = file.value?.path === path ? file.value : { path, kind: "loading" as const };
  // Offline at the start: open it when the link is back.
  useEffect(() => {
    if (st.kind === "offline" && live.value) openPath(path);
  }, [st.kind, live.value]);
  const buf = st.kind === "open" ? readBuf(store.topic("buf:" + st.buf).value?.data) : null;
  const dir = dirName(path);
  const controller = link.meta.value?.role === "controller";
  return (
    <View
      tab="files"
      title={baseName(path)}
      sub={dir ? <span class="trunc file-path">{dir}</span> : undefined}
      back={{ label: t("tab.files"), onClick: () => go({ tab: "files", rest: "", query: {} }) }}
      bodyRef={body}
      actions={
        <>
          {buf?.dirty && (
            <Chip tone="amber" dot="busy">
              {t("file.not_saved")}
            </Chip>
          )}
          <Chip
            on={wrap}
            onClick={() => {
              wrapOn.value = !wrap;
              setWrap(!wrap);
            }}
          >
            {t("term.wrap")}
          </Chip>
          {controller && (
            <Button kind="quiet" small needsLink onClick={() => showOnLaptop(path)}>
              {t("laptop.show")}
            </Button>
          )}
        </>
      }
    >
      <Body st={st} scroller={body} wrap={wrap} />
    </View>
  );
}
