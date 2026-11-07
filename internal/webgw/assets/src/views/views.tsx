// The five views of the shell. Their data views arrive with their own
// tasks (chat WEB-14, tree WEB-16, files WEB-18, terminal WEB-20, git
// WEB-23); until then each shows its loading and empty states.
import { h } from "preact";
import { useEffect } from "preact/hooks";
import { prefs, setPref, type Direction, type Language, type Theme } from "../lib/prefs";
import { go, route } from "../lib/router";
import { layout } from "../lib/layout";
import { t } from "../strings";
import { NotifyField } from "../ui/NotifyField";
import { BookmarkField } from "../ui/BookmarkField";
import { Segmented } from "../ui/Segmented";
import { TopicBody } from "./Placeholder";
import { RunsView } from "./more/RunsView";
import { WorkflowsView } from "./more/WorkflowsView";
import { SettingsView } from "./more/SettingsView";
import { AgentsView } from "./AgentsView";
import { ItermocView } from "./ItermocView";
import { MCPView } from "./more/MCPView";
import { ContainersView } from "./more/ContainersView";
import { DBView } from "./more/DBView";
import { SearchView } from "./more/SearchView";
import { TasksView } from "./more/TasksView";
import { TestingView } from "./run/TestingView";
import { DebugView } from "./run/DebugView";
import { ServicesView } from "./run/ServicesView";
import { KubeView } from "./run/KubeView";
import { View } from "./View";
import { SubView } from "./more/SubView";
import { FileView } from "./FileView";
import { home as phone, homeFailed as phoneFailed, loadHome } from "../lib/lazy";
import { Button } from "../ui/Button";
import { TreeView } from "./Files";
import { PhoneHost } from "./PhoneHost";

export { ChatView } from "./Chat";

export function FilesView() {
  const r = route.value;
  return r.tab === "files" && r.rest ? <FileView path={r.rest} /> : <TreeView />;
}

export { TermView } from "./Term";

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

/** ReviewLazy is #/more/review (MOB-14): the phone views' review, loaded
 * from home.js on first use. */
function ReviewLazy() {
  const m = phone.value;
  useEffect(() => {
    if (!m) loadHome().catch(() => undefined);
  }, [m]);
  if (m) return <m.ReviewView />;
  return (
    <SubView title={t("pill.review")} name="review">
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

export function MoreView() {
  const sub = route.value.tab === "more" ? route.value.rest : "";
  if (sub === "tools") return <ToolsView />;
  if (sub === "search") return <SearchView />;
  if (sub === "tasks") return <TasksView />;
  if (sub === "runs") return <RunsView />;
  if (sub === "workflows") return <WorkflowsView />;
  if (sub === "settings") return <SettingsView />;
  if (sub === "mcp") return <MCPView />;
  if (sub === "agents") return <AgentsView />;
  if (sub === "itermoc") return <ItermocView />;
  if (sub === "containers") return <ContainersView />;
  if (sub === "db" || sub.startsWith("db/")) return <DBView rest={sub} />;
  if (sub === "testing") return <TestingView />;
  if (sub === "debug") return <DebugView />;
  if (sub === "services") return <ServicesView />;
  if (sub === "kube") return <KubeView />;
  if (sub === "phone" && layout.value !== "desktop") return <PhoneHost />;
  if (sub === "review") return <ReviewLazy />;
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
  return (
    <View tab="more" title={t("tab.more")}>
      <div class="more">
        <nav class="more-links" aria-label={t("tab.more")}>
          {layout.value === "phone" && (
            // The phone's chat-first Home (MOB-4); its drawer says whether the page starts there.
            <button type="button" class="more-link" data-sub="home" onClick={() => go({ tab: "home", rest: "", query: {} })}>
              <span aria-hidden="true">✦ </span>
              {t("key.home")}
            </button>
          )}
          <button type="button" class="more-link" onClick={() => go({ tab: "more", rest: "search", query: {} })}>
            {t("more.search")}
          </button>
          <button type="button" class="more-link" onClick={() => go({ tab: "more", rest: "tasks", query: {} })}>
            {t("more.tasks")}
          </button>
          <button type="button" class="more-link" onClick={() => go({ tab: "more", rest: "runs", query: {} })}>
            {t("more.runs")}
          </button>
          <button type="button" class="more-link" data-sub="workflows" onClick={() => go({ tab: "more", rest: "workflows", query: {} })}>
            {t("more.workflows")}
          </button>
          <button type="button" class="more-link" data-sub="settings" onClick={() => go({ tab: "more", rest: "settings", query: {} })}>
            {t("more.settings")}
          </button>
          <button type="button" class="more-link" data-sub="db" onClick={() => go({ tab: "more", rest: "db", query: {} })}>
            {t("more.db")}
          </button>
          <button type="button" class="more-link" data-sub="containers" onClick={() => go({ tab: "more", rest: "containers", query: {} })}>
            {t("more.containers")}
          </button>
          <button type="button" class="more-link" data-sub="itermoc" onClick={() => go({ tab: "more", rest: "itermoc", query: {} })}>
            {t("im.title")}
          </button>
          <button type="button" class="more-link" data-sub="mcp" onClick={() => go({ tab: "more", rest: "mcp", query: {} })}>
            {t("more.mcp")}
          </button>
          <button type="button" class="more-link" data-sub="debug" onClick={() => go({ tab: "more", rest: "debug", query: {} })}>
            {t("act.run")}
          </button>
          <button type="button" class="more-link" data-sub="services" onClick={() => go({ tab: "more", rest: "services", query: {} })}>
            {t("act.services")}
          </button>
          <button type="button" class="more-link" data-sub="kube" onClick={() => go({ tab: "more", rest: "kube", query: {} })}>
            {t("act.kube")}
          </button>
          <button type="button" class="more-link" data-sub="testing" onClick={() => go({ tab: "more", rest: "testing", query: {} })}>
            {t("act.testing")}
          </button>
          <button type="button" class="more-link more-tools" data-sub="tools" onClick={() => go({ tab: "more", rest: "tools", query: {} })}>
            <span>
              <span aria-hidden="true">☰ </span>
              {t("tools.title")}
            </span>
          </button>
          {layout.value !== "desktop" && (
            <button type="button" class="more-link" data-sub="review" onClick={() => go({ tab: "more", rest: "review", query: {} })}>
              {t("pill.review")}
            </button>
          )}
        </nav>
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
