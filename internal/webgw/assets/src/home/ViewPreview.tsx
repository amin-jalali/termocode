// The Customize sheet's live preview (MOBUX-16): the block the laptop
// built from the draft spec, drawn small. The laptop applies the spec
// (view.run); this only draws the rows it sent. Table, list, card and
// detail are drawn; other kinds show their title only. Status is colour
// and shape (✓ ● ✗ ◐ ○ i).
import { h } from "preact";
import { t } from "../strings";

export type Tone = "ok" | "warn" | "bad" | "busy" | "idle" | "info";

/** PreviewBlock is the part of a block (blocks.Block) the preview draws. */
export interface PreviewBlock {
  id: string;
  kind: string;
  title: string;
  columns?: { id: string; label: string; kind?: string }[];
  rows?: string[][];
  items?: { id: string; title: string; sub?: string; tone?: Tone; badge?: string }[];
  groups?: { label: string; count: number; rows?: string[][]; items?: { id: string; title: string; sub?: string; tone?: Tone; badge?: string }[] }[];
  fields?: { label: string; value: string; tone?: Tone }[];
  total?: number;
  truncated?: boolean;
}

/** PREVIEW_ROWS is how many rows the preview draws; the count says the rest. */
export const PREVIEW_ROWS = 8;

const SHAPES: Record<Tone, string> = { ok: "✓", warn: "●", bad: "✗", busy: "◐", idle: "○", info: "i" };

function Mark({ tone }: { tone?: Tone }) {
  if (!tone) return null;
  return (
    <span class={`vp-mark tone-${tone}`} aria-hidden="true">
      {SHAPES[tone]}
    </span>
  );
}

function Table({ b, rows }: { b: PreviewBlock; rows: string[][] }) {
  const cols = b.columns ?? [];
  return (
    <table class="vp-table">
      <thead>
        <tr>
          {cols.map((c) => (
            <th key={c.id} scope="col" class={c.kind === "num" || c.kind === "cost" ? "is-num" : undefined}>
              {c.label}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.slice(0, PREVIEW_ROWS).map((r, i) => (
          <tr key={i}>
            {cols.map((c, j) => (
              <td key={c.id} class={c.kind === "num" || c.kind === "cost" ? "is-num" : undefined}>
                {r[j] ?? ""}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

type Item = NonNullable<PreviewBlock["items"]>[number];

function Items({ items }: { items: Item[] }) {
  return (
    <ul class="vp-list">
      {items.slice(0, PREVIEW_ROWS).map((it) => (
        <li key={it.id} class="vp-item">
          <Mark tone={it.tone} />
          <span class="vp-item-text">
            <span class="vp-item-title">{it.title}</span>
            {it.sub ? <span class="vp-item-sub">{it.sub}</span> : null}
          </span>
          {it.badge ? <span class="vp-badge">{it.badge}</span> : null}
        </li>
      ))}
    </ul>
  );
}

function Body({ b }: { b: PreviewBlock }) {
  if (b.groups?.length) {
    return (
      <div class="vp-groups">
        {b.groups.slice(0, 4).map((g) => (
          <div key={g.label} class="vp-group">
            <div class="vp-group-head">{t("cust.group_head", { name: g.label, n: g.count })}</div>
            {b.kind === "table" ? <Table b={b} rows={g.rows ?? []} /> : <Items items={g.items ?? []} />}
          </div>
        ))}
      </div>
    );
  }
  if (b.kind === "table") return <Table b={b} rows={b.rows ?? []} />;
  if (b.kind === "list") return <Items items={b.items ?? []} />;
  if (b.kind === "card" || b.kind === "detail") {
    return (
      <dl class={`vp-fields${b.kind === "card" ? " is-card" : ""}`}>
        {(b.fields ?? []).slice(0, b.kind === "card" ? PREVIEW_ROWS : 40).map((f, i) => (
          <div key={i} class="vp-field">
            <dt>{f.label}</dt>
            <dd>
              <Mark tone={f.tone} />
              {f.value}
            </dd>
          </div>
        ))}
      </dl>
    );
  }
  return null;
}

/** shownCount is how many rows (or items) the block holds. */
export function shownCount(b: PreviewBlock): number {
  if (b.groups?.length) return b.groups.reduce((n, g) => n + (g.rows?.length ?? g.items?.length ?? 0), 0);
  if (b.kind === "table") return b.rows?.length ?? 0;
  if (b.kind === "list") return b.items?.length ?? 0;
  return b.fields?.length ? 1 : 0;
}

export function ViewPreview({ block, density }: { block: PreviewBlock; density: string }) {
  const n = shownCount(block);
  const total = block.total ?? n;
  return (
    <div class={`vp vp-${block.kind} is-${density === "detailed" ? "detailed" : "compact"}`} data-kind={block.kind}>
      {n === 0 ? <p class="vp-empty">{t("cust.preview_empty")}</p> : <Body b={block} />}
      <p class="vp-count" aria-live="polite">
        {t("cust.rows_of", { n: Math.min(n, total), total })}
      </p>
    </div>
  );
}
