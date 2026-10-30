// The five views of the shell. Their data views arrive with their own
// tasks (chat WEB-14, tree WEB-16, files WEB-18, terminal WEB-20, git
// WEB-23); until then each shows its loading and empty states.
import { h } from "preact";
import { prefs, setPref, type Direction, type Language, type Theme } from "../lib/prefs";
import { go, route } from "../lib/router";
import { t } from "../strings";
import { Segmented } from "../ui/Segmented";
import { TopicBody } from "./Placeholder";
import { RunsView } from "./more/RunsView";
import { SearchView } from "./more/SearchView";
import { TasksView } from "./more/TasksView";
import { View } from "./View";
import { FileView } from "./FileView";
import { TreeView } from "./Files";

export { ChatView } from "./Chat";

export function FilesView() {
  const r = route.value;
  return r.tab === "files" && r.rest ? <FileView path={r.rest} /> : <TreeView />;
}

export { TermView } from "./Term";

export { GitView } from "./git/GitView";

export function MoreView() {
  const sub = route.value.tab === "more" ? route.value.rest : "";
  if (sub === "search") return <SearchView />;
  if (sub === "tasks") return <TasksView />;
  if (sub === "runs") return <RunsView />;
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
          <button type="button" class="more-link" onClick={() => go({ tab: "more", rest: "search", query: {} })}>
            {t("more.search")}
          </button>
          <button type="button" class="more-link" onClick={() => go({ tab: "more", rest: "tasks", query: {} })}>
            {t("more.tasks")}
          </button>
          <button type="button" class="more-link" onClick={() => go({ tab: "more", rest: "runs", query: {} })}>
            {t("more.runs")}
          </button>
        </nav>
        <section class="group" aria-labelledby="g-phone">
          <h2 class="group-title" id="g-phone">
            {t("more.this_phone")}
          </h2>
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
