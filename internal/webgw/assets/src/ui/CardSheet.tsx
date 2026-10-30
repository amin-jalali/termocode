// The card sheet (WEB-15, web UX 3.2, 5.3): the waiting card of the
// cards topic over the current tab, in the kit's safe Card. Deny (or
// Reject) is first and focused, the risky answer arms for 600 ms, Esc /
// swipe down / the dim area mean Later and send nothing. The answer is
// one cards.answer; the first answer wins, so a card the laptop or
// another browser answered first says so instead of answering.
import { h } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import type { Card as CardData } from "../../../../webproto/proto";
import { diffTab } from "../lib/difftab";
import { layout } from "../lib/layout";
import { go, route } from "../lib/router";
import { showFile } from "../lib/workbench";
import { toast } from "../lib/toasts";
import { OfflineError, ReqError } from "../net/link";
import { answeredBy, cards, link, live, store, waiting } from "../session";
import { answerable, riskyChoice, safeChoice } from "../state/cards";
import { t, type StringKey } from "../strings";
import { Card, type CardKind, type CardState } from "./Card";
import { DiffRow } from "./rows";
import { Sheet } from "./Sheet";

/** MAX_DIFF_ROWS caps the rows drawn; a longer diff is read on the laptop. */
const MAX_DIFF_ROWS = 400;

let pushed = false;

/** openCards opens the sheet on the first card the web may answer (or
 * the first card at all), over the current tab. */
export function openCards(): void {
  const w = waiting.value;
  const first = w.answer[0] ?? w.laptop[0];
  if (!first) return;
  pushed = true;
  go({ ...route.value, card: first.id });
}

/** closeCard closes the sheet: a step back when the sheet added one, so
 * the back button never opens it again. */
export function closeCard(): void {
  const r = route.peek();
  if (!r.card) return;
  if (pushed) {
    pushed = false;
    history.back();
    return;
  }
  go({ tab: r.tab, rest: r.rest, query: r.query }, true);
}

/** errText is the short line for a failed request. */
export function errText(e: unknown): string {
  const code = e instanceof ReqError ? e.code : "internal";
  const key: StringKey = code === "forbidden" ? "err.forbidden" : code === "limit" ? "err.limit" : code === "busy" ? "e.busy" : "err.internal";
  return t(key);
}

function CardDiff({ c }: { c: CardData }) {
  const rows = c.diff?.rows ?? [];
  return (
    <div class="card-diff">
      {rows.slice(0, MAX_DIFF_ROWS).map((r, i) => (
        <DiffRow key={i} kind={r.k === "add" || r.k === "del" ? r.k : "ctx"} a={r.old} b={r.new} text={r.text} />
      ))}
    </div>
  );
}

export function CardSheet({ id }: { id: string }) {
  const card = cards.value.find((c) => c.id === id);
  const last = useRef<CardData | undefined>(card);
  if (card) last.current = card;
  const shown = card ?? last.current;
  const [gone, setGone] = useState(false);
  const [sending, setSending] = useState(false);
  const loaded = store.topic("cards").value !== undefined;
  const by = answeredBy.value.get(id);

  // A card that is not there (an old link, or answered before the page
  // saw it) closes with a short line instead of an empty sheet.
  useEffect(() => {
    if (loaded && !shown) {
      toast("info", t("card.gone"));
      closeCard();
    }
  }, [loaded, shown]);
  if (!shown) return null;

  const web = answerable(shown) && riskyChoice(shown) !== undefined;
  let state: CardState;
  if (gone) state = "gone";
  else if (!card && !sending) state = by === "" ? "gone" : "answered";
  else if (!live.value) state = "offline";
  else if (web && shown.diff?.truncated) state = "long";
  else state = "open";
  const kind: CardKind = web ? (shown.kind as CardKind) : "trust";

  const answer = (which: "safe" | "risky") => {
    const choice = which === "safe" ? safeChoice(shown) : riskyChoice(shown);
    if (!choice || sending) return;
    setSending(true);
    const review = shown.kind === "review";
    link.request("cards.answer", { id, choice: choice.id }).then(
      () => {
        setSending(false);
        if (which === "safe") toast("info", t(review ? "toast.rejected" : "toast.denied"));
        else toast("ok", t(review ? "toast.accepted" : "toast.allowed"));
        closeCard();
      },
      (e: unknown) => {
        setSending(false);
        if (e instanceof ReqError && e.code === "gone") setGone(true);
        else if (!(e instanceof OfflineError)) toast("error", errText(e));
      },
    );
  };

  return (
    <Sheet onLater={closeCard} label={shown.title}>
      <Card
        key={shown.id}
        id={shown.id}
        kind={kind}
        state={state}
        subject={shown.title}
        where={shown.file}
        by={by}
        onAnswer={answer}
        onLater={closeCard}
        onClose={closeCard}
        onLaptop={closeCard}
      >
        {shown.body && <p class="card-text">{shown.body}</p>}
        {shown.diff && state !== "long" && <CardDiff c={shown} />}
        {shown.kind === "review" && shown.diff && !shown.diff.truncated && shown.file && layout.value === "desktop" && !shown.id.includes(":") && (
          <button
            type="button"
            class="link card-open-diff"
            onClick={() => {
              pushed = false;
              showFile(diffTab("review", shown.file ?? "", shown.id));
            }}
          >
            {t("card.open_diff")}
          </button>
        )}
      </Card>
    </Sheet>
  );
}
