// Review in chat on the phone (MOB-14): #/more/review lists the agent
// worktrees; #/more/review?wt=<id>&f=<path> shows one changed file as a
// line diff (− removed, + added, each with its colour and its mark).
// Tap a line to add a note; the notes of a worktree are kept on this
// page until they are sent. "Send N notes to agent" sends them in one
// chat message (ai.worktree_notes: the laptop quotes the lines, never
// from a file that may hold secrets). Merge and Discard stay laptop
// cards (Cancel first). Phone layout only; the desktop keeps its agent
// worktrees side bar.
import { Fragment, h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { AIWorktree, AIWorktrees, AIWorktreeText, ReviewNote } from "../../../../../webproto/proto";
import { hubSays } from "../../lib/ai";
import { go, route } from "../../lib/router";
import { toast } from "../../lib/toasts";
import { link, live } from "../../session";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Sheet } from "../../ui/Sheet";
import { SubView } from "../more/SubView";
import { diffRows, hunks, noteSide, type Row } from "./linediff";
import { addNote, clearNotes, notes, notesOf, removeNote } from "./notes";

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
  const mine = notesOf(wt).filter((n) => n.path === text.path);
  const noted = (r: Row) => {
    const at = noteSide(r);
    return mine.some((n) => n.side === at.side && n.line === at.line);
  };
  if (hs.length === 0) return <p class="empty">{t("rv.same")}</p>;
  return (
    <div class="rv-diff" dir="ltr">
      {hs.map((hk, i) => (
        <div class="rv-hunk" key={i}>
          {hk.rows.map((r) => (
            <button type="button" key={`${r.kind}${r.old ?? ""}:${r.new ?? ""}`} class={`rv-line rv-${r.kind}${noted(r) ? " has-note" : ""}`} aria-label={lineLabel(r)} onClick={() => onLine(r)}>
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
              {noted(r) && (
                <span class="rv-noted" aria-hidden="true">
                  ✎
                </span>
              )}
            </button>
          ))}
        </div>
      ))}
    </div>
  );
}

function NoteSheet({ path, row, onDone }: { path: string; row: Row; onDone: (text: string | null) => void }) {
  const at = noteSide(row);
  const [text, setText] = useState("");
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
  const keep = (note: string | null) => {
    const r = line;
    setLine(null);
    if (note === null || !r || !file) return;
    const s = noteSide(r);
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
      <Notes wt={wt.id} />
      <SendBar wt={wt.id} />
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
      {line && <NoteSheet path={file.path} row={line} onDone={keep} />}
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
