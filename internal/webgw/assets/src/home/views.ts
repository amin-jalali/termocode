// The view registry on the phone (MOB-9, mobile plan 3.11): the shapes of
// view.catalog, view.run and ev view_update, and the small pure helpers
// the Views menu and the Customize sheet share. The laptop alone applies
// a spec (filter, sort, group, limit, layout); the page only builds one.
// These mirror webproto/views.go until proto.d.ts is generated again.

import { dict } from "../state/dict";

export type FieldKind = "text" | "num" | "time" | "state" | "path" | "link" | "bool" | "cost" | "md";
export type CondOp = "eq" | "ne" | "gt" | "lt" | "ge" | "le" | "contains" | "starts" | "in" | "empty" | "not_empty";

export interface ViewCond {
  field: string;
  op: CondOp;
  value?: string;
  in?: string[];
}

export interface ViewSort {
  field: string;
  desc?: boolean;
}

export interface ViewSpec {
  view: string;
  ws?: string;
  args?: Record<string, string>;
  fields?: string[];
  filter?: ViewCond[];
  sort?: ViewSort[];
  group?: string;
  limit?: number;
  layout?: string;
  density?: "compact" | "detailed";
  title?: string;
  refresh?: number;
}

export interface ViewParam {
  id: string;
  label: string;
  kind: "ws" | "string" | "int" | "enum" | "path" | "table" | "conn" | "number_pr" | "uri";
  enum?: string[];
  required?: boolean;
}

export interface ViewField {
  id: string;
  label: string;
  kind: FieldKind;
  sortable?: boolean;
  filterable?: boolean;
  default?: boolean;
}

export interface ViewDef {
  id: string;
  feature: string;
  title: string;
  params?: ViewParam[];
  fields: ViewField[];
  layouts: string[];
  default: ViewSpec;
  role: "viewer" | "controller";
  live?: boolean;
  refresh?: number;
}

export interface ViewCatalog {
  features: string[];
  views: ViewDef[];
}

/** MAX_* mirror the laptop's caps (blocks.go); the laptop checks again. */
export const MAX_FILTER = 8;
export const MAX_SORT = 3;
export const MAX_LIMIT = 200;
export const MAX_LIVE = 8;

/** readCatalog checks the shape of a view.catalog answer. */
export function readCatalog(data: unknown): ViewCatalog | null {
  if (typeof data !== "object" || data === null) return null;
  const c = data as Partial<ViewCatalog>;
  if (!Array.isArray(c.features) || !Array.isArray(c.views)) return null;
  return { features: c.features.filter((f) => typeof f === "string"), views: c.views.filter((v) => typeof v?.id === "string" && Array.isArray(v.fields)) };
}

/** readLive is the live id of a view.run answer ("" when it is not live). */
export function readLive(data: unknown): string {
  const l = typeof data === "object" && data !== null ? (data as { live?: unknown }).live : undefined;
  return typeof l === "string" ? l : "";
}

/** readUpdate checks ev view_update's data. */
export function readUpdate(data: unknown): { id: string; block: unknown } | null {
  if (typeof data !== "object" || data === null) return null;
  const u = data as { id?: unknown; block?: unknown };
  return typeof u.id === "string" && typeof u.block === "object" && u.block !== null ? { id: u.id, block: u.block } : null;
}

/** specOf is the spec a block carries (Customize edits it). */
export function specOf(block: unknown): ViewSpec | null {
  const s = typeof block === "object" && block !== null ? (block as { spec?: unknown }).spec : undefined;
  return typeof s === "object" && s !== null && typeof (s as ViewSpec).view === "string" ? (s as ViewSpec) : null;
}

/** match is the menu's search: a view whose title, id or feature holds q. */
export function match(def: ViewDef, q: string): boolean {
  const t = q.trim().toLowerCase();
  if (!t) return true;
  return def.title.toLowerCase().includes(t) || def.id.toLowerCase().includes(t) || def.feature.toLowerCase().includes(t);
}

/** byFeature groups the views in the catalog's feature order. */
export function byFeature(cat: ViewCatalog, q: string): { feature: string; views: ViewDef[] }[] {
  const out: { feature: string; views: ViewDef[] }[] = [];
  for (const f of cat.features) {
    const views = cat.views.filter((v) => v.feature === f && match(v, q));
    if (views.length) out.push({ feature: f, views });
  }
  return out;
}

/** opsFor are the filter ops a field kind takes (as the laptop checks). */
export function opsFor(kind: FieldKind): CondOp[] {
  switch (kind) {
    case "num":
    case "cost":
    case "time":
      return ["eq", "ne", "gt", "lt", "ge", "le", "empty", "not_empty"];
    case "bool":
      return ["eq", "ne"];
    default:
      return ["eq", "ne", "contains", "starts", "empty", "not_empty"];
  }
}

/** needsValue: the op compares with a value. */
export function needsValue(op: CondOp): boolean {
  return op !== "empty" && op !== "not_empty";
}

/** shownFields are the fields a spec shows: its own, else the view's
 * default spec's, else the fields marked default. */
export function shownFields(def: ViewDef, spec: ViewSpec): string[] {
  if (spec.fields?.length) return spec.fields;
  if (def.default.fields?.length) return def.default.fields;
  return def.fields.filter((f) => f.default).map((f) => f.id);
}

/** move moves item i of list by d places (the fields' order without drag). */
export function move<T>(list: readonly T[], i: number, d: number): T[] {
  const j = i + d;
  if (j < 0 || j >= list.length) return [...list];
  const out = [...list];
  [out[i], out[j]] = [out[j], out[i]];
  return out;
}

/** cleanSpec drops what equals the view's default, so the laptop fills
 * it in, and caps what the page lets the user add. */
export function cleanSpec(def: ViewDef, s: ViewSpec): ViewSpec {
  const out: ViewSpec = { view: def.id };
  if (s.ws) out.ws = s.ws;
  if (s.args && Object.keys(s.args).length) out.args = s.args;
  const fields = s.fields ?? [];
  if (fields.length && fields.join(",") !== shownFields(def, { view: def.id }).join(",")) out.fields = fields;
  const filter = (s.filter ?? []).filter((c) => c.field && (!needsValue(c.op) || (c.value ?? "") !== "")).slice(0, MAX_FILTER);
  if (filter.length) out.filter = filter.map((c) => (needsValue(c.op) ? { field: c.field, op: c.op, value: c.value } : { field: c.field, op: c.op }));
  const sort = (s.sort ?? []).filter((o) => o.field).slice(0, MAX_SORT);
  if (sort.length) out.sort = sort;
  if (s.group) out.group = s.group;
  if (s.limit && s.limit > 0) out.limit = Math.min(MAX_LIMIT, Math.floor(s.limit));
  if (s.layout && s.layout !== (def.default.layout || def.layouts[0])) out.layout = s.layout;
  if (s.density) out.density = s.density;
  if (s.title) out.title = s.title;
  return out;
}

/** specFromRoute reads the deep link #/home/view/<id>?ws=…&<arg>=…. */
export function specFromRoute(rest: string, query: Readonly<Record<string, string>>): ViewSpec | null {
  if (!rest.startsWith("view/")) return null;
  const id = rest.slice(5);
  if (!/^[A-Za-z0-9_.:-]{1,64}$/.test(id)) return null;
  const spec: ViewSpec = { view: id };
  const args = dict<string>();
  for (const k of Object.keys(query)) {
    if (k === "ws") spec.ws = query[k];
    else if (/^[a-z_]{1,32}$/.test(k)) args[k] = query[k];
  }
  if (Object.keys(args).length) spec.args = args;
  return spec;
}
