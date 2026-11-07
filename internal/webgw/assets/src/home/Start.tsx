// The Home's empty state (MOBUX-4): what the phone shows before the
// first message or chip. A short hello, three cards in board order
// (needs you · working · done today) and the pinned views below them.
// A card is one 44 px button: a shape and a word say its state (never
// colour alone), a big number, the newest title. A tap opens the list
// behind it through a chip, so it costs no tokens. Data only from the
// laptop's inbox (state/home.ts homeSummary); nothing is stored here.
import { h } from "./jsx";
import { hs, type HomeKey } from "./strings";
import { PinnedViews } from "./saved";
import type { HomeSummary } from "../state/home";

/** StartCard names a card; the Home maps it to the chip it opens. */
export type StartCard = "needs" | "working" | "done";

const LOOK: Readonly<Record<StartCard, { glyph: string; title: HomeKey }>> = {
  needs: { glyph: "●", title: "start.needs" },
  working: { glyph: "◐", title: "start.working" },
  done: { glyph: "✓", title: "start.done" },
};

function Card({ card, n, first, extra, known, onCard }: { card: StartCard; n: number; first?: string; extra?: string; known: boolean; onCard: (c: StartCard) => void }) {
  const look = LOOK[card];
  const title = hs(look.title);
  const value = known || card === "needs" ? String(n) : "–";
  const detail = !known && card !== "needs" ? hs("start.waiting") : n === 0 ? hs(card === "needs" ? "start.none_needs" : card === "working" ? "start.none_working" : "start.none_done") : (first ?? "");
  return (
    <li>
      <button
        type="button"
        class={"home-card is-" + card + (n > 0 ? " has-items" : "")}
        aria-label={hs("start.card", { title, n: value, detail: [detail, extra].filter(Boolean).join(" · ") })}
        onClick={() => onCard(card)}
      >
        <span class="home-card-head" aria-hidden="true">
          <span class="home-card-mark">{look.glyph}</span> {title}
        </span>
        <span class="home-card-value" aria-hidden="true">
          {value}
        </span>
        <span class="home-card-detail trunc" dir="auto" aria-hidden="true">
          {detail}
        </span>
        {extra && (
          <span class="home-card-extra" aria-hidden="true">
            ✗ {extra}
          </span>
        )}
      </button>
    </li>
  );
}

/** HomeStart is the Home before its first message: hello, three cards,
 * pinned views. */
export function HomeStart({ sum, onCard }: { sum: HomeSummary; onCard: (c: StartCard) => void }) {
  const failed = sum.done.failed > 0 ? hs("start.failed", { n: sum.done.failed }) : undefined;
  return (
    <div class="home-start">
      <p class="home-hello" dir="auto">
        <span class="star" aria-hidden="true">
          ✦
        </span>{" "}
        <strong>{hs("start.hello")}</strong>
        <span class="home-hello-hint"> {hs("hello")}</span>
      </p>
      <ul class="home-cards" role="list" aria-label={hs("start.cards")}>
        <Card card="needs" n={sum.needs.n} first={sum.needs.first} known={sum.known} onCard={onCard} />
        <Card card="working" n={sum.working.n} first={sum.working.first} known={sum.known} onCard={onCard} />
        <Card card="done" n={sum.done.n} first={sum.done.first} extra={failed} known={sum.known} onCard={onCard} />
      </ul>
      <PinnedViews />
    </div>
  );
}
