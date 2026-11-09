// The desktop's Settings page, in the editor area like the laptop
// editor's settings editor: a search box, the groups on the left (a
// table of contents with counts), and each setting as its category and
// title in bold, its description, and its value in the control its type
// has. The laptop's settings are read only here (each shows a lock; a
// changed one has the blue bar and says its default). This device's page
// shares its frame (SetPage) and lives in device.js (src/device.tsx).
import { Fragment, h, type ComponentChildren } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useRef } from "preact/hooks";
import { link, store } from "../../session";
import { readSettings, type SettingView } from "../../state/more";
import { t } from "../../strings";
import { Cod } from "../../ui/icons";

const query = signal("");

/** words writes a key as a title: "font_size" → "Font Size". */
export function titleOf(key: string): string {
  const last = key.slice(key.lastIndexOf(".") + 1);
  return last
    .split(/[_\-\s]+/)
    .filter(Boolean)
    .map((w) => w[0].toUpperCase() + w.slice(1))
    .join(" ");
}

function matches(s: SettingView, q: string): boolean {
  if (!q) return true;
  const hay = `${s.key} ${titleOf(s.key)} ${s.group} ${s.description} ${s.value}`.toLowerCase();
  return q
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .every((w) => hay.includes(w));
}

/** Value is a laptop setting's value in its type's control, read only. */
function Value({ s }: { s: SettingView }) {
  const id = "set-" + s.key;
  if (s.type === "bool")
    return (
      <label class="set-check">
        <input type="checkbox" id={id} checked={s.value === "true"} disabled aria-describedby={id + "-d"} />
        <span id={id + "-d"}>{s.description || titleOf(s.key)}</span>
      </label>
    );
  if (s.type === "enum")
    return (
      <select class="set-input set-select" id={id} disabled aria-label={titleOf(s.key)}>
        <option>{s.value}</option>
      </select>
    );
  return <input class={`set-input${s.type === "int" || s.type === "number" ? " is-num" : ""}`} id={id} value={s.value} disabled aria-label={titleOf(s.key)} dir="ltr" />;
}

function Item({ s }: { s: SettingView }) {
  const changed = s.value !== s.def;
  return (
    <div class={`set-item${changed ? " is-changed" : ""}`} data-key={s.key}>
      <div class="set-title">
        <span class="set-cat">{s.group}: </span>
        <span class="set-name">{titleOf(s.key)}</span>
        <span class="set-lock" title={t("settings.readonly")}>
          <Cod name="lock" />
        </span>
        {changed && <span class="set-mod">{t("settings.default", { value: s.def })}</span>}
      </div>
      <div class="set-key" dir="ltr">
        {s.key}
      </div>
      {s.type !== "bool" && s.description && <p class="set-desc">{s.description}</p>}
      <div class="set-value">
        <Value s={s} />
      </div>
    </div>
  );
}

/** SetPage is the frame both pages share: search, groups, list. */
export function SetPage({ id, title, groups, search, children, note }: { id: string; title: string; groups: { id: string; label: string; n: number }[]; search?: boolean; note?: ComponentChildren; children: ComponentChildren }) {
  const list = useRef<HTMLDivElement | null>(null);
  return (
    <section class="view setp" data-view={id} aria-labelledby={`setp-${id}`}>
      <h1 class="setp-h view-title" id={`setp-${id}`}>
        {title}
      </h1>
      {search && (
        <div class="setp-search">
          <div class="pv-input">
            <input
              type="search"
              value={query.value}
              placeholder={t("set.search")}
              aria-label={t("set.search")}
              onInput={(e) => (query.value = (e.currentTarget as HTMLInputElement).value)}
            />
            <span class="setp-found">{query.value ? t("set.found", { n: groups.reduce((a, g) => a + g.n, 0) }) : ""}</span>
          </div>
        </div>
      )}
      <div class="setp-tabs" role="tablist">
        <span class="setp-tab is-on" role="tab" aria-selected="true">
          {title}
        </span>
      </div>
      {note && <p class="setp-note">{note}</p>}
      <div class="setp-body">
        <nav class="setp-toc" aria-label={t("set.toc")}>
          {groups.map((g) => (
            <button
              type="button"
              key={g.id}
              class="setp-toc-row"
              onClick={() => list.current?.querySelector(`[data-group="${CSS.escape(g.id)}"]`)?.scrollIntoView({ block: "start" })}
            >
              {g.label}
              <span class="setp-toc-n">{g.n}</span>
            </button>
          ))}
        </nav>
        <div class="setp-list" ref={list} tabIndex={id === "settings" ? 0 : undefined} aria-label={id === "settings" ? title : undefined}>
          {children}
        </div>
      </div>
    </section>
  );
}

/** DeskSettings is the laptop's settings, read only. */
export function DeskSettings() {
  useEffect(() => link.subscribe("settings"), []);
  const st = store.topic("settings").value;
  const all = readSettings(st?.data);
  const q = query.value.trim();
  const shown = all.map((g) => ({ group: g.group, items: g.items.filter((s) => matches(s, q)) })).filter((g) => g.items.length > 0);
  return (
    <SetPage
      id="settings"
      title={t("more.settings")}
      search
      note={
        <>
          <Cod name="lock" /> {t("settings.readonly")}
        </>
      }
      groups={shown.map((g) => ({ id: g.group, label: g.group, n: g.items.length }))}
    >
      {!st ? (
        <p class="setp-empty">{t("loading")}</p>
      ) : shown.length === 0 ? (
        <p class="setp-empty">{q ? t("set.none_found") : t("settings.none")}</p>
      ) : (
        shown.map((g) => (
          <section key={g.group} class="set-group" data-group={g.group} aria-label={g.group}>
            <h2 class="set-group-h">{g.group}</h2>
            {g.items.map((s) => (
              <Item key={s.key} s={s} />
            ))}
          </section>
        ))
      )}
    </SetPage>
  );
}
