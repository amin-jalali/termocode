// A quick block in the Home stream (MOB-4): the answer to a chip, drawn
// in a frame with its title, a "no tokens" tag and Close. A block the
// laptop built (view.run) is drawn by the page's block renderer (MOB-3)
// when the page has one; the page's own blocks (lists, groups, status
// lines from a topic) are drawn here. Every row is a 44 px button; every
// tone has a shape as well as a colour.
import { h, H } from "./jsx";
import { hs, type HomeKey } from "./strings";
import type { HomeBlock, HomeItem, Quick, Tone } from "../state/home";

const SHAPE: Record<Tone, string> = { ok: "✓", warn: "●", bad: "✗", busy: "◐", idle: "○", info: "i" };

/** ago writes how long ago ms was: "now", "5 m", "2 h", "3 d". */
export function ago(ms: number, now: number): string {
  const s = Math.max(0, Math.floor((now - ms) / 1000));
  if (s < 60) return "now";
  if (s < 3600) return `${Math.floor(s / 60)} m`;
  if (s < 86400) return `${Math.floor(s / 3600)} h`;
  return `${Math.floor(s / 86400)} d`;
}

export function ToneMark({ tone }: { tone?: Tone }) {
  const t = tone ?? "idle";
  return (
    <span class={`home-tone tone-${t}`}>
      <span aria-hidden="true">{SHAPE[t]}</span>
      <span class="sr-only">{hs(("tone." + t) as HomeKey)}</span>
    </span>
  );
}

function Row({ it, onOpen, now }: { it: HomeItem; onOpen: (id: string) => void; now: number }) {
  return (
    <li>
      <button type="button" class="home-row" onClick={() => onOpen(it.id)}>
        <ToneMark tone={it.tone} />
        <span class="home-row-main">
          <span class="home-row-title trunc" dir="auto">
            {it.title}
          </span>
          {it.sub && (
            <span class="home-row-sub trunc" dir="auto">
              {it.sub}
            </span>
          )}
        </span>
        {it.badge && <span class="home-row-badge mono">{it.badge}</span>}
        {it.time ? <span class="home-row-age">{ago(it.time, now)}</span> : null}
      </button>
    </li>
  );
}

/** PageBlock draws the kinds the Home builds itself: list (with or
 * without groups) and status. */
export function PageBlock({ b, onOpen }: { b: HomeBlock; onOpen: (id: string) => void }) {
  const now = Date.now();
  if (b.kind === "status") {
    return (
      <p class="home-status">
        <ToneMark tone={b.state} /> <span dir="auto">{b.title}</span>
      </p>
    );
  }
  const shown = b.groups ? b.groups.reduce((n, g) => n + g.items.length, 0) : (b.items?.length ?? 0);
  const rest = (b.total ?? shown) - shown;
  return (
    <div class="home-list">
      {b.groups
        ? b.groups.map((g) => (
            <section key={g.label} class="home-group">
              <h3 class="home-group-title">
                {g.label} <span class="home-count">{g.count}</span>
              </h3>
              <ul class="home-rows">
                {g.items.map((it) => (
                  <Row key={it.id} it={it} onOpen={onOpen} now={now} />
                ))}
              </ul>
            </section>
          ))
        : (
            <ul class="home-rows">
              {(b.items ?? []).map((it) => (
                <Row key={it.id} it={it} onOpen={onOpen} now={now} />
              ))}
            </ul>
          )}
      {rest > 0 && <p class="home-more">{hs("block.more", { n: rest })}</p>}
    </div>
  );
}

/** LaptopBlock draws a block of view.run: the page's renderer, or the
 * kinds the Home knows, or its title and "Open on laptop". */
function LaptopBlock({ block, onOpen }: { block: unknown; onOpen: (id: string) => void }) {
  const R = H.Block;
  if (R) return <R block={block} />;
  const b = block as HomeBlock;
  if (b.kind === "list" || b.kind === "status") return <PageBlock b={b} onOpen={onOpen} />;
  return <p class="home-status">{hs("block.laptop")}</p>;
}

export function QuickBlock({
  q,
  title,
  built,
  onOpen,
  onClose,
  onRetry,
}: {
  q: Quick;
  title: string;
  /** built is the page's block for q (from a topic), drawn live. */
  built: HomeBlock | null;
  onOpen: (id: string) => void;
  onClose: () => void;
  onRetry: () => void;
}) {
  const shownTitle = q.state === "ready" && q.from === "view" ? ((q.block as { title?: string }).title ?? title) : (built?.kind === "list" ? built.title : title);
  return (
    <article class={`home-block is-${q.state}`} aria-label={shownTitle} aria-busy={q.state === "loading" ? "true" : undefined}>
      <header class="home-block-head">
        <span class="home-block-mark" aria-hidden="true">
          ▢
        </span>
        <h2 class="home-block-title trunc" dir="auto">
          {shownTitle}
        </h2>
        <span class="home-block-tag">{hs("block.no_tokens")}</span>
        <button type="button" class="home-block-close" aria-label={hs("block.close")} title={hs("block.close")} onClick={onClose}>
          <span aria-hidden="true">×</span>
        </button>
      </header>
      {q.state === "loading" && (
        <p class="home-status">
          <span class="sk-row" aria-hidden="true" /> {hs("block.loading")}
        </p>
      )}
      {q.state === "error" && (
        <p class="home-status" role="alert">
          <ToneMark tone="bad" /> {hs("block.failed", { why: q.error ?? "" })}{" "}
          <button type="button" class="btn btn-small home-retry" onClick={onRetry}>
            {hs("block.retry")}
          </button>
        </p>
      )}
      {q.state === "ready" && q.from === "view" && <LaptopBlock block={q.block} onOpen={onOpen} />}
      {q.state === "ready" && q.from === "page" && built && <PageBlock b={built} onOpen={onOpen} />}
    </article>
  );
}
