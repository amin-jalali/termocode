// The db_connections card in the chat (owner 2026-11-15: "there are no
// actions on the results"): one row per connection with its status shape
// and badge, why (the last error, the missing field), and its buttons.
// Connect and Test ask the laptop with the Databases view's own ops (its
// rules and cards hold there); Open in Databases, List tables, Fix… and
// a tap on the row open the Databases view on the connection, New query
// a query on it; + Chat puts the row into the next message. The host
// says how (the desktop's workbench, the phone's pages). Drawn by the
// chat's look (look.tsx) on a tool line and under a one-step answer.
import type { Block, BlockAction, BlockItem } from "../../../../webproto/proto";
import { Fragment, H, h } from "./jsx";

const SHAPE: Record<string, string> = { ok: "✓", warn: "●", bad: "✗", busy: "◐", info: "i" };

/** dbCards are the database cards among blocks. */
export const dbCards = (list?: readonly Block[]): Block[] => (list ?? []).filter((b) => b.source === "db_connections" && b.kind === "list");

/** what a button does on the page (its op after "db."): the others are
 * ops of the laptop. */
const OPEN: Record<string, "open" | "query"> = { open: "open", tables: "open", new_query: "query" };

function press(a: BlockAction, conn: string): void {
  const op = a.op ?? "";
  const k = op.slice(op.indexOf(".") + 1);
  const to = OPEN[k] ?? (k.startsWith("fix_") ? "fix" : null);
  if (to) return H.dbOpen?.(conn, to);
  H.link.request(op, { conn }).then(
    (r) => H.toast("ok", (r as { text?: string }).text || a.label),
    (e: unknown) => H.toast("warn", (e as { message?: string })?.message || String(e)),
  );
}

function Row({ b, it }: { b: Block; it: BlockItem }) {
  const conn = it.ref?.id ?? "";
  const tone = it.tone ?? "idle";
  return (
    <div class={`dbcard-row is-${tone}`} data-conn={conn}>
      <button type="button" class="dbcard-head" title={H.t("dbcard.open", { name: it.title })} onClick={() => H.dbOpen?.(conn, "open")}>
        <span class="dbcard-shape" aria-hidden="true">
          {SHAPE[tone] ?? "○"}
        </span>
        <span class="dbcard-name">{it.title}</span>
        {it.badge && <span class="dbcard-badge">{it.badge}</span>}
      </button>
      {it.sub && <div class="dbcard-sub">{it.sub}</div>}
      <div class="dbcard-acts">
        {(it.actions ?? []).map((a) => (
          <button type="button" key={a.id} class={`dbcard-act${a.id === "fix" ? " is-fix" : ""}`} onClick={() => press(a, conn)}>
            {a.label}
          </button>
        ))}
        <button type="button" class="dbcard-act" onClick={() => H.dbChat?.(conn, b.id + "/" + it.id, it.title)}>
          {H.t("dbcard.chat")}
        </button>
      </div>
    </div>
  );
}

/** DBCards draws the database cards of a message or a step. */
export function DBCards({ list }: { list?: readonly Block[] }) {
  const cards = dbCards(list);
  if (!cards.length) return null;
  return (
    <>
      {cards.map((b) => (
        <div class="dbcard" key={b.id} dir="ltr">
          <div class="dbcard-title">{b.title}</div>
          {(b.items ?? []).map((it) => (
            <Row key={it.id} b={b} it={it} />
          ))}
        </div>
      ))}
    </>
  );
}
