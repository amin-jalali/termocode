// Interactive rebase (desktop editor tab "@git:rebase:<hash>"): the
// commit and every newer one of the branch, oldest first as git lists
// them, each with its action (pick, reword, edit, squash, fixup, drop);
// rows move by drag or Alt+↑/↓, letters set the action (p r e s f d). A
// reword row takes its new message here. Start asks the laptop: the
// laptop's card says what happens and the laptop's own rebase runs it.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { GitRebasePlan, GitRebaseStep } from "../../../../../webproto/proto";
import { rebaseTab } from "../../lib/gittab";
import { closeTab } from "../../lib/workbench";
import { link, live } from "../../session";
import { t, type StringKey } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Cod } from "../../ui/icons";
import { View } from "../View";
import { gitErrText } from "./errors";

type Action = GitRebaseStep["action"];
const ACTIONS: readonly Action[] = ["pick", "reword", "edit", "squash", "fixup", "drop"];
const KEYS: ReadonlyMap<string, Action> = new Map<string, Action>([["p", "pick"], ["r", "reword"], ["e", "edit"], ["s", "squash"], ["f", "fixup"], ["d", "drop"]]);
const HINTS: Record<Action, StringKey> = {
  pick: "gr.pick_hint",
  reword: "gr.reword_hint",
  edit: "gr.edit_hint",
  squash: "gr.squash_hint",
  fixup: "gr.fixup_hint",
  drop: "gr.drop_hint",
};

interface Row {
  hash: string;
  action: Action;
  subject: string;
  /** text is a reword row's new message (it starts as the old one). */
  text: string;
}

function readPlan(r: unknown): GitRebasePlan | null {
  const p = (r ?? {}) as Partial<GitRebasePlan>;
  return typeof p.base === "string" && Array.isArray(p.steps) ? (p as GitRebasePlan) : null;
}

/** kept counts the commits a list leaves (squash, fixup and drop meld or go). */
function kept(rows: readonly Row[]): number {
  return rows.filter((r) => r.action !== "squash" && r.action !== "fixup" && r.action !== "drop").length;
}

export function RebaseView({ hash }: { hash: string }) {
  const [plan, setPlan] = useState<GitRebasePlan | null>(null);
  const [rows, setRows] = useState<Row[]>([]);
  const [err, setErr] = useState("");
  const [asking, setAsking] = useState(false);
  const [drag, setDrag] = useState<number | null>(null);
  const list = useRef<HTMLOListElement | null>(null);
  const viewer = link.meta.value?.role === "viewer";
  const on = live.value;

  useEffect(() => {
    if (!on) return;
    let stop = false;
    link.request("git.rebase_plan", { from: hash }).then(
      (r) => {
        if (stop) return;
        const p = readPlan(r);
        setPlan(p);
        setRows((p?.steps ?? []).map((s) => ({ hash: s.hash, action: s.action, subject: s.subject ?? "", text: (s.subject ?? "") + (s.body ? "\n\n" + s.body : "") })));
        setErr("");
      },
      (e: unknown) => !stop && setErr(gitErrText(e)),
    );
    return () => {
      stop = true;
    };
  }, [hash, on]);

  const focusRow = (i: number) => {
    const all = list.current?.querySelectorAll<HTMLElement>("li.gr-row");
    all?.[i]?.focus();
  };
  const set = (i: number, action: Action) => setRows((old) => old.map((r, j) => (j === i ? { ...r, action } : r)));
  const move = (from: number, to: number) => {
    if (to < 0 || to >= rows.length || from === to) return;
    setRows((old) => {
      const next = old.slice();
      const [it] = next.splice(from, 1);
      next.splice(to, 0, it);
      return next;
    });
    requestAnimationFrame(() => focusRow(to));
  };
  const steps = (): GitRebaseStep[] =>
    rows.map((r) => (r.action === "reword" ? { hash: r.hash, action: r.action, message: r.text.trim() || r.subject } : { hash: r.hash, action: r.action }));
  const close = () => closeTab(rebaseTab(hash));
  const n = rows.length;
  const left = kept(rows);
  const top = rows[0]?.action;
  const blocked = top === "squash" || top === "fixup" || left === 0;

  return (
    <View
      tab="files"
      name="rebase"
      title={plan ? t("gr.title", { branch: plan.branch || "HEAD", base: plan.base ? plan.base.slice(0, 7) : t("gr.root") }) : t("gr.tab", { hash: hash.slice(0, 7) })}
      bodyClass="gr-body"
    >
      {err ? (
        <p class="empty refused">{err}</p>
      ) : !plan ? (
        <p class="empty">{t("loading")}</p>
      ) : (
        <div class="gr">
          <p class="gr-help">{t("gr.help_body")}</p>
          {plan.pushed && plan.pushed > 0 ? (
            <p class="gr-warn" role="note">
              <Cod name="warning" /> {t("gr.pushed_body", { n: plan.pushed, upstream: plan.upstream ?? "" })}
            </p>
          ) : null}
          {plan.dirty && (
            <p class="gr-note" role="note">
              <Cod name="info" /> {t("gr.dirty_body")}
            </p>
          )}
          <ol class="gr-list" ref={list} aria-label={t("gr.list")}>
            {rows.map((r, i) => {
              const meld = r.action === "squash" || r.action === "fixup";
              return (
                <li
                  key={r.hash}
                  class={`gr-row gr-${r.action}${meld ? " is-meld" : ""}${drag === i ? " is-drag" : ""}`}
                  tabIndex={0}
                  data-hash={r.hash}
                  draggable={!viewer}
                  aria-label={`${r.action} ${r.hash} ${r.subject}`}
                  onDragStart={(e) => {
                    setDrag(i);
                    if (e.dataTransfer) {
                      e.dataTransfer.effectAllowed = "move";
                      e.dataTransfer.setData("text/plain", r.hash);
                    }
                  }}
                  onDragEnd={() => setDrag(null)}
                  onDragOver={(e) => {
                    if (drag === null) return;
                    e.preventDefault();
                    if (drag !== i) {
                      move(drag, i);
                      setDrag(i);
                    }
                  }}
                  onDrop={(e) => {
                    e.preventDefault();
                    setDrag(null);
                  }}
                  onKeyDown={(e) => {
                    if (e.target !== e.currentTarget) return;
                    if (e.altKey && (e.key === "ArrowUp" || e.key === "ArrowDown")) {
                      e.preventDefault();
                      if (!viewer) move(i, e.key === "ArrowUp" ? i - 1 : i + 1);
                    } else if (e.key === "ArrowUp" || e.key === "ArrowDown") {
                      e.preventDefault();
                      focusRow(e.key === "ArrowUp" ? Math.max(0, i - 1) : Math.min(n - 1, i + 1));
                    } else if (!e.ctrlKey && !e.metaKey && !e.altKey && KEYS.has(e.key) && !viewer) {
                      e.preventDefault();
                      set(i, KEYS.get(e.key) ?? r.action);
                    }
                  }}
                >
                  <span class="gr-grip" aria-hidden="true">
                    <Cod name="gripper" />
                  </span>
                  <select
                    class="gr-action"
                    value={r.action}
                    disabled={viewer}
                    aria-label={t("gr.action_of", { hash: r.hash })}
                    title={t(HINTS[r.action])}
                    onChange={(e) => set(i, (e.currentTarget as HTMLSelectElement).value as Action)}
                  >
                    {ACTIONS.map((a) => (
                      <option key={a} value={a}>
                        {a}
                      </option>
                    ))}
                  </select>
                  <span class="gr-hash" dir="ltr">
                    {r.hash}
                  </span>
                  {r.action === "reword" ? (
                    <textarea
                      class="gr-msg"
                      dir="auto"
                      rows={Math.min(6, Math.max(1, r.text.split("\n").length))}
                      value={r.text}
                      aria-label={t("gr.message_of", { hash: r.hash })}
                      onInput={(e) => {
                        const v = (e.currentTarget as HTMLTextAreaElement).value;
                        setRows((old) => old.map((x, j) => (j === i ? { ...x, text: v } : x)));
                      }}
                    />
                  ) : (
                    <span class="gr-subject" dir="auto">
                      {r.subject}
                    </span>
                  )}
                </li>
              );
            })}
          </ol>
          <div class="gr-foot">
            <span class="gr-sum" role="status">
              {t("gr.summary", { n, left })}
            </span>
            {blocked && <span class="gr-bad">{t(left === 0 ? "gr.all_dropped" : "gr.meld_top")}</span>}
            <span class="gr-gap" />
            <button type="button" class="gv-btn gv-btn-sec" onClick={close}>
              {t("cancel")}
            </button>
            {!viewer && (
              <button type="button" class="gv-btn" data-act="start" disabled={blocked || !on} onClick={() => setAsking(true)}>
                {t("gr.start")}
              </button>
            )}
          </div>
        </div>
      )}
      {asking && plan && (
        <AskLaptop
          label={t("gr.start")}
          text={t("gr.ask_body", { n, left })}
          send={() => link.request("git.rebase", { base: plan.base, steps: steps() })}
          onDone={() => setAsking(false)}
          onConfirmed={close}
        />
      )}
    </View>
  );
}
