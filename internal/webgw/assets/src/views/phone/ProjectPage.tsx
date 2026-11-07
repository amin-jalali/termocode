// The phone's project page (MOBUX-7, mobile plan 2.4 / 3.6): one project
// of the projects topic with four tabs.
//   Tasks  its checklists, issues, pull requests and merge requests (the
//          project.tasks view, MOB-52); a tapped task opens a sheet, Cancel
//          first, with "Talk about it", which puts a reference (where it
//          is, never a file's contents) in the chat box
//   Runs   the AI runs (runs topic), with their own Stop / Pause / Resume
//   Git    branch, changed files and the last commits (git topic)
//   Board  the runs in four columns: needs you · working · review · done
// The project this page talks to uses the link; another running one goes
// through the door (its ws on every frame, MOB-50); an asleep one shows
// its tasks request's answer and says the rest shows while it runs.
// Phone layout only; status is always a shape and a colour; every
// target is 44 px. #/more/projects/<id>?tab=tasks|runs|git|board, and
// #/more/projects is the list (MOBUX-6).
import { Fragment, h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { ProjectTask } from "../../../../../webproto/proto";
import { pendingDraft } from "../../lib/drafts";
import { go, goTab, route } from "../../lib/router";
import { useNow } from "../../lib/time";
import { ProjectsList } from "../../phone/ProjectsList";
import { ProjectTasksView } from "../../phone/ProjectTasks";
import { PhoneSheet, safeFirst } from "../../phone/Sheet";
import { link, store } from "../../session";
import { age, readGit } from "../../state/git";
import {
  BOARD_COLS,
  boardCounts,
  PROJECT_TABS,
  projectTab,
  reachable,
  readProject,
  taskRef,
  validProjectID,
  type BoardCol,
  type ProjectInfo,
  type ProjectTab,
} from "../../state/project";
import { readRuns } from "../../state/tasks";
import { t, type StringKey } from "../../strings";
import { Button } from "../../ui/Button";
import { SkeletonRows } from "../../ui/Skeleton";
import { RunCard } from "../more/RunsView";
import { SubView } from "../more/SubView";
import { View } from "../View";
import { door } from "./door";

const TAB_LABEL: Record<ProjectTab, StringKey> = { tasks: "more.tasks", runs: "more.runs", git: "tab.git", board: "chips.board" };
const COL_LABEL: Record<BoardCol, StringKey> = { needs: "wfv.needs", working: "board.col.working", review: "pill.review", done: "done" };
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

/** TasksPanel is the project.tasks view; a tapped task opens a sheet,
 * Cancel first: "Talk about it" puts a reference in the chat box. */
function TasksPanel({ p }: { p: ProjectInfo }) {
  const [picked, setPicked] = useState<ProjectTask | null>(null);
  const talk = (task: ProjectTask) => {
    setPicked(null);
    pendingDraft.value = { text: taskRef(p.name, task), notChat: "" };
    goTab("chat");
  };
  return (
    <>
      <ProjectTasksView ws={p.ws} onPick={setPicked} />
      {picked && (
        <PhoneSheet
          title={picked.title}
          onLater={() => setPicked(null)}
          actions={safeFirst([
            { label: t("cancel"), safe: true, onClick: () => setPicked(null) },
            { label: t("pp.talk"), primary: true, onClick: () => talk(picked) },
          ])}
        />
      )}
    </>
  );
}

/** useTopic subscribes to a topic of the project (through the door for
 * another one; that subscription ends with the panel) and gives it. */
function useTopic(ws: string, topic: string) {
  useEffect(() => {
    door.subscribe(ws, topic);
    return ws ? () => door.unsubscribe(ws, topic) : undefined;
  }, [ws, topic]);
  return door.store(ws).topic(topic).value;
}

function RunsPanel({ ws }: { ws: string }) {
  const st = useTopic(ws, "runs");
  const runs = readRuns(st?.data);
  const now = useNow(runs.some((r) => r.live), 1000);
  const [since, setSince] = useState(() => Date.now());
  useEffect(() => setSince(Date.now()), [st?.seq]);
  const role = ws ? door.meta.value.get(ws)?.role : link.meta.value?.role;
  const canAct = role !== undefined && role !== "viewer";
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

function GitPanel({ ws, here }: { ws: string; here: boolean }) {
  const st = useTopic(ws, "git");
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
              <span class="pgit-meta" dir="ltr">
                {c.hash} · {age(c.when, now)}
                {c.unpushed && " · ↑"}
              </span>
            </li>
          ))}
        </ul>
      )}
      {here && (
        <Button small onClick={() => goTab("git")}>
          {t("pp.git_open")}
        </Button>
      )}
    </div>
  );
}

function BoardPanel({ ws, here }: { ws: string; here: boolean }) {
  const st = useTopic(ws, "runs");
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
                  {here ? (
                    <button type="button" class="pboard-tile" onClick={() => go({ tab: "more", rest: "runs", query: {} })}>
                      <bdi>{r.title}</bdi>
                    </button>
                  ) : (
                    <bdi class="pboard-tile">{r.title}</bdi>
                  )}
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
  if (!reachable(p)) return <p class="empty pother">{t("pp.asleep_body")}</p>;
  if (tab === "runs") return <RunsPanel key={p.id} ws={p.ws} />;
  if (tab === "git") return <GitPanel key={p.id} ws={p.ws} here={p.here} />;
  return <BoardPanel key={p.id} ws={p.ws} here={p.here} />;
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

/** ProjectsPage is the projects list (MOBUX-6) under More; a card opens
 * its project page. */
export function ProjectsPage() {
  useEffect(() => link.subscribe("projects"), []);
  return (
    <SubView title={t("ph.switch_title")} name="projects">
      <ProjectsList onOpen={(p) => go({ tab: "more", rest: "projects/" + p.id, query: {} })} />
    </SubView>
  );
}
