// The laptop's Open Config: Theme and Open Config: User Commands as web
// forms (desktop, WEBP-7). On the laptop each opens a config file in the
// editor; here each is an editor tab shaped like the settings editor:
// the Theme tab picks the laptop's color theme and its custom colors,
// the User Commands tab lists commands.json to add, change, delete and
// run them. Nothing is typed into a file by hand, so nothing breaks it.
import { h } from "preact";
import { useEffect, useState } from "preact/hooks";
import type { ThemeColor, ThemeColors, ThemeList, UserCmd, UserCmdList } from "../../../../../webproto/proto";
import { toast } from "../../lib/toasts";
import { link } from "../../session";
import { t } from "../../strings";
import { askHere } from "../../ui/AskHere";
import { Cod } from "../../ui/icons";
import { SetPage } from "./DeskSettings";

const canAct = () => link.meta.peek()?.role === "controller";
const failed = (e: unknown) => toast("warn", e instanceof Error ? e.message : t("cmd.failed"));
/** USER starts a user command's label on the laptop (its palette and tasks). */
const USER = "User: ";
const HEX = /^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$/;

/** long writes #abc as #aabbcc (the color input wants six digits). */
const long = (c: string) => (c.length === 4 ? "#" + c[1] + c[1] + c[2] + c[2] + c[3] + c[3] : c).toLowerCase();

/** ColorRow is one color of the custom theme: a swatch, its hex value
 * and (a custom one) a way back to the theme's own color. */
function ColorRow({ c, set }: { c: ThemeColor; set: (key: string, color: string) => void }) {
  const [text, setText] = useState(c.color);
  useEffect(() => setText(c.color), [c.color]);
  const id = "thm-" + c.key;
  const send = (v: string) => {
    const x = v.trim();
    if (x === c.color) return;
    if (!HEX.test(x)) {
      toast("warn", t("thm.bad"));
      setText(c.color);
      return;
    }
    set(c.key, x);
  };
  return (
    <div class={`set-item${c.custom ? " is-changed" : ""}`} data-key={c.key}>
      <div class="set-title">
        <span class="set-name">{c.key}</span>
        {c.custom && <span class="set-mod">{t("thm.custom")}</span>}
      </div>
      <div class="set-value cfg-color">
        <input type="color" class="cfg-swatch" value={long(c.color)} disabled={!canAct()} aria-label={c.key} onChange={(e) => send((e.currentTarget as HTMLInputElement).value)} />
        <input
          id={id}
          class="set-input is-num"
          dir="ltr"
          value={text}
          disabled={!canAct()}
          aria-label={t("cfg.hex", { key: c.key })}
          onInput={(e) => setText((e.currentTarget as HTMLInputElement).value)}
          onKeyDown={(e) => e.key === "Enter" && send(text)}
          onBlur={() => send(text)}
        />
        {c.custom && (
          <button type="button" class="pv-btn is-2" disabled={!canAct()} onClick={() => set(c.key, "")}>
            {t("thm.reset")}
          </button>
        )}
      </div>
    </div>
  );
}

/** ThemeTab is Open Config: Theme: the laptop's color theme and the
 * custom theme's colors. Each change shows on the laptop at once and is
 * saved there, as its own pickers do; this page follows its colors. */
export function ThemeTab() {
  const [list, setList] = useState<ThemeList | null>(null);
  const [colors, setColors] = useState<ThemeColor[]>([]);
  useEffect(() => {
    link.request("theme.list").then((r) => setList(r as ThemeList), failed);
    link.request("theme.colors").then((r) => setColors((r as ThemeColors).colors ?? []), failed);
  }, []);
  const pick = (id: string) =>
    link.request("theme.set", { id }).then((r) => {
      const l = r as ThemeList;
      setList(l);
      toast("ok", t("thm.set", { name: l.themes.find((x) => x.id === id)?.name ?? id }));
      link.request("theme.colors").then((c) => setColors((c as ThemeColors).colors ?? []), failed);
    }, failed);
  const setColor = (key: string, color: string) =>
    link.request("theme.color_set", { key, color }).then((r) => {
      setColors((r as ThemeColors).colors ?? []);
      toast("ok", color ? t("thm.color_set", { key, color }) : t("thm.color_reset", { key }));
      link.request("theme.list").then((l) => setList(l as ThemeList), failed);
    }, failed);
  const groups = [
    { id: "theme", label: t("cfg.theme_group"), n: 1 },
    { id: "colors", label: t("cfg.colors_group"), n: colors.length },
  ];
  return (
    <SetPage id="cfg-theme" title={t("theme")} groups={groups} note={t("cfg.theme_note")}>
      <section class="set-group" data-group="theme" aria-label={t("cfg.theme_group")}>
        <h2 class="set-group-h">{t("cfg.theme_group")}</h2>
        <div class="set-item">
          <div class="set-title">
            <span class="set-name">{t("theme")}</span>
          </div>
          <p class="set-desc">{t("thm.title")}</p>
          <div class="set-value">
            <select class="set-input set-select" aria-label={t("theme")} value={list?.active ?? ""} disabled={!list || !canAct()} onChange={(e) => pick((e.currentTarget as HTMLSelectElement).value)}>
              {(list?.themes ?? []).map((th) => (
                <option key={th.id} value={th.id}>
                  {th.light ? th.name + " · " + t("thm.light") : th.name}
                </option>
              ))}
            </select>
          </div>
        </div>
      </section>
      <section class="set-group" data-group="colors" aria-label={t("cfg.colors_group")}>
        <h2 class="set-group-h">{t("cfg.colors_group")}</h2>
        <p class="set-desc cfg-pad">{t("cfg.colors_desc")}</p>
        {colors.map((c) => (
          <ColorRow key={c.key} c={c} set={setColor} />
        ))}
      </section>
    </SetPage>
  );
}

/** Row is one command being edited; key keeps its row across edits. */
type Row = UserCmd & { key: number };

let nextKey = 1;
const rowsOf = (items: UserCmd[]): Row[] => items.map((c) => ({ ...c, key: nextKey++ }));
const plain = (rows: Row[]): UserCmd[] => rows.map(({ id, title, cmd, cwd }) => (cwd?.trim() ? { id: id.trim(), title: title.trim(), cmd, cwd: cwd.trim() } : { id: id.trim(), title: title.trim(), cmd }));

/** problem says what is wrong with the list (null: it can be saved). */
function problem(rows: Row[]): string | null {
  const seen = new Set<string>();
  for (const r of rows) {
    const id = r.id.trim();
    if (!/^[A-Za-z0-9_-]{1,40}$/.test(id)) return t("cfg.bad_id", { id: id || "—" });
    if (seen.has(id)) return t("cfg.dup_id", { id });
    seen.add(id);
    if (!r.title.trim()) return t("cfg.no_title", { id });
    if (!r.cmd.trim()) return t("cfg.no_cmd", { id });
  }
  return null;
}

/** freeID is a new command's id: cmd, cmd-2, cmd-3… */
function freeID(rows: Row[]): string {
  const ids = new Set(rows.map((r) => r.id));
  for (let i = 1; ; i++) {
    const id = i === 1 ? "cmd" : "cmd-" + i;
    if (!ids.has(id)) return id;
  }
}

/** UserCmdsTab is Open Config: User Commands: commands.json as a form.
 * Each command is a "User: <title>" row of the laptop's palette and of
 * Run Task…; Save writes the whole list, Run starts a saved one in a
 * laptop terminal (after a question here, as the Run view asks). */
export function UserCmdsTab() {
  const [saved, setSaved] = useState<UserCmd[] | null>(null);
  const [rows, setRows] = useState<Row[]>([]);
  const [broken, setBroken] = useState("");
  const load = (l: UserCmdList) => {
    setSaved(l.items ?? []);
    setRows(rowsOf(l.items ?? []));
    setBroken(l.error ?? "");
  };
  useEffect(() => {
    link.request("usercmds.list").then((r) => load(r as UserCmdList), failed);
  }, []);
  const act = canAct() && !broken && saved !== null;
  const dirty = saved !== null && JSON.stringify(plain(rows)) !== JSON.stringify(saved.map((c) => (c.cwd ? c : { id: c.id, title: c.title, cmd: c.cmd })));
  const bad = problem(rows);
  const edit = (key: number, k: "id" | "title" | "cmd" | "cwd", v: string) => setRows((o) => o.map((r) => (r.key === key ? { ...r, [k]: v } : r)));
  const save = () => {
    if (bad) {
      toast("warn", bad);
      return;
    }
    link.request("usercmds.save", { items: plain(rows) }).then((r) => {
      load(r as UserCmdList);
      toast("ok", t("cfg.saved"));
    }, failed);
  };
  const run = (c: UserCmd) => {
    const label = USER + (c.title || c.id);
    askHere.value = {
      label: t("tasks.run"),
      text: t("tasks.confirm", { label }),
      verb: t("tasks.run"),
      run: () => void link.request("tasks.run", { label }).then(() => toast("ok", t("tasks.started", { label })), failed),
    };
  };
  const isSaved = (r: Row) => !dirty && saved?.some((c) => c.id === r.id);
  return (
    <SetPage id="cfg-ucmd" title={t("cfg.ucmd_tab")} groups={[{ id: "cmds", label: t("cfg.ucmd_group"), n: rows.length }]} note={t("cfg.ucmd_note")}>
      <section class="set-group" data-group="cmds" aria-label={t("cfg.ucmd_group")}>
        <h2 class="set-group-h">{t("cfg.ucmd_group")}</h2>
        {broken && (
          <p class="set-desc cfg-pad cfg-warn" role="alert">
            <Cod name="warning" /> {broken}
          </p>
        )}
        {saved !== null && !broken && rows.length === 0 && <p class="setp-empty">{t("cfg.ucmd_none")}</p>}
        {rows.map((r) => (
          <div class="set-item cfg-cmd" key={r.key} data-key={r.id}>
            <div class="set-title">
              <span class="set-cat">{USER}</span>
              <span class="set-name">{r.title || r.id || "—"}</span>
            </div>
            <div class="cfg-fields">
              <label class="cfg-field">
                <span>{t("cfg.f_title")}</span>
                <input class="set-input" value={r.title} disabled={!act} onInput={(e) => edit(r.key, "title", (e.currentTarget as HTMLInputElement).value)} />
              </label>
              <label class="cfg-field">
                <span>{t("cfg.f_id")}</span>
                <input class="set-input is-num" dir="ltr" value={r.id} disabled={!act} onInput={(e) => edit(r.key, "id", (e.currentTarget as HTMLInputElement).value)} />
              </label>
              <label class="cfg-field is-wide">
                <span>{t("cfg.f_cmd")}</span>
                <input class="set-input cfg-mono" dir="ltr" value={r.cmd} disabled={!act} onInput={(e) => edit(r.key, "cmd", (e.currentTarget as HTMLInputElement).value)} />
              </label>
              <label class="cfg-field is-wide">
                <span>{t("cfg.f_cwd")}</span>
                <input class="set-input cfg-mono" dir="ltr" value={r.cwd ?? ""} placeholder={t("cfg.cwd_ph")} disabled={!act} onInput={(e) => edit(r.key, "cwd", (e.currentTarget as HTMLInputElement).value)} />
              </label>
            </div>
            <div class="pv-ask-row cfg-row-acts">
              <button type="button" class="pv-btn is-2" disabled={!act} onClick={() => setRows((o) => o.filter((x) => x.key !== r.key))}>
                <Cod name="trash" /> {t("cmd.snippet_delete")}
              </button>
              <button type="button" class="pv-btn is-2" disabled={!act || !isSaved(r)} title={isSaved(r) ? undefined : t("cfg.save_first")} onClick={() => run(r)}>
                <Cod name="play" /> {t("tasks.run")}
              </button>
            </div>
          </div>
        ))}
        <div class="pv-ask-row cfg-acts">
          <button type="button" class="pv-btn is-2" disabled={!act || rows.length >= 100} onClick={() => setRows((o) => [...o, { id: freeID(o), title: "", cmd: "", key: nextKey++ }])}>
            <Cod name="add" /> {t("cfg.add")}
          </button>
          <button type="button" class="pv-btn is-2" disabled={!act || !dirty} onClick={() => saved && setRows(rowsOf(saved))}>
            <Cod name="discard" /> {t("cfg.revert")}
          </button>
          <button type="button" class="pv-btn" disabled={!act || !dirty} title={bad ?? undefined} onClick={save}>
            <Cod name="save" /> {t("file.save")}
          </button>
        </div>
        {dirty && bad && <p class="set-desc cfg-pad cfg-warn">{bad}</p>}
      </section>
    </SetPage>
  );
}
