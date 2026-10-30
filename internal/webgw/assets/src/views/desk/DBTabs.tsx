// The database editor tabs (desktop): a table's rows and a query editor,
// with the results as a data grid like the laptop editor's: the header
// stays on top, columns resize by dragging their edge (a double click
// fits the text), numbers sit right, NULL is dim and secret values come
// masked (••••). Everything reads: a statement that may change data asks
// the laptop, which shows the exact SQL. #/files/@db:…, #/files/@sql:…
import { Fragment, h, type ComponentChildren } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useRef, useState } from "preact/hooks";
import { mountDesk, setDesk, type DeskHandle } from "../../lib/deskeditor";
import { sqlTab } from "../../lib/vtabs";
import { modKey } from "../../lib/platform";
import { showFile, wordWrap } from "../../lib/workbench";
import { ReqError } from "../../net/link";
import { link, live } from "../../session";
import { readDBRows, type DBRowsView } from "../../state/more";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { errText } from "../../ui/CardSheet";
import { Cod } from "../../ui/icons";
import { Act, Ask } from "../../ui/pane";
import { Portal } from "../../ui/Portal";
import { gitFailed as failed } from "../git/errors";

const NUM = /^-?\d+(\.\d+)?([eE][-+]?\d+)?$/;
const MIN_W = 48;
const MAX_W = 640;

/** numeric: every value of column j that is there is a number. */
function numeric(r: DBRowsView, j: number): boolean {
  let any = false;
  for (const row of r.rows) {
    const c = row[j];
    if (!c || c.n || c.m) continue;
    if (!NUM.test(c.v)) return false;
    any = true;
  }
  return any;
}

/** DBGrid draws rows as a grid; from is the first row's number - 1. */
export function DBGrid({ r, from = 0 }: { r: DBRowsView; from?: number }) {
  const [widths, setWidths] = useState<number[]>([]);
  const drag = useRef<{ j: number; x: number; w: number } | null>(null);
  const head = useRef<HTMLTableRowElement | null>(null);
  if (r.columns.length === 0) return <p class="dbg-empty">{t("db.no_rows")}</p>;
  const nums = r.columns.map((_, j) => numeric(r, j));
  const widthOf = (j: number) => widths[j] ?? Math.min(280, Math.max(80, r.columns[j].length * 8 + 32));
  const setW = (j: number, w: number) => {
    const n = r.columns.map((_, i) => widthOf(i));
    n[j] = Math.round(Math.min(MAX_W, Math.max(MIN_W, w)));
    setWidths(n);
  };
  const fit = (j: number) => {
    let max = r.columns[j].length;
    for (const row of r.rows) max = Math.max(max, (row[j]?.v ?? "").length);
    setW(j, max * 7.5 + 24);
  };
  return (
    <div class="dbg" role="region" aria-label={t("dbv.results")} tabIndex={0}>
      <table class="dbg-table" style={{ width: 48 + r.columns.reduce((a, _, j) => a + widthOf(j), 0) + "px" }}>
        <colgroup>
          <col style={{ width: "48px" }} />
          {r.columns.map((_, j) => (
            <col key={j} style={{ width: widthOf(j) + "px" }} />
          ))}
        </colgroup>
        <thead>
          <tr ref={head}>
            <th class="dbg-n" scope="col" aria-label={t("dbv.row")} />
            {r.columns.map((c, j) => (
              <th key={j} scope="col" class={nums[j] ? "is-num" : undefined} title={c} dir="ltr">
                <span class="dbg-h">{c}</span>
                <span
                  class="dbg-resize"
                  role="separator"
                  aria-orientation="vertical"
                  aria-label={t("dbv.resize", { col: c })}
                  onPointerDown={(e) => {
                    (e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
                    drag.current = { j, x: e.clientX, w: widthOf(j) };
                  }}
                  onPointerMove={(e) => {
                    const d = drag.current;
                    if (d) setW(d.j, d.w + e.clientX - d.x);
                  }}
                  onPointerUp={() => (drag.current = null)}
                  onDblClick={() => fit(j)}
                />
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {r.rows.map((row, i) => (
            <tr key={i}>
              <td class="dbg-n">{from + i + 1}</td>
              {row.map((c, j) => (
                <td key={j} class={`${c.n ? "is-null" : c.m ? "is-masked" : ""}${nums[j] ? " is-num" : ""}`} dir="ltr" title={c.n || c.m ? undefined : c.v}>
                  {c.n ? "NULL" : c.m ? "••••" : c.c ? c.v + "…" : c.v}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {r.rows.length === 0 && <p class="dbg-empty">{t("db.no_rows")}</p>}
    </div>
  );
}

/** Meta is the line under a grid: rows, time, hidden values, cut. */
function Meta({ r, children }: { r: DBRowsView; children?: ComponentChildren }) {
  return (
    <div class="dbg-meta" role="status">
      <span>
        {t("db.rows_meta", { n: r.rows.length, ms: r.ms })}
        {r.masked > 0 && ` · ${t("db.masked", { n: r.masked })}`}
        {r.truncated && ` · ${t("db.cut")}`}
      </span>
      {children}
    </div>
  );
}

/** DBTableTab is a table's rows, 100 at a time. */
export function DBTableTab({ conn, table, slot }: { conn: string; table: string; slot: string }) {
  const [from, setFrom] = useState(0);
  const [n, setN] = useState(0);
  const [res, setRes] = useState<{ r: DBRowsView | null; err: string }>({ r: null, err: "" });
  const on = live.value;
  useEffect(() => {
    if (!on) return;
    let stop = false;
    setRes((x) => ({ r: x.r, err: "" }));
    link.request("db.rows", { conn, table, from }).then(
      (d) => !stop && setRes({ r: readDBRows(d), err: "" }),
      (e: unknown) => !stop && setRes({ r: null, err: errText(e) }),
    );
    return () => {
      stop = true;
    };
  }, [conn, table, from, n, on]);
  const r = res.r;
  return (
    <section class="view dbv-tab" data-view="db-table" aria-label={table}>
      <Portal into={"ed-actions-" + slot}>
        <div class="view-actions">
          <Act icon="refresh" label={t("ctr.refresh")} disabled={!on} onClick={() => setN(n + 1)} />
          <Act icon="new-file" label={t("dbv.new_query")} onClick={() => showFile(sqlTab(conn))} />
        </div>
      </Portal>
      <div class="dbv-crumbs" dir="ltr">
        <Cod name="database" /> <span>{conn}</span> <span class="crumb-sep">›</span> <Cod name="table" /> <span>{table}</span>
        <span class="dbv-ro">
          <Cod name="lock" /> {t("db.readonly")}
        </span>
      </div>
      {res.err ? (
        <p class="dbg-empty c-err">{res.err}</p>
      ) : !r ? (
        <p class="dbg-empty">{t("loading")}</p>
      ) : (
        <>
          <DBGrid r={r} from={from} />
          <Meta r={r}>
            <span class="dbg-pages">
              <Act icon="chevron-left" label={t("db.prev_rows")} disabled={from === 0} onClick={() => setFrom(Math.max(0, from - 100))} />
              <span>{t("dbv.page", { a: r.rows.length ? from + 1 : 0, b: from + r.rows.length })}</span>
              <Act icon="chevron-right" label={t("db.next_rows")} disabled={r.next === 0} onClick={() => setFrom(r.next)} />
            </span>
          </Meta>
        </>
      )}
    </section>
  );
}

/** queries keeps each connection's query text while its tab is closed. */
const queries = signal<ReadonlyMap<string, string>>(new Map());

/** DBQueryTab is a query editor on a connection and its results. */
export function DBQueryTab({ conn, slot }: { conn: string; slot: string }) {
  const box = useRef<HTMLDivElement | null>(null);
  const ed = useRef<DeskHandle | null>(null);
  const [step, setStep] = useState<"" | "ask" | "laptop">("");
  const [busy, setBusy] = useState(false);
  const [res, setRes] = useState<{ r: DBRowsView | null; err: string }>({ r: null, err: "" });
  const [sql, setSQL] = useState(queries.peek().get(conn) ?? "");
  const canQuery = link.meta.value?.role !== "viewer";
  useEffect(() => {
    if (!box.current) return;
    const keep = (v: string) => {
      const n = new Map(queries.peek());
      n.set(conn, v);
      queries.value = n;
      setSQL(v);
    };
    const h0 = mountDesk(box.current, {
      doc: queries.peek().get(conn) ?? "",
      label: t("dbv.query_tab", { conn }),
      slot,
      lang: "sql",
      readOnly: !canQuery,
      wrap: wordWrap.peek(),
      tabSize: 2,
      insertSpaces: true,
      onChange: () => keep(h0.text()),
      onSave: () => undefined,
      onDone: () => undefined,
      onViewport: () => undefined,
    });
    ed.current = h0;
    setDesk(slot, h0);
    h0.focus();
    return () => {
      setDesk(slot, null);
      h0.destroy();
      ed.current = null;
    };
  }, [conn]);
  // Ctrl/Cmd+Enter (the editor passes it on) runs the query.
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      if (e.key === "Enter" && (e.ctrlKey || e.metaKey) && box.current?.isConnected) {
        e.preventDefault();
        ask();
      }
    };
    addEventListener("keydown", k);
    return () => removeEventListener("keydown", k);
  });
  const text = () => ed.current?.text() ?? sql;
  const ask = () => {
    if (!canQuery || !live.peek() || text().trim() === "" || busy) return;
    setStep("ask");
  };
  const run = () => {
    setStep("");
    setBusy(true);
    link
      .request("db.query", { conn, sql: text() })
      .then(
        (d) => setRes({ r: readDBRows(d), err: "" }),
        (e: unknown) => {
          if (e instanceof ReqError && e.code === "forbidden") setStep("laptop");
          else {
            setRes({ r: null, err: errText(e) });
            failed(e);
          }
        },
      )
      .finally(() => setBusy(false));
  };
  return (
    <section class="view dbv-tab dbq" data-view="db-query" aria-label={t("dbv.query_tab", { conn })}>
      <Portal into={"ed-actions-" + slot}>
        <div class="view-actions">
          {canQuery && <Act icon="play" act="run-query" label={t("db.run")} keys={`${modKey()}+Enter`} disabled={!live.value || busy || sql.trim() === ""} onClick={ask} />}
        </div>
      </Portal>
      <div class="dbv-crumbs" dir="ltr">
        <Cod name="database" /> <span>{conn}</span>
        <span class="dbv-hint">{t("db.query_hint")}</span>
      </div>
      <div class="dbq-editor">
        <div ref={box} class="editor-host desk-host" />
      </div>
      {step === "ask" && <Ask text={t("db.run_q", { conn })} verb={t("db.run")} onYes={run} onNo={() => setStep("")} />}
      {step === "laptop" && <AskLaptop label={t("db.query")} text={t("db.exec_q", { conn })} send={() => link.request("db.exec", { conn, sql: text() })} onDone={() => setStep("")} />}
      <div class="dbq-results">
        <div class="dbq-title">{t("dbv.results")}</div>
        {busy ? (
          <p class="dbg-empty">{t("loading")}</p>
        ) : res.err ? (
          <p class="dbg-empty c-err">{res.err}</p>
        ) : res.r ? (
          <>
            <DBGrid r={res.r} />
            <Meta r={res.r} />
          </>
        ) : (
          <p class="dbg-empty">{t("dbv.no_results", { keys: `${modKey()}+Enter` })}</p>
        )}
      </div>
    </section>
  );
}

