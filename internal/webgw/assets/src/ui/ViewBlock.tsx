// A view block on the phone (MOBUX-15): a block the laptop built from a
// view spec (internal/views). The laptop checks the spec, fetches the
// data and applies filter, sort, group and limit; this file only draws
// the result. It adds what a view needs on top of BlockView: the
// density (compact / detailed), time cells as ages ("2h"), and one line
// of spec chips that says what the view shows (filter, sort, group,
// limit) with the ⚙ that opens the customizer.
//
// Only the phone's lazy views (the Home) and the dev kit import this
// file, so app.js does not grow.
import { h } from "preact";
import type { Block, BlockColumn, ViewCond, ViewSpec } from "../../../../webproto/proto";
import { age } from "../state/git";
import { t } from "../strings";
import "../strings.phone";
import "../styles/viewblock.css";
import { BlockView, type BlockProps } from "./Blocks";

const OP_SIGN: Record<string, string> = {
  eq: "=",
  ne: "≠",
  gt: ">",
  lt: "<",
  ge: "≥",
  le: "≤",
  contains: "∋",
  starts: "^",
};

function label(cols: BlockColumn[] | undefined, id: string): string {
  const c = (cols ?? []).find((x) => x?.id === id);
  return c?.label || id;
}

/** condText is one filter condition as a short chip text. */
export function condText(c: ViewCond, cols?: BlockColumn[]): string {
  const f = label(cols, c.field);
  switch (c.op) {
    case "in":
      return t("view.cond_in", { field: f, values: (c.in ?? []).join(", ") });
    case "empty":
      return t("view.cond_empty", { field: f });
    case "not_empty":
      return t("view.cond_not_empty", { field: f });
  }
  return `${f} ${OP_SIGN[c.op] ?? c.op} ${c.value ?? ""}`.trim();
}

/** specChips says what a spec does, one short text per choice, in the
 * order the customizer shows them: filter, sort, group, limit. */
export function specChips(s: ViewSpec | undefined, cols?: BlockColumn[]): string[] {
  if (!s) return [];
  const out: string[] = [];
  for (const c of s.filter ?? []) out.push(condText(c, cols));
  for (const o of s.sort ?? []) out.push(t("view.sort", { field: label(cols, o.field), dir: o.desc ? "↓" : "↑" }));
  if (s.group) out.push(t("view.group", { field: label(cols, s.group) }));
  if (s.limit) out.push(t("view.limit", { n: s.limit }));
  return out;
}

function timeCols(b: Block): number[] {
  const out: number[] = [];
  (b.columns ?? []).forEach((c, i) => {
    if (c?.kind === "time") out.push(i);
  });
  return out;
}

function ageCells(rows: string[][] | undefined, at: number[], now: number): string[][] | undefined {
  if (!rows || !at.length) return rows;
  return rows.map((r) => {
    const out = r.slice();
    for (const i of at) {
      const ms = Number(out[i]);
      if (out[i] && Number.isFinite(ms) && ms > 0) out[i] = age(ms, now);
    }
    return out;
  });
}

/** withAges returns the block with each time cell (Unix ms from the
 * laptop) shown as an age, so a live view never shows a stale "2h". */
export function withAges(b: Block, now: number): Block {
  const at = timeCols(b);
  if (!at.length) return b;
  return {
    ...b,
    rows: ageCells(b.rows, at, now),
    groups: b.groups?.map((g) => ({ ...g, rows: ageCells(g.rows, at, now) })),
  };
}

export interface ViewBlockProps extends BlockProps {
  /** onCustomize opens the customizer on this block's spec; without it
   * the chips are plain text and there is no ⚙. */
  onCustomize?: (b: Block) => void;
}

/** ViewBlock draws a block that came from a view spec. A block without
 * a spec is drawn as a plain block. */
export function ViewBlock({ block, onCustomize, now = Date.now(), ...rest }: ViewBlockProps) {
  const b = block;
  if (!b || typeof b !== "object") return null;
  if (!b.spec) return <BlockView block={b} now={now} {...rest} />;
  const density = b.spec.density === "detailed" ? "detailed" : "compact";
  const chips = specChips(b.spec, b.columns);
  return (
    <section class={`vblk is-${density}`} aria-label={b.title}>
      {(chips.length > 0 || onCustomize) && (
        <div class="vblk-spec">
          <ul class="vblk-chips" aria-label={t("view.spec_label")}>
            {chips.map((c, i) => (
              <li key={i} class="vblk-chip" dir="auto">
                {c}
              </li>
            ))}
          </ul>
          {onCustomize && (
            <button type="button" class="vblk-gear" aria-label={t("view.customize")} title={t("view.customize")} onClick={() => onCustomize(b)}>
              <span aria-hidden="true">⚙</span>
            </button>
          )}
        </div>
      )}
      <BlockView block={withAges(b, now)} now={now} {...rest} />
    </section>
  );
}
