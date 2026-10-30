// Settings (WEB-46, web design 2.9): the laptop's settings, read only,
// for the keys the hub allows. A changed value shows its default under
// it. There is no way to change a setting here. #/more/settings
import { h } from "preact";
import { store } from "../../session";
import { readSettings } from "../../state/more";
import { t } from "../../strings";
import { TopicBody } from "../Placeholder";
import { SubView } from "./SubView";

function SettingsList() {
  const groups = readSettings(store.topic("settings").value?.data);
  if (groups.length === 0) return <p class="empty">{t("settings.none")}</p>;
  return (
    <div class="settings">
      <p class="settings-note">{t("settings.readonly")}</p>
      {groups.map((g, i) => (
        <section class="group" key={g.group} aria-labelledby={`sg-${i}`}>
          <h2 class="group-title" id={`sg-${i}`}>
            {g.group}
          </h2>
          <dl class="kv">
            {g.items.map((s) => (
              <div class="kv-row" key={s.key} data-key={s.key}>
                <dt class="kv-key" dir="ltr">
                  {s.key}
                </dt>
                <dd class="kv-value" dir="ltr">
                  {s.value}
                </dd>
                {s.value !== s.def && (
                  <dd class="kv-def" dir="ltr">
                    {t("settings.default", { value: s.def })}
                  </dd>
                )}
                {s.description && <dd class="kv-desc">{s.description}</dd>}
              </div>
            ))}
          </dl>
        </section>
      ))}
    </div>
  );
}

export function SettingsView() {
  return (
    <SubView title={t("more.settings")}>
      <TopicBody topic="settings" empty={<p class="empty">{t("settings.none")}</p>}>
        <SettingsList />
      </TopicBody>
    </SubView>
  );
}
