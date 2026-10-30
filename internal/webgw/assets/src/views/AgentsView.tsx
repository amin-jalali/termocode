// Agent worktrees (desktop side bar, WEBP-3): the laptop's "AI: Agent
// Worktrees…" list. Steps of an agent run that work at the same time
// each get their own git worktree; each one shows its branch, the step
// that owns it and the files it changed. A file opens as a diff tab
// (before and after the agent, read only). Merge and Discard run on the
// laptop after its card (Cancel first); a merge conflict asks there.
// #/more/agents
import { Fragment, h } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { AIWorktree, AIWorktrees } from "../../../../webproto/proto";
import { hubSays } from "../lib/ai";
import { agentTab } from "../lib/difftab";
import { showFile } from "../lib/workbench";
import { link, live } from "../session";
import { splitPath } from "../state/git";
import { t } from "../strings";
import { AskLaptop } from "../ui/AskLaptop";
import { Cod, FileIcon } from "../ui/icons";
import { GitBadge } from "./git/GitBadge";
import { View } from "./View";

type Ask = { kind: "merge" | "discard"; wt: AIWorktree };

function Worktree({ wt, canAct, onAsk }: { wt: AIWorktree; canAct: boolean; onAsk: (a: Ask) => void }) {
  const [open, setOpen] = useState(true);
  // As a tree row: the step's name (or the worktree's id), then the
  // branch and the state dim.
  const label = wt.owner || wt.id;
  const desc = wt.running ? t("tasks.state.running") : wt.owner ? wt.branch : `${wt.branch} · ${t("ai.wt_left")}`;
  return (
    <li class="agent-wt" data-id={wt.id}>
      <div class="agent-head">
        <button type="button" class="agent-open" aria-expanded={open ? "true" : "false"} onClick={() => setOpen(!open)}>
          <Cod name={open ? "chevron-down" : "chevron-right"} />
          <Cod name="worktree" />
          <bdi class="agent-name" dir="auto">
            {label}
          </bdi>
          <bdi class="agent-desc" dir="ltr" title={wt.branch}>
            {desc}
          </bdi>
        </button>
        {canAct && !wt.running && (
          <span class="agent-acts">
            <button type="button" class="tb tb-icon" data-act="merge" aria-label={t("ai.wt_merge")} title={t("ai.wt_merge")} onClick={() => onAsk({ kind: "merge", wt })}>
              <Cod name="git-merge" />
            </button>
            <button type="button" class="tb tb-icon" data-act="discard" aria-label={t("ai.wt_discard")} title={t("ai.wt_discard")} onClick={() => onAsk({ kind: "discard", wt })}>
              <Cod name="discard" />
            </button>
          </span>
        )}
        {!wt.running && <span class="agent-n">{wt.files.length}</span>}
      </div>
      {open && !wt.running && (
        <ul class="agent-files">
          {wt.files.length === 0 && <li class="agent-none">{t("ai.wt_no_files")}</li>}
          {wt.files.map((f) => {
            const { name, dir } = splitPath(f.path);
            return (
              <li key={f.path}>
                <button type="button" class="agent-file" data-path={f.path} title={f.path} onClick={() => showFile(agentTab(wt.id, f.path))}>
                  <FileIcon path={f.path} />
                  <bdi class="git-file" dir="ltr">
                    {name}
                  </bdi>
                  {dir && (
                    <bdi class="git-dir" dir="ltr">
                      {dir}
                    </bdi>
                  )}
                  <GitBadge code={f.status} />
                </button>
              </li>
            );
          })}
          {wt.cut && <li class="agent-none">{t("ai.wt_cut")}</li>}
        </ul>
      )}
    </li>
  );
}

export function AgentsView() {
  const [list, setList] = useState<AIWorktrees | null>(null);
  const [err, setErr] = useState("");
  const [ask, setAsk] = useState<Ask | null>(null);
  const [again, setAgain] = useState(0);
  const on = live.value;
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
  }, [on, again]);
  const canAct = link.meta.value?.role === "controller";
  const items = list?.items ?? [];
  return (
    <View
      tab="more"
      name="agents"
      title={t("ai.wt_title")}
      actions={
        <button type="button" class="tb tb-icon" data-act="refresh" aria-label={t("ctr.refresh")} title={t("ctr.refresh")} onClick={() => setAgain(again + 1)}>
          <Cod name="refresh" />
        </button>
      }
    >
      {err ? (
        <p class="empty refused">{err}</p>
      ) : !list ? (
        <p class="empty">{t("quick.loading")}</p>
      ) : list.noRepo ? (
        <p class="empty">{t("ai.wt_norepo")}</p>
      ) : (
        <>
          {list.off && <p class="view-note">{t("ai.wt_body_off")}</p>}
          {items.length === 0 ? (
            <p class="empty">{t("ai.wt_none")}</p>
          ) : (
            <ul class="agent-list">
              {items.map((wt) => (
                <Worktree key={wt.id} wt={wt} canAct={canAct} onAsk={setAsk} />
              ))}
            </ul>
          )}
        </>
      )}
      {ask && (
        <AskLaptop
          label={ask.kind === "merge" ? t("ai.wt_merge") : t("ai.wt_discard")}
          text={t(ask.kind === "merge" ? "ai.wt_body_merge" : "ai.wt_body_discard", { branch: ask.wt.branch })}
          send={() => link.request(ask.kind === "merge" ? "ai.worktree_merge" : "ai.worktree_discard", { id: ask.wt.id })}
          onDone={() => setAsk(null)}
          onConfirmed={() => {
            // The laptop runs it now: look again once it is done.
            setAgain((n) => n + 1);
            setTimeout(() => setAgain((n) => n + 1), 1500);
          }}
        />
      )}
    </View>
  );
}
