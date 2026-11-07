// The Customize sheet (MOB-9, mobile plan 2.3): ⚙ on a view block. It
// edits the block's spec (layout, density, fields and their order,
// filter, sort, group, limit) and Apply runs view.run again with it; the
// laptop checks and applies the spec. Fields move with ↑ ↓ buttons (no
// drag needed); every control is 44 px; Cancel is first.
import { h, H } from "./jsx";
import { hs, type HomeKey } from "./strings";
import {
  cleanSpec,
  MAX_FILTER,
  MAX_LIMIT,
  move,
  needsValue,
  opsFor,
  shownFields,
  type CondOp,
  type ViewCond,
  type ViewDef,
  type ViewSpec,
} from "./views";

function layoutLabel(l: string): string {
  return hs(("layout." + l) as HomeKey);
}

export function Customize({ def, spec, onApply, onClose }: { def: ViewDef; spec: ViewSpec; onApply: (s: ViewSpec) => void; onClose: () => void }) {
  const S = H.Sheet;
  const [layout, setLayout] = H.useState(spec.layout || def.default.layout || def.layouts[0]);
  const [density, setDensity] = H.useState<"compact" | "detailed">(spec.density ?? "compact");
  const [fields, setFields] = H.useState<string[]>(() => shownFields(def, spec));
  const [filter, setFilter] = H.useState<ViewCond[]>(() => [...(spec.filter ?? [])]);
  const sort0 = spec.sort?.[0] ?? def.default.sort?.[0];
  const [sortBy, setSortBy] = H.useState(sort0?.field ?? "");
  const [desc, setDesc] = H.useState(sort0?.desc ?? false);
  const [group, setGroup] = H.useState(spec.group ?? def.default.group ?? "");
  const [limit, setLimit] = H.useState(String(spec.limit || def.default.limit || 50));

  const order = [...fields, ...def.fields.map((f) => f.id).filter((id) => !fields.includes(id))];
  const label = (id: string) => def.fields.find((f) => f.id === id)?.label ?? id;
  const filterable = def.fields.filter((f) => f.filterable);
  const sortable = def.fields.filter((f) => f.sortable);
  const toggle = (id: string, on: boolean) => setFields(on ? order.filter((x) => x === id || fields.includes(x)) : fields.filter((x) => x !== id));
  const setCond = (i: number, c: Partial<ViewCond>) => setFilter(filter.map((x, j) => (j === i ? { ...x, ...c } : x)));

  const apply = () => {
    const n = parseInt(limit, 10);
    onApply(
      cleanSpec(def, {
        ...spec,
        layout,
        density,
        fields,
        filter,
        sort: sortBy ? [{ field: sortBy, desc }] : [],
        group,
        limit: Number.isFinite(n) ? Math.max(1, Math.min(MAX_LIMIT, n)) : undefined,
      }),
    );
    onClose();
  };

  return (
    <S onLater={onClose} label={hs("cust.title", { title: spec.title || def.title })}>
      <div class="home-panels home-form">
        <h2 class="home-panels-title" dir="auto">
          {hs("cust.title", { title: spec.title || def.title })}
        </h2>
        {def.layouts.length > 1 && (
          <fieldset class="home-fieldset">
            <legend>{hs("cust.layout")}</legend>
            {def.layouts.map((l) => (
              <label key={l} class="home-check">
                <input type="radio" name="cust-layout" checked={layout === l} onChange={() => setLayout(l)} />
                <span>{layoutLabel(l)}</span>
              </label>
            ))}
          </fieldset>
        )}
        <fieldset class="home-fieldset">
          <legend>{hs("cust.density")}</legend>
          {(["compact", "detailed"] as const).map((d) => (
            <label key={d} class="home-check">
              <input type="radio" name="cust-density" checked={density === d} onChange={() => setDensity(d)} />
              <span>{hs(("cust." + d) as HomeKey)}</span>
            </label>
          ))}
        </fieldset>
        <fieldset class="home-fieldset">
          <legend>{hs("cust.fields")}</legend>
          {order.map((id) => {
            const on = fields.includes(id);
            const i = fields.indexOf(id);
            return (
              <div key={id} class="home-field-row">
                <label class="home-check">
                  <input type="checkbox" checked={on} disabled={on && fields.length === 1} onChange={(e) => toggle(id, (e.currentTarget as HTMLInputElement).checked)} />
                  <span dir="auto">{label(id)}</span>
                </label>
                {on && (
                  <span class="home-move">
                    <button type="button" class="btn btn-small" aria-label={hs("cust.up", { label: label(id) })} disabled={i === 0} onClick={() => setFields(move(fields, i, -1))}>
                      ↑
                    </button>
                    <button
                      type="button"
                      class="btn btn-small"
                      aria-label={hs("cust.down", { label: label(id) })}
                      disabled={i === fields.length - 1}
                      onClick={() => setFields(move(fields, i, 1))}
                    >
                      ↓
                    </button>
                  </span>
                )}
              </div>
            );
          })}
        </fieldset>
        {filterable.length > 0 && (
          <fieldset class="home-fieldset">
            <legend>{hs("cust.filter")}</legend>
            {filter.map((c, i) => {
              const kind = def.fields.find((f) => f.id === c.field)?.kind ?? "text";
              return (
                <div key={i} class="home-cond">
                  <select aria-label={hs("cust.fields")} value={c.field} onChange={(e) => setCond(i, { field: (e.currentTarget as HTMLSelectElement).value, op: "eq" })}>
                    {filterable.map((f) => (
                      <option key={f.id} value={f.id}>
                        {f.label}
                      </option>
                    ))}
                  </select>
                  <select aria-label={hs("cust.filter")} value={c.op} onChange={(e) => setCond(i, { op: (e.currentTarget as HTMLSelectElement).value as CondOp })}>
                    {opsFor(kind).map((op) => (
                      <option key={op} value={op}>
                        {hs(("op." + op) as HomeKey)}
                      </option>
                    ))}
                  </select>
                  {needsValue(c.op) &&
                    (kind === "bool" ? (
                      <select aria-label={hs("cust.value")} value={c.value ?? "true"} onChange={(e) => setCond(i, { value: (e.currentTarget as HTMLSelectElement).value })}>
                        <option value="true">yes</option>
                        <option value="false">no</option>
                      </select>
                    ) : (
                      <input
                        aria-label={hs("cust.value")}
                        type={kind === "num" || kind === "cost" ? "number" : "text"}
                        autoComplete="off"
                        value={c.value ?? ""}
                        onInput={(e) => setCond(i, { value: (e.currentTarget as HTMLInputElement).value })}
                      />
                    ))}
                  <button type="button" class="btn btn-small" aria-label={hs("cust.remove")} onClick={() => setFilter(filter.filter((_, j) => j !== i))}>
                    ×
                  </button>
                </div>
              );
            })}
            {filter.length < MAX_FILTER && (
              <button type="button" class="btn" onClick={() => setFilter([...filter, { field: filterable[0].id, op: "eq", value: filterable[0].kind === "bool" ? "true" : "" }])}>
                {hs("cust.add_filter")}
              </button>
            )}
          </fieldset>
        )}
        {sortable.length > 0 && (
          <fieldset class="home-fieldset">
            <legend>{hs("cust.sort")}</legend>
            <select aria-label={hs("cust.sort")} value={sortBy} onChange={(e) => setSortBy((e.currentTarget as HTMLSelectElement).value)}>
              <option value="">{hs("cust.sort_none")}</option>
              {sortable.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.label}
                </option>
              ))}
            </select>
            {sortBy && (
              <label class="home-check">
                <input type="checkbox" checked={desc} onChange={(e) => setDesc((e.currentTarget as HTMLInputElement).checked)} />
                <span>{hs("cust.desc")}</span>
              </label>
            )}
          </fieldset>
        )}
        <div class="home-cond">
          <label class="home-field">
            <span>{hs("cust.group")}</span>
            <select value={group} onChange={(e) => setGroup((e.currentTarget as HTMLSelectElement).value)}>
              <option value="">{hs("cust.group_none")}</option>
              {def.fields.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.label}
                </option>
              ))}
            </select>
          </label>
          <label class="home-field">
            <span>{hs("cust.limit")}</span>
            <input type="number" inputMode="numeric" min={1} max={MAX_LIMIT} value={limit} onInput={(e) => setLimit((e.currentTarget as HTMLInputElement).value)} />
          </label>
        </div>
        <div class="home-actions">
          <button type="button" class="btn" onClick={onClose}>
            {hs("cust.cancel")}
          </button>
          <button type="button" class="btn btn-primary" onClick={apply}>
            {hs("cust.apply")}
          </button>
        </div>
      </div>
    </S>
  );
}
