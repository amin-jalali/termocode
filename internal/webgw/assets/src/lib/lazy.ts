// Loads the page's lazy files on first use, each a fixed same-origin
// path built beside app.js (cmd/webbuild) and listed with its hash in
// assets.lock; these are the page's only dynamic imports (lint_test.go
// allows exactly these). A failed load can be tried again.
//   editor.js     the in-page editor (WEB-39), when the user starts editing
//   workbench.js  the desktop workbench, on a wide screen only (a phone
//                 never loads it); index.html preloads it there with its SRI
//   strings-fa.js the Persian strings, when the UI language is Persian
//   picker.js     the session picker's list (WEB-55), once after a passkey sign-in
//   home.js       the phone's own views (This phone, MOBUX-20; the Home of
//                 MOB-4; All tools, MOBUX-5; review, MOB-14), on a phone
//                 only, when one opens
import { signal } from "@preact/signals";
import type { EditorModule } from "../editor/api";
import type { HomeHost } from "../home/api";
import { persian } from "../strings";
import type * as Workbench from "../workbench";
import type * as Home from "../home";
import type * as Picker from "../picker/main";

/** once makes a loader that runs load one time and again after a
 * failure (a failed load can be tried again). */
const once = <T>(load: () => Promise<T>): (() => Promise<T>) => {
  let p: Promise<T> | null = null;
  return () =>
    (p ??= load().catch((e: unknown) => {
      p = null;
      throw e;
    }));
};

export const loadEditor: () => Promise<EditorModule> = once(() => import("/assets/editor.js"));

/** DeskModule is what workbench.js gives the page. */
export type DeskModule = typeof Workbench;

/** desk is the loaded workbench (null: not yet); deskFailed says the
 * last load failed (loadDesk tries again). */
export const desk = signal<DeskModule | null>(null);
export const deskFailed = signal(false);

export const loadDesk: () => Promise<DeskModule> = once(() => {
  deskFailed.value = false;
  return import("/assets/workbench.js").then(
    (m: DeskModule) => (desk.value = m),
    (e: unknown) => {
      deskFailed.value = true;
      throw e;
    },
  );
});

/** loadPersian loads the Persian strings into persian (strings.ts); a
 * failed load can be tried again (the UI stays English meanwhile). */
export const loadPersian: () => Promise<void> = once(() =>
  import("/assets/strings-fa.js").then((m) => {
    persian.value = m.FA;
  }),
);

/** HomeModule is what home.js gives the page. */
export type HomeModule = typeof Home;

/** home is the loaded phone views (null: not yet); homeFailed says the
 * last load failed (loadHome tries again). */
export const home = signal<HomeModule | null>(null);
export const homeFailed = signal(false);

const homeModule = once(() => {
  homeFailed.value = false;
  return import("/assets/home.js").catch((e: unknown) => {
    homeFailed.value = true;
    throw e;
  });
});

/** homeHosted: home.js has the Home's host (init ran); the Home draws
 * only then. */
export const homeHosted = signal(false);

/** loadHome loads home.js once. host (MOB-4's Home) is built once, on
 * the first call that brings one, and handed to init, so the Home and the
 * page share one store and one link; This phone (MOBUX-20) and All tools
 * need no host, and a Home opened after them still gets it. */
export function loadHome(host?: () => HomeHost): Promise<HomeModule> {
  return homeModule().then((m: HomeModule) => {
    if (host && !homeHosted.peek()) {
      m.init(host());
      homeHosted.value = true;
    }
    return (home.value = m);
  });
}

/** PickerModule is what picker.js gives the page. */
export type PickerModule = typeof Picker;

/** loadPicker loads picker.js once; a failed load can be tried again. */
export const loadPicker: () => Promise<PickerModule> = once(() => import("/assets/picker.js"));
