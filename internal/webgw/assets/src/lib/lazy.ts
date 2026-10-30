// Loads the page's two lazy files on first use, each a fixed same-origin
// path built beside app.js (cmd/webbuild) and listed with its hash in
// assets.lock; these are the page's only dynamic imports (lint_test.go
// allows exactly these). A failed load can be tried again.
//   editor.js     the in-page editor (WEB-39), when the user starts editing
//   workbench.js  the desktop workbench, on a wide screen only (a phone
//                 never loads it); index.html preloads it there with its SRI
import { signal } from "@preact/signals";
import type { EditorModule } from "../editor/api";
import type * as Workbench from "../workbench";

let loading: Promise<EditorModule> | null = null;

export function loadEditor(): Promise<EditorModule> {
  if (!loading) {
    loading = import("/assets/editor.js").catch((e: unknown) => {
      loading = null;
      throw e;
    });
  }
  return loading;
}

/** DeskModule is what workbench.js gives the page. */
export type DeskModule = typeof Workbench;

/** desk is the loaded workbench (null: not yet); deskFailed says the
 * last load failed (loadDesk tries again). */
export const desk = signal<DeskModule | null>(null);
export const deskFailed = signal(false);

let deskLoading: Promise<DeskModule> | null = null;

export function loadDesk(): Promise<DeskModule> {
  if (!deskLoading) {
    deskFailed.value = false;
    deskLoading = import("/assets/workbench.js").then(
      (m: DeskModule) => (desk.value = m),
      (e: unknown) => {
        deskLoading = null;
        deskFailed.value = true;
        throw e;
      },
    );
  }
  return deskLoading;
}
