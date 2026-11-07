// The host the page gives the phone's Home (home.js, MOB-4; contract in
// home/api.ts): the page's own Preact, hooks, signals, store, link,
// router and views, so the Home shares one of each with the page. Built
// once, on the Home's first load.
import { Fragment, h } from "preact";
import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "preact/hooks";
import { computed, effect, signal } from "@preact/signals";
import type { HomeHost } from "./home/api";
import { setDraft } from "./lib/drafts";
import { addAttach } from "./lib/attach";
import { layout } from "./lib/layout";
import { prefs, setPref } from "./lib/prefs";
import { go, goTab, route } from "./lib/router";
import { OfflineError, ReqError } from "./net/link";
import { cards, link, live, status, store } from "./session";
import { readChat } from "./state/chat";
import { errText } from "./ui/CardSheet";
import { Sheet } from "./ui/Sheet";
import { Message } from "./views/Chat";
import { Composer } from "./views/Composer";

export function homeHost(): HomeHost {
  return {
    h,
    Fragment,
    useState,
    useEffect,
    useLayoutEffect,
    useRef,
    useMemo,
    signal,
    computed,
    effect,
    link,
    store,
    live,
    status,
    cards,
    layout,
    route,
    go,
    goTab,
    phoneHome: computed(() => prefs.value.phoneHome),
    setPhoneHome: (v) => setPref("phoneHome", v),
    readChat,
    setDraft,
    addAttach,
    errText,
    isOffline: (e) => e instanceof OfflineError,
    code: (e) => (e instanceof ReqError ? e.code : ""),
    Message,
    Composer,
    Sheet,
    // The page's block renderer (ui/Blocks.tsx, MOB-3) joins the host when
    // the chat draws a message's blocks (MOB-10), so it is in the page
    // already; until then the Home draws lists and status lines itself.
    Block: null,
  };
}
