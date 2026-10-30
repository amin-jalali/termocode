// The desktop's Databases view, a tree as the laptop editor's database
// tools draw one: connections (driver and database dim, production and
// read-only marked), their schema, Tables and Views with counts, and each
// table or view. A table opens its rows in an editor tab; New Query on a
// connection opens a query editor tab. Everything here reads; a statement
// that may change data asks the laptop (in the query tab). No host, user
// or password ever comes to the page.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect } from "preact/hooks";
import { dbTab, sqlTab } from "../../lib/vtabs";
import { showFile } from "../../lib/workbench";
import { link, live } from "../../session";
import { readDBConns, readDBTables, type DBConnView } from "../../state/more";
import { t } from "../../strings";
import { errText } from "../../ui/CardSheet";
import { Cod } from "../../ui/icons";
import { Act, PaneView, Row, Tree } from "../../ui/pane";

type Tables = ReturnType<typeof readDBTables>;
type Load<T> = { s: "loading" } | { s: "ok"; v: T } | { s: "err"; msg: string };

const conns = signal<Load<DBConnView[]> | null>(null);
const tables = signal<ReadonlyMap<string, Load<Tables>>>(new Map());
/** open: the tree nodes shown open (conn, conn/schema, conn/schema/kind). */
const open = signal<ReadonlySet<string>>(new Set());

function loadConns(): void {
  conns.value = { s: "loading" };
  tables.value = new Map();
  link.request("db.conns", {}).then(
    (r) => (conns.value = { s: "ok", v: readDBConns(r) }),
    (e: unknown) => (conns.value = { s: "err", msg: errText(e) }),
  );
}

function loadTables(conn: string): void {
  const set = (l: Load<Tables>) => {
    const n = new Map(tables.peek());
    n.set(conn, l);
    tables.value = n;
  };
  set({ s: "loading" });
  link.request("db.tables", { conn }).then(
    (r) => set({ s: "ok", v: readDBTables(r) }),
    (e: unknown) => set({ s: "err", msg: errText(e) }),
  );
}

function flip(key: string, conn?: string): void {
  const n = new Set(open.peek());
  if (n.has(key)) n.delete(key);
  else {
    n.add(key);
    if (conn && !tables.peek().has(conn)) loadTables(conn);
  }
  open.value = n;
}

function ConnRows({ c }: { c: DBConnView }) {
  const key = c.id;
  const isOpen = open.value.has(key) && c.sql;
  const tl = tables.value.get(c.id);
  const desc = [c.driver, c.database].filter(Boolean).join(" · ");
  return (
    <>
      <Row
        level={0}
        twist={c.sql ? (isOpen ? "open" : "closed") : null}
        onToggle={() => flip(key, c.id)}
        icon={<Cod name="database" />}
        label={c.name || c.id}
        desc={desc}
        title={[c.name || c.id, desc, c.env, c.readonly ? t("db.readonly") : "", c.problem || (!c.sql ? t("db.not_sql") : "")].filter(Boolean).join(" — ")}
        data={{ conn: c.id }}
        end={
          <>
            {c.env && <span class={`db-env${c.env === "production" ? " is-prod" : ""}`}>{c.env}</span>}
            {c.readonly && (
              <span class="c-dim" title={t("db.readonly")}>
                <Cod name="lock" />
              </span>
            )}
            {c.problem && (
              <span class="c-warn" title={c.problem}>
                <Cod name="warning" />
              </span>
            )}
          </>
        }
        actions={c.sql && <Act icon="new-file" label={t("dbv.new_query")} onClick={() => showFile(sqlTab(c.id))} />}
      />
      {isOpen && !tl && <p class="tr-note">{t("loading")}</p>}
      {isOpen && tl?.s === "loading" && <p class="tr-note">{t("loading")}</p>}
      {isOpen && tl?.s === "err" && <p class="tr-note is-err">{tl.msg}</p>}
      {isOpen && tl?.s === "ok" && <SchemaRows conn={c.id} v={tl.v} />}
    </>
  );
}

function SchemaRows({ conn, v }: { conn: string; v: Tables }) {
  const sk = conn + "/" + (v.schema || "-");
  const base = v.schema ? 2 : 1;
  const kinds: { id: "t" | "v"; label: string; items: Tables["tables"] }[] = [
    { id: "t", label: t("db.tables"), items: v.tables.filter((x) => !x.view) },
    { id: "v", label: t("tabs.label"), items: v.tables.filter((x) => x.view) },
  ];
  const schemaOpen = !open.value.has(sk + "!");
  return (
    <>
      {v.schema && (
        <Row level={1} twist={schemaOpen ? "open" : "closed"} onToggle={() => flip(sk + "!")} icon={<Cod name="symbol-namespace" />} label={v.schema} data={{ schema: v.schema }} />
      )}
      {schemaOpen &&
        kinds.map((k) => {
          if (k.items.length === 0 && k.id === "v") return null;
          const kk = sk + "/" + k.id;
          const kOpen = !open.value.has(kk + "!");
          return (
            <Fragment key={k.id}>
              <Row level={base} twist={kOpen ? "open" : "closed"} onToggle={() => flip(kk + "!")} icon={<Cod name="folder" />} label={k.label} data={{ kind: k.id }} end={<span class="count">{k.items.length}</span>} />
              {kOpen && k.items.length === 0 && <p class="tr-note">{t("db.no_tables")}</p>}
              {kOpen &&
                k.items.map((x) => (
                  <Row
                    key={x.name}
                    level={base + 1}
                    twist={null}
                    icon={<Cod name={x.view ? "eye" : "table"} />}
                    label={x.name}
                    title={`${v.schema ? v.schema + "." : ""}${x.name}`}
                    data={{ table: x.name }}
                    onClick={() => showFile(dbTab(conn, x.name), true)}
                    onDblClick={() => showFile(dbTab(conn, x.name))}
                  />
                ))}
            </Fragment>
          );
        })}
      {v.more > 0 && <p class="tr-note">{t("dbv.more", { n: v.more })}</p>}
    </>
  );
}

export function DeskDB() {
  const on = live.value;
  useEffect(() => {
    if (on && (!conns.peek() || conns.peek()?.s === "err")) loadConns();
  }, [on]);
  const c = conns.value;
  return (
    <PaneView id="db" title={t("more.db")} actions={<Act icon="refresh" label={t("ctr.refresh")} disabled={!on} onClick={loadConns} />}>
      <div class="pv-scroll">
        {!c || c.s === "loading" ? (
          <p class="pv-msg">{t("loading")}</p>
        ) : c.s === "err" ? (
          <p class="pv-msg c-err">{c.msg}</p>
        ) : c.v.length === 0 ? (
          <p class="pv-msg">{t("db.none")}</p>
        ) : (
          <Tree label={t("more.db")}>
            {c.v.map((x) => (
              <ConnRows key={x.id} c={x} />
            ))}
          </Tree>
        )}
        <p class="pv-msg">{t("dbv.readonly_note")}</p>
      </div>
    </PaneView>
  );
}
