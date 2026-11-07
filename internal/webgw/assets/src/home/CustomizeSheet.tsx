// CustomizeSheet.tsx: the MOBUX-16 Customize sheet, kept next to MOB-9's
// home/Customize.tsx at merge (one Customize to pick later).
// The Customize sheet (MOBUX-16, mobile plan 2.3 and 3.11.7): ⚙ on every
// view block opens it. The user picks fields (and their order), filters,
// a sort, a group, how many rows, the layout and the density; a live
// preview shows what the laptop builds from the draft. The laptop checks
// and runs every spec (view.run); the sheet only keeps the draft inside
// the same caps (home/viewspec.ts). Cancel is first and safe: dragging
// the sheet down, the dim area or Esc mean Cancel too, and nothing
// changes. Apply hands the spec back; "Ask the AI to change it" puts
// "Change this view: " in the box with the draft attached.
import { h, type ComponentChildren } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useRef, useState } from "preact/hooks";
import { t, type StringKey } from "../strings";
import { Button } from "../ui/Button";
import { Segmented } from "../ui/Segmented";
import { Sheet } from "../ui/Sheet";
import { SkeletonRows } from "../ui/Skeleton";
import { ViewPreview, type PreviewBlock } from "./ViewPreview";
import {
  addCond,
  changed,
  cleaned,
  condValue,
  draftOf,
  fieldOf,
  filterable,
  layoutOf,
  MAX_FILTER,
  MAX_LIMIT,
  maxFields,
  MIN_LIMIT,
  moveField,
  needsValue,
  opsFor,
  removeCond,
  setCond,
  setDensity,
  setGroup,
  setLayout,
  setLimit,
  setSort,
  shownFields,
  sortable,
  specKey,
  toggleField,
  type CondOp,
  type Density,
  type ViewInfo,
  type ViewSpec,
} from "./viewspec";

/** PREVIEW_WAIT_MS is how long the draft rests before the preview runs it. */
export const PREVIEW_WAIT_MS = 350;

/** CustomizeRequest is what a view block hands the sheet. */
export interface CustomizeRequest {
  info: ViewInfo;
  spec?: ViewSpec;
  /** block is the view's block as it is now: the first preview. */
  block?: PreviewBlock;
  /** run asks the laptop for the block of a spec (view.run, not live). */
  run: (spec: ViewSpec) => Promise<PreviewBlock>;
  /** onApply gets the new spec; the block runs it again. */
  onApply: (spec: ViewSpec) => void;
  /** onAsk puts text in the box with the spec attached (a view attach). */
  onAsk?: (text: string, spec: ViewSpec) => void;
}

/** customizing is the open sheet's request (null: closed). */
export const customizing = signal<CustomizeRequest | null>(null);

/** openCustomize opens the sheet for a view block. */
export function openCustomize(req: CustomizeRequest): void {
  customizing.value = req;
}

/** CustomizeLayer draws the open sheet; the Home mounts it once. */
export function CustomizeLayer() {
  const req = customizing.value;
  if (!req) return null;
  return <CustomizeSheet key={req.info.id} req={req} onClose={() => (customizing.value = null)} />;
}

/** CustomizeButton is the ⚙ on a view block's head. */
export function CustomizeButton({ title, onOpen }: { title: string; onOpen: () => void }) {
  return (
    <button type="button" class="cust-gear" aria-label={t("cust.open", { name: title })} title={t("cust.open", { name: title })} onClick={onOpen}>
      <span aria-hidden="true">⚙</span>
    </button>
  );
}

const LAYOUT_NAMES: Record<string, StringKey> = { table: "cust.l_table", list: "cust.l_list", card: "cust.l_card", detail: "cust.l_detail" };

const OP_NAMES: Record<CondOp, StringKey> = {
  eq: "cust.op_eq",
  ne: "cust.op_ne",
  gt: "cust.op_gt",
  lt: "cust.op_lt",
  ge: "cust.op_ge",
  le: "cust.op_le",
  contains: "cust.op_contains",
  starts: "cust.op_starts",
  in: "cust.op_in",
  empty: "cust.op_empty",
  not_empty: "cust.op_not_empty",
};

type Preview = { state: "busy" | "ok"; block?: PreviewBlock } | { state: "failed"; block?: PreviewBlock; why: string };

/** usePreview runs the draft on the laptop once it rests for
 * PREVIEW_WAIT_MS; a slower answer to an older draft is dropped. The
 * block the sheet opened with is the first preview (no run for it). */
function usePreview(req: CustomizeRequest, draft: ViewSpec): [Preview, () => void] {
  const [p, setP] = useState<Preview>(req.block ? { state: "ok", block: req.block } : { state: "busy" });
  const [again, setAgain] = useState(0);
  const skipFirst = useRef(req.block !== undefined);
  const seq = useRef(0);
  const key = specKey(draft);
  useEffect(() => {
    if (skipFirst.current) {
      skipFirst.current = false;
      return;
    }
    const n = ++seq.current;
    setP((old) => ({ state: "busy", block: old.block }));
    const timer = setTimeout(() => {
      req.run(cleaned(draft)).then(
        (block) => {
          if (n === seq.current) setP({ state: "ok", block });
        },
        (e: unknown) => {
          if (n === seq.current) setP((old) => ({ state: "failed", block: old.block, why: e instanceof Error ? e.message : String(e) }));
        },
      );
    }, PREVIEW_WAIT_MS);
    return () => clearTimeout(timer);
  }, [key, again]);
  useEffect(
    () => () => {
      seq.current = -1;
    },
    [],
  );
  return [p, () => setAgain((a) => a + 1)];
}

function Section({ label, children, id }: { label: string; children: ComponentChildren; id: string }) {
  return (
    <section class="cust-sec" aria-labelledby={`cust-${id}`} data-sec={id}>
      <h3 class="cust-h" id={`cust-${id}`}>
        {label}
      </h3>
      {children}
    </section>
  );
}

function Fields({ info, draft, set }: { info: ViewInfo; draft: ViewSpec; set: (d: ViewSpec) => void }) {
  const shown = shownFields(info, draft);
  const hidden = info.fields.filter((f) => !shown.includes(f.id));
  const full = shown.length >= maxFields(layoutOf(info, draft));
  return (
    <div class="cust-fields">
      <ul class="cust-list" aria-label={t("cust.fields_shown")}>
        {shown.map((id, i) => {
          const f = fieldOf(info, id)!;
          return (
            <li key={id} class="cust-field is-on" data-field={id}>
              <label class="cust-check">
                <input type="checkbox" checked disabled={shown.length === 1} onChange={() => set(toggleField(info, draft, id))} />
                <span>{f.label}</span>
              </label>
              <button type="button" class="cust-move" aria-label={t("keys.up", { name: f.label })} disabled={i === 0} onClick={() => set(moveField(info, draft, id, -1))}>
                <span aria-hidden="true">↑</span>
              </button>
              <button type="button" class="cust-move" aria-label={t("keys.down", { name: f.label })} disabled={i === shown.length - 1} onClick={() => set(moveField(info, draft, id, 1))}>
                <span aria-hidden="true">↓</span>
              </button>
            </li>
          );
        })}
      </ul>
      {hidden.length ? (
        <ul class="cust-list is-hidden" aria-label={t("cust.fields_hidden")}>
          {hidden.map((f) => (
            <li key={f.id} class="cust-field" data-field={f.id}>
              <label class="cust-check">
                <input type="checkbox" checked={false} disabled={full} onChange={() => set(toggleField(info, draft, f.id))} />
                <span>{f.label}</span>
              </label>
            </li>
          ))}
        </ul>
      ) : null}
      {full && hidden.length ? <p class="cust-hint">{t("cust.fields_max")}</p> : null}
    </div>
  );
}

function Filters({ info, draft, set }: { info: ViewInfo; draft: ViewSpec; set: (d: ViewSpec) => void }) {
  const fields = filterable(info);
  const conds = draft.filter ?? [];
  if (!fields.length) return <p class="cust-hint">{t("cust.no_filter")}</p>;
  return (
    <div class="cust-filters">
      {conds.map((c, i) => {
        const f = fieldOf(info, c.field);
        return (
          <div key={i} class="cust-cond" data-cond={i}>
            <select class="cust-sel" aria-label={t("cust.cond_field", { n: i + 1 })} value={c.field} onChange={(e) => set(setCond(info, draft, i, { field: (e.currentTarget as HTMLSelectElement).value }))}>
              {fields.map((ff) => (
                <option key={ff.id} value={ff.id}>
                  {ff.label}
                </option>
              ))}
            </select>
            <select class="cust-sel" aria-label={t("cust.cond_op", { n: i + 1 })} value={c.op} onChange={(e) => set(setCond(info, draft, i, { op: (e.currentTarget as HTMLSelectElement).value as CondOp }))}>
              {opsFor(f?.kind ?? "text").map((op) => (
                <option key={op} value={op}>
                  {t(OP_NAMES[op])}
                </option>
              ))}
            </select>
            {needsValue(c.op) ? (
              <input
                class="cust-val"
                type="text"
                inputMode={f?.kind === "num" || f?.kind === "cost" ? "decimal" : undefined}
                aria-label={t("cust.cond_value", { n: i + 1 })}
                placeholder={c.op === "in" ? t("cust.in_hint") : f?.kind === "time" ? t("cust.time_hint") : ""}
                value={condValue(c)}
                maxLength={200}
                onInput={(e) => set(setCond(info, draft, i, { value: (e.currentTarget as HTMLInputElement).value }))}
              />
            ) : null}
            <button type="button" class="cust-del" aria-label={t("cust.remove_filter", { n: i + 1 })} onClick={() => set(removeCond(draft, i))}>
              <span aria-hidden="true">✕</span>
            </button>
          </div>
        );
      })}
      <button type="button" class="cust-add" disabled={conds.length >= MAX_FILTER} onClick={() => set(addCond(info, draft))}>
        <span aria-hidden="true">+</span> {t("cust.add_filter")}
      </button>
    </div>
  );
}

function SortGroup({ info, draft, set }: { info: ViewInfo; draft: ViewSpec; set: (d: ViewSpec) => void }) {
  const sorts = sortable(info);
  const first = draft.sort?.[0];
  return (
    <div class="cust-sortgroup">
      <label class="cust-pair">
        <span>{t("cust.sort")}</span>
        <select class="cust-sel" value={first?.field ?? ""} disabled={!sorts.length} onChange={(e) => set(setSort(info, draft, (e.currentTarget as HTMLSelectElement).value, first?.desc ?? true))}>
          <option value="">{t("cust.sort_none")}</option>
          {sorts.map((f) => (
            <option key={f.id} value={f.id}>
              {f.label}
            </option>
          ))}
        </select>
      </label>
      {first ? (
        <Segmented
          label={t("cust.sort_dir")}
          value={first.desc ? "desc" : "asc"}
          choices={[
            { value: "desc", label: t("cust.desc") },
            { value: "asc", label: t("cust.asc") },
          ]}
          onChange={(v) => set(setSort(info, draft, first.field, v === "desc"))}
        />
      ) : null}
      <label class="cust-pair">
        <span>{t("cust.group")}</span>
        <select class="cust-sel" value={draft.group ?? ""} onChange={(e) => set(setGroup(info, draft, (e.currentTarget as HTMLSelectElement).value))}>
          <option value="">{t("cust.group_none")}</option>
          {info.fields.map((f) => (
            <option key={f.id} value={f.id}>
              {f.label}
            </option>
          ))}
        </select>
      </label>
      <label class="cust-pair">
        <span>{t("cust.limit")}</span>
        <input
          class="cust-num"
          type="number"
          inputMode="numeric"
          min={MIN_LIMIT}
          max={MAX_LIMIT}
          value={draft.limit}
          onChange={(e) => set(setLimit(draft, Number((e.currentTarget as HTMLInputElement).value)))}
        />
      </label>
    </div>
  );
}

function PreviewPane({ p, density, retry }: { p: Preview; density: string; retry: () => void }) {
  return (
    <div class={`cust-preview is-${p.state}`} aria-busy={p.state === "busy" ? "true" : undefined}>
      {p.state === "failed" ? (
        <div class="cust-failed" role="status">
          <span class="vp-mark tone-bad" aria-hidden="true">
            ✗
          </span>
          <span>{t("cust.preview_failed", { why: p.why })}</span>
          <Button onClick={retry}>
            {t("retry")}
          </Button>
        </div>
      ) : null}
      {p.block ? <ViewPreview block={p.block} density={density} /> : <SkeletonRows rows={4} mono={false} />}
      {p.state === "busy" && p.block ? <p class="cust-busy">{t("cust.preview_busy")}</p> : null}
    </div>
  );
}

/** CustomizeSheet is the sheet itself; onClose ends it (Apply or not). */
export function CustomizeSheet({ req, onClose }: { req: CustomizeRequest; onClose: () => void }) {
  const { info } = req;
  const start = useRef(draftOf(info, req.spec));
  const [draft, setDraft] = useState<ViewSpec>(start.current);
  const [preview, retry] = usePreview(req, draft);
  const title = req.spec?.title || info.title;
  const layout = layoutOf(info, draft);
  const layouts = info.layouts.filter((l) => LAYOUT_NAMES[l]);
  const apply = () => {
    if (changed(start.current, draft)) req.onApply(cleaned(draft));
    onClose();
  };
  const ask = () => {
    req.onAsk?.(t("cust.ask_text"), cleaned(draft));
    onClose();
  };
  return (
    <Sheet onLater={onClose} label={t("cust.title", { name: title })}>
      <div class="cust" data-view={info.id}>
        <h2 class="cust-title">{t("cust.title", { name: title })}</h2>
        <PreviewPane p={preview} density={draft.density ?? "compact"} retry={retry} />
        {layouts.length > 1 ? (
          <Section id="layout" label={t("cust.layout")}>
            <Segmented label={t("cust.layout")} value={layout} choices={layouts.map((l) => ({ value: l, label: t(LAYOUT_NAMES[l]) }))} onChange={(l) => setDraft(setLayout(info, draft, l))} />
          </Section>
        ) : null}
        <Section id="density" label={t("cust.density")}>
          <Segmented<Density>
            label={t("cust.density")}
            value={draft.density ?? "compact"}
            choices={[
              { value: "compact", label: t("cust.compact") },
              { value: "detailed", label: t("cust.detailed") },
            ]}
            onChange={(v) => setDraft(setDensity(draft, v))}
          />
        </Section>
        <Section id="fields" label={t("cust.fields")}>
          <Fields info={info} draft={draft} set={setDraft} />
        </Section>
        <Section id="filter" label={t("files.filter")}>
          <Filters info={info} draft={draft} set={setDraft} />
        </Section>
        <Section id="sort" label={t("cust.sort_group")}>
          <SortGroup info={info} draft={draft} set={setDraft} />
        </Section>
        {req.onAsk ? (
          <button type="button" class="cust-ask" onClick={ask}>
            {t("cust.ask")}
          </button>
        ) : null}
        <div class="cust-foot">
          <Button onClick={onClose}>
            {t("cancel")}
          </Button>
          <Button kind="primary" onClick={apply}>
            {t("cust.apply")}
          </Button>
        </div>
      </div>
    </Sheet>
  );
}
