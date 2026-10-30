// The Git tab (WEB-23, web UX 3.5, 5.7): the laptop's changed files in
// groups (Conflicts, Staged, Changes, Untracked), a diff sheet per file,
// stage and unstage by button or swipe with Undo, the commit box, and
// discard through the laptop. Routes: #/git, #/git/<path>?side=staged,
// #/git?show=<hash>.
import { h, Fragment } from "preact";
import { useEffect, useState } from "preact/hooks";
import { diffTab } from "../../lib/difftab";
import { GRAPH_TAB, PRS_TAB, mergeTab } from "../../lib/gittab";
import { layout } from "../../lib/layout";
import { go, route } from "../../lib/router";
import { showFile } from "../../lib/workbench";
import { toast } from "../../lib/toasts";
import { link, live, store } from "../../session";
import { age, groupFiles, readGit, splitPath, type GitFile, type Side } from "../../state/git";
import { Progress, SkeletonRows } from "../../ui/Skeleton";
import { t, type StringKey } from "../../strings";
import { openMenu } from "../../ui/ContextMenu";
import { Cod } from "../../ui/icons";
import { View } from "../View";
import { CommitBox } from "./CommitBox";
import { DiffSheet } from "./DiffSheet";
import { DiscardFlow } from "./DiscardFlow";
import { gitFailed } from "./errors";
import { GitRow } from "./GitRow";
import { GitActionFlow, gitAction, gitMoreMenu, type GitAction } from "./GitMore";
import { RebaseBanner } from "./RebaseBanner";
import { RemoteSheet } from "./RemoteSheet";
import { ShowSheet } from "./ShowSheet";

const LOG_SHOWN = 10;
const LOG_CLEAN = 5;

const groupTitles: Record<Side, StringKey> = {
  conflicts: "git.conflicts",
  staged: "git.staged",
  changes: "git.changes",
  untracked: "git.untracked",
};

/** stageFile stages or unstages one file and offers Undo. */
function stageFile(path: string, stage: boolean, undo = true): void {
  link.request(stage ? "git.stage" : "git.unstage", { paths: [path] }).then(() => {
    if (!undo) return;
    const name = splitPath(path).name;
    toast("ok", t(stage ? "git.staged_toast" : "git.unstaged_toast", { name }), undefined, {
      label: t("undo"),
      run: () => stageFile(path, !stage, false),
    });
  }, gitFailed);
}

export function GitView() {
  useEffect(() => link.subscribe("git"), []);
  const st = store.topic("git").value;
  const g = readGit(st?.data);
  const r = route.value;
  const viewer = link.meta.value?.role === "viewer";
  const canAct = !viewer && live.value;
  const [discard, setDiscard] = useState<string[] | null>(null);
  const [remote, setRemote] = useState(false);
  const action = gitAction.value;
  const setAction = (a: GitAction | null) => (gitAction.value = a);

  const branch = g.branch.name;
  const sub = g.repo && (
    <span class="git-branch" dir="ltr">
      {branch}
      {g.branch.ahead > 0 && <span class="git-ahead"> ↑{g.branch.ahead}</span>}
      {g.branch.behind > 0 && <span class="git-behind"> ↓{g.branch.behind}</span>}
    </span>
  );

  // The diff sheet of #/git/<path>, the commit of #/git?show=<hash>.
  const diffPath = r.tab === "git" ? r.rest : "";
  const showHash = r.tab === "git" ? r.query.show ?? "" : "";
  const sheetFile = diffPath ? g.files.find((f) => f.path === diffPath) ?? null : null;
  const staged = r.query.side === "staged";
  const close = () => go({ tab: "git", rest: "", query: {} });
  const desk = layout.value === "desktop";
  // The desktop opens a change as a diff editor tab (a new or conflicted
  // file as the file itself); the phone opens the diff sheet.
  const open = (f: GitFile, side: Side) =>
    desk
      ? showFile(side === "conflicts" ? mergeTab(f.path) : side === "untracked" ? f.path : diffTab(side === "staged" ? "idx" : "wt", f.path), true)
      : go({ tab: "git", rest: f.path, query: side === "staged" ? { side: "staged" } : {} });

  // A link to a change (#/git/<path>) on the desktop: its diff editor tab.
  useEffect(() => {
    if (desk && diffPath) showFile(diffTab(staged ? "idx" : "wt", diffPath));
  }, [desk, diffPath, staged]);

  let body;
  if (!st) {
    body = (
      <div class="loading">
        <Progress k="git" />
        <SkeletonRows rows={6} />
      </div>
    );
  } else if (g.loading) {
    body = <div class="empty">{t("git.reading")}</div>;
  } else if (!g.repo) {
    body = (
      <div class="empty">
        {t("git.not_repo")}
        {desk && canAct && (
          <button type="button" class="gv-btn gx-clone" data-act="clone" onClick={() => setAction("clone")}>
            {t("gx.clone")}…
          </button>
        )}
      </div>
    );
  } else {
    const groups = groupFiles(g.files);
    const sides: Side[] = ["conflicts", "staged", "changes", "untracked"];
    const clean = g.files.length === 0;
    body = (
      <Fragment>
        {g.op === "rebase" ? (
          <RebaseBanner seq={st.seq} />
        ) : (
          g.op && (
            <div class="git-op" role="status">
              {t("git.op", { op: g.op })}
            </div>
          )
        )}
        {clean && <p class="git-clean">{t("git.clean")}</p>}
        {sides.map((side) =>
          groups[side].length === 0 ? null : (
            <section class={`git-group git-${side}`} key={side} aria-label={t(groupTitles[side])}>
              <h2 class="group-title">
                {t(groupTitles[side])} <span class="git-n">({groups[side].length})</span>
              </h2>
              {groups[side].map((f) => (
                <GitRow
                  key={side + f.path}
                  file={f}
                  side={side}
                  canAct={canAct}
                  onOpen={() => open(f, side)}
                  onAct={() => stageFile(f.path, side !== "staged")}
                />
              ))}
            </section>
          ),
        )}
        {g.more > 0 && <p class="git-many">{t("git.many", { n: g.more })}</p>}
        {g.log.length > 0 && (
          <section class="git-group git-log" aria-label={t("git.log")}>
            <h2 class="group-title">{t("git.log")}</h2>
            {g.log.slice(0, clean ? LOG_CLEAN : LOG_SHOWN).map((c) => (
              <button
                type="button"
                class="git-commit"
                key={c.hash}
                onClick={() => go({ tab: "git", rest: "", query: { show: c.hash } })}
              >
                <span class={`git-hash${c.unpushed ? " is-unpushed" : ""}`} dir="ltr">
                  {c.hash}
                </span>
                <span class="git-subject trunc" dir="auto">
                  {c.subject}
                </span>
                <span class="git-age" dir="ltr">
                  {age(c.when, Date.now())}
                </span>
              </button>
            ))}
          </section>
        )}
        {!viewer && !g.op && groups.staged.length + groups.changes.length + groups.untracked.length > 0 && (
          <CommitBox staged={groups.staged.length} branch={branch} />
        )}
      </Fragment>
    );
  }

  return (
    <View
      tab="git"
      title={t("tab.git")}
      sub={sub}
      actions={
        <>
          {desk && g.repo && (
            <button type="button" class="tb tb-icon" data-act="graph" aria-label={t("gg.open")} title={t("gg.open")} onClick={() => showFile(GRAPH_TAB)}>
              <Cod name="git-commit" />
            </button>
          )}
          {desk && g.repo && (
            <button type="button" class="tb tb-icon" data-act="prs" aria-label={t("pr.tab")} title={t("pr.tab")} onClick={() => showFile(PRS_TAB)}>
              <Cod name="git-pull-request" />
            </button>
          )}
          {desk && st && !g.loading && (
            <button
              type="button"
              class="tb tb-icon"
              data-act="more"
              aria-haspopup="menu"
              aria-label={t("gx.more")}
              title={t("gx.more")}
              onClick={(e) => {
                const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
                openMenu(r.left, r.bottom, t("gx.more"), gitMoreMenu(g.repo, canAct, g.op, setAction));
              }}
            >
              <Cod name="ellipsis" />
            </button>
          )}
          {!viewer && g.repo && !g.op && (
            <button type="button" class="head-remote" aria-label={t("git.remote")} onClick={() => setRemote(true)}>
              ⇅
            </button>
          )}
        </>
      }
    >
      <div class={`git${st?.stale ? " is-stale" : ""}`}>{body}</div>
      {diffPath && !desk && (
        <DiffSheet
          file={sheetFile}
          path={diffPath}
          staged={sheetFile ? (staged ? sheetFile.staged : !(sheetFile.unstaged || sheetFile.untracked)) : staged}
          canAct={canAct}
          onSide={(s) => go({ tab: "git", rest: diffPath, query: s ? { side: "staged" } : {} }, true)}
          onClose={close}
          onStage={(stage) => stageFile(diffPath, stage)}
          onDiscard={() => setDiscard([diffPath])}
        />
      )}
      {showHash && <ShowSheet hash={showHash} onClose={close} />}
      {action && <GitActionFlow kind={action} onDone={() => setAction(null)} />}
      {discard && <DiscardFlow paths={discard} onDone={() => setDiscard(null)} />}
      {remote && (
        <RemoteSheet branch={branch} ahead={g.branch.ahead} behind={g.branch.behind} changed={g.files.length} onDone={() => setRemote(false)} />
      )}
    </View>
  );
}
