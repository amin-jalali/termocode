// The Databases view's forms (desktop), as the laptop's prompts have
// them: add a connection, its SSH tunnel and SSL settings, compare two
// schemas, import a CSV file, the transaction mode, pick a connection,
// a Redis key's TTL. A form never changes anything itself: what changes
// a database or a saved connection asks the laptop (AskLaptop), whose
// card shows exactly what happens. No form has a password field: the
// laptop asks for it on connect.
import { h } from "preact";
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { activeDesk } from "../../lib/deskeditor";
import { schemaCmpTab, sqlTab } from "../../lib/vtabs";
import { showFile } from "../../lib/workbench";
import { link } from "../../session";
import { t, type StringKey } from "../../strings";
import { AskLaptop } from "../../ui/AskLaptop";
import { connView, dbForm, loadConns, pushQuery, runNext, sqlConns, type DBForm } from "./dbstate";

type Vals = Record<string, string>;
interface Field {
  k: string;
  label: StringKey;
  opts?: readonly string[];
  check?: boolean;
  num?: boolean;
  when?: (v: Vals) => boolean;
}

const DRIVERS = ["postgres", "mysql", "sqlite", "redis"] as const;
const net = (v: Vals) => v.driver !== "sqlite";

function fieldsOf(f: DBForm): Field[] {
  switch (f.kind) {
    case "add":
      return [
        { k: "driver", label: "dbv.f_driver", opts: DRIVERS },
        { k: "id", label: "dbv.f_id" },
        { k: "name", label: "dbv.f_name" },
        { k: "host", label: "dbv.f_host", when: net },
        { k: "port", label: "dbv.f_port", num: true, when: net },
        { k: "user", label: "dbv.f_user", when: (v) => v.driver !== "sqlite" && v.driver !== "redis" },
        { k: "database", label: "dbv.f_db", when: (v) => v.driver !== "sqlite" && v.driver !== "redis" },
        { k: "path", label: "dbv.f_path", when: (v) => v.driver === "sqlite" },
        { k: "db", label: "dbv.f_db", num: true, when: (v) => v.driver === "redis" },
        { k: "env", label: "dbv.f_env", opts: ["", "development", "staging", "production"] },
        { k: "readonly", label: "db.readonly", check: true },
      ];
    case "ssh":
      return [
        { k: "host", label: "dbv.f_host" },
        { k: "port", label: "dbv.f_port", num: true },
        { k: "user", label: "dbv.f_user" },
        { k: "auth", label: "dbv.f_auth", opts: ["agent", "key", "password"] },
        { k: "keyfile", label: "dbv.f_key", when: (v) => v.auth === "key" },
        { k: "jump", label: "dbv.f_jump" },
      ];
    case "ssl":
      return [
        { k: "mode", label: "dbv.f_mode", opts: ["verify-full", "verify-ca", "require", "disable"] },
        { k: "ca", label: "dbv.f_ca" },
        { k: "cert", label: "dbv.f_cert" },
        { k: "key", label: "dbv.f_key" },
      ];
    case "compare":
      return [{ k: "target", label: "dbv.cmp_target", opts: sqlConns().filter((c) => c.id !== f.conn).map((c) => c.id) }];
    case "tx":
      return [{ k: "verb", label: "dbv.tx_mode", opts: ["manual", "auto"] }];
    case "import":
      return [
        { k: "path", label: "dbv.csv_file", opts: [] },
        { k: "header", label: "dbv.header", check: true },
      ];
    case "ttl":
      return [{ k: "ttl", label: "dbv.ttl_s", num: true }];
    default:
      return [{ k: "conn", label: "dev.link", opts: sqlConns().map((c) => c.id) }];
  }
}

function startVals(f: DBForm, fields: Field[]): Vals {
  const v: Vals = {};
  for (const x of fields) v[x.k] = x.opts?.[0] ?? (x.check ? "" : "");
  if (f.kind === "import") v.header = "1";
  if (f.kind === "tx") v.verb = connView(f.conn)?.manual ? "auto" : "manual";
  return v;
}

const num = (s: string): number | undefined => (s.trim() === "" ? undefined : Number(s));
const opt = (s: string): string | undefined => (s.trim() === "" ? undefined : s.trim());

/** request is what the form asks the laptop for (null: nothing to ask). */
function request(f: DBForm, v: Vals): { op: string; args: unknown; label: string } | null {
  switch (f.kind) {
    case "add":
      return {
        op: "db.add",
        label: t("dbv.add"),
        args: { id: v.id.trim(), name: opt(v.name), driver: v.driver, host: opt(v.host), port: num(v.port), user: opt(v.user), database: opt(v.database), path: opt(v.path), db: num(v.db), env: opt(v.env), readonly: v.readonly === "1" || undefined },
      };
    case "ssh": {
      const jump = v.jump.split(",").map((s) => s.trim()).filter(Boolean);
      return { op: "db.ssh", label: t("dbv.ssh"), args: { conn: f.conn, host: v.host.trim(), port: num(v.port), user: opt(v.user), auth: v.auth, keyfile: v.auth === "key" ? opt(v.keyfile) : undefined, jump: jump.length ? jump : undefined } };
    }
    case "ssl":
      return { op: "db.ssl", label: t("dbv.ssl"), args: { conn: f.conn, mode: v.mode, ca: opt(v.ca), cert: opt(v.cert), key: opt(v.key) } };
    case "tx":
      return { op: "db.tx", label: t("dbv.tx_mode"), args: { conn: f.conn, verb: v.verb } };
    case "import":
      return { op: "db.import", label: t("dbv.import"), args: { conn: f.conn, table: f.table, path: v.path, noheader: v.header !== "1" || undefined } };
    case "ttl":
      return { op: "redis.key", label: t("dbv.ttl"), args: { conn: f.conn, db: f.db || undefined, key: f.key, verb: "ttl", ttl: num(v.ttl) || undefined } };
  }
  return null;
}

/** done runs the forms that act here: open a tab. */
function done(f: DBForm, v: Vals): void {
  if (f.kind === "compare" && v.target) showFile(schemaCmpTab(f.conn, v.target));
  if ((f.kind === "pick-console" || f.kind === "pick-file") && v.conn) {
    if (f.kind === "pick-file") {
      pushQuery(v.conn, activeDesk.peek()?.text() ?? "");
      runNext.value = v.conn;
    }
    showFile(sqlTab(v.conn));
  }
}

function title(f: DBForm): string {
  switch (f.kind) {
    case "add":
      return t("dbv.add");
    case "pick-console":
      return t("dbv.new_console");
    case "pick-file":
      return t("dbv.run_file");
    case "import":
      return t("dbv.import") + " · " + f.table;
    case "ttl":
      return t("dbv.ttl") + " · " + f.key;
  }
  const label: Record<"ssh" | "ssl" | "compare" | "tx", StringKey> = { ssh: "dbv.ssh", ssl: "dbv.ssl", compare: "dbv.compare", tx: "dbv.tx_mode" };
  return t(label[f.kind]) + " · " + f.conn;
}

/** DBFormView is the open form, under the view's title. */
export function DBFormView() {
  const f = dbForm.value;
  const fields = f ? fieldsOf(f) : [];
  const [v, setV] = useState<Vals>({});
  const [files, setFiles] = useState<string[] | null>(null);
  const [ask, setAsk] = useState<{ op: string; args: unknown; label: string } | null>(null);
  const first = useRef<HTMLElement | null>(null);
  useEffect(() => {
    if (!f) return;
    setV(startVals(f, fields));
    setAsk(null);
    if (f.kind === "import") {
      setFiles(null);
      link.request("db.csv_files", {}).then(
        (r) => {
          const list = ((r as { files?: string[] }).files ?? []).filter((x) => typeof x === "string");
          setFiles(list);
          setV((o) => ({ ...o, path: o.path || list[0] || "" }));
        },
        () => setFiles([]),
      );
    }
  }, [f]);
  useLayoutEffect(() => first.current?.focus(), [f]);
  if (!f) return null;
  const close = () => (dbForm.value = null);
  const go = () => {
    const r = request(f, v);
    if (r) setAsk(r);
    else {
      done(f, v);
      close();
    }
  };
  const set = (k: string, x: string) => setV((o) => ({ ...o, [k]: x }));
  const shown = fields.filter((x) => !x.when || x.when(v));
  return (
    <form
      class="dbf"
      aria-label={title(f)}
      onSubmit={(e) => (e.preventDefault(), go())}
      onKeyDown={(e) => e.key === "Escape" && (e.stopPropagation(), close())}
    >
      <p class="dbf-title">{title(f)}</p>
      {shown.map((x, i) => {
        const id = "dbf-" + x.k;
        const ref = i === 0 ? (el: HTMLElement | null) => void (first.current = el) : undefined;
        if (x.check)
          return (
            <label key={f.kind + x.k} class="dbf-check">
              <input ref={ref} type="checkbox" checked={v[x.k] === "1"} onChange={(e) => set(x.k, (e.currentTarget as HTMLInputElement).checked ? "1" : "")} /> {t(x.label)}
            </label>
          );
        const opts = f.kind === "import" && x.k === "path" ? files ?? [] : x.opts;
        return (
          <div key={f.kind + x.k}>
            <label class="pv-label" for={id}>
              {t(x.label)}
            </label>
            <div class="pv-input">
              {opts ? (
                <select ref={ref} id={id} class="dbf-sel" value={v[x.k] ?? ""} onChange={(e) => set(x.k, (e.currentTarget as HTMLSelectElement).value)}>
                  {opts.map((o) => (
                    <option key={o} value={o}>
                      {o || "—"}
                    </option>
                  ))}
                </select>
              ) : (
                <input ref={ref} id={id} dir="ltr" inputMode={x.num ? "numeric" : undefined} onInput={(e) => set(x.k, (e.currentTarget as HTMLInputElement).value)} />
              )}
            </div>
          </div>
        );
      })}
      {f.kind === "import" && files?.length === 0 && <p class="dbf-note">{t("dbv.no_csv")}</p>}
      {(f.kind === "add" || f.kind === "ssh") && <p class="dbf-note">{t("dbv.no_pw")}</p>}
      <div class="pv-ask-row">
        <button type="button" class="pv-btn is-2" onClick={close}>
          {t("cancel")}
        </button>
        {f.kind === "ssh" && (
          <button type="button" class="pv-btn is-2" onClick={() => setAsk({ op: "db.ssh", label: t("dbv.ssh_remove"), args: { conn: f.conn, remove: true } })}>
            {t("dbv.ssh_remove")}
          </button>
        )}
        <button type="submit" class="pv-btn">
          {t("dbg.continue")}
        </button>
      </div>
      {ask && (
        <AskLaptop
          label={ask.label}
          text={t("dbv.laptop_q")}
          send={() => link.request(ask.op, ask.args)}
          onDone={() => {
            setAsk(null);
            close();
          }}
          onConfirmed={() => setTimeout(loadConns, 300)}
        />
      )}
    </form>
  );
}
