// Loads the page's three lazy files on first use, each a fixed same-origin
// path built beside app.js (cmd/webbuild) and listed with its hash in
// assets.lock; these are the page's only dynamic imports (lint_test.go
// allows exactly these). A failed load can be tried again.
//   editor.js     the in-page editor (WEB-39), when the user starts editing
//   workbench.js  the desktop workbench, on a wide screen only (a phone
//                 never loads it); index.html preloads it there with its SRI
//   strings-fa.js the Persian strings, when the UI language is Persian
//   phone.js      phone views the first files do not carry (All tools,
//                 MOBUX-5), when the user opens one
import { signal } from "@preact/signals";
import type { EditorModule } from "../editor/api";
import { persian } from "../strings";
import type * as Workbench from "../workbench";
import type * as Phone from "../phonechunk";

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

let faLoading: Promise<void> | null = null;

/** loadPersian loads the Persian strings into persian (strings.ts); a
 * failed load can be tried again (the UI stays English meanwhile). */
export function loadPersian(): Promise<void> {
  if (!faLoading) {
    faLoading = import("/assets/strings-fa.js").then(
      (m) => {
        persian.value = m.FA;
      },
      (e: unknown) => {
        faLoading = null;
        throw e;
      },
    );
  }
  return faLoading;
}

/** PhoneModule is what phone.js gives the page. */
export type PhoneModule = typeof Phone;

/** phone is the loaded phone chunk (null: not yet); phoneFailed says the
 * last load failed (loadPhone tries again). */
export const phone = signal<PhoneModule | null>(null);
export const phoneFailed = signal(false);

let phoneLoading: Promise<PhoneModule> | null = null;

export function loadPhone(): Promise<PhoneModule> {
  if (!phoneLoading) {
    phoneFailed.value = false;
    phoneLoading = import("/assets/phone.js").then(
      (m: PhoneModule) => (phone.value = m),
      (e: unknown) => {
        phoneLoading = null;
        phoneFailed.value = true;
        throw e;
      },
    );
  }
  return phoneLoading;
}
