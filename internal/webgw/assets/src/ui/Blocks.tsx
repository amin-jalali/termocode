// Typed blocks from the laptop (mobile plan 3.2, MOB-3): list, table,
// diff, card, detail, chart, status, form, json, text and board. A block
// is data only; each kind is drawn here from its fields, every string
// becomes a text node and markdown stays an AST (Md). An unknown kind
// shows its title and "Open on laptop". Status is colour and a shape
// (✓ ● ✗ ◐ ○ ⓘ), never colour alone. Phone layout; 44 px targets.
//
// Only the phone's lazy views (the Home) and the dev kit import this
// file, so app.js does not grow.
import { Fragment, h } from "preact";
import { useState } from "preact/hooks";
import type {
  Block,
  BlockAction,
  BlockBar,
  BlockBoardColumn,
  BlockColumn,
  BlockField,
  BlockFormField,
  BlockGroup,
  BlockItem,
} from "../../../../webproto/proto";
import { dict, own, type Dict } from "../state/dict";
import { age } from "../state/git";
import { t, type StringKey } from "../strings";
import "../strings.phone";
import { Button } from "./Button";
import { CodeBlock, Md } from "./Markdown";
import { DiffRow } from "./rows";

/** KINDS are the block kinds this page draws (internal/blocks.Kinds). */
export const KINDS = ["list", "table", "diff", "card", "detail", "chart", "status", "form", "json", "text", "board"] as const;

export type Tone = "ok" | "warn" | "bad" | "busy" | "idle" | "info";

/** SHAPES pair every tone with a shape, so no state is colour alone. */
export const SHAPES: Record<Tone, string> = { ok: "✓", warn: "●", bad: "✗", busy: "◐", idle: "○", info: "ⓘ" };

const TONE_LABEL: Record<Tone, StringKey> = {
  ok: "blk.tone.ok",
  warn: "blk.tone.warn",
  bad: "blk.tone.bad",
  busy: "blk.tone.busy",
  idle: "blk.tone.idle",
  info: "blk.tone.info",
};

/** toneOf reads a tone from data; anything else is no tone. */
export function toneOf(v: unknown): Tone | undefined {
  return typeof v === "string" && Object.prototype.hasOwnProperty.call(SHAPES, v) ? (v as Tone) : undefined;
}

/** onAction gets a pressed action with the block and item it belongs to. */
export type BlockActionFn = (a: BlockAction, b: Block, item?: BlockItem) => void;

export interface BlockProps {
  block: Block;
  onAction?: BlockActionFn;
  /** onLaptop: "Open on laptop" for a kind this page does not know. */
  onLaptop?: (b: Block) => void;
  /** onForm: the values of a form block; without it the form is read-only. */
  onForm?: (b: Block, values: Readonly<Record<string, string>>) => void;
  /** now is the clock for item ages (tests and the kit pin it). */
  now?: number;
}

function arr<T>(v: T[] | undefined | null): T[] {
  return Array.isArray(v) ? v : [];
}

export function ToneMark({ tone }: { tone?: string }) {
  const k = toneOf(tone);
  if (!k) return null;
  return (
    <span class={`blk-tone tone-${k}`} role="img" aria-label={t(TONE_LABEL[k])}>
      {SHAPES[k]}
    </span>
  );
}

/** safeFirst puts safe actions before risky ones (safe choice first). */
function safeFirst(as: BlockAction[]): BlockAction[] {
  return [...as.filter((a) => !a.risky), ...as.filter((a) => a.risky)];
}

function Actions({ list, block, item, onAction, small }: { list?: BlockAction[]; block: Block; item?: BlockItem; onAction?: BlockActionFn; small?: boolean }) {
  const as = safeFirst(arr(list)).slice(0, 6);
  if (!as.length) return null;
  return (
    <div class="blk-actions">
      {as.map((a) => (
        <Button key={a.id} small={small} kind={a.risky ? "danger" : "neutral"} needsLink={a.kind === "op"} disabled={!onAction} onClick={() => onAction?.(a, block, item)}>
          {a.label}
        </Button>
      ))}
    </div>
  );
}

function Item({ it, block, onAction, now }: { it: BlockItem; block: Block; onAction?: BlockActionFn; now: number }) {
  return (
    <li class="blk-item">
      <ToneMark tone={it.tone} />
      <div class="blk-item-main">
        <span class="blk-item-title" dir="auto">
          {it.title}
        </span>
        {it.sub && (
          <span class="blk-item-sub" dir="auto">
            {it.sub}
          </span>
        )}
      </div>
      {it.badge && (
        <span class="blk-badge" dir="ltr">
          {it.badge}
        </span>
      )}
      {it.time ? <span class="blk-age">{age(it.time, now)}</span> : null}
      <Actions list={it.actions} block={block} item={it} onAction={onAction} small />
    </li>
  );
}

function Items({ list, block, onAction, now }: { list: BlockItem[]; block: Block; onAction?: BlockActionFn; now: number }) {
  if (!list.length) return <p class="blk-empty">{t("blk.empty")}</p>;
  return (
    <ul class="blk-items">
      {list.map((it, i) => (
        <Item key={it.id + ":" + i} it={it} block={block} onAction={onAction} now={now} />
      ))}
    </ul>
  );
}

function GroupHead({ g }: { g: BlockGroup }) {
  return (
    <div class="blk-group">
      <span dir="auto">{g.label}</span>
      <span class="blk-count" aria-label={t("blk.count", { n: g.count })}>
        {g.count}
      </span>
    </div>
  );
}

function ListBlock({ b, onAction, now }: { b: Block; onAction?: BlockActionFn; now: number }) {
  const groups = arr(b.groups);
  if (!groups.length) return <Items list={arr(b.items)} block={b} onAction={onAction} now={now} />;
  return (
    <div>
      {groups.map((g, i) => (
        <div key={i}>
          <GroupHead g={g} />
          <Items list={arr(g.items)} block={b} onAction={onAction} now={now} />
        </div>
      ))}
    </div>
  );
}

function Cell({ col, v }: { col: BlockColumn; v: string }) {
  if (col.kind === "tone") {
    const k = toneOf(v);
    return <td class="blk-td is-tone">{k ? <ToneMark tone={k} /> : v}</td>;
  }
  const num = col.kind === "num" || col.kind === "cost";
  return (
    <td class={`blk-td${num ? " is-num" : ""}${col.kind === "mono" ? " is-mono" : ""}`} dir={num || col.kind === "mono" ? "ltr" : "auto"}>
      {v}
    </td>
  );
}

function Rows({ rows, cols }: { rows: string[][]; cols: BlockColumn[] }) {
  return (
    <>
      {rows.map((r, i) => (
        <tr key={i}>
          {cols.map((c, j) => (
            <Cell key={c.id + j} col={c} v={typeof r?.[j] === "string" ? r[j] : ""} />
          ))}
        </tr>
      ))}
    </>
  );
}

function TableBlock({ b }: { b: Block }) {
  const cols = arr(b.columns);
  const groups = arr(b.groups);
  const rows = arr(b.rows);
  if (!rows.length && !groups.length) return <p class="blk-empty">{t("blk.empty")}</p>;
  return (
    <div class="blk-table-wrap" tabIndex={0} role="region" aria-label={b.title}>
      <table class="blk-table">
        <thead>
          <tr>
            {cols.map((c) => (
              <th key={c.id} scope="col" class={c.kind === "num" || c.kind === "cost" ? "is-num" : undefined} dir="auto">
                {c.label}
              </th>
            ))}
          </tr>
        </thead>
        {groups.length ? (
          groups.map((g, i) => (
            <tbody key={i}>
              <tr>
                <th colSpan={cols.length || 1} scope="rowgroup" class="blk-th-group">
                  <GroupHead g={g} />
                </th>
              </tr>
              <Rows rows={arr(g.rows)} cols={cols} />
            </tbody>
          ))
        ) : (
          <tbody>
            <Rows rows={rows} cols={cols} />
          </tbody>
        )}
      </table>
    </div>
  );
}

function DiffBlock({ b }: { b: Block }) {
  const d = b.diff;
  if (!d) return null;
  return (
    <div class="blk-diff">
      <div class="blk-diff-head" dir="ltr">
        <span class="blk-diff-path">{d.path}</span>
        <span class="blk-diff-stat">{t("blk.diff_stat", { adds: d.adds ?? 0, dels: d.dels ?? 0 })}</span>
      </div>
      <div class="blk-diff-rows">
        {arr(d.hunks).map((hk, i) => (
          <div key={i}>
            {hk.header && <DiffRow kind="hunk" text={hk.header} />}
            {arr(hk.lines).map((l, j) => (
              <DiffRow key={j} kind={l.k === "add" || l.k === "del" ? l.k : "ctx"} a={l.a} b={l.b} text={l.text} />
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}

function FieldValue({ f }: { f: BlockField }) {
  const mono = f.kind === "mono" || f.kind === "num" || f.kind === "cost";
  return (
    <dd class={`blk-dd${mono ? " is-mono" : ""}`} dir={mono ? "ltr" : "auto"}>
      <ToneMark tone={f.tone} />
      {f.value}
    </dd>
  );
}

function FieldsBlock({ b, detail }: { b: Block; detail?: boolean }) {
  const fs = arr(b.fields);
  if (!fs.length) return <p class="blk-empty">{t("blk.empty")}</p>;
  return (
    <dl class={detail ? "blk-detail" : "blk-card"}>
      {fs.map((f, i) => (
        <div key={i} class="blk-field">
          <dt class="blk-dt" dir="auto">
            {f.label}
          </dt>
          <FieldValue f={f} />
        </div>
      ))}
    </dl>
  );
}

function Bar({ bar, max }: { bar: BlockBar; max: number }) {
  const v = typeof bar.value === "number" && isFinite(bar.value) && bar.value > 0 ? bar.value : 0;
  const pct = max > 0 ? Math.max(1, Math.round((v / max) * 100)) : 0;
  const shown = bar.text || String(v);
  const k = toneOf(bar.tone) ?? "info";
  return (
    <li class="blk-bar" aria-label={t("blk.chart_bar", { label: bar.label, value: shown })}>
      <span class="blk-bar-label" dir="auto" aria-hidden="true">
        {bar.label}
      </span>
      <span class="blk-bar-track" aria-hidden="true">
        <span class={`blk-bar-fill tone-${k}`} style={{ width: pct + "%" }} />
      </span>
      <span class="blk-bar-value" dir="ltr" aria-hidden="true">
        {shown}
      </span>
    </li>
  );
}

function ChartBlock({ b }: { b: Block }) {
  const bars = arr(b.bars);
  if (!bars.length) return <p class="blk-empty">{t("blk.empty")}</p>;
  const max = Math.max(0, ...bars.map((x) => (typeof x.value === "number" && isFinite(x.value) ? x.value : 0)));
  return (
    <ul class="blk-chart">
      {bars.map((bar, i) => (
        <Bar key={i} bar={bar} max={max} />
      ))}
    </ul>
  );
}

function StatusBlock({ b }: { b: Block }) {
  const k = toneOf(b.state) ?? "idle";
  return (
    <div class={`blk-status tone-${k}`}>
      <span class="blk-status-mark" aria-hidden="true">
        {SHAPES[k]}
      </span>
      <span class="blk-status-text">{t(TONE_LABEL[k])}</span>
    </div>
  );
}

function FormInput({ f, value, onInput, off }: { f: BlockFormField; value: string; onInput: (v: string) => void; off: boolean }) {
  const id = "blkf-" + f.id;
  const head = (
    <label class="blk-form-label" for={id} dir="auto">
      {f.label}
      {f.required ? <span aria-hidden="true"> *</span> : null}
    </label>
  );
  const hint = f.hint ? (
    <span class="blk-form-hint" dir="auto">
      {f.hint}
    </span>
  ) : null;
  if (f.kind === "bool") {
    return (
      <div class="blk-form-row is-bool">
        <input id={id} type="checkbox" checked={value === "true"} disabled={off} onChange={(e) => onInput((e.currentTarget as HTMLInputElement).checked ? "true" : "false")} />
        {head}
        {hint}
      </div>
    );
  }
  if (f.kind === "choice") {
    return (
      <div class="blk-form-row">
        {head}
        <select id={id} value={value} disabled={off} required={f.required} onChange={(e) => onInput((e.currentTarget as HTMLSelectElement).value)}>
          {arr(f.choices).map((c) => (
            <option key={c} value={c}>
              {c}
            </option>
          ))}
        </select>
        {hint}
      </div>
    );
  }
  if (f.kind === "long") {
    return (
      <div class="blk-form-row">
        {head}
        <textarea id={id} rows={3} value={value} disabled={off} required={f.required} dir="auto" onInput={(e) => onInput((e.currentTarget as HTMLTextAreaElement).value)} />
        {hint}
      </div>
    );
  }
  return (
    <div class="blk-form-row">
      {head}
      <input
        id={id}
        type="text"
        inputMode={f.kind === "num" ? "decimal" : undefined}
        autoComplete="off"
        value={value}
        disabled={off}
        required={f.required}
        dir="auto"
        onInput={(e) => onInput((e.currentTarget as HTMLInputElement).value)}
      />
      {hint}
    </div>
  );
}

function FormBlock({ b, onForm }: { b: Block; onForm?: BlockProps["onForm"] }) {
  const fields = arr(b.form);
  // Field ids come from data: the values live in a dict (no prototype).
  const [vals, setVals] = useState<Dict<string>>(() => {
    const v = dict<string>();
    for (const f of fields) v[f.id] = f.value ?? (f.kind === "choice" ? arr(f.choices)[0] ?? "" : "");
    return v;
  });
  const set = (id: string, v: string) => {
    const next = dict(vals);
    next[id] = v;
    setVals(next);
  };
  const off = !onForm;
  const missing = fields.some((f) => f.required && !(own(vals, f.id) ?? "").trim());
  return (
    <form
      class="blk-form"
      onSubmit={(e) => {
        e.preventDefault();
        if (!off && !missing) onForm?.(b, vals);
      }}
    >
      {fields.map((f) => (
        <FormInput key={f.id} f={f} value={own(vals, f.id) ?? ""} off={off} onInput={(v) => set(f.id, v)} />
      ))}
      {off ? (
        <p class="blk-form-hint">{t("blk.form_laptop")}</p>
      ) : (
        <Button kind="primary" needsLink disabled={missing} onClick={() => onForm?.(b, vals)}>
          {t("blk.form_send")}
        </Button>
      )}
    </form>
  );
}

/** pretty indents JSON for reading; text that is not JSON stays as it is. */
export function pretty(s: string): string {
  try {
    return JSON.stringify(JSON.parse(s), null, 2);
  } catch {
    return s;
  }
}

function JSONBlock({ b }: { b: Block }) {
  return (
    <div class="blk-json" aria-label={t("blk.raw")} role="group">
      <CodeBlock text={pretty(b.json ?? "")} lang="json" />
    </div>
  );
}

function TextBlock({ b }: { b: Block }) {
  return b.md ? <Md doc={b.md} /> : null;
}

function BoardColumn({ c, block, onAction, now }: { c: BlockBoardColumn; block: Block; onAction?: BlockActionFn; now: number }) {
  const items = arr(c.items);
  return (
    <section class="blk-board-col" aria-label={c.label}>
      <header class="blk-board-head">
        <ToneMark tone={c.tone} />
        <span dir="auto">{c.label}</span>
        <span class="blk-count" aria-label={t("blk.count", { n: items.length })}>
          {items.length}
        </span>
      </header>
      <Items list={items} block={block} onAction={onAction} now={now} />
    </section>
  );
}

function BoardBlock({ b, onAction, now }: { b: Block; onAction?: BlockActionFn; now: number }) {
  const cols = arr(b.board?.columns);
  return (
    <div class="blk-board">
      {cols.map((c) => (
        <BoardColumn key={c.id} c={c} block={b} onAction={onAction} now={now} />
      ))}
    </div>
  );
}

function Body({ b, onAction, onForm, now }: { b: Block; onAction?: BlockActionFn; onForm?: BlockProps["onForm"]; now: number }) {
  switch (b.kind) {
    case "list":
      return <ListBlock b={b} onAction={onAction} now={now} />;
    case "table":
      return <TableBlock b={b} />;
    case "diff":
      return <DiffBlock b={b} />;
    case "card":
      return <FieldsBlock b={b} />;
    case "detail":
      return <FieldsBlock b={b} detail />;
    case "chart":
      return <ChartBlock b={b} />;
    case "status":
      return <StatusBlock b={b} />;
    case "form":
      return <FormBlock b={b} onForm={onForm} />;
    case "json":
      return <JSONBlock b={b} />;
    case "text":
      return <TextBlock b={b} />;
    case "board":
      return <BoardBlock b={b} onAction={onAction} now={now} />;
    default:
      return null;
  }
}

/** known says whether this page can draw the block's kind. */
export function known(b: Block): boolean {
  return (KINDS as readonly string[]).includes(b?.kind);
}

/** BlockView draws one block: head (title, live, offline), body, the
 * "showing n of total" line, the for-you line and the actions. */
export function BlockView({ block, onAction, onLaptop, onForm, now = Date.now() }: BlockProps) {
  const b = block;
  if (!b || typeof b !== "object") return null;
  const ok = known(b);
  const shown = b.kind === "table" ? arr(b.rows).length : arr(b.items).length;
  return (
    <article class={`blk blk-${ok ? b.kind : "unknown"}${b.stale ? " is-stale" : ""}`} aria-label={b.title}>
      <header class="blk-head">
        <h3 class="blk-title" dir="auto">
          {b.title}
        </h3>
        {b.live && !b.stale && (
          <span class="blk-live">
            <span class="blk-live-dot" aria-hidden="true">
              ●
            </span>
            {t("blk.live")}
          </span>
        )}
        {b.stale && <span class="blk-stale">{t("blk.stale")}</span>}
      </header>
      {ok ? (
        <div class="blk-body">
          <Body b={b} onAction={onAction} onForm={onForm} now={now} />
        </div>
      ) : (
        <div class="blk-body">
          <p class="blk-empty">{t("blk.unknown")}</p>
          <Button onClick={() => onLaptop?.(b)} disabled={!onLaptop} needsLink>
            {t("laptop.open")}
          </Button>
        </div>
      )}
      {ok && (b.truncated || (b.total ?? 0) > shown) && shown > 0 && <p class="blk-more">{t("blk.more", { shown, total: Math.max(b.total ?? 0, shown) })}</p>}
      {ok && b.for_you && (
        <p class="blk-for-you" dir="auto">
          <span aria-hidden="true">✦ </span>
          {b.for_you}
        </p>
      )}
      {ok && <Actions list={b.actions} block={b} onAction={onAction} />}
    </article>
  );
}

/** Blocks draws the blocks of one message or step, in order. */
export function Blocks({ list, ...rest }: Omit<BlockProps, "block"> & { list?: Block[] | null }) {
  const bs = arr(list ?? undefined);
  if (!bs.length) return null;
  return (
    <div class="blks">
      {bs.map((b, i) => (
        <BlockView key={(b?.id ?? "") + ":" + i} block={b} {...rest} />
      ))}
    </div>
  );
}
