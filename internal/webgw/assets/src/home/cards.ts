// Cards inline in the phone's Home (MOB-6, plan §2.6/2.8): the waiting
// cards sit in the Home stream as the kit's Card in inline mode, with
// the sheet's rules from state/cards.ts: the safe answer first (it does
// not take the focus here; the sheet does), the risky one arms for
// 600 ms, Esc is Later, a review with a diff offers only Reject and
// "Open Changes" (the sheet, with the diff), the first answer wins and a
// card answered elsewhere leaves the stream; Later / Esc put it out of
// the stream for now (the pill still counts it). The body and the hunk boxes
// stay in the sheet (#/home?card=<id>); a tap on the title opens it.
import type { Card as CardData } from "../../../../webproto/proto";
import { answerArgs, cardState, homeCards } from "../state/cards";
import type { HomeCardsHost } from "./cardshost";

/** MAX_BODY cuts a long body in the stream; the sheet shows it all. */
const MAX_BODY = 160;

function InlineCard({ host, c }: { host: HomeCardsHost; c: CardData }) {
  const h = host.h;
  const [sending, setSending] = host.useState(false);
  const [gone, setGone] = host.useState(false);
  // later: Later (or Esc) puts the card out of the stream for now; it
  // still waits in the pill and on the laptop. Nothing is answered.
  const [later, setLater] = host.useState(false);
  const by = host.answeredBy.value.get(c.id);
  const { state, kind } = cardState(c, { present: true, sending, gone, live: host.live.value, by, inline: true });

  const answer = (which: "safe" | "risky") => {
    if (sending) return;
    const args = answerArgs(c, which);
    if (!args) return;
    if (args === "keep_none") {
      host.openCard(c.id);
      return;
    }
    const review = c.kind === "review";
    setSending(true);
    host.request("cards.answer", args).then(
      () => {
        setSending(false);
        host.haptic(which === "safe" ? "light" : "medium");
        if (which === "safe") host.toast("info", host.t(review ? "toast.rejected" : "toast.denied"));
        else host.toast("ok", host.t(review ? "toast.accepted" : "toast.allowed"));
      },
      (e: unknown) => {
        setSending(false);
        const f = host.failure(e);
        if (f === "gone") setGone(true);
        else if (f !== "offline") host.toast("error", f);
      },
    );
  };

  if (later) return null;
  const body = c.body && c.body.length > MAX_BODY ? c.body.slice(0, MAX_BODY - 1) + "…" : c.body;
  // Plain h() calls: home.js gets Preact from the page, so no JSX
  // namespace is in scope here.
  return h(
    "li",
    { class: "home-waiting-card" },
    h(
      host.Card,
      {
        id: `home-${c.id}`,
        kind,
        state,
        mode: "inline",
        subject: c.title,
        where: c.file,
        by,
        onAnswer: answer,
        onLater: () => setLater(true),
        onCancel: () => host.toast("info", host.t("card.cancelled")),
        onClose: () => setLater(true),
        onLaptop: () => (state === "peek" ? host.openCard(c.id) : setLater(true)),
      },
      body ? h("button", { type: "button", class: "link home-card-more", onClick: () => host.openCard(c.id) }, body) : null,
    ),
  );
}

/** HomeCards is the stream's "needs you" part: nothing when no card waits. */
export function HomeCards({ host }: { host: HomeCardsHost }) {
  const h = host.h;
  const list = homeCards(host.cards.value);
  if (list.length === 0) return null;
  return h(
    "ul",
    { class: "home-waiting", "aria-label": host.cardsWaiting(list.length) },
    list.map((c) => h(InlineCard, { key: c.id, host, c })),
  );
}
