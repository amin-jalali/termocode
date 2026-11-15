// The phone's tool pages (pages.js, UI pages 10, 11): loaded with this
// file's one dynamic import when one opens, after its stylesheet
// (pages.css), so a page never shows unstyled. pages.js imports no value
// of the page: it gets Preact, the session and the shared views through
// init (pages/host.ts). A failed load can be tried again.
import { Fragment, h } from "preact";
import { signal } from "@preact/signals";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import type * as PagesModule from "../pages/main";
import type { PagesHost } from "../pages/main";
import { link, store } from "../session";
import { readChat } from "../state/chat";
import { addStrings, t, type StringKey } from "../strings";
import { errText } from "../ui/CardSheet";
import { Sheet } from "../ui/Sheet";
import { TopicBody } from "../views/Placeholder";
import { View } from "../views/View";
import { ask, canAct } from "../views/run/bits";
import { addAttach } from "./attach";
import { goTo } from "./lsp";
import { pendingDraft } from "./drafts";
import { go } from "./router";
import { toast } from "./toasts";

export type Pages = typeof PagesModule;

/** pages is pages.js once in; pagesFailed says the last load failed. */
export const pages = signal<Pages | null>(null);
export const pagesFailed = signal(false);

const host = (): PagesHost => ({
  h,
  Fragment,
  useState,
  useEffect,
  useMemo,
  useRef,
  signal,
  link,
  store,
  go,
  toast,
  errText,
  Sheet,
  View,
  TopicBody,
  addAttach,
  pendingDraft,
  canAct,
  ask,
  openAt: (path, line) => goTo(path, Math.max(0, line - 1), 0),
  chatID: () => readChat(store.topic("chat").peek()?.data)?.id ?? "",
  t: (k, v) => t(k as StringKey, v),
  addStrings,
});

let mod: Promise<Pages> | undefined;
let css: Promise<void> | undefined;

const sheet = (): Promise<void> =>
  (css ??= new Promise<void>((ok, no) => {
    const l = document.createElement("link");
    l.rel = "stylesheet";
    l.href = "/assets/pages.css";
    l.onload = () => ok();
    l.onerror = () => {
      l.remove();
      css = undefined;
      no(new Error("pages.css"));
    };
    document.head.append(l);
  }));

export function loadPages(): Promise<Pages> {
  pagesFailed.value = false;
  return (mod ??= Promise.all([import("/assets/pages.js"), sheet()]).then(
    ([m]) => {
      const p = m as Pages;
      p.init(host());
      return (pages.value = p);
    },
    (e: unknown) => {
      mod = undefined;
      pagesFailed.value = true;
      throw e;
    },
  ));
}
