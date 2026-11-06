// The workbench's quick input: Go to file (Ctrl/Cmd+P), the command list
// (Ctrl/Cmd+Shift+P, or ">" in Go to file) and Go to line (":"). One
// box at the top of the editor area; arrows move, Enter runs, Esc closes.
// A view's list to pick from (the tags, the stash, the reflog) uses it too.
import { h, type JSX } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import type { FilesListRes } from "../../../../webproto/proto";
import { allCommands, cmdTitle, gotoLine, loadLaptopCmds, noteRun, recentCmds, type Command } from "../lib/commands";
import { rank, type Hit } from "../lib/fuzzy";
import { quick, showFile, type Pick, type PickItem } from "../lib/workbench";
import { link } from "../session";
import { t } from "../strings";
import { FileIcon } from "./icons";

const MAX_ROWS = 50;

/** files is the workspace list of this page, asked once per open. */
let files: { list: string[]; at: number } | null = null;
const FILES_TTL_MS = 30_000;

function Marked({ text, at }: { text: string; at: readonly number[] }) {
  if (!at.length) return <span>{text}</span>;
  const set = new Set(at);
  const out: JSX.Element[] = [];
  let run = "", on = false;
  const flush = (k: number) => {
    if (run) out.push(on ? <mark key={k}>{run}</mark> : <span key={k}>{run}</span>);
    run = "";
  };
  for (let i = 0; i < text.length; i++) {
    const m = set.has(i);
    if (m !== on) {
      flush(i);
      on = m;
    }
    run += text[i];
  }
  flush(text.length);
  return <span>{out}</span>;
}

interface Row {
  key: string;
  label: string;
  at: number[];
  detail?: string;
  detailAt?: number[];
  keys?: string;
  /** keysTitle says whose keys they are (the laptop's). */
  keysTitle?: string;
  /** tag starts a group on the right: "recently used", "other commands". */
  tag?: string;
  file?: boolean;
  /** icon is the file whose icon shows (key, when not given). */
  icon?: string;
  run: () => void;
}

function fileRows(q: string, list: readonly string[]): Row[] {
  return rank(q, list, (p) => p, MAX_ROWS).map(({ item, hit }) => {
    const cut = item.lastIndexOf("/") + 1;
    return {
      key: item,
      label: item.slice(cut),
      at: hit.at.filter((i) => i >= cut).map((i) => i - cut),
      detail: item.slice(0, Math.max(0, cut - 1)),
      detailAt: hit.at.filter((i) => i < cut - 1),
      file: true,
      run: () => showFile(item),
    };
  });
}

/** PICK_MS is the pause in typing before a picker asks again. */
const PICK_MS = 150;

/** pickRows: a picker's items that match q (its own list), or the rows
 * it loaded for what is typed (in the order they came). */
function pickRows(q: string, pk: Pick, loaded: readonly PickItem[] | null): Row[] {
  const row = (it: PickItem, i: number, at: number[]): Row => ({ key: String(i) + "#" + it.label, label: it.label, at, detail: it.detail, file: it.file !== undefined, icon: it.file, run: it.run });
  if (pk.items) return rank(q, pk.items, (it) => it.label, MAX_ROWS).map(({ item, hit }, i) => row(item, i, hit.at));
  return (loaded ?? []).slice(0, MAX_ROWS).map((it, i) => row(it, i, []));
}

/** commandRows: the commands that match q, the recently used ones first
 * (newest first), then the rest by how well they match. */
function commandRows(q: string): Row[] {
  const list = allCommands().filter((c) => !c.when || c.when());
  const recent = recentCmds.value;
  const ranked = rank(q, list, cmdTitle, list.length);
  const at = (c: Command) => recent.indexOf(c.id);
  // A command whose whole name was typed comes first: Enter runs it, not
  // a recently used one that also holds those letters.
  const whole = q.trim().toLowerCase();
  const exact = whole ? ranked.filter((x) => cmdTitle(x.item).toLowerCase() === whole) : [];
  const others = ranked.filter((x) => !exact.includes(x));
  const used = others.filter((x) => at(x.item) >= 0).sort((a, b) => at(a.item) - at(b.item));
  const rest = others.filter((x) => at(x.item) < 0);
  const u0 = exact.length, u1 = u0 + used.length;
  const rows = [...exact, ...used, ...rest].slice(0, MAX_ROWS).map(({ item, hit }: { item: Command; hit: Hit }, i): Row => ({
    key: item.id,
    label: cmdTitle(item),
    at: hit.at,
    detail: item.laptop ? t("quick.laptop") : undefined,
    keys: item.keys,
    keysTitle: item.laptop ? t("quick.laptop_keys") : undefined,
    tag: used.length && i === u0 ? t("quick.recent") : used.length && i === u1 ? t("quick.other") : undefined,
    run: () => {
      noteRun(item.id);
      item.run();
    },
  }));
  return rows;
}

export function QuickInput() {
  const q0 = quick.value;
  const prefix = q0?.mode === "commands" ? ">" : q0?.mode === "line" ? ":" : "";
  const [text, setText] = useState(prefix);
  const [sel, setSel] = useState(0);
  const [list, setList] = useState<string[] | null>(files?.list ?? null);
  const [note, setNote] = useState("");
  const box = useRef<HTMLInputElement | null>(null);

  const ask = q0?.mode === "ask" ? (q0.ask ?? null) : null;
  const pk = q0?.mode === "pick" ? (q0.pick ?? null) : null;
  const [loaded, setLoaded] = useState<{ q: string; items: readonly PickItem[] } | null>(null);
  const wantFiles = !ask && !pk && !text.startsWith(">") && !text.startsWith(":");
  const wantCmds = text.startsWith(">");
  useLayoutEffect(() => box.current?.focus(), []);
  useEffect(() => {
    if (wantCmds) loadLaptopCmds();
  }, [wantCmds]);
  useEffect(() => {
    if (!wantFiles || (files && Date.now() - files.at < FILES_TTL_MS)) return;
    link.request("files.list").then(
      (data) => {
        const res = data as FilesListRes;
        files = { list: res.paths, at: Date.now() };
        setList(res.paths);
      },
      (e: unknown) => setNote(e instanceof Error ? e.message : ""),
    );
  }, [wantFiles]);

  // A picker that asks for what is typed: once typing pauses.
  useEffect(() => {
    const load = pk?.load;
    if (!load) return;
    let stop = false;
    const timer = setTimeout(
      () =>
        load(text).then(
          (items) => !stop && setLoaded({ q: text, items }),
          (e: unknown) => !stop && (setLoaded({ q: text, items: [] }), setNote(e instanceof Error ? e.message : "")),
        ),
      PICK_MS,
    );
    return () => {
      stop = true;
      clearTimeout(timer);
    };
  }, [pk, text]);

  const close = () => (quick.value = null);
  // rowsOf is the list for a box text: Enter reads the box itself, so a
  // key typed just before it counts even when the list has not drawn yet.
  const rowsOf = (text: string): Row[] => {
    if (pk) return pickRows(text, pk, loaded?.items ?? null);
    if (ask) {
      // A question (Edit with AI): Enter sends what was typed.
      return text.trim() ? [{ key: "ask", label: text.trim(), at: [], detail: t("quick.ask_go"), run: () => ask.run(text.trim()) }] : [];
    }
    if (text.startsWith(">")) return commandRows(text.slice(1));
    if (text.startsWith(":")) {
      const n = parseInt(text.slice(1), 10);
      return Number.isFinite(n) && n > 0 ? [{ key: "line", label: t("quick.line_go", { n }), at: [], run: () => gotoLine(n) }] : [];
    }
    return list ? fileRows(text, list) : [];
  };
  const rows = rowsOf(text);
  const placeholder = pk ? pk.title : ask ? ask.title : text.startsWith(">") ? t("quick.commands") : text.startsWith(":") ? t("quick.line") : t("quick.files");
  const cur = Math.min(sel, Math.max(0, rows.length - 1));
  const pick = (r: Row | undefined) => {
    if (!r) return;
    close();
    r.run();
  };
  return (
    <div class="quick-layer" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div class="quick" role="dialog" aria-label={placeholder}>
        <input
          ref={box}
          class="quick-box"
          type="text"
          role="combobox"
          aria-expanded="true"
          aria-controls="quick-list"
          aria-activedescendant={rows.length ? `quick-${cur}` : undefined}
          spellcheck={false}
          autocomplete="off"
          placeholder={placeholder}
          value={text}
          onInput={(e) => {
            setText((e.currentTarget as HTMLInputElement).value);
            setSel(0);
          }}
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              e.preventDefault();
              close();
            } else if (e.key === "ArrowDown") {
              e.preventDefault();
              setSel(Math.min(cur + 1, rows.length - 1));
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              setSel(Math.max(cur - 1, 0));
            } else if (e.key === "Enter") {
              e.preventDefault();
              const now = e.currentTarget.value;
              pick(now === text ? rows[cur] : rowsOf(now)[0]);
            }
          }}
        />
        <ul class="quick-list" id="quick-list" role="listbox">
          {rows.map((r, i) => (
            <li
              key={r.key}
              id={`quick-${i}`}
              role="option"
              aria-selected={i === cur ? "true" : "false"}
              class={`quick-row${i === cur ? " is-on" : ""}`}
              onMouseDown={(e) => {
                e.preventDefault();
                pick(r);
              }}
              onMouseMove={() => i !== cur && setSel(i)}
            >
              {r.file && <FileIcon path={r.icon ?? r.key} />}
              <span class="quick-label" dir="auto">
                <Marked text={r.label} at={r.at} />
              </span>
              {r.detail && (
                <span class="quick-detail" dir="auto">
                  <Marked text={r.detail} at={r.detailAt ?? []} />
                </span>
              )}
              {r.keys && (
                <kbd class="quick-keys" title={r.keysTitle}>
                  {r.keys}
                </kbd>
              )}
              {r.tag && <span class="quick-tag">{r.tag}</span>}
            </li>
          ))}
          {!rows.length && (
            <li class="quick-empty" role="presentation">
              {ask ? t("quick.ask_type") : pk ? note || (pk.load && loaded?.q !== text ? t("quick.loading") : t("quick.none")) : note || (list === null && !text.startsWith(">") && !text.startsWith(":") ? t("quick.loading") : t("quick.none"))}
            </li>
          )}
        </ul>
      </div>
    </div>
  );
}
