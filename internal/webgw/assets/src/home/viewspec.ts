// The view spec on the phone (mobile plan 3.11.2) and the pure edits the
// Customize sheet (MOBUX-16) makes to a draft of it. The laptop checks
// and runs every spec (view.run); these helpers only keep a draft inside
// the same caps, so the sheet never offers what the laptop would refuse.
// Pure: no imports, so the Home chunk and the page can both use it.

/** Field kinds of a view (internal/views, def.go). */
export type FieldKind = "text" | "num" | "time" | "state" | "path" | "link" | "bool" | "cost" | "md";

/** CondOp is a filter condition's operator. */
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

export type Density = "compact" | "detailed";

/** ViewSpec matches blocks.ViewSpec (JSON names). */
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
  density?: Density;
  title?: string;
  refresh?: number;
}

/** ViewFieldInfo is one field of a view as view.catalog gives it. */
export interface ViewFieldInfo {
  id: string;
  label: string;
  kind: FieldKind;
  sortable?: boolean;
  filterable?: boolean;
  default?: boolean;
}

/** ViewInfo is one view of view.catalog: what the sheet may offer. */
export interface ViewInfo {
  id: string;
  title: string;
  fields: ViewFieldInfo[];
  layouts: string[]; // the first is the default
  default?: ViewSpec;
}

// Caps of a spec (internal/blocks, internal/views).
export const MAX_FILTER = 8;
export const MAX_SORT = 3;
export const MAX_IN = 16;
export const MAX_TABLE_FIELDS = 12;
export const MAX_FIELDS = 40;
export const MIN_LIMIT = 1;
export const MAX_LIMIT = 200;
export const DEFAULT_LIMIT = 50;
export const MAX_VALUE = 200;

/** The layouts the sheet draws a choice for, in this order. */
export const ROW_LAYOUTS = ["table", "list", "card", "detail"] as const;

const ORDERED: CondOp[] = ["gt", "lt", "ge", "le"];
const TEXTUAL: FieldKind[] = ["text", "path", "link", "md", "state"];
const NUMERIC: FieldKind[] = ["num", "time", "cost"];

/** opsFor lists the filter operators a field kind allows, the plain
 * "is" first (the same table as internal/views condKinds). */
export function opsFor(kind: FieldKind): CondOp[] {
  const ops: CondOp[] = ["eq", "ne"];
  if (TEXTUAL.includes(kind)) ops.push("contains", "starts");
  if (NUMERIC.includes(kind)) ops.push(...ORDERED);
  ops.push("in", "empty", "not_empty");
  return ops;
}

/** needsValue says whether an operator takes a value. */
export function needsValue(op: CondOp): boolean {
  return op !== "empty" && op !== "not_empty";
}

/** fieldOf returns the view's field with id. */
export function fieldOf(info: ViewInfo, id: string): ViewFieldInfo | undefined {
  return info.fields.find((f) => f.id === id);
}

/** shownFields are the fields the spec shows, in order: its own list,
 * else the view's default spec, else the fields marked default. */
export function shownFields(info: ViewInfo, spec: ViewSpec): string[] {
  const known = (ids?: string[]) => (ids ?? []).filter((id) => fieldOf(info, id));
  const own = known(spec.fields);
  if (own.length) return own;
  const def = known(info.default?.fields);
  if (def.length) return def;
  const marked = info.fields.filter((f) => f.default).map((f) => f.id);
  return marked.length ? marked : info.fields.slice(0, 1).map((f) => f.id);
}

/** layoutOf is the spec's layout, else the view's first. */
export function layoutOf(info: ViewInfo, spec: ViewSpec): string {
  return spec.layout && info.layouts.includes(spec.layout) ? spec.layout : info.layouts[0] ?? "list";
}

/** maxFields is how many fields the layout may show. */
export function maxFields(layout: string): number {
  return layout === "table" ? MAX_TABLE_FIELDS : MAX_FIELDS;
}

function copy(spec: ViewSpec): ViewSpec {
  return {
    ...spec,
    args: spec.args ? { ...spec.args } : undefined,
    fields: spec.fields ? [...spec.fields] : undefined,
    filter: spec.filter ? spec.filter.map((c) => ({ ...c, in: c.in ? [...c.in] : undefined })) : undefined,
    sort: spec.sort ? spec.sort.map((s) => ({ ...s })) : undefined,
  };
}

/** draftOf starts a draft from a block's spec: every choice made
 * explicit (fields, layout, density, limit), so the sheet shows it. */
export function draftOf(info: ViewInfo, spec: ViewSpec | undefined): ViewSpec {
  const base = copy(spec ?? info.default ?? { view: info.id });
  base.view = info.id;
  base.fields = shownFields(info, base);
  base.layout = layoutOf(info, base);
  base.density = base.density ?? info.default?.density ?? "compact";
  base.limit = clampLimit(base.limit ?? info.default?.limit ?? DEFAULT_LIMIT);
  base.filter = (base.filter ?? []).filter((c) => fieldOf(info, c.field)?.filterable);
  base.sort = (base.sort ?? []).filter((s) => fieldOf(info, s.field)?.sortable).slice(0, MAX_SORT);
  if (base.group && !fieldOf(info, base.group)) base.group = undefined;
  return base;
}

/** toggleField shows or hides a field; the last shown field stays, and
 * a table never shows more than its cap (the tap is then a no-op). */
export function toggleField(info: ViewInfo, d: ViewSpec, id: string): ViewSpec {
  if (!fieldOf(info, id)) return d;
  const fields = shownFields(info, d);
  const i = fields.indexOf(id);
  if (i >= 0) {
    if (fields.length === 1) return d;
    fields.splice(i, 1);
  } else {
    if (fields.length >= maxFields(layoutOf(info, d))) return d;
    // A field comes back at its place in the view's own order.
    const order = info.fields.map((f) => f.id);
    const at = fields.findIndex((f) => order.indexOf(f) > order.indexOf(id));
    fields.splice(at < 0 ? fields.length : at, 0, id);
  }
  return { ...d, fields };
}

/** moveField moves a shown field one place up (-1) or down (+1). */
export function moveField(info: ViewInfo, d: ViewSpec, id: string, by: -1 | 1): ViewSpec {
  const fields = shownFields(info, d);
  const i = fields.indexOf(id);
  const j = i + by;
  if (i < 0 || j < 0 || j >= fields.length) return d;
  [fields[i], fields[j]] = [fields[j], fields[i]];
  return { ...d, fields };
}

/** setLayout picks a layout; a table keeps only its first 12 fields. */
export function setLayout(info: ViewInfo, d: ViewSpec, layout: string): ViewSpec {
  if (!info.layouts.includes(layout)) return d;
  const fields = shownFields(info, d).slice(0, maxFields(layout));
  return { ...d, layout, fields };
}

export function setDensity(d: ViewSpec, density: Density): ViewSpec {
  return { ...d, density };
}

/** filterable are the fields a filter may name. */
export function filterable(info: ViewInfo): ViewFieldInfo[] {
  return info.fields.filter((f) => f.filterable);
}

/** sortable are the fields a sort may name. */
export function sortable(info: ViewInfo): ViewFieldInfo[] {
  return info.fields.filter((f) => f.sortable);
}

/** addCond adds a condition on the first filterable field ("is …");
 * nothing at the cap or when no field filters. */
export function addCond(info: ViewInfo, d: ViewSpec): ViewSpec {
  const f = filterable(info)[0];
  const filter = d.filter ?? [];
  if (!f || filter.length >= MAX_FILTER) return d;
  return { ...d, filter: [...filter, { field: f.id, op: "eq", value: "" }] };
}

/** setCond changes condition i: a new field keeps the operator when
 * its kind allows it (else "is"); a new operator keeps the value when it
 * takes one. An "in" value is a comma list. */
export function setCond(info: ViewInfo, d: ViewSpec, i: number, change: { field?: string; op?: CondOp; value?: string }): ViewSpec {
  const filter = [...(d.filter ?? [])];
  const c = filter[i];
  if (!c) return d;
  const next: ViewCond = { ...c };
  if (change.field !== undefined) {
    const f = fieldOf(info, change.field);
    if (!f || !f.filterable) return d;
    next.field = f.id;
    if (!opsFor(f.kind).includes(next.op)) next.op = "eq";
  }
  if (change.op !== undefined) {
    const f = fieldOf(info, next.field);
    if (!f || !opsFor(f.kind).includes(change.op)) return d;
    next.op = change.op;
  }
  const raw = change.value !== undefined ? change.value : next.op === "in" ? (next.in ?? []).join(", ") || next.value || "" : next.value ?? (next.in ?? []).join(", ");
  const value = raw.slice(0, MAX_VALUE);
  if (!needsValue(next.op)) {
    next.value = undefined;
    next.in = undefined;
  } else if (next.op === "in") {
    next.in = value
      .split(",")
      .map((v) => v.trim())
      .filter((v) => v !== "")
      .slice(0, MAX_IN);
    next.value = undefined;
  } else {
    next.value = value;
    next.in = undefined;
  }
  filter[i] = next;
  return { ...d, filter };
}

export function removeCond(d: ViewSpec, i: number): ViewSpec {
  const filter = (d.filter ?? []).filter((_, j) => j !== i);
  return { ...d, filter };
}

/** condValue is condition c's value as the sheet's text box shows it. */
export function condValue(c: ViewCond): string {
  return c.op === "in" ? (c.in ?? []).join(", ") : c.value ?? "";
}

/** setSort sets the first sort key ("" = the view's own order); the
 * other keys stay behind it, without the new field. */
export function setSort(info: ViewInfo, d: ViewSpec, field: string, desc: boolean): ViewSpec {
  const rest = (d.sort ?? []).slice(1).filter((s) => s.field !== field);
  if (field === "") return { ...d, sort: rest.length ? rest : [] };
  if (!fieldOf(info, field)?.sortable) return d;
  return { ...d, sort: [{ field, desc }, ...rest].slice(0, MAX_SORT) };
}

/** setGroup groups by a field ("" = no groups). */
export function setGroup(info: ViewInfo, d: ViewSpec, field: string): ViewSpec {
  if (field !== "" && !fieldOf(info, field)) return d;
  return { ...d, group: field || undefined };
}

export function clampLimit(n: number): number {
  if (!Number.isFinite(n)) return DEFAULT_LIMIT;
  return Math.min(MAX_LIMIT, Math.max(MIN_LIMIT, Math.round(n)));
}

export function setLimit(d: ViewSpec, n: number): ViewSpec {
  return { ...d, limit: clampLimit(n) };
}

/** cleaned is the draft as it goes to the laptop: conditions that still
 * miss their value are left out (the user is typing), empty lists drop. */
export function cleaned(d: ViewSpec): ViewSpec {
  const out = copy(d);
  out.filter = (out.filter ?? []).filter((c) => !needsValue(c.op) || (c.op === "in" ? (c.in ?? []).length > 0 : (c.value ?? "").trim() !== ""));
  if (!out.filter.length) delete out.filter;
  if (!out.sort?.length) delete out.sort;
  if (!out.group) delete out.group;
  if (!out.fields?.length) delete out.fields;
  if (out.args && !Object.keys(out.args).length) delete out.args;
  return out;
}

/** specKey is a stable text of a spec: two specs with the same key run
 * the same (the preview skips a run when nothing changed). */
export function specKey(s: ViewSpec): string {
  const c = cleaned(s);
  const keys = Object.keys(c).sort() as (keyof ViewSpec)[];
  return JSON.stringify(keys.map((k) => [k, k === "args" && c.args ? Object.keys(c.args).sort().map((a) => [a, c.args![a]]) : c[k]]));
}

/** changed says whether the draft differs from where it started. */
export function changed(start: ViewSpec, d: ViewSpec): boolean {
  return specKey(start) !== specKey(d);
}
