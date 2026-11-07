// The page's side of the Home's inline cards (MOB-6): home.js is built
// alone, so the page hands it its own Preact, hooks, kit Card, cards
// topic, link and strings (home/cardshost.ts). MOB-4's HomeHost carries
// this as `cardsHost`.
import { h } from "preact";
import { useRef, useState } from "preact/hooks";
import type { HomeCardsHost } from "../home/cardshost";
import { OfflineError, ReqError } from "../net/link";
import { answeredBy, cards, link, live } from "../session";
import { t, tn } from "../strings";
import { Card } from "../ui/Card";
import { errText, openCard } from "../ui/CardSheet";
import { haptic } from "./haptic";
import { toast } from "./toasts";

export function homeCardsHost(): HomeCardsHost {
  return {
    h,
    useState,
    useRef,
    Card,
    cards,
    live,
    answeredBy,
    t,
    cardsWaiting: (n) => tn("pill.cards", n),
    toast: (severity, title) => void toast(severity, title),
    request: (op, args) => link.request(op, args),
    failure: (e) => (e instanceof ReqError && e.code === "gone" ? "gone" : e instanceof OfflineError ? "offline" : errText(e)),
    openCard,
    haptic,
  };
}
