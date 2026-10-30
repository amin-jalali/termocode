// The five views of the shell. Their data views arrive with their own
// tasks (chat WEB-14, tree WEB-16, files WEB-18, terminal WEB-20, git
// WEB-23); until then each shows its loading and empty states.
import { h } from "preact";
import { prefs, setPref, type Direction, type Language, type Theme } from "../lib/prefs";
import { t } from "../strings";
import { Segmented } from "../ui/Segmented";
import { TopicBody } from "./Placeholder";
import { View } from "./View";

export { ChatView } from "./Chat";

export function FilesView() {
  return (
    <View tab="files" title={t("tab.files")}>
      <TopicBody topic="tree" also={["bufs"]} empty={<p>{t("files.no_ws")}</p>} />
    </View>
  );
}

export function TermView() {
  return (
    <View tab="term" title={t("tab.term")}>
      <TopicBody topic="terms" empty={<p>{t("term.none")}</p>} />
    </View>
  );
}

export function GitView() {
  return (
    <View tab="git" title={t("tab.git")}>
      <TopicBody topic="git" empty={<p>{t("git.clean")}</p>} />
    </View>
  );
}

export function MoreView() {
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
