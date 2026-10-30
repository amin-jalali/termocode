// The v2 More topics as their pages read them (web design 2.9): settings,
// mcp, containers and db. Pure: no DOM, timers or network. Every reader
// takes unknown data and keeps only well-formed rows.

type Obj = Record<string, unknown>;
const obj = (v: unknown): Obj => (typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Obj) : {});
const str = (v: unknown): string => (typeof v === "string" ? v : "");
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);

export interface SettingView {
  key: string;
  group: string;
  type: string;
  value: string;
  def: string;
  description: string;
}

/** readSettings reads the settings topic, grouped in the hub's order. */
export function readSettings(data: unknown): { group: string; items: SettingView[] }[] {
  const out: { group: string; items: SettingView[] }[] = [];
  for (const x of list(obj(data).items).map(obj)) {
    const key = str(x.key);
    if (!key) continue;
    const it: SettingView = {
      key,
      group: str(x.group),
      type: str(x.type),
      value: str(x.value),
      def: str(x.default),
      description: str(x.description),
    };
    const last = out[out.length - 1];
    if (last && last.group === it.group) last.items.push(it);
    else out.push({ group: it.group, items: [it] });
  }
  return out;
}

export interface MCPAgentView {
  name: string;
  since: number;
  calls: number;
  asked: number;
}

export interface MCPView {
  mode: string;
  listening: boolean;
  why: string;
  agents: MCPAgentView[];
  calls: string[];
}

const num = (v: unknown): number => (typeof v === "number" && Number.isFinite(v) ? v : 0);

/** readMCP reads the mcp topic. */
export function readMCP(data: unknown): MCPView {
  const d = obj(data);
  return {
    mode: str(d.mode),
    listening: d.listening === true,
    why: str(d.why),
    agents: list(d.agents)
      .map(obj)
      .map((a) => ({ name: str(a.name), since: num(a.since), calls: num(a.calls), asked: num(a.asked) })),
    calls: list(d.calls).map(str).filter((s) => s !== ""),
  };
}

/** hhmm writes a Unix ms time as the local "09:41". */
export function hhmm(ms: number): string {
  const d = new Date(ms);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

export interface CtrView {
  id: string;
  name: string;
  image: string;
  state: string;
  status: string;
  ports: string;
  project: string;
  busy: boolean;
}

export interface ContainersView {
  engine: string;
  context: string;
  containers: CtrView[];
  more: number;
}

/** readContainers reads the answer of containers.list. */
export function readContainers(data: unknown): ContainersView {
  const d = obj(data);
  return {
    engine: str(d.engine),
    context: str(d.context),
    more: num(d.more),
    containers: list(d.containers)
      .map(obj)
      .filter((c) => /^[0-9a-f]{12,64}$/.test(str(c.id)))
      .map((c) => ({
        id: str(c.id),
        name: str(c.name),
        image: str(c.image),
        state: str(c.state),
        status: str(c.status),
        ports: str(c.ports),
        project: str(c.project),
        busy: c.busy === true,
      })),
  };
}

export interface DBConnView {
  id: string;
  name: string;
  driver: string;
  database: string;
  env: string;
  readonly: boolean;
  problem: string;
  sql: boolean;
}

/** readDBConns reads the answer of db.conns. */
export function readDBConns(data: unknown): DBConnView[] {
  return list(obj(data).conns)
    .map(obj)
    .filter((c) => str(c.id) !== "")
    .map((c) => ({
      id: str(c.id),
      name: str(c.name),
      driver: str(c.driver),
      database: str(c.database),
      env: str(c.env),
      readonly: c.readonly === true,
      problem: str(c.problem),
      sql: c.sql === true,
    }));
}

/** readDBTables reads the answer of db.tables. */
export function readDBTables(data: unknown): { schema: string; tables: { name: string; view: boolean }[]; more: number } {
  const d = obj(data);
  return {
    schema: str(d.schema),
    more: num(d.more),
    tables: list(d.tables)
      .map(obj)
      .filter((x) => str(x.name) !== "")
      .map((x) => ({ name: str(x.name), view: x.view === true })),
  };
}

export interface DBCellView {
  v: string;
  n: boolean;
  m: boolean;
  c: boolean;
}

export interface DBRowsView {
  columns: string[];
  rows: DBCellView[][];
  next: number;
  truncated: boolean;
  masked: number;
  ms: number;
}

/** readDBRows reads the answer of db.rows and db.query. */
export function readDBRows(data: unknown): DBRowsView {
  const d = obj(data);
  return {
    columns: list(d.columns).map(str),
    rows: list(d.rows).map((r) =>
      list(r)
        .map(obj)
        .map((c) => ({ v: str(c.v), n: c.n === true, m: c.m === true, c: c.c === true })),
    ),
    next: num(d.next),
    truncated: d.truncated === true,
    masked: num(d.masked),
    ms: num(d.ms),
  };
}
