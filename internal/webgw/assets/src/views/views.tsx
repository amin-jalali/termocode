// The five views of the shell. Their data views arrive with their own
// tasks (chat WEB-14, tree WEB-16, files WEB-18, terminal WEB-20, git
// WEB-23); until then each shows its loading and empty states.
import { Fragment, h, type ComponentChildren } from "preact";
import { useEffect, useState } from "preact/hooks";
import { prefs, setPref, type Direction, type Language, type Motion, type Theme } from "../lib/prefs";
import { go, route } from "../lib/router";
import { layout } from "../lib/layout";
import { t, type StringKey } from "../strings";
import { NotifyField } from "../ui/NotifyField";
import { BookmarkField } from "../ui/BookmarkField";
import { Segmented } from "../ui/Segmented";
import { TopicBody } from "./Placeholder";
import { SkeletonRows } from "../ui/Skeleton";
import { Button } from "../ui/Button";
import { RunsView } from "./more/RunsView";
import { WorkflowsView } from "./more/WorkflowsView";
import { RacesView } from "./more/RacesView";
import { BoardRoute } from "./more/BoardRoute";
import { SettingsView } from "./more/SettingsView";
import { AgentsView } from "./AgentsView";
import { ItermocView } from "./ItermocView";
import { MCPView } from "./more/MCPView";
import { SearchView } from "./more/SearchView";
import { TasksView } from "./more/TasksView";
import { NewTaskView } from "./more/NewTaskView";
import { TestingView } from "./run/TestingView";
import { DebugView } from "./run/DebugView";
import { ServicesView } from "./run/ServicesView";
import { KubeView } from "./run/KubeView";
import { View } from "./View";
import { SubView } from "./more/SubView";
import { FileView } from "./FileView";
import { home as phone, homeFailed as phoneFailed, loadHome } from "../lib/lazy";
import { TreeView } from "./Files";
import { PhoneHost } from "./PhoneHost";
import { loadRadial, radial, radialHost } from "../lib/radial";

export { ChatView } from "./Chat";

export function FilesView() {
  const r = route.value;
  return r.tab === "files" && r.rest ? <FileView path={r.rest} /> : <TreeView />;
}

/** TermView is the phone's and the tablet's terminal (PHONE-TERM), from
 * home.js, loaded on first open. */
export function TermView() {
  return <PhoneLazy tkey="tab.term" name="term" draw={(m) => <m.PhoneTermView />} />;
}

export { GitView } from "./git/GitView";

/** ToolsView is the "All tools" menu (MOBUX-5), from the phone chunk
 * (home.js since the merge), loaded on first open. */
function ToolsView() {
  const m = phone.value;
  useEffect(() => {
    if (!phone.peek()) loadHome().catch(() => undefined);
  }, []);
  if (m) return <m.AllToolsView />;
  return (
    <View tab="more" name="tools" title={t("tools.title")}>
      {phoneFailed.value ? (
        <Button kind="neutral" onClick={() => loadHome().catch(() => undefined)}>
          {t("retry")}
        </Button>
      ) : (
        <p class="empty">{t("loading")}</p>
      )}
    </View>
  );
}

/** PhoneProject is the phone's projects list or one project's page
 * (MOBUX-6, MOBUX-7), from home.js (the one phone chunk): a skeleton while it loads, Try
 * again when the load failed. */
function PhoneProject({ id }: { id: string }) {
  const m = phone.value;
  useEffect(() => {
    if (!phone.peek()) loadHome().catch(() => undefined);
  }, []);
  if (m) return id ? <m.ProjectPage id={id} /> : <m.ProjectsPage />;
  return (
    <View tab="more" title={t("loading")} name="project-loading">
      {phoneFailed.value ? (
        <Button small onClick={() => loadHome().catch(() => undefined)}>
          {t("retry")}
        </Button>
      ) : (
        <SkeletonRows rows={6} mono={false} />
      )}
    </View>
  );
}

/** PhoneLazy is a phone view from home.js, loaded on first use: the
 * review (#/more/review, MOB-14), Containers and Databases (with their
 * hosts tree and a database read's questions). */
function PhoneLazy({ tkey, name, draw }: { tkey: StringKey; name: string; draw: (m: NonNullable<typeof phone.value>) => ComponentChildren }) {
  const m = phone.value;
  useEffect(() => {
    if (!m) loadHome().catch(() => undefined);
  }, [m]);
  if (m) return <>{draw(m)}</>;
  return (
    <SubView title={t(tkey)} name={name}>
      {phoneFailed.value ? (
        <button type="button" class="btn btn-neutral" onClick={() => loadHome().catch(() => undefined)}>
          {t("retry")}
        </button>
      ) : (
        <p class="empty">{t("loading")}</p>
      )}
    </SubView>
  );
}

/** MoreLinks is the More list (RM-3) from radial.js: search, Recent, the
 * grouped views with their icons; a skeleton while it loads. */
function MoreLinks() {
  const m = radial.value;
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    if (!radial.peek()) loadRadial().catch(() => setFailed(true));
  }, []);
  if (m) return <m.MoreList host={radialHost} />;
  if (!failed) return <SkeletonRows rows={8} mono={false} />;
  return (
    <Button kind="neutral" onClick={() => (setFailed(false), loadRadial().catch(() => setFailed(true)))}>
      {t("retry")}
    </Button>
  );
}

/** OnOff is a switch drawn as a two-choice row (haptics). */
type OnOff = "on" | "off";

export function MoreView() {
  const sub = route.value.tab === "more" ? route.value.rest : "";
  if (sub === "tools") return <ToolsView />;
  if (sub === "projects" || sub.startsWith("projects/")) return <PhoneProject id={sub.slice("projects/".length)} />;
  if (sub === "search") return <SearchView />;
  if (sub === "tasks") return <TasksView />;
  if (sub === "new-task") return <NewTaskView />;
  if (sub === "runs") return <RunsView />;
  if (sub === "workflows") return <WorkflowsView />;
  if (sub === "races") return <RacesView />;
  if (sub === "board") return <BoardRoute />;
  if (sub === "settings") return <SettingsView />;
  if (sub === "mcp") return <MCPView />;
  if (sub === "agents") return <AgentsView />;
  if (sub === "itermoc") return <ItermocView />;
  if (sub === "containers") return <PhoneLazy tkey="more.containers" name="containers" draw={(m) => <m.ContainersView />} />;
  if (sub === "db" || sub.startsWith("db/")) return <PhoneLazy tkey="more.db" name="db" draw={(m) => <m.DBView rest={sub} />} />;
  if (sub === "testing") return <TestingView />;
  if (sub === "debug") return <DebugView />;
  if (sub === "services") return <ServicesView />;
  if (sub === "kube") return <KubeView />;
  if (sub === "phone" && layout.value !== "desktop") return <PhoneHost />;
  if (sub === "review") return <PhoneLazy tkey="pill.review" name="review" draw={(m) => <m.ReviewView />} />;
  const p = prefs.value;
  // Built on each draw, so a language change shows at once.
  const themes = [
    { value: "system", label: t("more.theme.system") },
    { value: "dark", label: t("more.theme.dark") },
    { value: "light", label: t("more.theme.light") },
  ] as const;
  const dirs = [
    { value: "auto", label: t("more.dir.auto") },
    { value: "ltr", label: t("more.dir.ltr") },
    { value: "rtl", label: t("more.dir.rtl") },
  ] as const;
  const langs = [
    { value: "auto", label: t("more.dir.auto") },
    { value: "en", label: t("more.lang.en") },
    { value: "fa", label: t("more.lang.fa") },
  ] as const;
  const motions = [
    { value: "system", label: t("ph.motion.system") },
    { value: "reduce", label: t("ph.motion.reduce") },
  ] as const;
  const buzz = [
    { value: "on", label: t("ph.on") },
    { value: "off", label: t("ph.off") },
  ] as const;
  return (
    <View tab="more" title={t("tab.more")}>
      <div class="more">
        <MoreLinks />
        <section class="group" aria-labelledby="g-phone">
          <h2 class="group-title" id="g-phone">
            {t("more.this_phone")}
          </h2>
          {layout.value !== "desktop" && (
            <button type="button" class="more-link" data-sub="phone" onClick={() => go({ tab: "more", rest: "phone", query: {} })}>
              {t("more.this_phone")} ›
            </button>
          )}
          <div class="field">
            <span class="field-label">{t("theme")}</span>
            <Segmented<Theme> label={t("theme")} value={p.theme} choices={themes} onChange={(v) => setPref("theme", v)} />
          </div>
          <div class="field">
            <span class="field-label">{t("more.lang")}</span>
            <Segmented<Language> label={t("more.lang")} value={p.lang} choices={langs} onChange={(v) => setPref("lang", v)} />
          </div>
          <div class="field">
            <span class="field-label">{t("more.dir")}</span>
            <Segmented<Direction> label={t("more.dir")} value={p.dir} choices={dirs} onChange={(v) => setPref("dir", v)} />
          </div>
          <div class="field">
            <span class="field-label">{t("ph.motion")}</span>
            <Segmented<Motion> label={t("ph.motion")} value={p.motion} choices={motions} onChange={(v) => setPref("motion", v)} />
          </div>
          <div class="field">
            <span class="field-label">{t("more.haptics")}</span>
            <Segmented<OnOff> label={t("more.haptics")} value={p.haptics ? "on" : "off"} choices={buzz} onChange={(v) => setPref("haptics", v === "on")} />
          </div>
          <NotifyField />
          <BookmarkField />
        </section>
        <section class="group" aria-labelledby="g-about">
          <h2 class="group-title" id="g-about">
            {t("more.about")}
          </h2>
          <p class="group-text">{t("more.about.body")}</p>
          <p class="group-text">
            <a class="link" href="/assets/licenses.txt">
              {t("more.licenses")}
            </a>
          </p>
        </section>
      </div>
    </View>
  );
}
