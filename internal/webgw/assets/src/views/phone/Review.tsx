// Review in chat on the phone (MOB-14): #/more/review lists the agent
// worktrees; #/more/review?wt=<id>&f=<path> shows one changed file as a
// line diff (− removed, + added, each with its colour and its mark).
// Swipe sideways (or ‹ ›) for the other files. Long-press a line to add
// a note (Enter or Space on a focused line does it too; a plain tap only
// scrolls); a note shows under its line and a tap opens it again to
// change or remove it (MOBUX-11). The notes of a worktree are kept on
// this page until they are sent from the bottom bar. "Send N notes to agent" sends them in one
// chat message (ai.worktree_notes: the laptop quotes the lines, never
// from a file that may hold secrets). Merge and Discard stay laptop
// cards (Cancel first). Phone layout only; the desktop keeps its agent
// worktrees side bar.
import { Fragment, h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { AIWorktree, AIWorktrees, AIWorktreeText, ReviewNote } from "../../../../../webproto/proto";
import { hubSays } from "../../lib/ai";
import { useLongPress } from "../../lib/longpress";
import { go, route } from "../../lib/router";
import { toast } from "../../lib/toasts";
import { link, live } from "../../session";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Sheet } from "../../ui/Sheet";
import { SubView } from "../more/SubView";
import { diffRows, hunks, noteSide, type Row } from "./linediff";
import { addNote, clearNotes, dropNote, noteAt, notes, notesOf, removeNote } from "./notes";
import { useSwipe } from "./swipe";

function here(q: Record<string, string>): void {
  go({ tab: "more", rest: "review", query: q });
}

function List({ list, onPick }: { list: AIWorktrees; onPick: (wt: AIWorktree) => void }) {
  if (list.noRepo) return <p class="empty">{t("ai.wt_norepo")}</p>;
  if (list.items.length === 0) return <p class="empty">{t("ai.wt_none")}</p>;
  return (
    <>
      <p class="view-note">{t("rv.pick")}</p>
      <ul class="rv-list">
        {list.items.map((wt) => {
          const n = notesOf(wt.id).length;
          return (
            <li key={wt.id}>
              <button type="button" class="rv-wt" data-id={wt.id} disabled={wt.running} onClick={() => onPick(wt)}>
                <span class={`rv-state ${wt.running ? "is-running" : "is-done"}`} aria-hidden="true">
                  {wt.running ? "◐" : "●"}
                </span>
                <bdi class="rv-wt-name" dir="auto">
                  {wt.owner || wt.id}
                </bdi>
                <bdi class="rv-wt-sub" dir="ltr">
                  {wt.running ? t("tasks.state.running") : wt.owner ? wt.branch : `${wt.branch} · ${t("ai.wt_left")}`}
                </bdi>
                {!wt.running && <span class="rv-count">{wt.files.length}</span>}
                {n > 0 && <span class="rv-count rv-count-notes">✎ {n}</span>}
              </button>
            </li>
          );
        })}
      </ul>
    </>
  );
}

function lineLabel(r: Row): string {
  if (r.kind === "add") return t("rv.line_add", { line: r.new ?? 0 });
  if (r.kind === "del") return t("rv.line_del", { line: r.old ?? 0 });
  return t("rv.line_ctx", { line: r.new ?? 0 });
}

function Diff({ text, wt, onLine }: { text: AIWorktreeText; wt: string; onLine: (r: Row) => void }) {
  const hs = hunks(diffRows(text.before, text.after));
  const box = useRef<HTMLDivElement | null>(null);
  const noteOn = (r: Row) => noteAt(wt, { path: text.path, ...noteSide(r) });
  const key = (r: Row) => {
    const at = noteSide(r);
    return `${at.side}:${at.line}`;
  };
  const rows = new Map<string, Row>();
  for (const hk of hs) for (const r of hk.rows) rows.set(key(r), r);
  // A long press on a line asks for its note; a tap is a scroll.
  useLongPress(box, (el) => {
    const r = rows.get(el.closest<HTMLElement>("[data-line]")?.dataset.line ?? "");
    if (r) onLine(r);
  });
  if (hs.length === 0) return <p class="empty">{t("rv.same")}</p>;
  return (
    <div class="rv-diff" dir="ltr" ref={box}>
      {hs.map((hk, i) => (
        <div class="rv-hunk" key={i}>
          {hk.rows.map((r) => {
            const note = noteOn(r);
            return (
            <Fragment key={`${r.kind}${r.old ?? ""}:${r.new ?? ""}`}>
            <button type="button" class={`rv-line rv-${r.kind}${note ? " has-note" : ""}`} data-line={key(r)} aria-label={lineLabel(r)} onClick={(e) => e.detail === 0 && onLine(r)}>
              <span class="rv-no" aria-hidden="true">
                {r.kind === "add" ? "" : r.old}
              </span>
              <span class="rv-no" aria-hidden="true">
                {r.kind === "del" ? "" : r.new}
              </span>
              <span class="rv-mark" aria-hidden="true">
                {r.kind === "add" ? "+" : r.kind === "del" ? "−" : " "}
              </span>
              <span class="rv-text">{r.text}</span>
              {note && (
                <span class="rv-noted" aria-hidden="true">
                  ✎
                </span>
              )}
            </button>
            {note && (
              <button type="button" class="rv-inline-note" aria-label={t("rv.note_open", { line: noteSide(r).line, text: note.text })} onClick={() => onLine(r)}>
                <span aria-hidden="true">✎</span>
                <bdi dir="auto">{note.text}</bdi>
              </button>
            )}
            </Fragment>
            );
          })}
        </div>
      ))}
    </div>
  );
}

function NoteSheet({ path, row, start, onDone }: { path: string; row: Row; start: string; onDone: (text: string | null, remove?: boolean) => void }) {
  const at = noteSide(row);
  const [text, setText] = useState(start);
  const label = at.side === "old" ? t("rv.note_side_old", { path, line: at.line }) : t("rv.note_on", { path, line: at.line });
  const ok = text.trim() !== "" && new TextEncoder().encode(text).length <= 500;
  return (
    <Sheet onLater={() => onDone(null)} label={label}>
      <div class="ask rv-note-sheet">
        <p class="ask-text">{label}</p>
        <p class="rv-quote" dir="ltr">
          {row.text}
        </p>
        <textarea class="rv-note-input" rows={3} maxLength={500} placeholder={t("rv.note_hint")} aria-label={t("rv.note_hint")} value={text} onInput={(e) => setText((e.currentTarget as HTMLTextAreaElement).value)} />
        <div class="choices">
          <button type="button" class="btn btn-neutral" onClick={() => onDone(null)}>
            {t("cancel")}
          </button>
          <button type="button" class="btn btn-primary" disabled={!ok} onClick={() => onDone(text.trim())}>
            {t("rv.note_keep")}
          </button>
        </div>
        {start !== "" && (
          <button type="button" class="btn btn-neutral rv-note-drop" onClick={() => onDone(null, true)}>
            {t("rv.note_remove")}
          </button>
        )}
      </div>
    </Sheet>
  );
}

function Notes({ wt }: { wt: string }) {
  const mine = notesOf(wt);
  if (mine.length === 0) return null;
  return (
    <section class="rv-notes" aria-labelledby="rv-notes-h">
      <h2 class="group-title" id="rv-notes-h">
        {t("rv.notes")}
      </h2>
      <ul>
        {mine.map((n, i) => (
          <li key={i} class="rv-note">
            <bdi class="rv-note-at" dir="ltr">
              {n.side === "old" ? "−" : "+"} {n.path}:{n.line}
            </bdi>
            <bdi class="rv-note-text" dir="auto">
              {n.text}
            </bdi>
            <button type="button" class="tb rv-note-x" aria-label={t("rv.note_remove")} title={t("rv.note_remove")} onClick={() => removeNote(wt, i)}>
              ✕
            </button>
          </li>
        ))}
      </ul>
    </section>
  );
}

function SendBar({ wt }: { wt: string }) {
  const mine = notesOf(wt);
  const [busy, setBusy] = useState(false);
  const controller = link.meta.value?.role === "controller";
  if (mine.length === 0) return null;
  const send = () => {
    if (busy || !live.value || !controller) return;
    setBusy(true);
    const args: { id: string; notes: ReviewNote[] } = { id: wt, notes: mine.map((n) => ({ path: n.path, line: n.line, side: n.side, text: n.text })) };
    link.request("ai.worktree_notes", args).then(
      () => {
        setBusy(false);
        clearNotes(wt);
        toast("ok", t("rv.sent"));
        go({ tab: "chat", rest: "", query: {} });
      },
      (e: unknown) => {
        setBusy(false);
        toast("error", hubSays(e));
      },
    );
  };
  return (
    <div class="rv-send">
      {!controller && <p class="view-note">{t("rv.viewer")}</p>}
      {controller && !live.value && <p class="view-note">{t("rv.offline")}</p>}
      <button type="button" class="btn btn-primary rv-send-btn" data-act="send-notes" disabled={busy || !live.value || !controller} onClick={send}>
        {busy ? t("rv.sending") : mine.length === 1 ? t("rv.send_one") : t("rv.send_n", { n: mine.length })}
      </button>
    </div>
  );
}

function Worktree({ wt, path }: { wt: AIWorktree; path: string }) {
  const files = wt.files;
  const at = Math.max(0, files.findIndex((f) => f.path === path));
  const file = files[at];
  const [text, setText] = useState<AIWorktreeText | null>(null);
  const [err, setErr] = useState("");
  const [line, setLine] = useState<Row | null>(null);
  const [ask, setAsk] = useState<"merge" | "discard" | null>(null);
  const top = useRef<HTMLDivElement | null>(null);
  const body = useRef<HTMLDivElement | null>(null);
  const on = live.value;
  const controller = link.meta.value?.role === "controller";
  useEffect(() => {
    if (!on || !file) return;
    let stop = false;
    setText(null);
    setErr("");
    link.request("ai.worktree_text", { id: wt.id, path: file.path }).then(
      (r) => !stop && setText(r as AIWorktreeText),
      (e: unknown) => !stop && setErr(hubSays(e)),
    );
    top.current?.scrollIntoView?.({ block: "start" });
    return () => {
      stop = true;
    };
  }, [wt.id, file?.path, on]);
  const move = (d: number) => {
    const next = files[at + d];
    if (next) here({ wt: wt.id, f: next.path });
  };
  // A swipe to the left is the next file, to the right the one before.
  useSwipe(body, (dir) => move(dir === -1 ? 1 : -1));
  const keep = (note: string | null, remove?: boolean) => {
    const r = line;
    setLine(null);
    if (!r || !file) return;
    const s = noteSide(r);
    if (remove) {
      dropNote(wt.id, { path: file.path, ...s });
      return;
    }
    if (note === null) return;
    if (!addNote(wt.id, { path: file.path, line: s.line, side: s.side, text: note })) toast("info", t("rv.notes_max"));
  };
  if (wt.running) return <p class="empty">{t("rv.running")}</p>;
  if (!file) return <p class="empty">{t("ai.wt_no_files")}</p>;
  return (
    <div class="rv" ref={top}>
      <div class="rv-head">
        <bdi class="rv-wt-sub" dir="ltr">
          {wt.branch}
        </bdi>
        <div class="rv-nav" role="group" aria-label={t("rv.files")}>
          <button type="button" class="btn btn-neutral rv-nav-btn" aria-label={t("rv.prev")} disabled={at === 0} onClick={() => move(-1)}>
            ‹
          </button>
          <span class="rv-pos">{t("rv.file_of", { i: at + 1, n: files.length })}</span>
          <button type="button" class="btn btn-neutral rv-nav-btn" aria-label={t("rv.next")} disabled={at >= files.length - 1} onClick={() => move(1)}>
            ›
          </button>
        </div>
        <bdi class="rv-path" dir="ltr">
          {file.status} {file.path}
        </bdi>
      </div>
      <div class="rv-body" ref={body}>
      {text?.truncated && <p class="view-note">{t("rv.cut")}</p>}
      {err ? (
        <p class="empty refused">{err}</p>
      ) : !text ? (
        <p class="empty">{t("quick.loading")}</p>
      ) : text.secret ? (
        <p class="empty refused">{t("rv.secret")}</p>
      ) : text.binary ? (
        <p class="empty">{t("rv.binary")}</p>
      ) : (
        <Diff text={text} wt={wt.id} onLine={setLine} />
      )}
      {files.length > 1 && <p class="view-note rv-hint">{t("rv.swipe")}</p>}
      <Notes wt={wt.id} />
      </div>
      {controller && (
        <div class="choices rv-acts">
          <button type="button" class="btn btn-neutral" data-act="discard" onClick={() => setAsk("discard")}>
            {t("rv.discard_laptop")}
          </button>
          <button type="button" class="btn btn-neutral" data-act="merge" onClick={() => setAsk("merge")}>
            {t("rv.merge_laptop")}
          </button>
        </div>
      )}
      <SendBar wt={wt.id} />
      {line && <NoteSheet path={file.path} row={line} start={noteAt(wt.id, { path: file.path, ...noteSide(line) })?.text ?? ""} onDone={keep} />}
      {ask && (
        <AskLaptop
          label={ask === "merge" ? t("ai.wt_merge") : t("ai.wt_discard")}
          text={t(ask === "merge" ? "ai.wt_body_merge" : "ai.wt_body_discard", { branch: wt.branch })}
          send={() => link.request(ask === "merge" ? "ai.worktree_merge" : "ai.worktree_discard", { id: wt.id })}
          onDone={() => setAsk(null)}
          onConfirmed={() => {
            clearNotes(wt.id);
            here({});
          }}
        />
      )}
    </div>
  );
}

/** ReviewView is #/more/review on a phone. */
export function ReviewView() {
  const q = route.value.query;
  const [list, setList] = useState<AIWorktrees | null>(null);
  const [err, setErr] = useState("");
  const [again, setAgain] = useState(0);
  const on = live.value;
  void notes.value; // draw again when a note changes
  useEffect(() => {
    if (!on) return;
    let stop = false;
    link.request("ai.worktrees").then(
      (r) => {
        if (stop) return;
        setErr("");
        setList(r as AIWorktrees);
      },
      (e: unknown) => !stop && setErr(hubSays(e)),
    );
    return () => {
      stop = true;
    };
  }, [on, again, q.wt ?? ""]);
  const wt = q.wt ? list?.items.find((w) => w.id === q.wt) : undefined;
  return (
    <SubView
      title={t("rv.title")}
      name="review"
      actions={
        <button type="button" class="tb" data-act="refresh" aria-label={t("ctr.refresh")} title={t("ctr.refresh")} onClick={() => setAgain(again + 1)}>
          ↻
        </button>
      }
    >
      {err ? (
        <p class="empty refused">{err}</p>
      ) : !list ? (
        <p class="empty">{t("quick.loading")}</p>
      ) : q.wt && !wt ? (
        <p class="empty">{t("ai.wt_none")}</p>
      ) : wt ? (
        <Worktree key={wt.id} wt={wt} path={q.f ?? ""} />
      ) : (
        <List list={list} onPick={(w) => here({ wt: w.id })} />
      )}
    </SubView>
  );
}
