// Databases (WEB-43, web design 2.9): the laptop's connections, the
// tables of one and their rows, read in a read-only session. Secret
// values come masked from the laptop. A typed statement runs as a read
// after an in-page confirm; one that may change data asks the laptop,
// which shows the exact SQL. No password is ever sent or shown.
// #/more/db, #/more/db/<conn>, #/more/db/<conn>/<table>
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { go } from "../../lib/router";
import { ReqError } from "../../net/link";
import { link, live } from "../../session";
import { readDBConns, readDBRows, readDBTables, type DBConnView, type DBRowsView } from "../../state/more";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { Button } from "../../ui/Button";
import { SkeletonRows } from "../../ui/Skeleton";
import { gitFailed as failed } from "../git/errors";
import { SubView } from "./SubView";

const to = (rest: string) => go({ tab: "more", rest, query: {} });

/** useAsk runs a request once per key and keeps its answer or its error. */
function useAsk<T>(op: string, args: Record<string, unknown>, read: (r: unknown) => T, key: string): { data: T | null; err: string } {
  const [data, setData] = useState<T | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    setData(null);
    setErr("");
    link.request(op, args).then(
      (r) => setData(read(r)),
      (e: unknown) => setErr(e instanceof Error ? e.message : t("err.internal")),
    );
  }, [key]);
  return { data, err };
}

function Conns() {
  const { data, err } = useAsk("db.conns", {}, readDBConns, "conns");
  if (err) return <p class="empty refused">{err}</p>;
  if (!data) return <SkeletonRows rows={4} />;
  if (data.length === 0) return <p class="empty">{t("db.none")}</p>;
  return (
    <div class="db-list">
      {data.map((c: DBConnView) => (
        <button type="button" class="db-conn" key={c.id} data-conn={c.id} disabled={!c.sql} onClick={() => to("db/" + c.id)}>
          <span class="db-name" dir="auto">
            {c.name || c.id}
          </span>
          <span class="db-meta" dir="ltr">
            {c.driver}
            {c.database && ` · ${c.database}`}
          </span>
          <span class="db-chips">
            {c.env && <span class={`db-chip${c.env === "production" ? " is-prod" : ""}`}>{c.env}</span>}
            {c.readonly && <span class="db-chip">{t("db.readonly")}</span>}
          </span>
          {c.problem ? (
            <span class="db-note" dir="auto">
              {c.problem}
            </span>
          ) : (
            !c.sql && <span class="db-note">{t("db.not_sql")}</span>
          )}
        </button>
      ))}
    </div>
  );
}

function RowsTable({ r }: { r: DBRowsView }) {
  if (r.columns.length === 0 || r.rows.length === 0) return <p class="empty">{t("db.no_rows")}</p>;
  return (
    <div class="db-grid-box">
      <table class="db-grid">
        <thead>
          <tr>
            {r.columns.map((c, i) => (
              <th key={i} scope="col" dir="ltr">
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {r.rows.map((row, i) => (
            <tr key={i}>
              {row.map((c, j) => (
                <td key={j} class={c.n ? "is-null" : c.m ? "is-masked" : undefined} dir="ltr">
                  {c.n ? "NULL" : c.c ? c.v + "…" : c.v}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function RowsNotes({ r }: { r: DBRowsView }) {
  return (
    <p class="view-note" dir="auto">
      {t("db.rows_meta", { n: r.rows.length, ms: r.ms })}
      {r.masked > 0 && ` · ${t("db.masked", { n: r.masked })}`}
      {r.truncated && ` · ${t("db.cut")}`}
    </p>
  );
}

function Query({ conn }: { conn: string }) {
  const [sql, setSQL] = useState("");
  const [step, setStep] = useState<"" | "ask" | "laptop">("");
  const [res, setRes] = useState<DBRowsView | null>(null);
  const safe = useRef<HTMLButtonElement | null>(null);
  useEffect(() => {
    if (step === "ask") safe.current?.focus();
  }, [step]);
  const run = () => {
    setStep("");
    link.request("db.query", { conn, sql }).then(
      (r) => setRes(readDBRows(r)),
      (e: unknown) => {
        if (e instanceof ReqError && e.code === "forbidden") setStep("laptop");
        else failed(e);
      },
    );
  };
  return (
    <section class="group db-query" aria-labelledby="db-q">
      <h2 class="group-title" id="db-q">
        {t("db.query")}
      </h2>
      <p class="view-note">{t("db.query_hint")}</p>
      <textarea
        class="db-sql"
        dir="ltr"
        rows={3}
        spellcheck={false}
        autocapitalize="off"
        aria-label={t("db.query")}
        value={sql}
        disabled={!live.value}
        onInput={(e) => setSQL((e.currentTarget as HTMLTextAreaElement).value)}
      />
      {step === "ask" ? (
        <div class="row-ask" role="group" aria-label={t("db.run_q", { conn })}>
          <p class="row-ask-text">{t("db.run_q", { conn })}</p>
          <div class="choices">
            <button type="button" class="btn btn-neutral" ref={safe} onClick={() => setStep("")}>
              {t("cancel")}
            </button>
            <Button kind="primary" needsLink onClick={run}>
              {t("db.run")}
            </Button>
          </div>
        </div>
      ) : (
        <div class="choices">
          <Button kind="primary" needsLink disabled={sql.trim() === ""} onClick={() => setStep("ask")}>
            {t("db.run")}
          </Button>
        </div>
      )}
      {step === "laptop" && (
        <AskLaptop label={t("db.query")} text={t("db.exec_q", { conn })} send={() => link.request("db.exec", { conn, sql })} onDone={() => setStep("")} />
      )}
      {res && (
        <div class="db-result">
          <RowsNotes r={res} />
          <RowsTable r={res} />
        </div>
      )}
    </section>
  );
}

function Tables({ conn }: { conn: string }) {
  const { data, err } = useAsk("db.tables", { conn }, readDBTables, "t/" + conn);
  const canQuery = link.meta.value?.role !== "viewer";
  return (
    <div class="dbv-body">
      {err ? (
        <p class="empty refused" dir="auto">
          {err}
        </p>
      ) : !data ? (
        <SkeletonRows rows={6} />
      ) : (
        <section class="group" aria-labelledby="db-t">
          <h2 class="group-title" id="db-t">
            {t("db.tables")}
            {data.schema && <span class="git-n" dir="ltr">{` · ${data.schema}`}</span>}
          </h2>
          {data.tables.length === 0 ? (
            <p class="empty">{t("db.no_tables")}</p>
          ) : (
            <div class="db-list">
              {data.tables.map((x) => (
                <button type="button" class="db-table" key={x.name} data-table={x.name} dir="ltr" onClick={() => to("db/" + conn + "/" + x.name)}>
                  <span class="db-name">{x.name}</span>
                  {x.view && <span class="db-chip">{t("db.view")}</span>}
                </button>
              ))}
            </div>
          )}
        </section>
      )}
      {canQuery && <Query conn={conn} />}
    </div>
  );
}

function Rows({ conn, table }: { conn: string; table: string }) {
  const [from, setFrom] = useState(0);
  const { data, err } = useAsk("db.rows", { conn, table, from }, readDBRows, conn + "/" + table + "@" + from);
  if (err) return <p class="empty refused">{err}</p>;
  if (!data) return <SkeletonRows rows={8} />;
  return (
    <div class="db-result">
      <RowsNotes r={data} />
      <RowsTable r={data} />
      <div class="choices">
        {from > 0 && (
          <Button small needsLink onClick={() => setFrom(Math.max(0, from - 100))}>
            {t("db.prev_rows")}
          </Button>
        )}
        {data.next > 0 && (
          <Button small needsLink onClick={() => setFrom(data.next)}>
            {t("db.next_rows")}
          </Button>
        )}
      </div>
    </div>
  );
}

export function DBView({ rest }: { rest: string }) {
  const parts = rest.split("/");
  const conn = parts[1] ?? "";
  const table = parts.slice(2).join("/");
  const title = table || conn || t("more.db");
  let body = <Conns />;
  if (conn && table) body = <Rows conn={conn} table={table} />;
  else if (conn) body = <Tables conn={conn} />;
  return (
    <SubView title={title}>
      <div class="dbv">
        {conn && (
          <button type="button" class="sub-back" onClick={() => to(table ? "db/" + conn : "db")}>
            ‹ {table ? conn : t("more.db")}
          </button>
        )}
        {body}
      </div>
    </SubView>
  );
}
