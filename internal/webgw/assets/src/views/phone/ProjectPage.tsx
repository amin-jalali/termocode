// The phone's project page (MOBUX-7, mobile plan 2.4 / 3.6): one project
// of the projects topic with four tabs.
//   Tasks  its checklists (TASKS.md, TODO.md), issues, pull requests and
//          merge requests (project.tasks, MOB-52), one section per source;
//          a task opens in place with "Talk about it", which puts a
//          reference (where it is, never a file's contents) in the chat box
//   Runs   the AI runs (runs topic), with their own Stop / Pause / Resume
//   Git    branch, changed files and the last commits (git topic)
//   Board  the runs in four columns: needs you · working · review · done
// Runs, Git and Board are live for the project the laptop window has open
// ("here"); another project shows its tasks through the door (the frame's
// ws, MOB-50) and says where to see the rest. Phone layout only; status
// is always a shape and a colour; every target is 44 px.
// #/more/projects/<id>?tab=tasks|runs|git|board
import { Fragment, h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { pendingDraft } from "../../lib/drafts";
import { go, goTab, route } from "../../lib/router";
import { useNow } from "../../lib/time";
import { ReqError } from "../../net/link";
import { link, live, store } from "../../session";
import { age, readGit } from "../../state/git";
import {
  BOARD_COLS,
  boardCounts,
  checksMark,
  PROJECT_TABS,
  projectTab,
  readProject,
  readProjectTasks,
  taskMark,
  taskRef,
  validProjectID,
  type BoardCol,
  type ProjectInfo,
  type ProjectSectionView,
  type ProjectTab,
  type ProjectTaskView,
} from "../../state/project";
import { readRuns } from "../../state/tasks";
import { t, type StringKey } from "../../strings";
import { Button } from "../../ui/Button";
import { SkeletonRows } from "../../ui/Skeleton";
import { RunCard } from "../more/RunsView";
import { View } from "../View";

const TAB_LABEL: Record<ProjectTab, StringKey> = { tasks: "more.tasks", runs: "more.runs", git: "tab.git", board: "pp.board" };
const COL_LABEL: Record<BoardCol, StringKey> = { needs: "wfv.needs", working: "pp.col.working", review: "pill.review", done: "done" };
const COL_MARK: Record<BoardCol, string> = { needs: "●", working: "◐", review: "◆", done: "✓" };

function backToProjects(): void {
  go({ tab: "more", rest: "projects", query: {} });
}

/** Tabs is the tab row: a real tab list (arrows move along it), each tab
 * a 44 px target. */
function Tabs({ id, tab }: { id: string; tab: ProjectTab }) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const pick = (to: ProjectTab, focus = false) => {
    go({ tab: "more", rest: "projects/" + id, query: to === "tasks" ? {} : { tab: to } }, true);
    if (focus) refs.current[PROJECT_TABS.indexOf(to)]?.focus();
  };
  const onKey = (e: KeyboardEvent) => {
    const i = PROJECT_TABS.indexOf(tab);
    const n = PROJECT_TABS.length;
    const to = e.key === "ArrowRight" ? (i + 1) % n : e.key === "ArrowLeft" ? (i + n - 1) % n : e.key === "Home" ? 0 : e.key === "End" ? n - 1 : -1;
    if (to < 0) return;
    e.preventDefault();
    pick(PROJECT_TABS[to], true);
  };
  return (
    <div class="ptabs" role="tablist" aria-label={t("pp.tabs")} onKeyDown={onKey}>
      {PROJECT_TABS.map((x, i) => (
        <button
          key={x}
          ref={(el) => (refs.current[i] = el)}
          type="button"
          role="tab"
          id={`ptab-${x}`}
          aria-selected={x === tab ? "true" : "false"}
          aria-controls="ppanel"
          tabIndex={x === tab ? 0 : -1}
          class={`ptab${x === tab ? " is-on" : ""}`}
          onClick={() => pick(x)}
        >
          {t(TAB_LABEL[x])}
        </button>
      ))}
    </div>
  );
}

function taskMeta(task: ProjectTaskView): string {
  const bits: string[] = [];
  if (task.kind === "ptask") {
    if (task.group) bits.push(task.group);
    bits.push(`${task.path}:${task.line}`);
  } else {
    bits.push((task.kind === "mr" ? "!" : "#") + task.number);
    if (task.branch) bits.push(task.branch);
    if (task.author) bits.push(t("pp.by", { name: task.author }));
    if (task.labels.length) bits.push(task.labels.join(", "));
    const c = checksMark(task.checks);
    if (c) bits.push(t("pp.checks", { mark: c }));
  }
  return bits.join(" · ");
}

function TaskRow({ project, task, open, onToggle }: { project: string; task: ProjectTaskView; open: boolean; onToggle: () => void }) {
  const talk = () => {
    pendingDraft.value = { text: taskRef(project, task), notChat: "" };
    goTab("chat");
  };
  return (
    <li class={`ptask pdepth-${task.depth} st-${task.state}`} data-id={task.id}>
      <button type="button" class="ptask-open" aria-expanded={open ? "true" : "false"} onClick={onToggle}>
        <span class={`pmark st-${task.state}`} aria-hidden="true">
          {taskMark(task)}
        </span>
        <span class="ptask-name">
          <bdi class="ptask-title">{task.title}</bdi>
          <span class="ptask-meta" dir="ltr">
            {taskMeta(task)}
          </span>
        </span>
      </button>
      {open && (
        <div class="choices ptask-acts">
          <Button small kind="primary" onClick={talk}>
            {t("pp.talk")}
          </Button>
        </div>
      )}
    </li>
  );
}

function Section({ project, sec, open, setOpen }: { project: string; sec: ProjectSectionView; open: string; setOpen: (id: string) => void }) {
  return (
    <section class="psec" data-kind={sec.kind} aria-labelledby={`psec-${sec.kind}`}>
      <h2 class="psec-title" id={`psec-${sec.kind}`}>
        {sec.open > 0 ? t("pp.open_n", { title: sec.title, n: sec.open }) : sec.title}
      </h2>
      {sec.note && <p class="sheet-note">{sec.note}</p>}
      {sec.items.length > 0 && (
        <ul class="ptask-list">
          {sec.items.map((x) => (
            <TaskRow key={x.id} project={project} task={x} open={open === x.id} onToggle={() => setOpen(open === x.id ? "" : x.id)} />
          ))}
        </ul>
      )}
    </section>
  );
}

type TasksState = { kind: "loading" } | { kind: "ok"; sections: ProjectSectionView[] } | { kind: "err"; why: string };

function TasksPanel({ p }: { p: ProjectInfo }) {
  const [st, setSt] = useState<TasksState>({ kind: "loading" });
  const [tries, setTries] = useState(0);
  const [open, setOpen] = useState("");
  const ws = p.here ? "" : p.id;
  useEffect(() => {
    if (!live.value) return;
    let stop = false;
    setSt({ kind: "loading" });
    link.requestIn(ws, "project.tasks", {}).then(
      (r) => !stop && setSt({ kind: "ok", sections: readProjectTasks(r) }),
      (e: unknown) => !stop && setSt({ kind: "err", why: e instanceof ReqError ? e.message : "" }),
    );
    return () => {
      stop = true;
    };
  }, [ws, tries, live.value]);
  if (st.kind === "loading") return <SkeletonRows rows={6} mono={false} />;
  if (st.kind === "err") {
    return (
      <div class="err-line" role="status">
        <p dir="auto">{t("pp.tasks_err", { reason: st.why })}</p>
        <Button small needsLink onClick={() => setTries(tries + 1)}>
          {t("retry")}
        </Button>
      </div>
    );
  }
  if (st.sections.length === 0) return <p class="empty">{t("pp.no_tasks_body")}</p>;
  return (
    <div class="ptasks">
      {st.sections.map((s) => (
        <Section key={s.kind} project={p.name} sec={s} open={open} setOpen={setOpen} />
      ))}
    </div>
  );
}

/** Other stands in for Runs, Git and Board of a project the laptop
 * window does not have open: its topics come from its own window. */
function Other({ p }: { p: ProjectInfo }) {
  return (
    <div class="pother">
      <p class="empty">{t("pp.other_body")}</p>
      {p.where && (
        <p class="sheet-note" dir="ltr">
          {p.where}
        </p>
      )}
    </div>
  );
}

function RunsPanel() {
  useEffect(() => link.subscribe("runs"), []);
  const st = store.topic("runs").value;
  const runs = readRuns(st?.data);
  const now = useNow(runs.some((r) => r.live), 1000);
  const [since, setSince] = useState(() => Date.now());
  useEffect(() => setSince(Date.now()), [st?.seq]);
  const canAct = link.meta.value?.role !== "viewer";
  if (!st) return <SkeletonRows rows={4} mono={false} />;
  if (runs.length === 0) return <p class="empty">{t("pp.no_runs")}</p>;
  return (
    <div class="runs pruns">
      {runs.map((r) => (
        <RunCard key={r.key} run={r} since={since} now={now} canAct={canAct} />
      ))}
    </div>
  );
}

function GitPanel() {
  useEffect(() => link.subscribe("git"), []);
  const st = store.topic("git").value;
  const g = readGit(st?.data);
  if (!st || g.loading) return <SkeletonRows rows={4} />;
  if (!g.repo) return <p class="empty">{t("pp.git_none")}</p>;
  const now = Date.now();
  return (
    <div class="pgit">
      <p class="pgit-branch" dir="ltr">
        <span aria-hidden="true">⎇ </span>
        {g.branch.name}
        {g.branch.ahead > 0 && ` ↑${g.branch.ahead}`}
        {g.branch.behind > 0 && ` ↓${g.branch.behind}`}
      </p>
      <p class="sheet-note">{t("pp.changes", { n: g.files.length + g.more })}</p>
      {g.log.length > 0 && (
        <ul class="pgit-log">
          {g.log.slice(0, 5).map((c) => (
            <li key={c.hash} class="pgit-commit">
              <bdi class="pgit-subject">{c.subject}</bdi>
              <span class="ptask-meta" dir="ltr">
                {c.hash} · {age(c.when, now)}
                {c.unpushed && " · ↑"}
              </span>
            </li>
          ))}
        </ul>
      )}
      <Button small onClick={() => goTab("git")}>
        {t("pp.git_open")}
      </Button>
    </div>
  );
}

function BoardPanel() {
  useEffect(() => link.subscribe("runs"), []);
  const st = store.topic("runs").value;
  if (!st) return <SkeletonRows rows={4} mono={false} />;
  const cols = boardCounts(readRuns(st.data));
  return (
    <div class="pboard">
      {BOARD_COLS.map((c) => (
        <section key={c} class={`pboard-col col-${c}`} aria-labelledby={`pcol-${c}`}>
          <h2 class="pboard-head" id={`pcol-${c}`}>
            <span class={`pmark col-${c}`} aria-hidden="true">
              {COL_MARK[c]}
            </span>
            <span class="pboard-label">{t(COL_LABEL[c])}</span>
            <span class="pboard-n">{cols[c].length}</span>
          </h2>
          {cols[c].length === 0 ? (
            <p class="sheet-note">{t("pp.col_none")}</p>
          ) : (
            <ul class="pboard-list">
              {cols[c].slice(0, 3).map((r) => (
                <li key={r.key}>
                  <button type="button" class="pboard-tile" onClick={() => go({ tab: "more", rest: "runs", query: {} })}>
                    <bdi>{r.title}</bdi>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </section>
      ))}
    </div>
  );
}

function Panel({ p, tab }: { p: ProjectInfo; tab: ProjectTab }) {
  if (tab === "tasks") return <TasksPanel key={p.id} p={p} />;
  if (!p.here) return <Other p={p} />;
  if (tab === "runs") return <RunsPanel />;
  if (tab === "git") return <GitPanel />;
  return <BoardPanel />;
}

function stateLine(p: ProjectInfo): string {
  if (p.here) return t("pp.here");
  if (p.state === "asleep") return t("pp.asleep");
  return t("mcpv.running");
}

export function ProjectPage({ id }: { id: string }) {
  useEffect(() => link.subscribe("projects"), []);
  const st = store.topic("projects").value;
  const p = validProjectID(id) && st ? readProject(st.data, id) : null;
  const tab = projectTab(route.value.query.tab);
  return (
    <View tab="more" name="project" title={p ? p.name : t("loading")} back={{ label: t("back"), onClick: backToProjects }} sub={p && <span class="psub">{stateLine(p)}</span>}>
      <div class="ppage">
        {!st && validProjectID(id) ? (
          <SkeletonRows rows={6} mono={false} />
        ) : !p ? (
          <p class="empty">{t("pp.gone")}</p>
        ) : (
          <>
            <Tabs id={id} tab={tab} />
            <div class="ppanel" id="ppanel" role="tabpanel" aria-labelledby={`ptab-${tab}`}>
              <Panel p={p} tab={tab} />
            </div>
          </>
        )}
      </div>
    </View>
  );
}
