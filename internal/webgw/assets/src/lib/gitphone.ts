// The phone's Git pages (gitphone.js and gitphone.css, contract in
// gitphone/api.ts): home.js loads them with its one dynamic import when
// the phone opens Git, links the stylesheet then, and gives them the
// page's Preact, link, router, strings and git sheets, so there is one of
// each and neither app.js nor home.js carries the pages.
import { Fragment, h } from "preact";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { signal } from "@preact/signals";
import type { GitHost } from "../gitphone/api";
import type * as GitPhone from "../gitphone/main";
import { link, live, store } from "../session";
import { readChat } from "../state/chat";
import { t, tn } from "../strings";
import { AskLaptop } from "../ui/AskLaptop";
import { Sheet } from "../ui/Sheet";
import { View } from "../views/View";
import { DiffSheet } from "../views/git/DiffSheet";
import { gitFailed } from "../views/git/errors";
import { RemoteSheet } from "../views/git/RemoteSheet";
import { ShowSheet } from "../views/git/ShowSheet";
import { stageFile } from "../views/git/GitView";
import { setDraft } from "./drafts";
import { haptic } from "./haptic";
import { go, goTab, route } from "./router";
import { toast } from "./toasts";
import { addRef } from "./tochat";

type Mod = typeof GitPhone;

/** gitPhone is the loaded pages (null: not yet); gitPhoneFailed says the
 * last load failed (loadGitPhone tries again). */
export const gitPhone = signal<Mod | null>(null);
export const gitPhoneFailed = signal(false);

let mod: Promise<Mod> | undefined;

const host = (): GitHost => ({
  h,
  Fragment,
  useState,
  useEffect,
  useRef,
  useMemo,
  link,
  live,
  store,
  route,
  go,
  goTab,
  t,
  tn,
  toast,
  addRef,
  draft: (text) => {
    const c = readChat(store.topic("chat").peek()?.data);
    if (c) setDraft(c.id, text);
    goTab("chat");
  },
  failed: gitFailed,
  haptic: () => haptic(),
  stage: stageFile,
  View,
  Sheet,
  AskLaptop,
  DiffSheet,
  ShowSheet,
  RemoteSheet,
});

/** loadGitPhone loads gitphone.js once (again after a failure). */
export function loadGitPhone(): Promise<Mod> {
  gitPhoneFailed.value = false;
  if (!mod) {
    const l = document.createElement("link");
    l.rel = "stylesheet";
    l.href = "/assets/gitphone.css";
    document.head.append(l);
    mod = import("/assets/gitphone.js").then((m: Mod) => {
      m.init(host());
      return (gitPhone.value = m);
    });
    mod.catch(() => {
      mod = undefined;
      l.remove();
      gitPhoneFailed.value = true;
    });
  }
  return mod;
}
