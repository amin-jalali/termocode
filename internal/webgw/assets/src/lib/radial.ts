// The floating radial menu (RM-2) on a touch phone or tablet, and the More
// list's rows (RM-3) on any phone or tablet: radial.js and radial.css load with this file's one dynamic import when the shell
// shows (radial.js links radial.css itself), and get the page's own ops (radial/actions.ts), so the page's
// first files and the desktop never carry them. A failed load stays off.
import { h, render } from "preact";
import { useEffect, useState } from "preact/hooks";
import { signal } from "@preact/signals";
import type * as RadialModule from "../radial";
import { link, live, store } from "../session";
import { readGit } from "../state/git";
import { t } from "../strings";
import { AskLaptop } from "../ui/AskLaptop";
import { newChat } from "../views/Chat";
import { showOnLaptop } from "../views/FileView";
import { newTerm } from "../views/Term";
import { layout } from "./layout";
import { prefs, setPref } from "./prefs";
import { editing } from "./live";
import { go, route } from "./router";
import { toast } from "./toasts";
import { addRef } from "./tochat";

export const radialHost: RadialModule.RadialHost = {
  h, render, AskLaptop, go, route, layout, link, live, t, toast, addRef, newChat, newTerm, showOnLaptop, prefs, setPref, useState,
  git: () => readGit(store.topic("git").peek()?.data),
  edit: (p) => (editing.value = p),
};

/** radial is radial.js once in (the More list draws from it too). */
export const radial = signal<typeof RadialModule | null>(null);
let mod: Promise<typeof RadialModule> | null = null;
export const loadRadial = (): Promise<typeof RadialModule> =>
  (mod ??= import("/assets/radial.js").then(
    (m: typeof RadialModule) => (radial.value = m),
    (e: unknown) => {
      mod = null;
      throw e;
    },
  ));

/** useRadial shows the radial while on (a phone or tablet shell) on a
 * touch screen. */
export function useRadial(on: boolean): void {
  useEffect(() => {
    if (!on || !(navigator.maxTouchPoints > 0 || matchMedia("(pointer: coarse)").matches)) return;
    let stop: (() => void) | null = null, gone = false;
    loadRadial().then((m) => !gone && (stop = m.init(radialHost)), () => undefined);
    return () => {
      gone = true;
      stop?.();
    };
  }, [on]);
}
