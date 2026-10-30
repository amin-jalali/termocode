// The Git tab (WEB-23, web UX 3.5, 5.7): the laptop's changed files in
// groups (Conflicts, Staged, Changes, Untracked), a diff sheet per file,
// stage and unstage with Undo. Routes: #/git, #/git/<path>?side=staged,
// #/git?show=<hash>.
import { h, Fragment } from "preact";
import { useEffect } from "preact/hooks";
import { go, route } from "../../lib/router";
import { toast } from "../../lib/toasts";
import { link, live, store } from "../../session";
import { age, groupFiles, readGit, splitPath, type GitFile, type Side } from "../../state/git";
import { Progress, SkeletonRows } from "../../ui/Skeleton";
import { t, type StringKey } from "../../strings";
import { View } from "../View";
import { DiffSheet } from "./DiffSheet";
import { gitFailed } from "./errors";
import { GitRow } from "./GitRow";
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
  const open = (f: GitFile, side: Side) => go({ tab: "git", rest: f.path, query: side === "staged" ? { side: "staged" } : {} });

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
    body = <div class="empty">{t("git.not_repo")}</div>;
  } else {
    const groups = groupFiles(g.files);
    const sides: Side[] = ["conflicts", "staged", "changes", "untracked"];
    const clean = g.files.length === 0;
    body = (
      <Fragment>
        {g.op && (
          <div class="git-op" role="status">
            {t("git.op", { op: g.op })}
          </div>
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
      </Fragment>
    );
  }

  return (
    <View tab="git" title={t("tab.git")} sub={sub}>
      <div class={`git${st?.stale ? " is-stale" : ""}`}>{body}</div>
      {diffPath && (
        <DiffSheet
          file={sheetFile}
          path={diffPath}
          staged={sheetFile ? (staged ? sheetFile.staged : !(sheetFile.unstaged || sheetFile.untracked)) : staged}
          canAct={canAct}
          onSide={(s) => go({ tab: "git", rest: diffPath, query: s ? { side: "staged" } : {} }, true)}
          onClose={close}
          onStage={(stage) => stageFile(diffPath, stage)}
        />
      )}
      {showHash && <ShowSheet hash={showHash} onClose={close} />}
    </View>
  );
}
