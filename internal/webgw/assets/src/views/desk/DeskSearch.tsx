// The desktop's Search view, as the laptop's editor has it: the query
// with Match Case, Match Whole Word and Use Regular Expression inside the
// box (Alt+C, Alt+W, Alt+R), files to include and exclude behind "…",
// a line that counts the results, and the results as a tree by file:
// the file's name, its folder dim, its count; each match with the found
// text marked. A match opens the file in the editor at that place; a
// result can be dismissed; the title has Refresh, Clear and Collapse All.
// The search and its results stay while other views show.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useRef } from "preact/hooks";
import { goTo } from "../../lib/lsp";
import { link, live } from "../../session";
import { byFile, readBatch, readSummary, segments, type SearchHit, type SearchSummary } from "../../state/search";
import { t } from "../../strings";
import { Cod, FileIcon } from "../../ui/icons";
import { Act, PaneView, Row, Tree } from "../../ui/pane";
import { gitFailed as failed } from "../git/errors";

interface Run {
  id: number;
  hits: SearchHit[];
  done: SearchSummary | null;
}

const query = signal("");
const opts = signal({ cs: false, word: false, regex: false });
const details = signal(false);
const include = signal("");
const exclude = signal("");
const run = signal<Run | null>(null);
/** shut: files whose matches are folded; gone: dismissed results. */
const shut = signal<ReadonlySet<string>>(new Set());
const gone = signal<ReadonlySet<string>>(new Set());
let current = 0;

link.onEvent((kind, data) => {
  if (kind !== "search.batch") return;
  const b = readBatch(data);
  const r = run.peek();
  if (!r || b.id !== r.id || b.id !== current) return;
  run.value = { ...r, hits: [...r.hits, ...b.hits] };
});

function start(): void {
  const q = query.peek();
  if (!q.trim() || !live.peek()) return;
  const o = opts.peek();
  const args: Record<string, unknown> = { query: q };
  if (o.regex) args.regex = true;
  if (o.cs) args.case = true;
  if (o.word) args.word = true;
  if (details.peek() && include.peek().trim()) args.include = include.peek().trim();
  if (details.peek() && exclude.peek().trim()) args.exclude = exclude.peek().trim();
  const { id, result } = link.requestWithID("search.run", args);
  current = id;
  shut.value = new Set();
  gone.value = new Set();
  run.value = { id, hits: [], done: null };
  result.then(
    (d) => {
      const r = run.peek();
      if (r && r.id === id) run.value = { ...r, done: readSummary(d) };
    },
    (e: unknown) => {
      if (current !== id) return;
      run.value = null;
      failed(e);
    },
  );
}

function clear(): void {
  if (run.peek() && !run.peek()?.done) link.request("search.stop").catch(() => undefined);
  current = 0;
  run.value = null;
  query.value = "";
}

const hitKey = (x: SearchHit) => `${x.path}:${x.line}`;

function toggle(set: typeof shut, key: string): void {
  const n = new Set(set.peek());
  if (n.has(key)) n.delete(key);
  else n.add(key);
  set.value = n;
}

/** Opt is one of the toggles inside the query box. */
function Opt({ k, icon, label, keys }: { k: "cs" | "word" | "regex"; icon: string; label: string; keys: string }) {
  const on = opts.value[k];
  return (
    <button
      type="button"
      class={`act opt${on ? " is-on" : ""}`}
      data-opt={k}
      aria-pressed={on ? "true" : "false"}
      aria-label={label}
      title={`${label} (${keys})`}
      onClick={() => {
        opts.value = { ...opts.peek(), [k]: !on };
        if (query.peek().trim()) start();
      }}
    >
      <Cod name={icon} />
    </button>
  );
}

/** optKeys: Alt+C, Alt+W, Alt+R flip the toggles while the view has the focus. */
function optKeys(e: KeyboardEvent): void {
  if (!e.altKey || e.ctrlKey || e.metaKey) return;
  const k = e.code === "KeyC" ? "cs" : e.code === "KeyW" ? "word" : e.code === "KeyR" ? "regex" : null;
  if (!k) return;
  e.preventDefault();
  opts.value = { ...opts.peek(), [k]: !opts.peek()[k] };
  if (query.peek().trim()) start();
}

function Match({ hit }: { hit: SearchHit }) {
  const m = hit;
  // The line from its first non-space, with the found text marked.
  const lead = m.text.length - m.text.trimStart().length;
  const shift = Math.max(0, Math.min(lead, (m.marks[0]?.s ?? lead) - 24));
  const text = m.text.slice(shift);
  const marks = m.marks.map((m) => ({ s: m.s - shift, e: m.e - shift }));
  return (
    <Row
      level={1}
      twist={null}
      label={
        <span class="sr-line" dir="ltr">
          {segments(text, marks).map((s, i) => (s.mark ? <mark key={i} class="sr-mark">{s.text}</mark> : s.text))}
        </span>
      }
      title={`${m.path}:${m.line}`}
      data={{ path: m.path, line: String(m.line) }}
      onClick={() => goTo(m.path, Math.max(0, m.line - 1), Math.max(0, m.marks[0]?.s ?? 0))}
      actions={<Act icon="close" label={t("sv.dismiss")} onClick={() => toggle(gone, hitKey(m))} />}
    />
  );
}

export function DeskSearch() {
  const box = useRef<HTMLInputElement | null>(null);
  useEffect(() => box.current?.focus(), []);
  const r = run.value;
  const g = gone.value;
  const groups = r ? byFile(r.hits.filter((x) => !g.has(hitKey(x)) && !g.has(x.path))) : [];
  const n = groups.reduce((a, x) => a + x.hits.length, 0);
  const d = r?.done;
  let note = "";
  if (r && d) {
    if (n === 0 && d.stopped === "") note = t("search.none");
    else note = t(groups.length === 1 ? (n === 1 ? "sv.count_one" : "sv.count_file") : "sv.count", { n, files: groups.length });
    if (d.stopped === "timeout") note += " · " + t("search.timeout");
    else if (d.stopped === "limit" || d.truncated) note += " · " + t("search.limit");
  }
  const allShut = groups.length > 0 && groups.every((x) => shut.value.has(x.path));
  return (
    <PaneView
      id="search"
      title={t("more.search")}
      actions={
        <>
          <Act icon="refresh" label={t("ctr.refresh")} disabled={!query.value.trim()} onClick={start} />
          <Act icon="clear-all" label={t("sv.clear")} disabled={!r && !query.value} onClick={clear} />
          <Act
            icon={allShut ? "expand-all" : "collapse-all"}
            label={t(allShut ? "sv.expand" : "sv.collapse")}
            disabled={groups.length === 0}
            onClick={() => (shut.value = allShut ? new Set() : new Set(groups.map((x) => x.path)))}
          />
        </>
      }
    >
      <div class="sv-form" onKeyDown={optKeys}>
        <div class="pv-input sv-query">
          <input
            ref={box}
            type="text"
            dir="auto"
            spellcheck={false}
            value={query.value}
            disabled={!live.value}
            aria-label={t("more.search")}
            placeholder={live.value ? t("more.search") : t("offline.input")}
            onInput={(e) => (query.value = (e.currentTarget as HTMLInputElement).value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                start();
              }
            }}
          />
          <span class="opts">
            <Opt k="cs" icon="case-sensitive" label={t("sv.case")} keys="Alt+C" />
            <Opt k="word" icon="whole-word" label={t("sv.word")} keys="Alt+W" />
            <Opt k="regex" icon="regex" label={t("sv.regex")} keys="Alt+R" />
          </span>
        </div>
        <div class="sv-more">
          <Act icon="ellipsis" label={t("sv.details")} on={details.value} act="details" onClick={() => (details.value = !details.peek())} />
        </div>
        {details.value && (
          <>
            <label class="pv-label" for="sv-inc">
              {t("search.include")}
            </label>
            <div class="pv-input">
              <input id="sv-inc" dir="ltr" spellcheck={false} value={include.value} onInput={(e) => (include.value = (e.currentTarget as HTMLInputElement).value)} onKeyDown={(e) => e.key === "Enter" && start()} />
            </div>
            <label class="pv-label" for="sv-exc">
              {t("search.exclude")}
            </label>
            <div class="pv-input">
              <input id="sv-exc" dir="ltr" spellcheck={false} value={exclude.value} onInput={(e) => (exclude.value = (e.currentTarget as HTMLInputElement).value)} onKeyDown={(e) => e.key === "Enter" && start()} />
            </div>
          </>
        )}
      </div>
      {r && (
        <p class="sv-note" role="status">
          {!d ? (
            <span>
              <Cod name="loading" /> {t("search.running")}
              {" · "}
              <button type="button" class="link-btn" onClick={() => link.request("search.stop").catch(failed)}>
                {t("stop")}
              </button>
            </span>
          ) : (
            note
          )}
        </p>
      )}
      <div class="pv-scroll">
        <Tree label={t("sv.results")}>
          {groups.map((x) => {
            const open = !shut.value.has(x.path);
            const i = x.path.lastIndexOf("/");
            return (
              <Fragment key={x.path}>
                <Row
                  level={0}
                  twist={open ? "open" : "closed"}
                  onToggle={() => toggle(shut, x.path)}
                  icon={<FileIcon path={x.path} />}
                  label={x.path.slice(i + 1)}
                  desc={i > 0 ? x.path.slice(0, i) : ""}
                  title={x.path}
                  data={{ file: x.path }}
                  end={<span class="count">{x.hits.length}</span>}
                  actions={<Act icon="close" label={t("sv.dismiss")} onClick={() => toggle(gone, x.path)} />}
                />
                {open && x.hits.map((one) => <Match key={hitKey(one)} hit={one} />)}
              </Fragment>
            );
          })}
        </Tree>
      </div>
    </PaneView>
  );
}
