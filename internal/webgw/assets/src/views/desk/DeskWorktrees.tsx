// The desktop's WORKTREES view (WTX-16): the laptop's WORKTREES list (the
// worktrees topic) as a side bar tree like the laptop's: groups (Pinned,
// Active, Asleep, External, Archived), each worktree with its agent
// state's icon, branch, ahead / behind and changed files, its last note
// and link. A filter box narrows the list here (the laptop's filter
// stays). On hover: Pin, Sleep or Wake, Note, Archive (or Restore) and
// Delete. Pin, Note and Wake run at once; Sleep asks here first, and the
// laptop asks again when agent tabs would end; Archive and Delete ask
// the laptop (a card there, then the laptop's own steps).
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useState } from "preact/hooks";
import type { WorktreeDone, WorktreeItem, WorktreesSnap } from "../../../../../webproto/proto";
import { toast } from "../../lib/toasts";
import { link, store } from "../../session";
import { t, type StringKey } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Cod } from "../../ui/icons";
import { Act, Ask, PaneSection, PaneView, Row, Tree } from "../../ui/pane";
import { gitFailed as failed } from "../git/errors";

const GROUPS: readonly [string, StringKey][] = [
  ["pinned", "chat.pinned"],
  ["active", "wt.g_active"],
  ["asleep", "wt.g_asleep"],
  ["external", "wt.g_external"],
  ["archived", "wt.g_archived"],
];

const shut = signal<ReadonlySet<string>>(new Set());
const filter = signal("");

/** readWorktrees is the worktrees topic (null: not here yet or odd). */
export function readWorktrees(data: unknown): WorktreesSnap | null {
  return typeof data === "object" && data !== null && Array.isArray((data as { worktrees?: unknown }).worktrees) ? (data as WorktreesSnap) : null;
}

/** matchWorktree: the filter text is in its name, branch or folder. */
export function matchWorktree(w: WorktreeItem, q: string): boolean {
  const f = q.trim().toLowerCase();
  return !f || [w.title, w.branch ?? "", w.where, w.agent ?? ""].some((s) => s.toLowerCase().includes(f));
}

function icon(w: WorktreeItem) {
  switch (w.state) {
    case "working":
      return <span class="c-ok"><Cod name="circle-filled" /></span>;
    case "waiting":
      return <span class="c-warn"><Cod name="bell-dot" /></span>;
    case "done":
      return <span class="c-ok"><Cod name="check" /></span>;
    case "asleep":
      return <Cod name="debug-pause" />;
    case "gone":
      return <span class="c-err"><Cod name="error" /></span>;
    case "archived":
      return <Cod name="archive" />;
  }
  return <Cod name="worktree" />;
}

/** desc is the row's second part: "feat/x · 3↑ 1↓ · 4 dirty". */
function desc(w: WorktreeItem): string {
  const parts = [w.branch ?? ""];
  if (w.status) {
    const ab = [w.ahead ? `${w.ahead}↑` : "", w.behind ? `${w.behind}↓` : ""].filter(Boolean).join(" ");
    if (ab) parts.push(ab);
    parts.push(w.dirty ? t("wt.dirty", { n: w.dirty }) : t("wt.clean"));
  }
  if (w.here) parts.push(t("wt.here"));
  return parts.filter(Boolean).join(" · ");
}

type Pending = { id: string; op: "sleep" | "note" } | null;
type Laptop = { w: WorktreeItem; op: "archive" | "delete" } | null;

function NoteEdit({ w, onDone }: { w: WorktreeItem; onDone: () => void }) {
  const [text, setText] = useState("");
  const save = () => {
    const v = text.trim();
    if (!v) return onDone();
    link.request("worktrees.note", { id: w.id, text: v.slice(0, 500) }).then(() => toast("ok", t("wt.noted", { name: w.title })), failed);
    onDone();
  };
  return (
    <div class="wt-note pv-ask" role="group" aria-label={t("wt.note")}>
      <div class="pv-input">
        <input
          value={text}
          maxLength={500}
          placeholder={t("wt.note_ph")}
          aria-label={t("wt.note")}
          ref={(el) => el?.focus()}
          onInput={(e) => setText((e.currentTarget as HTMLInputElement).value)}
          onKeyDown={(e) => {
            if (e.key === "Escape") onDone();
            if (e.key === "Enter") save();
          }}
        />
      </div>
      <div class="pv-ask-row">
        <button type="button" class="pv-btn is-2" onClick={onDone}>
          {t("cancel")}
        </button>
        <button type="button" class="pv-btn" onClick={save}>
          {t("wt.note")}
        </button>
      </div>
    </div>
  );
}

export function DeskWorktrees() {
  useEffect(() => link.subscribe("worktrees"), []);
  const s = readWorktrees(store.topic("worktrees").value?.data);
  const [pending, setPending] = useState<Pending>(null);
  const [laptop, setLaptop] = useState<Laptop>(null);
  const canAct = link.meta.value?.role !== "viewer";
  const q = filter.value;
  const flip = (k: string) => {
    const n = new Set(shut.peek());
    if (n.has(k)) n.delete(k);
    else n.add(k);
    shut.value = n;
  };
  const pin = (w: WorktreeItem) => link.request("worktrees.pin", { id: w.id, on: !w.pinned }).catch(failed);
  const sleep = (w: WorktreeItem, on: boolean) => {
    setPending(null);
    link.request("worktrees.sleep", { id: w.id, on }).then((r) => {
      const d = (r ?? {}) as WorktreeDone;
      if (d.asked) toast("info", t("wt.asked"));
      else if (d.note) toast("info", d.note);
    }, failed);
  };
  const rows = (list: readonly WorktreeItem[]) =>
    list.map((w) => {
      const asleep = w.state === "asleep";
      const archived = w.state === "archived";
      const can = canAct && w.kind !== "main" && !w.here && w.kind !== "agent" && !w.locked;
      return (
        <Fragment key={w.id}>
          <Row
            level={Math.min(w.depth ?? 0, 2)}
            icon={icon(w)}
            label={w.title}
            desc={desc(w)}
            title={[w.where, w.agent && `${w.agent} · ${w.state}`, w.note && `“${w.note}”`, w.link, w.checkpoint].filter(Boolean).join("\n")}
            data={{ id: w.id, state: w.state }}
            end={<span class="tr-age">{w.agent ? w.agent : w.link ?? ""}</span>}
            actions={
              canAct && (
                <>
                  {!archived && <Act icon={w.pinned ? "pinned" : "pin"} act="pin" label={t(w.pinned ? "wt.unpin" : "wt.pin")} onClick={() => pin(w)} />}
                  {!archived && !w.gone && <Act icon="comment" act="note" label={t("wt.note")} onClick={() => setPending({ id: w.id, op: "note" })} />}
                  {can && !archived && !w.gone && (asleep ? <Act icon="play" act="wake" label={t("wt.wake")} onClick={() => sleep(w, false)} /> : <Act icon="debug-pause" act="sleep" label={t("wt.sleep")} onClick={() => setPending({ id: w.id, op: "sleep" })} />)}
                  {can && (!w.gone || archived) && <Act icon={archived ? "history" : "archive"} act="archive" label={t(archived ? "wt.restore" : "wt.archive")} onClick={() => setLaptop({ w, op: "archive" })} />}
                  {can && (!w.gone || archived) && <Act icon="trash" act="delete" label={t("wt.delete")} onClick={() => setLaptop({ w, op: "delete" })} />}
                </>
              )
            }
          />
          {pending?.id === w.id && pending.op === "sleep" && <Ask text={t("wt.sleep_q", { name: w.title })} verb={t("wt.sleep")} onYes={() => sleep(w, true)} onNo={() => setPending(null)} />}
          {pending?.id === w.id && pending.op === "note" && <NoteEdit w={w} onDone={() => setPending(null)} />}
        </Fragment>
      );
    });
  const shown = (s?.worktrees ?? []).filter((w) => matchWorktree(w, q));
  return (
    <PaneView id="worktrees" title={t("wt.title")} actions={<Act icon="refresh" act="refresh" label={t("ctr.refresh")} onClick={() => link.request("worktrees.refresh", {}).catch(failed)} />}>
      <div class="pv-input wt-filter">
        <input value={q} placeholder={t("wt.filter")} aria-label={t("wt.filter")} onInput={(e) => (filter.value = (e.currentTarget as HTMLInputElement).value)} onKeyDown={(e) => e.key === "Escape" && (filter.value = "")} />
      </div>
      {!s || s.state === "loading" ? (
        <p class="pv-msg">{t("loading")}</p>
      ) : s.state === "none" ? (
        <p class="pv-msg">{t("wt.none")}</p>
      ) : (
        GROUPS.map(([g, label]) => {
          const list = shown.filter((w) => w.group === g);
          if (list.length === 0) return null;
          return (
            <PaneSection key={g} id={"wt-" + g} title={t(label)} open={!shut.value.has(g)} onToggle={() => flip(g)} count={list.length} fixed>
              <Tree label={t(label)}>{rows(list)}</Tree>
            </PaneSection>
          );
        })
      )}
      {s && s.state === "ready" && shown.length === 0 && <p class="pv-msg">{t("wt.no_match")}</p>}
      {laptop && (
        <AskLaptop
          label={t(laptop.op === "delete" ? "wt.delete" : laptop.w.state === "archived" ? "wt.restore" : "wt.archive")}
          text={t(laptop.op === "delete" ? "wt.delete_q" : laptop.w.state === "archived" ? "wt.restore_q" : "wt.archive_q", { name: laptop.w.title })}
          send={() => link.request("worktrees." + laptop.op, { id: laptop.w.id })}
          onDone={() => setLaptop(null)}
        />
      )}
    </PaneView>
  );
}
