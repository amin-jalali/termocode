// The Customize sheet (MOBUX-16) in the kit: a "Pull requests" view
// block with its ⚙, sample rows, and a stand-in for the laptop's
// view.run that applies the spec here (on the real page the laptop does
// it). "Laptop refuses" makes the next runs fail, to show the error line.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { CustomizeButton, CustomizeLayer, openCustomize } from "../home/Customize";
import { ViewPreview, type PreviewBlock } from "../home/ViewPreview";
import { cleaned, layoutOf, shownFields, type ViewCond, type ViewInfo, type ViewSpec } from "../home/viewspec";

export const prView: ViewInfo = {
  id: "pr.list",
  title: "Pull requests",
  layouts: ["table", "list", "card"],
  fields: [
    { id: "number", label: "#", kind: "link", sortable: true, filterable: true, default: true },
    { id: "title", label: "Title", kind: "text", sortable: true, filterable: true, default: true },
    { id: "checks", label: "CI", kind: "state", sortable: true, filterable: true, default: true },
    { id: "age", label: "Age", kind: "num", sortable: true, filterable: true, default: true },
    { id: "author", label: "Author", kind: "text", sortable: true, filterable: true },
    { id: "branch", label: "Branch", kind: "path", filterable: true },
    { id: "draft", label: "Draft", kind: "bool", filterable: true },
  ],
  default: { view: "pr.list", fields: ["number", "title", "checks", "age"], sort: [{ field: "age", desc: false }], limit: 50 },
};

type Row = Record<string, string | number | boolean>;

const rows: Row[] = [
  { number: "#418", title: "fix toast", checks: "fail", age: 2, author: "amin", branch: "fix/toast", draft: false },
  { number: "#412", title: "login rate limit", checks: "running", age: 24, author: "sara", branch: "feat/rate", draft: false },
  { number: "#409", title: "docs: web guide", checks: "ok", age: 50, author: "amin", branch: "docs/web", draft: true },
  { number: "#401", title: "db pool size", checks: "fail", age: 120, author: "li", branch: "fix/pool", draft: false },
  { number: "#398", title: "kube logs follow", checks: "ok", age: 200, author: "sara", branch: "feat/kube", draft: false },
];

function holds(r: Row, c: ViewCond): boolean {
  const v = r[c.field];
  const s = String(v).toLowerCase();
  const want = (c.value ?? "").toLowerCase();
  switch (c.op) {
    case "eq":
      return s === want;
    case "ne":
      return s !== want;
    case "contains":
      return s.includes(want);
    case "starts":
      return s.startsWith(want);
    case "gt":
      return Number(v) > Number(c.value);
    case "lt":
      return Number(v) < Number(c.value);
    case "ge":
      return Number(v) >= Number(c.value);
    case "le":
      return Number(v) <= Number(c.value);
    case "in":
      return (c.in ?? []).some((x) => x.toLowerCase() === s);
    case "empty":
      return s === "";
    case "not_empty":
      return s !== "";
  }
}

const TONES: Record<string, "ok" | "bad" | "busy"> = { ok: "ok", fail: "bad", running: "busy" };

/** fakeRun is the kit's stand-in for the laptop's view.run. */
export function fakeRun(spec: ViewSpec): PreviewBlock {
  const fields = shownFields(prView, spec);
  let out = rows.filter((r) => (spec.filter ?? []).every((c) => holds(r, c)));
  for (const s of [...(spec.sort ?? [])].reverse()) {
    out = [...out].sort((a, b) => {
      const x = a[s.field], y = b[s.field];
      const d = typeof x === "number" && typeof y === "number" ? x - y : String(x).localeCompare(String(y));
      return s.desc ? -d : d;
    });
  }
  const total = out.length;
  out = out.slice(0, spec.limit ?? 50);
  const label = (id: string) => prView.fields.find((f) => f.id === id)?.label ?? id;
  const layout = layoutOf(prView, spec);
  const base = { id: "kit-pr", title: spec.title || prView.title, total };
  const cell = (r: Row, id: string) => String(r[id]);
  if (layout === "table") {
    const columns = fields.map((id) => ({ id, label: label(id), kind: id === "age" ? "num" : "text" }));
    if (spec.group) {
      const g = new Map<string, string[][]>();
      for (const r of out) g.set(cell(r, spec.group), [...(g.get(cell(r, spec.group)) ?? []), fields.map((id) => cell(r, id))]);
      return { ...base, kind: "table", columns, groups: [...g].map(([k, v]) => ({ label: k, count: v.length, rows: v })) };
    }
    return { ...base, kind: "table", columns, rows: out.map((r) => fields.map((id) => cell(r, id))) };
  }
  if (layout === "card") {
    const r = out[0];
    return { ...base, kind: "card", fields: r ? fields.map((id) => ({ label: label(id), value: cell(r, id), tone: id === "checks" ? TONES[cell(r, id)] : undefined })) : [] };
  }
  return {
    ...base,
    kind: "list",
    items: out.map((r) => ({
      id: cell(r, "number"),
      title: fields.map((id) => cell(r, id)).join(" · "),
      sub: cell(r, "branch"),
      tone: TONES[cell(r, "checks")],
    })),
  };
}

const spec = signal<ViewSpec>(prView.default!);
const refuse = signal(false);
const asked = signal("");

function run(s: ViewSpec): Promise<PreviewBlock> {
  return new Promise((done, fail) =>
    setTimeout(() => (refuse.value ? fail(new Error("view pr.list has no field \"x\"")) : done(fakeRun(cleaned(s)))), 120),
  );
}

/** KitCustomize is the kit's view block with its ⚙ and the sheet. */
export function KitCustomize() {
  const block = fakeRun(spec.value);
  return (
    <>
      <div class="kit-row">
        <button type="button" class="btn" onClick={() => (refuse.value = !refuse.value)} aria-pressed={refuse.value}>
          Laptop refuses
        </button>
        <button type="button" class="btn" onClick={() => (spec.value = prView.default!)}>
          Reset view
        </button>
      </div>
      <div class="kit-custblock" data-spec={JSON.stringify(spec.value)}>
        <div class="kit-custhead">
          <strong>{block.title}</strong>
          <CustomizeButton
            title={block.title}
            onOpen={() =>
              openCustomize({
                info: prView,
                spec: spec.value,
                block,
                run,
                onApply: (s) => (spec.value = s),
                onAsk: (text, s) => (asked.value = text + JSON.stringify(s)),
              })
            }
          />
        </div>
        <ViewPreview block={block} density={spec.value.density ?? "compact"} />
        {asked.value ? <p class="kit-custasked">{asked.value}</p> : null}
      </div>
      <CustomizeLayer />
    </>
  );
}
