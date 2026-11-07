// The logic of the phone's MCP default views (MOBUX-14), kept apart from
// the drawing so it is easy to test: the wire types of mcp.views and
// mcp.view_run (internal/webproto/ops_mcpviews.go), search over every
// server's views, the shape mark of a layout (a shape, never colour
// alone), and the check of the param form before it goes to the laptop
// (the laptop checks again; this only says early what is missing).
import type { Block } from "../../../../../webproto/proto";
import type { McpKey } from "./strings";

export type Layout = "table" | "list" | "card" | "detail" | "text" | "json" | "status";

export interface MCPViewParam {
  id: string;
  label: string;
  kind: "string" | "int" | "number" | "bool" | "enum" | "uri";
  enum?: string[];
  required?: boolean;
  hint?: string;
}

export interface MCPViewField {
  id: string;
  label: string;
  kind: string;
}

export interface MCPViewChoice {
  uri: string;
  name: string;
  template?: boolean;
  vars?: string[];
}

export interface MCPViewInfo {
  id: string;
  tool: string;
  title: string;
  description?: string;
  params: MCPViewParam[];
  fields: MCPViewField[];
  layout: Layout;
  typed?: "schema" | "learned";
  run?: boolean;
  why?: string;
  resources?: MCPViewChoice[];
}

export interface MCPViewServer {
  name: string;
  known: boolean;
  off?: boolean;
  views: MCPViewInfo[];
}

export interface MCPViews {
  servers: MCPViewServer[];
}

export interface MCPViewData {
  view: string;
  title: string;
  layout: Layout;
  block?: Block;
  raw: string;
  truncated?: boolean;
  error?: boolean;
  ask?: string;
}

/** OP_VIEWS and OP_RUN are the two ops (webproto OpMCPViews, OpMCPViewRun). */
export const OP_VIEWS = "mcp.views";
export const OP_RUN = "mcp.view_run";

/** Caps the laptop holds too (webproto ops_mcpviews.go). */
export const MAX_ARGS = 12;
export const MAX_ARG = 2048;

/** SHAPE pairs each layout with a shape mark and its name. */
export const SHAPE: Record<Layout, { mark: string; key: McpKey }> = {
  table: { mark: "▦", key: "mcpv.shape.table" },
  list: { mark: "☰", key: "mcpv.shape.list" },
  card: { mark: "▭", key: "mcpv.shape.card" },
  detail: { mark: "▤", key: "mcpv.shape.detail" },
  text: { mark: "¶", key: "mcpv.shape.text" },
  json: { mark: "{ }", key: "mcpv.shape.json" },
  status: { mark: "ⓘ", key: "mcpv.shape.status" },
};

export function shapeOf(layout: string): { mark: string; key: McpKey } {
  return Object.prototype.hasOwnProperty.call(SHAPE, layout) ? SHAPE[layout as Layout] : SHAPE.json;
}

/** Row is one row of the tools list: a view with its server. */
export interface Row {
  server: string;
  view: MCPViewInfo;
}

/** rowsOf lists every view of every server, runnable ones first within
 * a server, in the laptop's order otherwise; q keeps the rows whose
 * title, tool, server or description holds every word of q. */
export function rowsOf(data: MCPViews | null, q: string): Row[] {
  if (!data || !Array.isArray(data.servers)) return [];
  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  const out: Row[] = [];
  for (const s of data.servers) {
    const views = Array.isArray(s.views) ? s.views : [];
    const run = views.filter((v) => v.run);
    const ask = views.filter((v) => !v.run);
    for (const v of [...run, ...ask]) {
      const hay = `${v.title} ${v.tool} ${s.name} ${v.description ?? ""}`.toLowerCase();
      if (words.every((w) => hay.includes(w))) out.push({ server: s.name, view: v });
    }
  }
  return out;
}

/** SECRETISH are token shapes the laptop refuses from the phone; the
 * page says so before sending (the laptop's check is the real one). */
const SECRETISH = /\b(sk-[A-Za-z0-9_-]{16,}|gh[pousr]_[A-Za-z0-9]{20,}|xox[abpr]-[A-Za-z0-9-]{10,}|bearer\s+[A-Za-z0-9._~+/-]{16,}|AKIA[0-9A-Z]{16})/i;

export interface ArgProblem {
  id: string;
  key: McpKey;
  label: string;
}

/** checkArgs turns the form's values into the op's args, or says the
 * first problem: a missing required value, a number that is not one, a
 * value that looks like a secret. Empty values are not sent. */
export function checkArgs(v: MCPViewInfo, values: Readonly<Record<string, string>>): { id: string; value: string }[] | ArgProblem {
  const out: { id: string; value: string }[] = [];
  for (const p of v.params ?? []) {
    const raw = (values[p.id] ?? "").trim();
    if (!raw) {
      if (p.required) return { id: p.id, key: "mcpv.need", label: p.label };
      continue;
    }
    if ((p.kind === "int" && !/^-?\d+$/.test(raw)) || (p.kind === "number" && !Number.isFinite(Number(raw)))) {
      return { id: p.id, key: "mcpv.number", label: p.label };
    }
    if (SECRETISH.test(raw)) return { id: p.id, key: "mcpv.secret", label: p.label };
    if (out.length < MAX_ARGS) out.push({ id: p.id, value: raw.slice(0, MAX_ARG) });
  }
  return out;
}

/** isProblem tells a problem from args. */
export function isProblem(x: ReturnType<typeof checkArgs>): x is ArgProblem {
  return !Array.isArray(x);
}

/** prettyRaw indents raw JSON for the Raw view; other text stays as it is. */
export function prettyRaw(raw: string): string {
  const s = raw.trim();
  if (!(s.startsWith("{") || s.startsWith("["))) return raw;
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return raw; // cut JSON: shown as it came
  }
}
