// The database editor tabs (desktop): a table's rows, a query editor, a
// table's DDL, a schema compare's script and the SQL history, with the
// results as a data grid like the laptop editor's: the header stays on
// top, columns resize by dragging their edge (a double click fits the
// text), numbers sit right, NULL is dim and secret values come masked
// (••••). A table with a key can be edited: a double click edits a cell,
// rows are added and deleted, and the pending changes (marked) are
// reviewed on the laptop's card before they are saved; Revert drops
// them. A query runs as a read here; a plan comes from Explain (Explain
// Analyze runs a read); a statement that may change data asks the
// laptop, which shows the exact SQL. Results can be pinned and exported
// (what the grid shows: masked values stay masked).
import { Fragment, h, type ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { Fmt } from "../../aictx/export";
import { aiGrid, withAI } from "../../lib/aictx";
import { mountDesk, setDesk, type DeskHandle } from "../../lib/deskeditor";
import { sqlTab } from "../../lib/vtabs";
import { modKey } from "../../lib/platform";
import { saveBlob } from "../../lib/transfer";
import { showFile, wordWrap } from "../../lib/workbench";
import { ReqError } from "../../net/link";
import { link, live } from "../../session";
import { readDBRows, type DBCellView, type DBRowsView } from "../../state/more";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { openMenu } from "../../ui/ContextMenu";
import { Cod } from "../../ui/icons";
import { Act, Ask } from "../../ui/pane";
import { Portal } from "../../ui/Portal";
import type { DBHistory, DBPlan } from "../../../../../webproto/proto";
import { gitFailed as failed } from "../git/errors";
import { canAct, connView, dbErr, pushed, pushQuery, queries, runNext, setQuery } from "./dbstate";

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

// ── Export ──────────────────────────────────────────────────────────────

/** exportMenu offers the formats under the button (or at x, y). */
function exportMenu(e: MouseEvent | { x: number; y: number }, r: DBRowsView, name: string): void {
  const b = "currentTarget" in e ? (e.currentTarget as HTMLElement).getBoundingClientRect() : { left: e.x, bottom: e.y };
  const save = (f: Fmt) => withAI((m) => saveBlob(new Blob([m.exportText(r, f)], { type: "text/plain" }), name + "." + f));
  openMenu(b.left, b.bottom, t("dbv.export"), [
    { label: "CSV", run: () => save("csv") },
    { label: "JSON", run: () => save("json") },
    { label: "Markdown", run: () => save("md") },
  ]);
}

/** exportNow is Export Result… of the command list (the open tab's grid). */
export let exportNow: ((e: MouseEvent | { x: number; y: number }) => void) | null = null;

// ── Grid ────────────────────────────────────────────────────────────────

/** Edit is a table's pending changes: cells set (by "row:col"), rows
 * deleted, rows added (null: not set, the column's default). */
interface Edit {
  set: ReadonlyMap<string, DBCellView>;
  del: ReadonlySet<number>;
  add: readonly (DBCellView | null)[][];
}
const NO_EDIT: Edit = { set: new Map(), del: new Set(), add: [] };
const editCount = (e: Edit) => new Set([...e.set.keys()].map((k) => k.split(":")[0])).size + e.del.size + e.add.length;

interface GridEdit {
  e: Edit;
  sel: number;
  /** can: the cell (row i, column j; i past the rows: an added row) is editable. */
  can(i: number, j: number): boolean;
  onSet(i: number, j: number, c: DBCellView): void;
  onSel(i: number): void;
}

function EditCell({ v, done }: { v: string; done: (v: string | null) => void }) {
  const box = useRef<HTMLInputElement | null>(null);
  useEffect(() => (box.current?.focus(), box.current?.select()), []);
  return (
    <input
      ref={box}
      class="dbg-in"
      dir="ltr"
      value={v}
      onKeyDown={(e) => {
        if (e.key === "Enter") done((e.currentTarget as HTMLInputElement).value);
        else if (e.key === "Escape") (e.stopPropagation(), done(null));
      }}
      onBlur={(e) => done((e.currentTarget as HTMLInputElement).value)}
    />
  );
}

/** DBGrid draws rows as a grid; from is the first row's number - 1. */
/** ai: the grid is a table's rows (Add to AI Chat on a right click). */
export function DBGrid({ r, from = 0, edit, ai }: { r: DBRowsView; from?: number; edit?: GridEdit; ai?: { conn: string; table: string } }) {
  const [widths, setWidths] = useState<number[]>([]);
  const [at, setAt] = useState<string | null>(null);
  const drag = useRef<{ j: number; x: number; w: number } | null>(null);
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
  const rows: (DBCellView | null)[][] = [...r.rows, ...(edit?.e.add ?? [])];
  return (
    <div class="dbg" role="region" aria-label={t("dbv.results")} tabIndex={0}>
      <table class="dbg-table" onContextMenu={ai && aiGrid({ ...ai, r, from })} style={{ width: 48 + r.columns.reduce((a, _, j) => a + widthOf(j), 0) + "px" }}>
        <colgroup>
          <col style={{ width: "48px" }} />
          {r.columns.map((_, j) => (
            <col key={j} style={{ width: widthOf(j) + "px" }} />
          ))}
        </colgroup>
        <thead>
          <tr>
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
          {rows.map((row, i) => {
            const added = i >= r.rows.length;
            const cls = `${edit?.e.del.has(i) ? "is-del" : added ? "is-new" : ""}${edit?.sel === i ? " is-sel" : ""}`;
            return (
              <tr key={i} class={cls || undefined} data-row={i}>
                <td class="dbg-n" onClick={edit && (() => edit.onSel(i))}>
                  {added ? "+" : from + i + 1}
                </td>
                {r.columns.map((_, j) => {
                  const k = i + ":" + j;
                  const c = edit?.e.set.get(k) ?? row[j];
                  const changed = !!edit?.e.set.get(k);
                  if (at === k && edit)
                    return (
                      <td key={j} class="is-edit">
                        <EditCell
                          v={c && !c.n ? c.v : ""}
                          done={(v) => {
                            setAt(null);
                            if (v !== null && (!c || c.n || v !== c.v)) edit.onSet(i, j, { v, n: false, m: false, c: false });
                          }}
                        />
                      </td>
                    );
                  return (
                    <td
                      key={j}
                      class={`${!c ? "is-unset" : c.n ? "is-null" : c.m ? "is-masked" : ""}${nums[j] ? " is-num" : ""}${changed ? " is-changed" : ""}`}
                      dir="ltr"
                      title={!c || c.n || c.m ? undefined : c.v}
                      onDblClick={edit && edit.can(i, j) ? () => setAt(k) : undefined}
                    >
                      {!c ? "" : c.n ? "NULL" : c.m ? "••••" : c.c ? c.v + "…" : c.v}
                    </td>
                  );
                })}
              </tr>
            );
          })}
        </tbody>
      </table>
      {rows.length === 0 && <p class="dbg-empty">{t("db.no_rows")}</p>}
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

// ── A table's rows ──────────────────────────────────────────────────────

/** editRequest is db.edit's args for the pending changes. */
function editRequest(conn: string, table: string, r: DBRowsView, e: Edit): unknown {
  const val = (col: string, c: DBCellView) => (c.n ? { col, n: true } : { col, v: c.v });
  const keyOf = (row: DBCellView[]) => r.key.map((k) => val(k, row[r.columns.indexOf(k)]));
  const edits: unknown[] = [];
  e.del.forEach((i) => edits.push({ kind: "delete", key: keyOf(r.rows[i]) }));
  r.rows.forEach((row, i) => {
    if (e.del.has(i)) return;
    const set: unknown[] = [];
    const old: unknown[] = [];
    r.columns.forEach((col, j) => {
      const c = e.set.get(i + ":" + j);
      if (!c) return;
      set.push(val(col, c));
      if (!row[j].m && !row[j].c) old.push(val(col, row[j]));
    });
    if (set.length) edits.push({ kind: "update", key: keyOf(row), set, old: old.length ? old : undefined });
  });
  e.add.forEach((row, a) => {
    const set: unknown[] = [];
    r.columns.forEach((col, j) => {
      const c = e.set.get(r.rows.length + a + ":" + j) ?? row[j];
      if (c) set.push(val(col, c));
    });
    if (set.length) edits.push({ kind: "insert", set });
  });
  return { conn, table, edits };
}

/** pendingOf is the open table tab's pending changes (commands). */
export let pendingOf: { submit(): void; revert(): void; n: number } | null = null;

/** DBTableTab is a table's rows, 100 at a time, editable with a key. */
export function DBTableTab({ conn, table, slot }: { conn: string; table: string; slot: string }) {
  const [from, setFrom] = useState(0);
  // Cassandra pages by cursor (no OFFSET): the cursors of the pages after
  // the first, so Previous goes back one.
  const [cursors, setCursors] = useState<string[]>([]);
  const [n, setN] = useState(0);
  const [res, setRes] = useState<{ r: DBRowsView | null; err: string }>({ r: null, err: "" });
  const [e, setE] = useState<Edit>(NO_EDIT);
  const [sel, setSel] = useState(-1);
  const [step, setStep] = useState<"" | "revert" | "laptop">("");
  const on = live.value;
  useEffect(() => {
    if (!on) return;
    let stop = false;
    setRes((x) => ({ r: x.r, err: "" }));
    link.request("db.rows", { conn, table, from, cursor: cursors[cursors.length - 1] || undefined }).then(
      (d) => !stop && (setRes({ r: readDBRows(d), err: "" }), setE(NO_EDIT), setSel(-1)),
      (err: unknown) => !stop && setRes({ r: null, err: dbErr(err) }),
    );
    return () => {
      stop = true;
    };
  }, [conn, table, from, cursors, n, on]);
  const r = res.r;
  const cv = connView(conn);
  const cql = cv?.driver === "cassandra";
  const start = cql ? cursors.length * 100 : from;
  const editable = !!r && r.key.length > 0 && canAct() && !cv?.readonly;
  const dirty = editCount(e);
  const keyCol = (j: number) => !!r && r.key.includes(r.columns[j]);
  const grid: GridEdit | undefined =
    editable && r
      ? {
          e,
          sel,
          can: (i, j) => {
            if (e.del.has(i)) return false;
            if (i >= r.rows.length) return true;
            const c = r.rows[i][j];
            return !c.m && !c.c && !(keyCol(j) && r.rows[i].some((x, jj) => keyCol(jj) && x.m));
          },
          onSet: (i, j, c) => {
            const set = new Map(e.set);
            set.set(i + ":" + j, c);
            setE({ ...e, set });
          },
          onSel: (i) => setSel(sel === i ? -1 : i),
        }
      : undefined;
  const delRow = () => {
    if (!r || sel < 0) return;
    if (sel >= r.rows.length) {
      const a = sel - r.rows.length;
      const set = new Map([...e.set].filter(([k]) => +k.split(":")[0] < r.rows.length));
      setE({ ...e, set, add: e.add.filter((_, x) => x !== a) });
      setSel(-1);
      return;
    }
    const del = new Set(e.del);
    if (del.has(sel)) del.delete(sel);
    else del.add(sel);
    setE({ ...e, del });
  };
  const submit = () => dirty > 0 && live.peek() && setStep("laptop");
  const revert = () => dirty > 0 && setStep("revert");
  pendingOf = editable ? { submit, revert, n: dirty } : null;
  useEffect(
    () => () => {
      pendingOf = null;
    },
    [],
  );
  exportNow = r ? (ev) => exportMenu(ev, r, table) : null;
  return (
    <section class="view dbv-tab" data-view="db-table" aria-label={table}>
      <Portal into={"ed-actions-" + slot}>
        <div class="view-actions">
          {editable && (
            <>
              <Act icon="add" label={t("dbv.add_row")} onClick={() => (setE({ ...e, add: [...e.add, r.columns.map(() => null)] }), setSel(r.rows.length + e.add.length))} />
              <Act icon="remove" label={t("dbv.del_row")} disabled={sel < 0} onClick={delRow} />
              <Act icon="discard" act="revert" label={t("dbv.revert")} disabled={dirty === 0} onClick={revert} />
              <Act icon="check" act="submit" label={t("dbv.submit")} disabled={dirty === 0 || !on} onClick={submit} />
            </>
          )}
          <Act icon="export" label={t("dbv.export")} disabled={!r} onClick={(ev) => r && exportMenu(ev, r, table)} />
          <Act icon="refresh" label={t("ctr.refresh")} disabled={!on || dirty > 0} onClick={() => setN(n + 1)} />
          <Act icon="new-file" label={t("dbv.new_query")} onClick={() => showFile(sqlTab(conn))} />
        </div>
      </Portal>
      <div class="dbv-crumbs" dir="ltr">
        <Cod name="database" /> <span>{conn}</span> <span class="crumb-sep">›</span> <Cod name="table" /> <span>{table}</span>
        {dirty > 0 ? (
          <span class="dbv-ro is-dirty">{t("dbv.pending", { n: dirty })}</span>
        ) : (
          <span class="dbv-ro">
            <Cod name={editable ? "edit" : "lock"} /> {editable ? t("dbv.editable") : r && r.key.length === 0 && !cql ? t("dbv.no_key") : t("db.readonly")}
          </span>
        )}
      </div>
      {step === "revert" && <Ask text={t("dbv.revert_q", { n: dirty })} verb={t("dbv.revert")} danger onYes={() => (setE(NO_EDIT), setSel(-1), setStep(""))} onNo={() => setStep("")} />}
      {step === "laptop" && r && (
        <AskLaptop
          label={t("dbv.submit")}
          text={t("dbv.submit_q", { n: dirty, table, conn })}
          send={() => link.request("db.edit", editRequest(conn, table, r, e))}
          onDone={() => setStep("")}
          onConfirmed={() => setTimeout(() => setN((x) => x + 1), 300)}
        />
      )}
      {res.err ? (
        <p class="dbg-empty c-err">{res.err}</p>
      ) : !r ? (
        <p class="dbg-empty">{t("loading")}</p>
      ) : (
        <>
          <DBGrid r={r} from={start} edit={grid} ai={{ conn, table }} />
          <Meta r={r}>
            <span class="dbg-pages">
              <Act icon="chevron-left" label={t("db.prev_rows")} disabled={start === 0 || dirty > 0} onClick={() => (cql ? setCursors(cursors.slice(0, -1)) : setFrom(Math.max(0, from - 100)))} />
              <span>{t("dbv.page", { a: r.rows.length ? start + 1 : 0, b: start + r.rows.length })}</span>
              <Act icon="chevron-right" label={t("db.next_rows")} disabled={(cql ? r.cursor === "" : r.next === 0) || dirty > 0} onClick={() => (cql ? setCursors([...cursors, r.cursor]) : setFrom(r.next))} />
            </span>
          </Meta>
        </>
      )}
    </section>
  );
}

// ── A query ─────────────────────────────────────────────────────────────

type Out = { kind: "rows"; r: DBRowsView } | { kind: "plan"; p: DBPlan };

function PlanView({ p }: { p: DBPlan }) {
  return (
    <div class="dbp" role="tree" aria-label={t("dbv.plan")} tabIndex={0}>
      {(p.lines ?? []).map((l, i) => (
        <div key={i} role="treeitem" class={`dbp-line${l.detail ? " is-detail" : ""}${l.costly ? " is-costly" : ""}`} style={{ paddingInlineStart: 14 + l.depth * 16 + "px" }} dir="ltr">
          {!l.detail && <Cod name={l.costly ? "warning" : "circle-outline"} />} {l.text}
        </div>
      ))}
      {(p.summary ?? []).map((s, i) => (
        <div key={"s" + i} class="dbp-sum" dir="ltr">
          {s}
        </div>
      ))}
    </div>
  );
}

/** queryRun is the open query tab's actions (commands). */
export let queryRun: { pin(): void } | null = null;

/** DBQueryTab is a query editor on a connection and its results. */
export function DBQueryTab({ conn, slot }: { conn: string; slot: string }) {
  const box = useRef<HTMLDivElement | null>(null);
  const ed = useRef<DeskHandle | null>(null);
  const [step, setStep] = useState<"" | "ask" | "analyze" | "laptop">("");
  const [busy, setBusy] = useState(false);
  const [ran, setRan] = useState("");
  const [res, setRes] = useState<{ o: Out | null; err: string }>({ o: null, err: "" });
  const [pins, setPins] = useState<DBRowsView[]>([]);
  const [shown, setShown] = useState(-1); // -1: the last result; else a pin
  const [sql, setSQL] = useState(queries.peek().get(conn) ?? "");
  const canQuery = canAct();
  useEffect(() => {
    if (!box.current) return;
    const h0 = mountDesk(box.current, {
      doc: queries.peek().get(conn) ?? "",
      label: t("dbv.query_tab", { conn }),
      slot,
      lang: "sql",
      readOnly: !canQuery,
      wrap: wordWrap.peek(),
      tabSize: 2,
      insertSpaces: true,
      onChange: () => {
        const v = h0.text();
        setQuery(conn, v);
        setSQL(v);
      },
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
  // A text set elsewhere (History, Run File) replaces the editor's; Run
  // File asks to run it.
  const push = pushed.value;
  useEffect(() => {
    const h0 = ed.current;
    if (h0 && push?.conn === conn) {
      pushed.value = null;
      if (h0.text() !== push.sql) h0.replace(push.sql);
      setSQL(push.sql);
    }
    if (runNext.peek() === conn && (queries.peek().get(conn) ?? "").trim()) {
      runNext.value = null;
      setStep("ask");
    }
  }, [push, runNext.value]);
  // Ctrl/Cmd+Enter (the editor passes it on) runs the query.
  useEffect(() => {
    const k = (e: KeyboardEvent) => {
      if (e.key === "Enter" && (e.ctrlKey || e.metaKey) && box.current?.isConnected) {
        e.preventDefault();
        ask("ask");
      }
    };
    addEventListener("keydown", k);
    return () => removeEventListener("keydown", k);
  });
  const text = () => ed.current?.text() ?? sql;
  const ask = (s: "ask" | "analyze") => {
    if (!canQuery || !live.peek() || text().trim() === "" || busy) return;
    setStep(s);
  };
  const go = (op: string, args: unknown, plan: boolean) => {
    setStep("");
    setBusy(true);
    setShown(-1);
    link
      .request(op, args)
      .then(
        (d) => setRes({ o: plan ? { kind: "plan", p: d as DBPlan } : { kind: "rows", r: readDBRows(d) }, err: "" }),
        (e: unknown) => {
          if (e instanceof ReqError && e.code === "forbidden" && !plan) setStep("laptop");
          else {
            setRes({ o: null, err: dbErr(e) });
            failed(e);
          }
        },
      )
      .finally(() => setBusy(false));
  };
  const run = () => {
    setRan(text());
    go("db.query", { conn, sql: text() }, false);
  };
  // Cassandra: the rest of the rows the last run read, by its cursor.
  const more = (cursor: string) => go("db.query", { conn, sql: ran, cursor }, false);
  const cql = connView(conn)?.driver === "cassandra";
  const explain = (analyze: boolean) => go("db.explain", { conn, sql: text(), analyze: analyze || undefined }, true);
  const last = res.o?.kind === "rows" ? res.o.r : null;
  const view: Out | null = shown >= 0 && pins[shown] ? { kind: "rows", r: pins[shown] } : res.o;
  const pin = () => last && !pins.includes(last) && (setPins([...pins, last]), setShown(pins.length));
  queryRun = { pin };
  useEffect(
    () => () => {
      queryRun = null;
    },
    [],
  );
  const rows = view?.kind === "rows" ? view.r : null;
  exportNow = rows ? (ev) => exportMenu(ev, rows, "result") : null;
  return (
    <section class="view dbv-tab dbq" data-view="db-query" aria-label={t("dbv.query_tab", { conn })}>
      <Portal into={"ed-actions-" + slot}>
        <div class="view-actions">
          {canQuery && <Act icon="play" act="run-query" label={t("db.run")} keys={`${modKey()}+Enter`} disabled={!live.value || busy || sql.trim() === ""} onClick={() => ask("ask")} />}
          {canQuery && !cql && <Act icon="list-tree" act="explain" label={t("dbv.explain")} disabled={!live.value || busy || sql.trim() === ""} onClick={() => explain(false)} />}
          {canQuery && !cql && <Act icon="pulse" act="analyze" label={t("dbv.analyze")} disabled={!live.value || busy || sql.trim() === ""} onClick={() => ask("analyze")} />}
          {busy && <Act icon="debug-stop" act="cancel" label={t("dbv.cancel_q")} onClick={() => link.request("db.cancel", { conn }).catch(failed)} />}
          <Act icon="pin" act="pin" label={t("dbv.pin")} disabled={!last || pins.includes(last)} onClick={pin} />
          <Act icon="export" act="export" label={t("dbv.export")} disabled={!rows} onClick={(ev) => rows && exportMenu(ev, rows, "result")} />
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
      {step === "analyze" && <Ask text={t("dbv.analyze_q", { conn })} verb={t("dbv.analyze")} onYes={() => explain(true)} onNo={() => setStep("")} />}
      {step === "laptop" && <AskLaptop label={t("db.query")} text={t("db.exec_q", { conn })} send={() => link.request("db.exec", { conn, sql: text() })} onDone={() => setStep("")} />}
      <div class="dbq-results">
        <div class="dbq-title" role="tablist" aria-label={t("dbv.results")}>
          <button type="button" role="tab" aria-selected={shown < 0} class={`dbq-tab${shown < 0 ? " is-on" : ""}`} onClick={() => setShown(-1)}>
            {view?.kind === "plan" && shown < 0 ? t("dbv.plan") : t("dbv.results")}
          </button>
          {pins.map((_, i) => (
            <span key={i} class={`dbq-tab${shown === i ? " is-on" : ""}`}>
              <button type="button" role="tab" aria-selected={shown === i} class="dbq-pin" onClick={() => setShown(i)}>
                <Cod name="pin" /> {t("dbv.pinned", { n: i + 1 })}
              </button>
              <Act icon="close" label={t("cmd.close_editor")} onClick={() => (setPins(pins.filter((_, x) => x !== i)), setShown(-1))} />
            </span>
          ))}
        </div>
        {busy && shown < 0 ? (
          <p class="dbg-empty">{t("loading")}</p>
        ) : res.err && shown < 0 ? (
          <p class="dbg-empty c-err">{res.err}</p>
        ) : view?.kind === "plan" ? (
          <PlanView p={view.p} />
        ) : view ? (
          <>
            <DBGrid r={view.r} />
            <Meta r={view.r}>
              {shown < 0 && view.r.cursor !== "" && <Act icon="chevron-right" label={t("db.next_rows")} disabled={busy || !live.value} onClick={() => more(view.r.cursor)} />}
            </Meta>
          </>
        ) : (
          <p class="dbg-empty">{t("dbv.no_results", { keys: `${modKey()}+Enter` })}</p>
        )}
      </div>
    </section>
  );
}

// ── Text tabs: a table's DDL, a schema compare ──────────────────────────

/** DBTextTab shows a read-only SQL text the laptop gives (DDL, a compare). */
export function DBTextTab({ kind, conn, table, slot }: { kind: "ddl" | "scmp"; conn: string; table: string; slot: string }) {
  const box = useRef<HTMLDivElement | null>(null);
  const [st, setSt] = useState<{ text: string | null; err: string; note: string }>({ text: null, err: "", note: "" });
  const on = live.value;
  useEffect(() => {
    if (!on) return;
    const req =
      kind === "ddl"
        ? link.request("db.ddl", { conn, table }).then((d) => ({ text: (d as { text?: string }).text ?? "", note: "" }))
        : link.request("db.compare", { conn, target: table }).then((d) => {
            const c = d as { script?: string; changes?: number; destructive?: number; truncated?: boolean };
            return { text: c.script ?? "", note: t("dbv.cmp_note", { n: c.changes ?? 0, d: c.destructive ?? 0 }) + (c.truncated ? " · " + t("db.cut") : "") };
          });
    req.then(
      (v) => setSt({ ...v, err: "" }),
      (e: unknown) => setSt({ text: null, err: dbErr(e), note: "" }),
    );
  }, [kind, conn, table, on]);
  useEffect(() => {
    if (!box.current || st.text === null) return;
    const h0 = mountDesk(box.current, {
      doc: st.text,
      label: kind === "ddl" ? t("dbv.ddl_tab", { table }) : t("dbv.compare"),
      slot,
      lang: "sql",
      readOnly: true,
      wrap: false,
      tabSize: 2,
      insertSpaces: true,
      onChange: () => undefined,
      onSave: () => undefined,
      onDone: () => undefined,
      onViewport: () => undefined,
    });
    setDesk(slot, h0);
    return () => {
      setDesk(slot, null);
      h0.destroy();
    };
  }, [st.text]);
  return (
    <section class="view dbv-tab" data-view={kind === "ddl" ? "db-ddl" : "db-compare"} aria-label={kind === "ddl" ? t("dbv.ddl_tab", { table }) : t("dbv.compare")}>
      <div class="dbv-crumbs" dir="ltr">
        <Cod name="database" /> <span>{conn}</span> <span class="crumb-sep">{kind === "ddl" ? "›" : "↔"}</span> <span>{table}</span>
        {st.note && <span class="dbv-hint">{st.note}</span>}
      </div>
      {st.err ? <p class="dbg-empty c-err">{st.err}</p> : st.text === null ? <p class="dbg-empty">{t("loading")}</p> : <div ref={box} class="editor-host desk-host dbt-host" />}
    </section>
  );
}

// ── The SQL history ─────────────────────────────────────────────────────

/** DBHistoryTab lists the SQL runs, newest first; one opens in its
 * connection's query tab (running it asks as usual). */
export function DBHistoryTab() {
  const [h, setH] = useState<{ v: DBHistory | null; err: string }>({ v: null, err: "" });
  const on = live.value;
  useEffect(() => {
    if (on)
      link.request("db.history", {}).then(
        (d) => setH({ v: d as DBHistory, err: "" }),
        (e: unknown) => setH({ v: null, err: dbErr(e) }),
      );
  }, [on]);
  const list = h.v?.entries ?? [];
  return (
    <section class="view dbv-tab" data-view="db-history" aria-label={t("dbv.history")}>
      {h.err ? (
        <p class="dbg-empty c-err">{h.err}</p>
      ) : !h.v ? (
        <p class="dbg-empty">{t("loading")}</p>
      ) : list.length === 0 ? (
        <p class="dbg-empty">{t("dbv.no_history")}</p>
      ) : (
        <div class="dbh" role="list">
          {list.map((e, i) => (
            <button
              key={i}
              type="button"
              role="listitem"
              class="dbh-row"
              onClick={() => {
                pushQuery(e.conn, e.sql);
                showFile(sqlTab(e.conn));
              }}
            >
              <span class="dbh-sql" dir="ltr">
                {e.sql.replace(/\s+/g, " ")}
              </span>
              <span class="dbh-meta" dir="ltr">
                {[e.conn, new Date(e.at).toLocaleString(), e.ms ? e.ms + " ms" : ""].filter(Boolean).join(" · ")}
              </span>
            </button>
          ))}
        </div>
      )}
    </section>
  );
}

