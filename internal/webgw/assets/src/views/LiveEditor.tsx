// The in-page editor view (WEB-39): CodeMirror (editor.js, loaded now)
// over the laptop's buffer, kept in sync by lib/live.ts. When both sides
// changed the text, a bar keeps the user's text and offers the choices;
// the laptop's change can be read first. If the editor cannot load, the
// simple edit sheet opens instead.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { BufSnap, GitBlame, GitDiff, GitSnap } from "../../../../webproto/proto";
import type { EditorHandle, EditorModule } from "../editor/api";
import { startEdit } from "../lib/edit";
import { hl, hlRows, wantHl } from "../lib/hl";
import { mountDesk, setDesk, type DeskHandle } from "../lib/deskeditor";
import { loadEditor } from "../lib/lazy";
import { editing, liveStatus, LiveSync, session, setSync, syncs } from "../lib/live";
import { toast } from "../lib/toasts";
import { link, live, store } from "../session";
import { gutterMarks } from "../lib/gutter";
import { BpAsk, onGlyph, useRunGutter } from "../lib/rungutter";
import { blameOn, blameRows } from "../lib/blame";
import { ask, diagSnap, fileMarkers, findRefs, goTo, revealAt, wordAt } from "../lib/lsp";
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

/** LiveDiff makes the desktop editor a diff editor: original is the
 * older side (null while git reads it). */
export interface LiveDiff {
  original: string | null;
  inline: boolean;
  collapse: boolean;
  onCount(changes: number, added: number, removed: number): void;
}

export function LiveEditor({ path, buf, desk, slot = "g0", diff }: { path: string; buf: number; desk?: DeskOpts; slot?: string; diff?: LiveDiff }) {
  const topic = "buf:" + buf;
  const ts = store.topic(topic).value;
  const snap = readBuf(ts?.data);
  const host = useRef<HTMLDivElement | null>(null);
  const ed = useRef<EditorHandle | null>(null);
  const [ready, setReady] = useState(false);
  const diffRef = useRef(diff);
  diffRef.current = diff;

  useEffect(() => {
    let dead = false;
    const peek = () => readBuf(store.topic(topic).peek()?.data);
    const d = desk;
    const load: Promise<EditorModule> = d
      ? Promise.resolve({
          mount: (el, host) =>
            mountDesk(el, {
              ...host,
              ...d,
              slot,
              onDone: () => undefined,
              // The laptop's language servers: hover, completion, go to
              // definition (another file opens in its tab), references.
              path,
              lsp: true,
              refsLabel: t("menu.refs"),
              onLsp: (kind, line, col) => ask(kind, buf, line, col),
              onRefs: (line, col) => findRefs(path, buf, line, col, wordAt(peek()?.lines[line] ?? "", col)),
              onOpen: (to, line, col) => goTo(to, line, col),
              diff: diff ? { original: diff.original ?? "", inline: diff.inline, collapse: diff.collapse } : undefined,
              onDiff: (n, a, r) => diffRef.current?.onCount(n, a, r),
              onGlyph: diff ? undefined : (line, menu, x, y) => onGlyph(path, line, menu, x, y),
            }),
        })
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
        if (d) setDesk(slot, handle as DeskHandle);
        setSync(slot, sync);
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
      const mine = syncs.peek().get(slot);
      if (mine && mine.buf === buf) {
        mine.dispose();
        setSync(slot, null);
      }
      if (ed.current) setDesk(slot, null);
      ed.current?.destroy();
      ed.current = null;
    };
  }, [buf]);

  // The laptop's copy: every delta goes to the sync.
  const own = syncs.value.get(slot) ?? null;
  useEffect(() => {
    if (snap) own?.store(snap, isGone(ts?.data));
  }, [snap, ready, own]);
  // Git change marks (desktop): the file's unstaged diff on disk, asked
  // again when the laptop saves or git's list changes.
  const gitData = desk ? (store.topic("git").value?.data as GitSnap | undefined) : undefined;
  const changed = diff ? undefined : gitData?.files?.find((f) => f.path === path && (f.unstaged || f.untracked));
  const saved = snap ? !snap.dirty : false;
  useEffect(() => {
    const h = ed.current as DeskHandle | null;
    if (!desk || !ready || !h) return;
    if (!changed) {
      h.gutter([]);
      return;
    }
    if (!saved) return; // the marks follow the file on disk
    const timer = setTimeout(() => {
      link.request("git.diff", { path }).then(
        (data) => (ed.current as DeskHandle | null)?.gutter(gutterMarks((data as GitDiff).rows ?? [])),
        () => undefined,
      );
    }, 300);
    return () => clearTimeout(timer);
  }, [ready, !!changed, changed?.x, changed?.y, saved, snap?.tick, gitData]);
  // Blame (desktop): who last changed each line of the saved file, asked
  // again after a save or a commit; off while the buffer has unsaved
  // edits (its lines no longer match the file on disk).
  const head = gitData?.log?.[0]?.hash ?? "";
  const tracked = gitData?.repo === true && !gitData.files?.some((f) => f.path === path && f.untracked);
  const blaming = !!desk && !diff && blameOn.value && tracked;
  useEffect(() => {
    const h = ed.current as DeskHandle | null;
    if (!desk || !ready || !h) return;
    if (!blaming || !saved) {
      h.blame(null);
      return;
    }
    let stop = false;
    const timer = setTimeout(() => {
      link.request("git.blame", { path }).then(
        (d) => !stop && (ed.current as DeskHandle | null)?.blame(blameRows(d as GitBlame)),
        () => !stop && (ed.current as DeskHandle | null)?.blame(null),
      );
    }, 300);
    return () => {
      stop = true;
      clearTimeout(timer);
    };
  }, [ready, blaming, saved, head, changed?.x, changed?.y]);
  // Problems: the file's diagnostics as squiggles (desktop).
  const fileDiags = desk ? diagSnap(store.topic("diagnostics").value?.data)?.files.find((f) => f.path === path) : undefined;
  useEffect(() => {
    if (desk && ready) (ed.current as DeskHandle | null)?.markers(fileMarkers(fileDiags));
  }, [ready, fileDiags]);
  // Breakpoints, tests and the stopped row in the glyph margin (desktop).
  useRunGutter(path, !!desk && ready && !diff, (items, stop) => (ed.current as DeskHandle | null)?.glyphs(items, stop));
  // A place to show in this file (a problem, a definition, a reference).
  const at = revealAt.value;
  useEffect(() => {
    const h = ed.current as DeskHandle | null;
    if (!desk || !ready || !h || !at || at.path !== path) return;
    revealAt.value = null;
    h.reveal(at.line + 1, at.col + 1);
  }, [ready, at]);
  // A diff editor: the older side and the view follow the tab.
  const orig = diff?.original ?? null;
  useEffect(() => {
    if (ready && orig !== null) (ed.current as DeskHandle | null)?.original(orig);
  }, [ready, orig]);
  useEffect(() => {
    if (ready && diff) (ed.current as DeskHandle | null)?.diffView({ inline: diff.inline, collapse: diff.collapse });
  }, [ready, diff?.inline, diff?.collapse]);
  // The desktop editor's settings follow the view.
  useEffect(() => (ed.current as DeskHandle | null)?.opts?.({ wrap: desk?.wrap, readOnly: desk?.readOnly }), [desk?.wrap, desk?.readOnly, ready]);
  // Colours: rows whose text still matches.
  const h0 = hl.value;
  useEffect(() => ed.current?.setHl(hlRows(h0, buf)), [h0, ready]);
  // The link came back: send what waited.
  const on = live.value;
  useEffect(() => {
    if (on && own?.status.peek().phase === "waiting") void own.flush();
  }, [on]);

  const st = own?.status.value ?? { phase: "synced" as const };
  const sync = own;
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
      {desk && !diff && <BpAsk path={path} />}
    </div>
  );
}
