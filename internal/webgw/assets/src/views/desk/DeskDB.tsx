// The desktop's Databases view, a tree as the laptop editor's database
// tools draw one: connections (driver and database dim, production and
// read-only marked, a dot when the laptop is connected, the open
// transaction's changes), their schema, Tables and Views with counts,
// each table or view, and a Redis connection's keys (names, types and
// TTLs; never a value). A table opens its rows in an editor tab; New
// Query opens a query editor tab; the "…" menu (or a right click) has
// the rest. Reads run here; what changes a database or a saved
// connection asks the laptop, which shows exactly what happens. No
// connection's host, user or password ever comes to the page. SERVERS is
// the hosts tree (the ssh:// hosts as "name · where", their compose
// projects and database containers): picking one adds it and opens it.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect } from "preact/hooks";
import { DBH_TAB, dbTab, ddlTab, sqlTab } from "../../lib/vtabs";
import { toast } from "../../lib/toasts";
import { showFile } from "../../lib/workbench";
import { link, live } from "../../session";
import { readDBTables, type DBConnView } from "../../state/more";
import { t } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { onMenu, openMenu, type MenuItems } from "../../ui/ContextMenu";
import { Cod } from "../../ui/icons";
import { Act, PaneView, Row, Tree } from "../../ui/pane";
import type { RedisKeys } from "../../../../../webproto/proto";
import { DBFormView } from "./DBForms";
import { DBNeedAsk } from "./DBNeed";
import { AddServerRow, HostRows, termAsk } from "./DeskHosts";
import { hosts, loadHosts, openNext, pickDB, type HostCtr, type HostView } from "../../state/hosts";
import { canAct, conns, dbErr, dbFilter, dbForm, dbNeed, loadConns, tables, type Load } from "./dbstate";

type Tables = ReturnType<typeof readDBTables>;

/** open: the tree nodes shown open (conn, conn/schema, conn/schema/kind). */
const open = signal<ReadonlySet<string>>(new Set());
/** laptop is the question for the laptop a menu item asked. */
const laptop = signal<{ label: string; op: string; args: unknown; text?: string } | null>(null);

function loadChildren(c: DBConnView): void {
  const set = (l: Load<Tables | RedisKeys>) => {
    const n = new Map(tables.peek());
    n.set(c.id, l);
    tables.value = n;
  };
  set({ s: "loading" });
  const redis = c.driver === "redis";
  link.request(redis ? "redis.keys" : "db.tables", { conn: c.id }).then(
    (r) => set({ s: "ok", v: redis ? (r as RedisKeys) : readDBTables(r) }),
    (e: unknown) => set({ s: "err", msg: dbErr(e), need: dbNeed(e) }),
  );
}

/** dropNeed keeps the reason of conn's failed read and drops its question. */
function dropNeed(id: string): void {
  const l = tables.peek().get(id);
  if (l?.s !== "err") return;
  const n = new Map(tables.peek());
  n.set(id, { s: "err", msg: l.msg });
  tables.value = n;
}

function refresh(): void {
  tables.value = new Map();
  loadConns();
}

function flip(key: string, c?: DBConnView): void {
  const n = new Set(open.peek());
  if (n.has(key)) n.delete(key);
  else {
    n.add(key);
    if (c && (!tables.peek().has(c.id) || tables.peek().get(c.id)?.s === "err")) loadChildren(c); // a failed read is tried again
  }
  open.value = n;
}

/** ask asks the laptop (a menu item's change). */
const ask = (label: string, op: string, args: unknown) => (laptop.value = { label, op, args });

/** read runs a read and tells its one line. */
function read(op: string, args: unknown): void {
  link.request(op, args).then(
    (r) => toast("ok", (r as { text?: string }).text ?? t("wait.done")),
    (e: unknown) => toast("warn", dbErr(e)),
  );
}

function connItems(c: DBConnView): MenuItems {
  const act = canAct();
  const sql = c.sql;
  return [
    { label: t("dbv.new_query"), disabled: !sql, run: () => showFile(sqlTab(c.id)) },
    c.connected
      ? { label: t("dbv.disconnect"), disabled: !act, run: () => ask(t("dbv.disconnect"), "db.disconnect", { conn: c.id }) }
      : { label: t("dbv.connect"), disabled: !act, run: () => read("db.connect", { conn: c.id }) },
    { label: t("dbv.test"), disabled: !act || !sql, run: () => read("db.test", { conn: c.id }) },
    null,
    { label: t("dbv.commit"), disabled: !act || !c.tx, run: () => ask(t("dbv.commit"), "db.tx", { conn: c.id, verb: "commit" }) },
    { label: t("dbv.rollback"), disabled: !act || !c.tx, run: () => ask(t("dbv.rollback"), "db.tx", { conn: c.id, verb: "rollback" }) },
    { label: t("dbv.tx_mode"), disabled: !act || !sql, run: () => (dbForm.value = { kind: "tx", conn: c.id }) },
    null,
    { label: t("dbv.compare"), disabled: !sql, run: () => (dbForm.value = { kind: "compare", conn: c.id }) },
    { label: t("dbv.ssh"), disabled: !act || c.driver === "sqlite", run: () => (dbForm.value = { kind: "ssh", conn: c.id }) },
    { label: t("dbv.ssl"), disabled: !act || c.driver === "sqlite", run: () => (dbForm.value = { kind: "ssl", conn: c.id }) },
  ];
}

function tableItems(c: DBConnView, name: string, view: boolean): MenuItems {
  return [
    { label: t("dbv.open_rows"), run: () => showFile(dbTab(c.id, name)) },
    { label: t("dbv.ddl"), run: () => showFile(ddlTab(c.id, name)) },
    { label: t("dbv.import"), disabled: !canAct() || view || c.readonly, run: () => (dbForm.value = { kind: "import", conn: c.id, table: name }) },
  ];
}

function keyItems(c: DBConnView, db: number, key: string): MenuItems {
  const act = canAct() && !c.readonly;
  return [
    { label: t("dbv.ttl"), disabled: !act, run: () => (dbForm.value = { kind: "ttl", conn: c.id, db, key }) },
    { label: t("dbv.del_key"), disabled: !act, run: () => ask(t("dbv.del_key"), "redis.key", { conn: c.id, db: db || undefined, key, verb: "delete" }) },
  ];
}

const match = (s: string): boolean => {
  const f = dbFilter.value?.trim().toLowerCase();
  return !f || s.toLowerCase().includes(f);
};

function ConnRows({ c }: { c: DBConnView }) {
  const key = c.id;
  const opens = c.sql || c.driver === "redis";
  const isOpen = open.value.has(key) && opens;
  const tl = tables.value.get(c.id);
  const desc = [c.driver, c.database].filter(Boolean).join(" · ");
  const more = (e: MouseEvent) => {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    openMenu(r.left, r.bottom, t("scm.more_actions"), connItems(c));
  };
  return (
    <>
      <Row
        level={0}
        twist={opens ? (isOpen ? "open" : "closed") : null}
        onToggle={() => flip(key, c)}
        icon={<Cod name="database" />}
        label={c.name || c.id}
        desc={desc}
        title={[c.name || c.id, desc, c.env, c.readonly ? t("db.readonly") : "", c.connected ? t("dbv.connected") : "", c.problem || (!opens ? t("db.not_sql") : "")].filter(Boolean).join(" — ")}
        data={{ conn: c.id }}
        ai={{ item: "db_conn", conn: c.id }}
        onContextMenu={onMenu(t("scm.more_actions"), () => connItems(c))}
        end={
          <>
            {c.tx && <span class="db-tx">{t("dbv.pending", { n: c.writes })}</span>}
            {c.connected && <span class="db-on" title={t("dbv.connected")} />}
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
        actions={
          <>
            {c.sql && <Act icon="new-file" label={t("dbv.new_query")} onClick={() => showFile(sqlTab(c.id))} />}
            <Act icon="ellipsis" label={t("scm.more_actions")} onClick={more} />
          </>
        }
      />
      {isOpen && (!tl || tl.s === "loading") && <p class="tr-note">{t("loading")}</p>}
      {isOpen && tl?.s === "err" && (tl.need ? <DBNeedAsk conn={c.id} need={tl.need} msg={tl.msg} onDone={() => loadChildren(c)} onNo={() => dropNeed(c.id)} /> : <p class="tr-note is-err">{tl.msg}</p>)}
      {isOpen && tl?.s === "ok" && ("keys" in tl.v ? <KeyRows c={c} v={tl.v} /> : <SchemaRows c={c} v={tl.v} />)}
    </>
  );
}

function KeyRows({ c, v }: { c: DBConnView; v: RedisKeys }) {
  const keys = (v.keys ?? []).filter((k) => match(k.key));
  return (
    <>
      {keys.length === 0 && <p class="tr-note">{t("dbv.no_keys")}</p>}
      {keys.map((k) => (
        <Row
          key={k.key}
          level={1}
          twist={null}
          icon={<Cod name="symbol-key" />}
          label={k.key}
          desc={k.type}
          title={k.key}
          data={{ key: k.key }}
          end={k.ttl > 0 ? <span class="count">{Math.ceil(k.ttl / 1000) + "s"}</span> : undefined}
          onContextMenu={onMenu(k.key, () => keyItems(c, v.db, k.key))}
          actions={
            canAct() &&
            !c.readonly && (
              <>
                <Act icon="watch" label={t("dbv.ttl")} onClick={() => (dbForm.value = { kind: "ttl", conn: c.id, db: v.db, key: k.key })} />
                <Act icon="trash" label={t("dbv.del_key")} onClick={() => ask(t("dbv.del_key"), "redis.key", { conn: c.id, db: v.db || undefined, key: k.key, verb: "delete" })} />
              </>
            )
          }
        />
      ))}
      {v.more && <p class="tr-note">{t("dbv.keys_more")}</p>}
    </>
  );
}

function SchemaRows({ c, v }: { c: DBConnView; v: Tables }) {
  const conn = c.id;
  const sk = conn + "/" + (v.schema || "-");
  const base = v.schema ? 2 : 1;
  const all = v.tables.filter((x) => match(x.name));
  const kinds: { id: "t" | "v"; label: string; items: Tables["tables"] }[] = [
    { id: "t", label: t("db.tables"), items: all.filter((x) => !x.view) },
    { id: "v", label: t("tabs.label"), items: all.filter((x) => x.view) },
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
                    ai={{ item: "db_table", conn, table: x.name }}
                    onClick={() => showFile(dbTab(conn, x.name), true)}
                    onDblClick={() => showFile(dbTab(conn, x.name))}
                    onContextMenu={onMenu(x.name, () => tableItems(c, x.name, x.view))}
                    actions={<Act icon="code" label={t("dbv.ddl")} onClick={() => showFile(ddlTab(conn, x.name))} />}
                  />
                ))}
            </Fragment>
          );
        })}
      {v.more > 0 && <p class="tr-note">{t("dbv.more", { n: v.more })}</p>}
    </>
  );
}

/** openConfig opens the laptop's connections file in an editor tab. */
export function openConfig(): void {
  link.request("db.config", {}).then(
    (r) => showFile((r as { path?: string }).path ?? ".termocode/databases.json"),
    (e: unknown) => toast("warn", dbErr(e)),
  );
}

/** serversOpen: the SERVERS rows are shown (null: open as soon as there
 * is an ssh:// host). */
const serversOpen = signal<boolean | null>(null);
/** srvOpen are the SERVERS hosts shown open. */
const srvOpen = signal<ReadonlySet<string>>(new Set());

/** pick adds a host's database container and opens it (its tables, or
 * what it asks first: the host key, the password). */
function pick(host: string, c: HostCtr): void {
  pickDB(host, c.id).then(
    (r) => {
      toast("ok", t("srv.db_added", { name: r.name || c.name }));
      openNext.value = r.conn;
      loadConns();
    },
    (e: unknown) => toast("warn", dbErr(e)),
  );
}

/** termActs is Open Terminal on a SERVERS host row: its hover button
 * and right-click menu (asks the laptop). */
function termActs(host: HostView, act: boolean) {
  const a = act ? termAsk(host) : null;
  if (!a) return {};
  const run = () => (laptop.value = a);
  return {
    actions: <Act icon="terminal" act="terminal" label={a.label} onClick={run} />,
    onMenu: onMenu(host.name, () => [{ label: a.label, run }]),
  };
}

/** Servers is SERVERS: the ssh:// hosts, their compose projects and
 * database containers, then + Add Server…. */
function Servers({ act }: { act: boolean }) {
  const hl = hosts.value;
  const list = hl?.s === "ok" ? hl.v.filter((x) => x.ssh) : [];
  const isOpen = serversOpen.value ?? list.length > 0;
  const flipAll = () => {
    serversOpen.value = !isOpen;
    if (!isOpen && (!hosts.peek() || hosts.peek()?.s === "err")) loadHosts();
  };
  return (
    <Tree label={t("srv.servers")} class="db-servers">
      <Row level={0} twist={isOpen ? "open" : "closed"} onToggle={flipAll} icon={<Cod name="server" />} label={t("srv.servers")} data={{ servers: "1" }} end={hl?.s === "ok" ? <span class="count">{list.length}</span> : undefined} />
      {isOpen && (!hl || hl.s === "loading") && <p class="tr-note">{t("loading")}</p>}
      {isOpen && hl?.s === "err" && <p class="tr-note is-err">{hl.msg}</p>}
      {isOpen && hl?.s === "ok" && list.length === 0 && <p class="tr-note">{t("srv.no_hosts")}</p>}
      {isOpen && list.map((x) => <HostRows key={x.name} h={x} open={srvOpen} db level={1} onPick={act ? (c) => pick(x.name, c) : undefined} {...termActs(x, act)} />)}
      {isOpen && hl?.s === "ok" && (
        <AddServerRow
          level={1}
          canAct={act}
          onAdded={() => {
            loadHosts();
          }}
        />
      )}
    </Tree>
  );
}

export function DeskDB() {
  const on = live.value;
  useEffect(() => {
    if (on && (!conns.peek() || conns.peek()?.s === "err")) loadConns();
    if (on && !hosts.peek()) loadHosts(); // SERVERS opens when there is an ssh:// host
  }, [on]);
  // A database container just added: open it once it is listed.
  const next = openNext.value;
  useEffect(() => {
    const l = conns.value;
    if (!next || l?.s !== "ok") return;
    const it = l.v.find((x) => x.id === next);
    if (!it) return;
    openNext.value = null;
    if (!open.peek().has(it.id)) flip(it.id, it);
    else loadChildren(it);
  }, [next, conns.value]);
  const c = conns.value;
  const act = canAct();
  const f = dbFilter.value;
  const l = laptop.value;
  return (
    <PaneView
      id="db"
      title={t("more.db")}
      actions={
        <>
          <Act icon="filter" label={t("files.filter")} on={f !== null} onClick={() => (dbFilter.value = f === null ? "" : null)} />
          {act && <Act icon="add" label={t("dbv.add")} disabled={!on} onClick={() => (dbForm.value = { kind: "add" })} />}
          <Act icon="history" label={t("dbv.history")} onClick={() => showFile(DBH_TAB)} />
          {act && <Act icon="gear" label={t("dbv.configure")} disabled={!on} onClick={openConfig} />}
          <Act icon="refresh" label={t("ctr.refresh")} disabled={!on} onClick={refresh} />
        </>
      }
    >
      <div class="pv-scroll">
        {f !== null && (
          <div class="pv-input dbf-filter">
            <input
              ref={(el) => void (el && !el.dataset.f && ((el.dataset.f = "1"), el.focus()))}
              aria-label={t("files.filter")}
              placeholder={t("files.filter")}
              value={f}
              onInput={(e) => (dbFilter.value = (e.currentTarget as HTMLInputElement).value)}
              onKeyDown={(e) => e.key === "Escape" && (dbFilter.value = null)}
            />
          </div>
        )}
        <DBFormView />
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
        <Servers act={act} />
        <p class="pv-msg">{t("dbv.readonly_note")}</p>
      </div>
      {l && (
        <AskLaptop
          label={l.label}
          text={l.text ?? t("dbv.laptop_q")}
          send={() => link.request(l.op, l.args)}
          onDone={() => (laptop.value = null)}
          onConfirmed={() => setTimeout(refresh, 300)}
        />
      )}
    </PaneView>
  );
}
