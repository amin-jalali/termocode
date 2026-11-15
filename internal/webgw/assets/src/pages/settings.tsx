// Settings on the phone (UI page 10): the laptop's allow-listed settings
// as grouped rows (an icon, a plain name, the value, ›), not a page of
// help text. A switch turns a yes/no setting on or off at once; a tap on
// any other row opens a picker sheet with one line per choice (or a
// field), the long help under More…. Changes go to the laptop
// (settings.set) and apply there at once; a key that lowers a safety
// check asks first. Search in the top bar and chips (Changed and the
// groups) narrow the list; empty groups are not drawn. The list is the
// "settings" view of the view registry grouped by group, so ⚙ hands it
// to the AI with the spec, and a long press on a row gives Ask AI about
// this / Add to chat with that one setting.
import { Fragment, h, H } from "./host";
import { aiItems, askAI, copy, Ic, openMenu, PressMenu, press, Section, Switch, useChat, viewAttach, type IconName } from "./kit";
import { tx } from "./strings";

type Obj = Record<string, unknown>;
const obj = (v: unknown): Obj => (typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Obj) : {});
const str = (v: unknown): string => (typeof v === "string" ? v : "");

export interface Setting {
  key: string;
  group: string;
  type: string;
  value: string;
  def: string;
  about: string;
  choices: string[];
}

/** readItems reads the settings topic (well-formed rows only). */
export function readItems(data: unknown): Setting[] {
  const items = obj(data).items;
  if (!Array.isArray(items)) return [];
  return items
    .map(obj)
    .filter((x) => str(x.key) !== "")
    .map((x) => ({
      key: str(x.key),
      group: str(x.group),
      type: str(x.type),
      value: str(x.value),
      def: str(x.default),
      about: str(x.description),
      choices: Array.isArray(x.choices) ? x.choices.filter((c): c is string => typeof c === "string") : [],
    }));
}

// Words a key is made of that are names, not words.
const UPPER: Record<string, string> = { ai: "AI", mcp: "MCP", http: "HTTP", tls: "TLS", kb: "KB", mb: "MB", usd: "USD", ssh: "SSH", url: "URL" };
const NAMES: Record<string, string> = { show_hidden: "Show hidden files", ai_until_done_budget_usd: "AI until-done budget (USD)", ai_until_done_rounds: "AI until-done rounds" };

/** words turns a config key into a name: dashboard_logo → Dashboard logo. */
export function words(key: string): string {
  if (NAMES[key]) return NAMES[key];
  const w = key.split("_").map((p) => UPPER[p] ?? p);
  const s = w.join(" ");
  return s.charAt(0).toUpperCase() + s.slice(1);
}

/** valueWords is an enum value as the row shows it: nerd_font → Nerd font. */
const valueWords = (v: string): string => (v ? (v.charAt(0).toUpperCase() + v.slice(1)).replace(/_/g, " ") : "—");

// Each row's icon tile: the sheet's for the common keys, the group's else.
const TILE: Record<string, [IconName, string]> = {
  theme: ["palette", "var(--ai)"],
  icons: ["type", "var(--blue)"],
  animations: ["bolt", "var(--amber)"],
  images: ["eye", "var(--green)"],
  dashboard_logo: ["palette", "var(--blue)"],
  font_delta: ["type", "var(--green)"],
  auto_save: ["file", "var(--blue)"],
  word_wrap: ["wrap", "#555"],
  show_hidden: ["eyeOff", "#555"],
  tab_size: ["list", "#555"],
  key_hints: ["type", "#555"],
};
function tile(s: Setting): [IconName, string] {
  if (TILE[s.key]) return TILE[s.key];
  if (s.key.startsWith("ai_")) return ["spark", "var(--ai)"];
  if (s.key.startsWith("mcp_")) return ["plug", "var(--green)"];
  if (s.key.startsWith("http_")) return ["globe", "var(--blue)"];
  if (s.key.includes("session") || s.key.includes("reconnect")) return ["term", "#555"];
  if (s.key.startsWith("update")) return ["refresh", "#555"];
  return ["gear", "#555"];
}

/** RISKY keys lower a safety check: a change asks first. */
const RISKY = new Set(["http_tls_insecure", "mcp_strict", "mcp_share", "mcp_trust_annotations"]);

/** SPEC is the list as a view of the registry ("settings", grouped by
 * group): what ⚙ and Add to chat hand the AI. */
const SPEC = { group: "group" };

type Filter = "" | "changed" | `g:${string}`;

/** set asks the laptop to change one setting; the list comes back with
 * it (the row shows the change, so only a refusal says something). */
async function set(s: Setting, value: string): Promise<boolean> {
  try {
    await H.link.request("settings.set", { key: s.key, value });
    return true;
  } catch (e) {
    H.toast("warn", H.errText(e));
    return false;
  }
}

const oneSetting = (s: Setting) => viewAttach("settings", words(s.key), { filter: [{ field: "key", op: "eq", value: s.key }], layout: "detail" });

export function SettingsPage() {
  useChat();
  const [q, setQ] = H.useState<string | null>(null);
  const back = () => H.go({ tab: "more", rest: "", query: {} });
  return (
    <H.View
      tab="more"
      name="settings"
      title={tx("ps.title")}
      back={{ label: tx("pg.back"), onClick: back }}
      actions={
        <button type="button" class="pg-tool" data-act="search" aria-label={tx("ps.search")} aria-pressed={q !== null ? "true" : "false"} onClick={() => setQ(q === null ? "" : null)}>
          <Ic n="search" size={22} />
        </button>
      }
    >
      <H.TopicBody topic="settings" empty={<p class="pg-empty">{tx("ps.none")}</p>}>
        <SettingsList q={q} setQ={setQ} />
      </H.TopicBody>
      <PressMenu />
    </H.View>
  );
}

function SettingsList({ q, setQ }: { q: string | null; setQ: (v: string) => void }) {
  const all = readItems(H.store.topic("settings").value?.data);
  const [filter, setFilter] = H.useState<Filter>("");
  const [open, setOpen] = H.useState<string | null>(null);
  const [ask, setAsk] = H.useState<{ s: Setting; value: string; name: string } | null>(null);
  const themes = useThemes();
  const changed = all.filter((s) => s.value !== s.def);
  const groups = H.useMemo(() => [...new Set(all.map((s) => s.group))], [all.map((s) => s.group).join("\n")]);
  if (all.length === 0) return <p class="pg-empty">{tx("ps.none")}</p>;

  const needle = (q ?? "").trim().toLowerCase();
  const shown = all.filter(
    (s) =>
      (filter === "" || (filter === "changed" ? s.value !== s.def : s.group === filter.slice(2))) &&
      (!needle || [s.key, words(s.key), s.group, s.about].some((x) => x.toLowerCase().includes(needle))),
  );
  const valueName = (s: Setting, v: string) => (s.key === "theme" ? themes.get(v) ?? v : s.type === "enum" ? valueWords(v) : v);
  const change = (s: Setting, v: string) => {
    const name = s.type === "bool" ? tx(v === "true" ? "ps.on" : "ps.off") : valueName(s, v);
    if (RISKY.has(s.key)) return setAsk({ s, value: v, name });
    void set(s, v).then((ok) => ok && setOpen(null));
  };
  const spec = (g?: string) => ({ ...SPEC, ...(g ? { filter: [{ field: "group", op: "eq", value: g }] } : {}) });
  const cur = open ? all.find((s) => s.key === open) : undefined;

  return (
    <div class="pg pg-settings">
      {q !== null ? (
        <label class="pg-search">
          <Ic n="search" />
          <input type="search" value={q} placeholder={tx("ps.search")} aria-label={tx("ps.search")} autoFocus onInput={(e) => setQ((e.target as HTMLInputElement).value)} />
        </label>
      ) : (
        <p class="pg-hint">{tx("ps.hint")}</p>
      )}
      <div class="pg-chips" role="group" aria-label={tx("ps.title")}>
        {changed.length > 0 && (
          <button type="button" class="pg-chip" data-chip="changed" aria-pressed={filter === "changed" ? "true" : "false"} onClick={() => setFilter(filter === "changed" ? "" : "changed")}>
            {tx("ps.changed")} <span class="n">{changed.length}</span>
          </button>
        )}
        {groups.map((g) => (
          <button type="button" class="pg-chip" key={g} data-chip={g} aria-pressed={filter === `g:${g}` ? "true" : "false"} onClick={() => setFilter(filter === `g:${g}` ? "" : `g:${g}`)}>
            {g}
          </button>
        ))}
      </div>
      {shown.length === 0 && <p class="pg-empty">{tx("ps.no_match")}</p>}
      {groups.map((g) => {
        const rows = shown.filter((s) => s.group === g);
        if (!rows.length) return null;
        const gear = (
          <button
            type="button"
            class="pg-tool"
            data-act="customize"
            aria-label={tx("pg.customize", { name: g })}
            onClick={(e) =>
              openMenu(e.clientX, e.clientY, g, [
                { id: "ask-view", label: tx("pg.ask_view"), icon: "spark", ai: true, run: () => askAI(viewAttach("settings", g, spec(g)), tx("pg.ask_view_text", { page: tx("ps.title") })) },
              ])
            }
          >
            <Ic n="gear" size={17} />
          </button>
        );
        return (
          <Section key={g} id={g} title={g} tool={gear}>
            {rows.map((s) => {
              const [icon, bg] = tile(s);
              const name = words(s.key);
              const menu = press((x, y) => openMenu(x, y, s.key, [...aiItems(oneSetting(s), tx("ps.ask_text") + s.key), { id: "copy", label: tx("pg.copy"), icon: "copy", run: () => copy(s.value) }]));
              const lead = (
                <span class="pg-tile" style={{ background: bg }}>
                  <Ic n={icon} size={16} />
                </span>
              );
              if (s.type === "bool") {
                const on = s.value === "true";
                return (
                  <button type="button" class="pg-row" key={s.key} data-key={s.key} role="switch" aria-checked={on ? "true" : "false"} onClick={() => change(s, on ? "false" : "true")} onContextMenu={menu}>
                    {lead}
                    <span class="t">
                      <b>{name}</b>
                    </span>
                    <Switch on={on} />
                  </button>
                );
              }
              return (
                <button type="button" class="pg-row" key={s.key} data-key={s.key} onClick={() => setOpen(s.key)} onContextMenu={menu}>
                  {lead}
                  <span class="t">
                    <b>{name}</b>
                  </span>
                  <span class="r">{valueName(s, s.value)}</span>
                  <span class="pg-chev">
                    <Ic n="chev" />
                  </span>
                </button>
              );
            })}
          </Section>
        );
      })}
      {cur && <Picker s={cur} themes={themes} valueName={valueName} onPick={(v) => change(cur, v)} onClose={() => setOpen(null)} />}
      {ask && (
        <H.Sheet label={tx("ps.risky", { name: words(ask.s.key), value: ask.name })} onLater={() => setAsk(null)}>
          <div class="pg-sh">
            <b>{tx("ps.risky", { name: words(ask.s.key), value: ask.name })}</b>
          </div>
          <p class="pg-more" style={{ paddingTop: "10px" }}>
            {tx("ps.risky_why")}
          </p>
          <div class="pg-acts">
            <button type="button" class="pg-btn" data-act="cancel" onClick={() => setAsk(null)}>
              {tx("pg.cancel")}
            </button>
            <button
              type="button"
              class="pg-btn danger"
              data-act="confirm"
              onClick={() => {
                const a = ask;
                setAsk(null);
                void set(a.s, a.value).then((ok) => ok && setOpen(null));
              }}
            >
              {tx("ps.change")}
            </button>
          </div>
        </H.Sheet>
      )}
    </div>
  );
}

/** useThemes is the laptop's theme names by id (theme.list), once. */
function useThemes(): Map<string, string> {
  const [names, setNames] = H.useState<Map<string, string>>(new Map());
  H.useEffect(() => {
    H.link.request("theme.list").then(
      (r) => {
        const out = new Map<string, string>();
        for (const th of (obj(r).themes as unknown[] | undefined) ?? []) out.set(str(obj(th).id), str(obj(th).name));
        setNames(out);
      },
      () => undefined,
    );
  }, []);
  return names;
}

/** Picker is a setting's sheet: one line per choice (✓ on the current
 * one) or a field, the default and the long help under More…, and
 * Ask AI about this setting. */
function Picker({ s, themes, valueName, onPick, onClose }: { s: Setting; themes: Map<string, string>; valueName: (s: Setting, v: string) => string; onPick: (v: string) => void; onClose: () => void }) {
  const [more, setMore] = H.useState(false);
  const [draft, setDraft] = H.useState(s.value);
  const choices = s.key === "theme" ? [...themes.keys()] : s.type === "enum" ? s.choices : [];
  const num = s.type === "int";
  const step = (d: number) => setDraft(String((parseInt(draft, 10) || 0) + d));
  return (
    <H.Sheet label={words(s.key)} onLater={onClose}>
      <div class="pg-picker" data-picker={s.key}>
        <div class="pg-sh">
          <button type="button" class="pg-tool" aria-label={tx("pg.back")} onClick={onClose}>
            <Ic n="back" />
          </button>
          <b>{words(s.key)}</b>
        </div>
        {choices.length > 0 ? (
          <div role="radiogroup" aria-label={words(s.key)}>
            {choices.map((c) => (
              <button type="button" class="pg-row pg-pick" key={c} data-choice={c} role="radio" aria-checked={c === s.value ? "true" : "false"} onClick={() => c !== s.value && onPick(c)}>
                <span class="ck">{c === s.value && <Ic n="check" />}</span>
                <span class="t">
                  <b>{valueName(s, c)}</b>
                </span>
              </button>
            ))}
          </div>
        ) : (
          <form
            class="pg-step"
            onSubmit={(e) => {
              e.preventDefault();
              if (draft.trim() !== s.value) onPick(draft.trim());
            }}
          >
            {num && (
              <button type="button" aria-label={tx("ps.less_one")} onClick={() => step(-1)}>
                −
              </button>
            )}
            <input value={draft} inputMode={num || s.type === "number" ? "decimal" : "text"} aria-label={tx("ps.value")} onInput={(e) => setDraft((e.target as HTMLInputElement).value)} />
            {num && (
              <button type="button" aria-label={tx("ps.more_one")} onClick={() => step(1)}>
                +
              </button>
            )}
            <button type="submit" class="pg-btn pri" data-act="save">
              {tx("pg.save")}
            </button>
          </form>
        )}
        <p class="pg-note">
          {tx("ps.default", { value: valueName(s, s.def) })}
          {s.about && (
            <>
              {" · "}
              <button type="button" data-act="more" aria-expanded={more ? "true" : "false"} onClick={() => setMore(!more)}>
                {tx(more ? "ps.less" : "ps.more")}
              </button>
            </>
          )}
        </p>
        {more && <p class="pg-more">{s.about}</p>}
        <div class="pg-acts">
          <button type="button" class="pg-chip ai" data-act="ask" onClick={() => askAI(oneSetting(s), tx("ps.ask_text") + s.key)}>
            ✦ {tx("ps.ask")}
          </button>
        </div>
      </div>
    </H.Sheet>
  );
}
