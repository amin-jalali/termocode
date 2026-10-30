// Search (WEB-24, web UX 5.8): a query box with Regex / Case / Word
// chips and optional file filters; the hits stream in under a count line
// ("12 files · 48 matches · 0.4 s"), grouped by file, the matches marked.
// A hit opens the file at its line. One search at a time: a new one, or
// Stop, stops the old. Offline the box is locked. #/more/search?q=…
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { go, route } from "../../lib/router";
import { link, live } from "../../session";
import { byFile, readBatch, readSummary, seconds, segments, type SearchHit, type SearchSummary } from "../../state/search";
import { Button } from "../../ui/Button";
import { t } from "../../strings";
import { gitFailed as failed } from "../git/errors";
import { SubView } from "./SubView";

interface Run {
  id: number;
  hits: SearchHit[];
  done: SearchSummary | null;
  started: number;
}

function Chip({ on, label, set }: { on: boolean; label: string; set: (v: boolean) => void }) {
  return (
    <button type="button" class={`chip-toggle${on ? " is-on" : ""}`} aria-pressed={on ? "true" : "false"} onClick={() => set(!on)}>
      {label}
    </button>
  );
}

export function SearchView() {
  const [query, setQuery] = useState(route.value.query.q ?? "");
  const [regex, setRegex] = useState(false);
  const [cs, setCase] = useState(false);
  const [word, setWord] = useState(false);
  const [filters, setFilters] = useState(false);
  const [include, setInclude] = useState("");
  const [exclude, setExclude] = useState("");
  const [run, setRun] = useState<Run | null>(null);
  const cur = useRef(0); // the id whose batches count

  useEffect(
    () =>
      link.onEvent((kind, data) => {
        if (kind !== "search.batch") return;
        const b = readBatch(data);
        if (b.id !== cur.current) return;
        setRun((r) => (r && r.id === b.id ? { ...r, hits: [...r.hits, ...b.hits] } : r));
      }),
    [],
  );

  const start = () => {
    const q = query;
    if (!q.trim() || !live.value) return;
    go({ tab: "more", rest: "search", query: { q } }, true);
    const args: Record<string, unknown> = { query: q };
    if (regex) args.regex = true;
    if (cs) args.case = true;
    if (word) args.word = true;
    if (include.trim()) args.include = include.trim();
    if (exclude.trim()) args.exclude = exclude.trim();
    const { id, result } = link.requestWithID("search.run", args);
    cur.current = id;
    setRun({ id, hits: [], done: null, started: Date.now() });
    result.then(
      (d) => setRun((r) => (r && r.id === id ? { ...r, done: readSummary(d) } : r)),
      (e: unknown) => {
        if (cur.current !== id) return;
        setRun(null);
        failed(e);
      },
    );
  };

  const stop = () => {
    link.request("search.stop").catch(failed);
  };

  const open = (h: SearchHit) => {
    link.request("search.open", { path: h.path, line: h.line }).then(
      () => go({ tab: "files", rest: h.path, query: { line: String(h.line) } }),
      failed,
    );
  };

  const groups = run ? byFile(run.hits) : [];
  const d = run?.done;
  let note = "";
  if (d) {
    if (d.stopped === "timeout") note = t("search.timeout");
    else if (d.stopped === "limit") note = t("search.limit");
    else if (d.truncated && d.stopped === "") note = t("search.cap");
    else if (d.matches === 0 && d.stopped === "") note = t("search.none");
  }

  return (
    <SubView title={t("more.search")}>
      <form
        class="search-box"
        onSubmit={(e) => {
          e.preventDefault();
          start();
        }}
      >
        <div class="search-line">
          <input
            class="search-input"
            type="search"
            dir="auto"
            value={query}
            disabled={!live.value}
            aria-label={t("search.placeholder")}
            placeholder={live.value ? t("search.placeholder") : t("offline.input")}
            onInput={(e) => setQuery((e.currentTarget as HTMLInputElement).value)}
          />
          {run && !run.done ? (
            <Button kind="neutral" needsLink onClick={stop}>
              {t("stop")}
            </Button>
          ) : (
            <Button kind="primary" needsLink disabled={!query.trim()} onClick={start}>
              {t("more.search")}
            </Button>
          )}
        </div>
        <div class="search-chips">
          <Chip on={regex} label={t("search.regex")} set={setRegex} />
          <Chip on={cs} label={t("search.case")} set={setCase} />
          <Chip on={word} label={t("search.word")} set={setWord} />
          <Chip on={filters} label={t("search.filters")} set={setFilters} />
        </div>
        {filters && (
          <div class="search-filters">
            <input class="search-input" dir="ltr" value={include} aria-label={t("search.include")} placeholder={t("search.include")}
              onInput={(e) => setInclude((e.currentTarget as HTMLInputElement).value)} />
            <input class="search-input" dir="ltr" value={exclude} aria-label={t("search.exclude")} placeholder={t("search.exclude")}
              onInput={(e) => setExclude((e.currentTarget as HTMLInputElement).value)} />
          </div>
        )}
      </form>
      {run && (
        <p class="search-count" role="status">
          {t("search.count", {
            files: groups.length,
            matches: run.hits.length,
            time: seconds(d ? d.ms : Date.now() - run.started),
          })}
          {!d && <span class="search-running"> · {t("search.running")}</span>}
        </p>
      )}
      {note && <p class="search-note">{note}</p>}
      <div class="search-results">
        {groups.map((g) => (
          <section class="search-file" key={g.path}>
            <h2 class="search-path">
              <bdi dir="ltr">{g.path}</bdi>
            </h2>
            {g.hits.map((x) => (
              <button type="button" class="search-hit" key={x.line} onClick={() => open(x)}>
                <span class="search-n" dir="ltr">
                  {x.line}
                </span>
                <span class="search-text" dir="ltr">
                  {segments(x.text, x.marks).map((s, i) => (s.mark ? <mark key={i}>{s.text}</mark> : s.text))}
                </span>
              </button>
            ))}
          </section>
        ))}
      </div>
    </SubView>
  );
}
