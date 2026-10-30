// The in-page editor view (WEB-39): CodeMirror (editor.js, loaded now)
// over the laptop's buffer, kept in sync by lib/live.ts. When both sides
// changed the text, a bar keeps the user's text and offers the choices;
// the laptop's change can be read first. If the editor cannot load, the
// simple edit sheet opens instead.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { BufSnap } from "../../../../webproto/proto";
import type { EditorHandle, EditorModule } from "../editor/api";
import { startEdit } from "../lib/edit";
import { hl, hlRows, wantHl } from "../lib/hl";
import { activeDesk, mountDesk, type DeskHandle } from "../lib/deskeditor";
import { loadEditor } from "../lib/lazy";
import { editing, liveStatus, LiveSync, session } from "../lib/live";
import { toast } from "../lib/toasts";
import { live, store } from "../session";
import { isGone } from "../state/files";
import { t } from "../strings";
import { Button } from "../ui/Button";
import { DiffRow } from "../ui/rows";
import { diffRows } from "./EditSheet";

function readBuf(data: unknown): BufSnap | null {
  if (typeof data !== "object" || data === null) return null;
  const d = data as BufSnap;
  return Array.isArray(d.lines) ? d : null;
}

/** doneLive closes the editor once nothing is left unsent. */
export function doneLive(): void {
  const s = session.peek();
  if (!s) {
    editing.value = null;
    return;
  }
  s.flush().then(() => {
    if (session.peek() !== s) return;
    if (s.dirty() && liveStatus.peek().phase !== "gone") {
      toast("warn", liveStatus.peek().phase === "conflict" ? t("live.save_first") : t("live.waiting"));
      return;
    }
    editing.value = null;
  });
}

/** saveLive sends what is unsent and saves through the laptop's editor. */
export function saveLive(): void {
  const s = session.peek();
  if (!s) return;
  if (liveStatus.peek().phase === "conflict") {
    toast("warn", t("live.save_first"));
    return;
  }
  s.save().then((ok) => ok && toast("ok", t("file.saved")));
}

function Conflict({ base, laptop, sync }: { base: string; laptop: string; sync: LiveSync }) {
  const [show, setShow] = useState(false);
  const dr = diffRows(base.split("\n"), laptop.split("\n"));
  return (
    <div class="live-conflict" role="alert">
      <p class="file-note">
        <span>{t("live.conflict_body")}</span>
        <button type="button" class="link live-diff-btn" aria-expanded={show ? "true" : "false"} onClick={() => setShow(!show)}>
          {t("live.show_laptop")}
        </button>
      </p>
      {show && (
        <div class="edit-diff">
          {dr.del.map((x, i) => (
            <DiffRow key={"d" + i} kind="del" a={dr.at + i + 1} text={x} />
          ))}
          {dr.add.map((x, i) => (
            <DiffRow key={"a" + i} kind="add" b={dr.at + i + 1} text={x} />
          ))}
        </div>
      )}
      <div class="live-choices">
        <Button kind="neutral" small onClick={() => sync.takeLaptop()}>
          {t("live.take_laptop")}
        </Button>
        <Button kind="neutral" small needsLink onClick={() => void sync.sendMine()}>
          {t("live.send_mine")}
        </Button>
      </div>
    </div>
  );
}

/** DeskOpts: the desktop's editor (lib/deskeditor.ts) and its settings. */
export interface DeskOpts {
  readOnly: boolean;
  wrap: boolean;
  tabSize: number;
  insertSpaces: boolean;
}

export function LiveEditor({ path, buf, desk }: { path: string; buf: number; desk?: DeskOpts }) {
  const topic = "buf:" + buf;
  const ts = store.topic(topic).value;
  const snap = readBuf(ts?.data);
  const host = useRef<HTMLDivElement | null>(null);
  const ed = useRef<EditorHandle | null>(null);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    let dead = false;
    const peek = () => readBuf(store.topic(topic).peek()?.data);
    const d = desk;
    const load: Promise<EditorModule> = d
      ? Promise.resolve({ mount: (el, host) => mountDesk(el, { ...host, ...d, onDone: () => undefined }) })
      : loadEditor();
    load.then(
      (mod) => {
        const first = peek();
        if (dead || !host.current || !first) return;
        let sync: LiveSync | null = null;
        const handle = mod.mount(host.current, {
          doc: first.lines.join("\n"),
          label: t("file.edit") + " " + path,
          onChange: () => sync?.local(),
          onSave: saveLive,
          onDone: doneLive,
          onViewport: (a, b) => {
            const s = peek();
            if (s) wantHl(buf, s.tick, a, b, () => peek()?.lines ?? [], () => peek()?.tick ?? -1);
          },
        });
        sync = new LiveSync(buf, handle, first);
        ed.current = handle;
        if (d) activeDesk.value = handle as DeskHandle;
        session.value = sync;
        handle.setHl(hlRows(hl.peek(), buf));
        handle.focus();
        setReady(true);
      },
      () => {
        if (dead) return;
        toast("warn", t("live.load_failed"));
        editing.value = null;
        const s = peek();
        if (s) startEdit(path, s, 0, 0);
      },
    );
    return () => {
      dead = true;
      session.peek()?.dispose();
      session.value = null;
      if (activeDesk.peek() === ed.current) activeDesk.value = null;
      ed.current?.destroy();
      ed.current = null;
    };
  }, [buf]);

  // The laptop's copy: every delta goes to the sync.
  useEffect(() => {
    if (snap) session.peek()?.store(snap, isGone(ts?.data));
  }, [snap, ready]);
  // The desktop editor's settings follow the view.
  useEffect(() => (ed.current as DeskHandle | null)?.opts?.({ wrap: desk?.wrap, readOnly: desk?.readOnly }), [desk?.wrap, desk?.readOnly, ready]);
  // Colours: rows whose text still matches.
  const h0 = hl.value;
  useEffect(() => ed.current?.setHl(hlRows(h0, buf)), [h0, ready]);
  // The link came back: send what waited.
  const on = live.value;
  useEffect(() => {
    if (on && liveStatus.peek().phase === "waiting") void session.peek()?.flush();
  }, [on]);

  const st = liveStatus.value;
  const sync = session.value;
  return (
    <div class="live">
      {st.phase === "conflict" && sync && st.base !== undefined && st.laptop !== undefined && <Conflict base={st.base} laptop={st.laptop} sync={sync} />}
      {st.phase === "gone" && (
        <p class="file-note" role="status">
          {t("file.gone")}
        </p>
      )}
      {st.phase === "error" && (
        <p class="file-note save-err" role="alert">
          <span dir="auto">{t("live.not_sent", { msg: st.note ?? "" })}</span>
        </p>
      )}
      {!ready && !desk && <div class="empty">{t("live.loading")}</div>}
      <div ref={host} class={desk ? "editor-host desk-host" : "editor-host"} />
    </div>
  );
}
